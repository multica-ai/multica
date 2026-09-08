#!/usr/bin/env python3
"""Local CEO workbench API — portfolio status, queue, and dispatch."""

from __future__ import annotations

import json
import os
import re
import subprocess
import threading
import time
import uuid
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse

SCRIPT_DIR = Path(__file__).resolve().parent
MULTICA_ROOT = SCRIPT_DIR.parent.parent
WORKBENCH_DIR = SCRIPT_DIR / "workbench"
REGISTRY = Path(
    os.environ.get(
        "REGISTRY",
        MULTICA_ROOT / ".ai-company/templates/project-registry.yaml",
    )
)
GITHUB_ORG = os.environ.get("GITHUB_ORG", "chenzh")
JOBS_DIR = Path.home() / ".multica" / "ceo-workbench" / "jobs"
DEFAULT_PORT = int(os.environ.get("CEO_WORKBENCH_PORT", "9477"))

_jobs_lock = threading.Lock()
_jobs: dict[str, dict[str, Any]] = {}


@dataclass
class Project:
    id: str
    repo: str
    paused: bool = False
    priority: int = 0
    max_nightly_tickets: int = 1
    tier: str = ""
    notes: str = ""
    domain: str = ""
    cloudflare_project: str = ""
    delivery_slug: str = ""
    kind: str = "product"
    dispatch_mode: str = ""
    content_workbench_url: str = ""
    portfolio_group: str = ""
    workbench_url: str = ""
    executor: str = ""
    publish_policy: str = ""
    channels: list[str] = field(default_factory=list)


CONTENT_WORKBENCH_URL = os.environ.get(
    "CONTENT_WORKBENCH_URL",
    "https://hq.revoices.app/#content/review",
)
CONTENT_AGENT_URL = os.environ.get("CONTENT_AGENT_URL", "https://agent.revoices.app/")
CONTENT_REMOTE_SSH = os.environ.get("CONTENT_REMOTE_SSH", "lighthouse")

OPENWORLD_HERMES_URL = os.environ.get(
    "OPENWORLD_HERMES_URL",
    "https://hermes.nowifiwebgames.com",
)
OPENWORLD_METADATA_VIEWER_SITE = os.environ.get(
    "OPENWORLD_METADATA_VIEWER_SITE",
    "https://www.nowifiwebgames.com",
)


NORM_LINKS: list[tuple[str, str]] = [
    ("Harness 设计总览", "docs/32-opc-harness-knowledge-design.md"),
    ("硅谷文档规范", "docs/30-silicon-valley-doc-standards.md"),
    ("Harness 布局", "docs/29-harness-layout.md"),
    ("规范分层", "docs/28-norm-layers.md"),
    ("规范同步", "docs/27-norm-sync.md"),
    ("完成定义 DoD", "docs/18-definition-of-done.md"),
    ("好票写法", "docs/20-issue-brief-style-guide.md"),
    ("Label / BLOCKED", "docs/21-label-state-machine.md"),
    ("质量门禁", "docs/07-quality-gates.md"),
    ("任务分级", "docs/06-task-grading.md"),
    ("资产台账", "docs/19-asset-registry.md"),
    ("Git / fork", "docs/22-git-and-remotes.md"),
    ("本机环境", "docs/23-local-agent-environment.md"),
    ("脱手清单", "HANDS-OFF-COMPLETE.md"),
]

_overview_cache: dict[str, Any] = {"at": 0.0, "data": None}
OVERVIEW_CACHE_SECS = 45


def run_cmd(cmd: list[str], *, cwd: Path | None = None, timeout: int = 120) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=cwd or MULTICA_ROOT,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
    )


def hq_git_sha() -> str:
    result = run_cmd(["git", "-C", str(MULTICA_ROOT), "rev-parse", "--short", "HEAD"], timeout=10)
    return result.stdout.strip() if result.returncode == 0 else "unknown"


def load_company_assets() -> dict[str, dict[str, Any]]:
    """Parse company-assets.local.yaml (or .example) — projects.<id> blocks."""
    for name in ("company-assets.local.yaml", "company-assets.local.yaml.example"):
        path = MULTICA_ROOT / ".ai-company/config" / name
        if path.is_file():
            return _parse_assets_yaml(path.read_text(encoding="utf-8"))
    return {}


def _parse_assets_yaml(text: str) -> dict[str, dict[str, Any]]:
    projects: dict[str, dict[str, Any]] = {}
    current_id: str | None = None
    section: str | None = None
    in_projects = False
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        if line.strip() == "projects:":
            in_projects = True
            continue
        if not in_projects:
            continue
        if re.match(r"^\S", line) and not line.startswith(" "):
            break
        m_id = re.match(r"^  (\w[\w-]*):\s*$", line)
        if m_id:
            current_id = m_id.group(1)
            projects[current_id] = {}
            section = None
            continue
        if current_id is None:
            continue
        m_sec = re.match(r"^    (\w+):\s*$", line)
        if m_sec:
            section = m_sec.group(1)
            projects[current_id].setdefault(section, {})
            continue
        m_kv = re.match(r"^    (\w+):\s*(.*)$", line)
        if m_kv and section is None:
            key, val = m_kv.group(1), m_kv.group(2).strip().strip('"').strip("'")
            if key == "repo":
                projects[current_id]["repo"] = val
            elif key == "tier":
                projects[current_id]["tier"] = val
            elif key == "notes":
                projects[current_id]["notes"] = val
            continue
        m_nested = re.match(r"^      (\w+):\s*(.*)$", line)
        if m_nested and section:
            key, val = m_nested.group(1), m_nested.group(2).strip().strip('"').strip("'")
            bucket = projects[current_id].setdefault(section, {})
            if isinstance(bucket, dict):
                bucket[key] = val
        m_list = re.match(r"^      - (.+)$", line)
        if m_list and section:
            bucket = projects[current_id].setdefault(section, [])
            if isinstance(bucket, list):
                bucket.append(m_list.group(1).strip())
    return projects


def read_company_os_meta(local_path: str) -> dict[str, Any]:
    readme = Path(local_path) / ".delivery/company-os/README.md"
    if not local_path or not readme.is_file():
        return {"synced": False, "synced_at": "", "hq_sha": "", "file_count": 0}
    text = readme.read_text(encoding="utf-8", errors="replace")
    synced_at = ""
    hq_sha = ""
    m_time = re.search(r"> synced:\s*([^\n·]+)", text)
    if m_time:
        synced_at = m_time.group(1).strip()
    m_sha = re.search(r"multica @ `([^`]+)`", text)
    if m_sha:
        hq_sha = m_sha.group(1)
    file_count = len(re.findall(r"^- `([^`]+)`", text, re.MULTILINE))
    claude = (Path(local_path) / "CLAUDE.md").is_file()
    return {
        "synced": True,
        "synced_at": synced_at,
        "hq_sha": hq_sha,
        "file_count": file_count,
        "claude_md": claude,
        "hq_sha_current": hq_sha == hq_git_sha() if hq_sha else False,
    }


def process_health() -> dict[str, Any]:
    cron_ok = False
    cron_line = ""
    crontab = run_cmd(["crontab", "-l"], timeout=10)
    if crontab.returncode == 0:
        for line in crontab.stdout.splitlines():
            if "ceo-nightly" in line or "multica-ai-company-nightly" in line:
                cron_ok = True
                cron_line = line.strip()
                break
    nightly_log = Path.home() / ".multica/ceo-nightly.log"
    last_nightly = ""
    if nightly_log.is_file():
        try:
            last_nightly = nightly_log.read_text(encoding="utf-8", errors="replace").splitlines()[-1][:120]
        except OSError:
            last_nightly = ""
    manifest = MULTICA_ROOT / ".ai-company/config/company-os-sync-manifest.yaml"
    manifest_paths = 0
    if manifest.is_file():
        for line in manifest.read_text(encoding="utf-8").splitlines():
            if line.strip().startswith("- "):
                manifest_paths += 1
    return {
        "cron_installed": cron_ok,
        "cron_line": cron_line,
        "last_nightly_log_line": last_nightly,
        "manifest_paths": manifest_paths,
        "hq_sha": hq_git_sha(),
    }


def verify_hands_off_summary() -> dict[str, Any]:
    result = run_cmd(["bash", str(SCRIPT_DIR / "verify-hands-off.sh")], timeout=150)
    ok = warn = fail = 0
    for line in result.stdout.splitlines():
        if line.strip().startswith("✅"):
            ok += 1
        elif line.strip().startswith("❌"):
            fail += 1
        elif line.strip().startswith("⚠️"):
            warn += 1
    m = re.search(r"结果:\s*(\d+)\s*通过\s*·\s*(\d+)\s*警告\s*·\s*(\d+)\s*失败", result.stdout)
    if m:
        ok, warn, fail = int(m.group(1)), int(m.group(2)), int(m.group(3))
    return {
        "ok": ok,
        "warn": warn,
        "fail": fail,
        "green": fail == 0,
        "exit_code": result.returncode,
        "ran_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }


def company_overview(*, refresh_verify: bool = False) -> dict[str, Any]:
    now = time.time()
    if (
        not refresh_verify
        and _overview_cache["data"] is not None
        and now - float(_overview_cache["at"]) < OVERVIEW_CACHE_SECS
    ):
        return _overview_cache["data"]  # type: ignore[return-value]

    assets = load_company_assets()
    process = process_health()
    if refresh_verify:
        verify: dict[str, Any] = verify_hands_off_summary()
        verify["skipped"] = False
    elif _overview_cache["data"] is not None and _overview_cache["data"].get("verify"):
        verify = dict(_overview_cache["data"]["verify"])  # type: ignore[union-attr]
    else:
        verify = {"ok": 0, "warn": 0, "fail": 0, "green": None, "skipped": True, "ran_at": ""}

    rows = dashboard_rows()
    registry = {p.id: p for p in parse_registry()}
    projects_out: list[dict[str, Any]] = []
    content_lines: list[dict[str, Any]] = []
    openworld_lines: list[dict[str, Any]] = []
    row_by_id = {row.get("id", ""): row for row in rows}
    for row in rows:
        pid = row.get("id", "")
        reg = registry.get(pid)
        kind = reg.kind if reg else "product"
        if kind == "content":
            content_lines.append(
                {
                    "id": pid,
                    "repo": row.get("repo", ""),
                    "paused": row.get("paused", False),
                    "dispatch_mode": reg.dispatch_mode if reg else "",
                    "executor": reg.executor if reg else "",
                    "publish_policy": reg.publish_policy if reg else "",
                    "channels": reg.channels if reg else [],
                    "workbench_url": (reg.content_workbench_url if reg and reg.content_workbench_url else CONTENT_WORKBENCH_URL),
                    "blocked": int(row.get("blocked", 0)),
                    "running": int(row.get("running", 0)),
                    "agent_safe": int(row.get("agent_safe", 0)),
                    "notes": reg.notes if reg else row.get("notes", ""),
                }
            )
            continue
        if reg and reg.portfolio_group == "openworld":
            openworld_lines.append(
                {
                    "id": pid,
                    "repo": row.get("repo", ""),
                    "paused": row.get("paused", False),
                    "local_path": row.get("local_path", ""),
                    "local_path_resolved": row.get("local_path_resolved", False),
                    "domain": reg.domain,
                    "workbench_url": reg.workbench_url,
                    "delivery_slug": reg.delivery_slug,
                    "blocked": int(row.get("blocked", 0)),
                    "running": int(row.get("running", 0)),
                    "agent_safe": int(row.get("agent_safe", 0)),
                    "notes": reg.notes if reg else row.get("notes", ""),
                }
            )
            continue
        asset = assets.get(pid, {})
        local_path = row.get("local_path", "")
        domains = asset.get("domains", {}) if isinstance(asset.get("domains"), dict) else {}
        hosting = asset.get("hosting", {}) if isinstance(asset.get("hosting"), dict) else {}
        domain = (reg.domain if reg and reg.domain else "") or domains.get("production", "")
        cf_project = (reg.cloudflare_project if reg and reg.cloudflare_project else "") or hosting.get(
            "project", ""
        )
        projects_out.append(
            {
                **row,
                "kind": kind,
                "domain": domain,
                "cloudflare_project": cf_project,
                "delivery_slug": reg.delivery_slug if reg else "",
                "company_os": read_company_os_meta(local_path),
                "asset_notes": asset.get("notes", ""),
            }
        )

    for reg in registry.values():
        if reg.kind != "content" or reg.id in row_by_id:
            continue
        content_lines.append(
            {
                "id": reg.id,
                "repo": reg.repo,
                "paused": reg.paused,
                "dispatch_mode": reg.dispatch_mode or "remote-pull",
                "executor": reg.executor,
                "publish_policy": reg.publish_policy,
                "channels": reg.channels,
                "workbench_url": reg.content_workbench_url or CONTENT_WORKBENCH_URL,
                "blocked": 0,
                "running": 0,
                "agent_safe": 0,
                "notes": reg.notes,
            }
        )

    for reg in registry.values():
        if reg.portfolio_group != "openworld" or reg.id in row_by_id:
            continue
        local_path = resolve_repo_path(reg.id, reg.repo)
        openworld_lines.append(
            {
                "id": reg.id,
                "repo": reg.repo,
                "paused": reg.paused,
                "local_path": local_path,
                "local_path_resolved": bool(local_path),
                "domain": reg.domain,
                "workbench_url": reg.workbench_url,
                "delivery_slug": reg.delivery_slug,
                "blocked": 0,
                "running": 0,
                "agent_safe": 0,
                "notes": reg.notes,
            }
        )

    payload: dict[str, Any] = {
        "hq": {
            "multica_root": str(MULTICA_ROOT),
            "sha": process["hq_sha"],
            "registry": str(REGISTRY),
        },
        "process": process,
        "verify": verify,
        "norms": [{"title": title, "path": rel} for title, rel in NORM_LINKS],
        "projects": projects_out,
        "content": {
            "workbench_url": CONTENT_WORKBENCH_URL,
            "agent_url": CONTENT_AGENT_URL,
            "remote_ssh": CONTENT_REMOTE_SSH,
            "lines": sorted(content_lines, key=lambda item: item.get("id", "")),
            "totals": {
                "blocked": sum(int(line.get("blocked", 0)) for line in content_lines),
                "running": sum(int(line.get("running", 0)) for line in content_lines),
                "agent_safe": sum(int(line.get("agent_safe", 0)) for line in content_lines),
            },
        },
        "openworld": {
            "hermes_dashboard_url": OPENWORLD_HERMES_URL,
            "metadata_viewer_site": OPENWORLD_METADATA_VIEWER_SITE,
            "lines": sorted(openworld_lines, key=lambda item: item.get("id", "")),
            "totals": {
                "blocked": sum(int(line.get("blocked", 0)) for line in openworld_lines),
                "running": sum(int(line.get("running", 0)) for line in openworld_lines),
                "agent_safe": sum(int(line.get("agent_safe", 0)) for line in openworld_lines),
            },
        },
        "totals": {
            "blocked": sum(int(p.get("blocked", 0)) for p in projects_out),
            "running": sum(int(p.get("running", 0)) for p in projects_out),
            "agent_safe": sum(int(p.get("agent_safe", 0)) for p in projects_out),
            "merged_prs": sum(int(p.get("merged_prs", 0)) for p in projects_out),
        },
    }
    _overview_cache["at"] = now
    _overview_cache["data"] = payload
    return payload


def parse_registry() -> list[Project]:
    if not REGISTRY.is_file():
        return []

    projects: list[Project] = []
    current: dict[str, Any] = {}
    for raw in REGISTRY.read_text(encoding="utf-8").splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        if line.startswith("- id:"):
            if current.get("id"):
                if "kind" not in current:
                    current["kind"] = "product"
                if "channels" not in current:
                    current["channels"] = []
                projects.append(Project(**current))  # type: ignore[arg-type]
            current = {"id": line.split(":", 1)[1].strip()}
            continue
        for key in ("repo", "tier", "notes", "domain", "cloudflare_project", "delivery_slug", "kind", "dispatch_mode", "content_workbench_url", "portfolio_group", "workbench_url", "executor", "publish_policy"):
            if line.startswith(f"{key}:"):
                val = line.split(":", 1)[1].strip().strip('"')
                current[key] = val
        if line.startswith("channels:"):
            raw_channels = line.split(":", 1)[1].strip()
            if raw_channels.startswith("[") and raw_channels.endswith("]"):
                inner = raw_channels[1:-1].strip()
                current["channels"] = [c.strip().strip('"') for c in inner.split(",") if c.strip()]
        if line.startswith("paused:"):
            current["paused"] = line.split(":", 1)[1].strip() == "true"
        if line.startswith("priority:"):
            current["priority"] = int(line.split(":", 1)[1].strip())
        if line.startswith("max_nightly_tickets:"):
            current["max_nightly_tickets"] = int(line.split(":", 1)[1].strip())
    if current.get("id"):
        if "kind" not in current:
            current["kind"] = "product"
        if "channels" not in current:
            current["channels"] = []
        projects.append(Project(**current))  # type: ignore[arg-type]

    for project in projects:
        repo = project.repo
        repo = repo.removeprefix("github.com/").removeprefix("https://github.com/")
        if repo.startswith("your-org/"):
            repo = repo.replace("your-org/", f"{GITHUB_ORG}/", 1)
        project.repo = repo
    return projects


def cursor_agent_ready() -> bool:
    result = run_cmd(["cursor-agent", "status"], timeout=15)
    return result.returncode == 0 and "Logged in" in (result.stdout + result.stderr)


def portfolio_dispatch_mode() -> str:
    if not cursor_agent_ready():
        return "gha"
    if os.environ.get("PORTFOLIO_DISPATCH_ASYNC", "1") == "1":
        return "local-cli-async"
    return "local-cli-sync"


def multica_runtime_status() -> dict[str, Any]:
    result = run_cmd(
        ["bash", str(SCRIPT_DIR / "multica-runtime-status.sh"), "--json"],
        timeout=60,
    )
    if result.returncode != 0:
        return {"api_ok": False, "api_error": result.stderr.strip() or "multica-runtime-status failed"}
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return {"api_ok": False, "api_error": "invalid JSON from multica-runtime-status"}


def resolve_repo_path(project_id: str = "", repo: str = "") -> str:
    cmd = ["bash", str(SCRIPT_DIR / "resolve-repo-path.sh"), "--quiet"]
    if project_id:
        cmd.extend(["--id", project_id])
    if repo:
        cmd.extend(["--repo", repo])
    result = run_cmd(cmd, timeout=30)
    if result.returncode != 0:
        return ""
    return result.stdout.strip()


def dashboard_rows() -> list[dict[str, Any]]:
    result = run_cmd(
        [
            "bash",
            str(SCRIPT_DIR / "ceo-dashboard.sh"),
            "--registry",
            str(REGISTRY),
            "--org",
            GITHUB_ORG,
            "--json",
        ],
        timeout=180,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "ceo-dashboard failed")

    rows: list[dict[str, Any]] = []
    registry = {p.id: p for p in parse_registry()}
    for line in result.stdout.splitlines():
        if not line.strip():
            continue
        row = json.loads(line)
        project = registry.get(row["id"])
        if project:
            row["local_path"] = resolve_repo_path(project.id, project.repo)
            row["local_path_resolved"] = bool(row["local_path"])
            row["priority"] = project.priority
            row["max_nightly_tickets"] = project.max_nightly_tickets
            row["tier"] = project.tier
            row["notes"] = project.notes
            row["kind"] = project.kind
        rows.append(row)
    rows.sort(key=lambda item: item.get("priority", 0), reverse=True)
    return rows


def list_queue(repo: str) -> list[dict[str, Any]]:
    result = run_cmd(
        [
            "gh",
            "issue",
            "list",
            "-R",
            repo,
            "-l",
            "agent-safe",
            "-s",
            "open",
            "--json",
            "number,title,labels,url,updatedAt",
            "--jq",
            (
                '.[] | select([.labels[].name] | '
                '(index("agent-running") | not) and '
                '(index("agent-blocked") | not) and '
                '(index("agent-done") | not))'
            ),
        ],
        timeout=60,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "gh issue list failed")
    return json.loads(result.stdout or "[]")


def list_label(repo: str, label: str) -> list[dict[str, Any]]:
    result = run_cmd(
        [
            "gh",
            "issue",
            "list",
            "-R",
            repo,
            "-l",
            label,
            "-s",
            "open",
            "--json",
            "number,title,url,updatedAt",
        ],
        timeout=60,
    )
    if result.returncode != 0:
        return []
    return json.loads(result.stdout or "[]")


def save_job(job: dict[str, Any]) -> None:
    JOBS_DIR.mkdir(parents=True, exist_ok=True)
    path = JOBS_DIR / f"{job['id']}.json"
    path.write_text(json.dumps(job, indent=2), encoding="utf-8")


def load_jobs() -> list[dict[str, Any]]:
    if not JOBS_DIR.exists():
        return []
    jobs: list[dict[str, Any]] = []
    for path in sorted(JOBS_DIR.glob("*.json"), reverse=True):
        try:
            jobs.append(json.loads(path.read_text(encoding="utf-8")))
        except json.JSONDecodeError:
            continue
    return jobs[:20]


def start_dispatch_job(*, mode: str, repo: str = "", issue: str = "", max_total: int = 1, local_path: str = "") -> dict[str, Any]:
    job_id = uuid.uuid4().hex[:12]
    log_path = JOBS_DIR / f"{job_id}.log"
    JOBS_DIR.mkdir(parents=True, exist_ok=True)

    if mode == "portfolio":
        cmd = [
            "bash",
            str(SCRIPT_DIR / "portfolio-dispatch.sh"),
            "--registry",
            str(REGISTRY),
            "--max-total",
            str(max_total),
        ]
        if cursor_agent_ready():
            cmd.append("--local")
    elif mode == "issue":
        if not repo or not issue:
            raise ValueError("repo and issue are required")
        if not local_path:
            raise ValueError(f"local_path missing for {repo}")
        cmd = [
            "bash",
            str(SCRIPT_DIR / "../agent-delivery/dispatch-cursor-agent-cli.sh"),
            issue,
        ]
        env = os.environ.copy()
        env["GITHUB_REPOSITORY"] = repo
        env["REPO_ROOT"] = local_path
    else:
        raise ValueError(f"unknown mode: {mode}")

    with open(log_path, "w", encoding="utf-8") as log_file:
        if mode == "issue":
            proc = subprocess.Popen(
                cmd,
                cwd=MULTICA_ROOT,
                stdout=log_file,
                stderr=subprocess.STDOUT,
                env={**os.environ, "GITHUB_REPOSITORY": repo, "REPO_ROOT": local_path},
            )
        else:
            proc = subprocess.Popen(
                cmd,
                cwd=MULTICA_ROOT,
                stdout=log_file,
                stderr=subprocess.STDOUT,
            )

    job = {
        "id": job_id,
        "mode": mode,
        "repo": repo,
        "issue": issue,
        "max_total": max_total,
        "status": "running",
        "pid": proc.pid,
        "log_path": str(log_path),
        "started_at": datetime.now(timezone.utc).isoformat(),
        "finished_at": None,
        "exit_code": None,
    }
    save_job(job)

    def watcher() -> None:
        code = proc.wait()
        job["status"] = "success" if code == 0 else "failed"
        job["exit_code"] = code
        job["finished_at"] = datetime.now(timezone.utc).isoformat()
        save_job(job)

    threading.Thread(target=watcher, daemon=True).start()
    return job


def start_site_factory_job(
    *,
    intake: str,
    create_repo: bool = False,
    activate_autopilot: bool | None = None,
    notify: bool = True,
    max_dispatch: int = 2,
    dry_run: bool = False,
) -> dict[str, Any]:
    intake = intake.strip()
    if not intake:
        raise ValueError("intake is required")

    job_id = uuid.uuid4().hex[:12]
    log_path = JOBS_DIR / f"{job_id}.log"
    JOBS_DIR.mkdir(parents=True, exist_ok=True)

    cmd = [
        "bash",
        str(SCRIPT_DIR / "site-factory.sh"),
        "--intake",
        intake,
        "--max-dispatch",
        str(max_dispatch),
    ]
    if dry_run:
        cmd.append("--dry-run")
    if create_repo:
        cmd.extend(["--create-repo", "--push"])
        if activate_autopilot is True:
            cmd.append("--activate-autopilot")
        elif activate_autopilot is False:
            cmd.append("--no-autopilot")
    if notify:
        cmd.append("--notify")

    with open(log_path, "w", encoding="utf-8") as log_file:
        proc = subprocess.Popen(
            cmd,
            cwd=MULTICA_ROOT,
            stdout=log_file,
            stderr=subprocess.STDOUT,
        )

    job = {
        "id": job_id,
        "mode": "site-factory",
        "intake": intake,
        "create_repo": create_repo,
        "activate_autopilot": activate_autopilot,
        "max_dispatch": max_dispatch,
        "dry_run": dry_run,
        "status": "running",
        "pid": proc.pid,
        "log_path": str(log_path),
        "started_at": datetime.now(timezone.utc).isoformat(),
        "finished_at": None,
        "exit_code": None,
    }
    save_job(job)

    def watcher() -> None:
        code = proc.wait()
        job["status"] = "success" if code == 0 else "failed"
        job["exit_code"] = code
        job["finished_at"] = datetime.now(timezone.utc).isoformat()
        save_job(job)

    threading.Thread(target=watcher, daemon=True).start()
    return job


def tail_log(path: str, lines: int = 40) -> str:
    file_path = Path(path)
    if not file_path.is_file():
        return ""
    content = file_path.read_text(encoding="utf-8", errors="replace").splitlines()
    return "\n".join(content[-lines:])


class Handler(BaseHTTPRequestHandler):
    server_version = "CEOWorkbench/0.1"

    def log_message(self, fmt: str, *args: Any) -> None:
        return

    def _send_json(self, status: int, payload: Any) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0:
            return {}
        return json.loads(self.rfile.read(length).decode("utf-8"))

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        path = parsed.path

        try:
            if path == "/api/health":
                self._send_json(HTTPStatus.OK, {"ok": True})
                return

            if path == "/api/meta":
                self._send_json(
                    HTTPStatus.OK,
                    {
                        "org": GITHUB_ORG,
                        "registry": str(REGISTRY),
                        "cursor_agent_ready": cursor_agent_ready(),
                        "dispatch_mode": portfolio_dispatch_mode(),
                    },
                )
                return

            if path == "/api/multica-runtime":
                self._send_json(HTTPStatus.OK, multica_runtime_status())
                return

            if path == "/api/company-overview":
                query = parse_qs(parsed.query)
                refresh = query.get("refresh_verify", ["0"])[0] in ("1", "true", "yes")
                self._send_json(HTTPStatus.OK, company_overview(refresh_verify=refresh))
                return

            if path == "/api/projects":
                rows = dashboard_rows()
                totals = {
                    "blocked": sum(int(row.get("blocked", 0)) for row in rows),
                    "running": sum(int(row.get("running", 0)) for row in rows),
                    "agent_safe": sum(int(row.get("agent_safe", 0)) for row in rows),
                    "merged_prs": sum(int(row.get("merged_prs", 0)) for row in rows),
                }
                self._send_json(HTTPStatus.OK, {"projects": rows, "totals": totals})
                return

            if path == "/api/queue":
                query = parse_qs(parsed.query)
                repo = query.get("repo", [""])[0]
                if not repo:
                    self._send_json(HTTPStatus.BAD_REQUEST, {"error": "repo is required"})
                    return
                queue = list_queue(repo)
                blocked = list_label(repo, "agent-blocked")
                running = list_label(repo, "agent-running")
                self._send_json(
                    HTTPStatus.OK,
                    {"repo": repo, "queue": queue, "blocked": blocked, "running": running},
                )
                return

            if path == "/api/jobs":
                self._send_json(HTTPStatus.OK, {"jobs": load_jobs()})
                return

            if path == "/api/site-factory":
                self._send_json(
                    HTTPStatus.OK,
                    {
                        "ok": True,
                        "method": "POST",
                        "fields": ["intake", "create_repo", "activate_autopilot", "notify", "max_dispatch"],
                    },
                )
                return

            match = re.fullmatch(r"/api/jobs/([a-f0-9]+)", path)
            if match:
                job_id = match.group(1)
                jobs = {job["id"]: job for job in load_jobs()}
                job = jobs.get(job_id)
                if not job:
                    self._send_json(HTTPStatus.NOT_FOUND, {"error": "job not found"})
                    return
                job = dict(job)
                job["log_tail"] = tail_log(job.get("log_path", ""))
                self._send_json(HTTPStatus.OK, job)
                return

            if path in ("/", "/index.html"):
                file_path = WORKBENCH_DIR / "index.html"
                content = file_path.read_bytes()
                self.send_response(HTTPStatus.OK)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(content)))
                self.end_headers()
                self.wfile.write(content)
                return

            for name in ("styles.css", "app.js"):
                if path == f"/{name}":
                    file_path = WORKBENCH_DIR / name
                    content = file_path.read_bytes()
                    content_type = "text/css" if name.endswith(".css") else "application/javascript"
                    self.send_response(HTTPStatus.OK)
                    self.send_header("Content-Type", f"{content_type}; charset=utf-8")
                    self.send_header("Content-Length", str(len(content)))
                    self.end_headers()
                    self.wfile.write(content)
                    return

            self._send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
        except Exception as exc:  # noqa: BLE001
            self._send_json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": str(exc)})

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        try:
            if parsed.path == "/api/site-factory":
                body = self._read_json()
                intake = str(body.get("intake", "")).strip()
                if not intake:
                    self._send_json(HTTPStatus.BAD_REQUEST, {"error": "intake is required"})
                    return
                create_repo = bool(body.get("create_repo", False))
                activate_raw = body.get("activate_autopilot")
                activate_autopilot: bool | None
                if activate_raw is None:
                    activate_autopilot = True if create_repo else None
                else:
                    activate_autopilot = bool(activate_raw)
                job = start_site_factory_job(
                    intake=intake,
                    create_repo=create_repo,
                    activate_autopilot=activate_autopilot,
                    notify=body.get("notify", True) is not False,
                    max_dispatch=int(body.get("max_dispatch", 2)),
                    dry_run=bool(body.get("dry_run", False)),
                )
                self._send_json(HTTPStatus.ACCEPTED, job)
                return

            if parsed.path != "/api/dispatch":
                self._send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
                return

            body = self._read_json()
            mode = body.get("mode", "portfolio")
            max_total = int(body.get("max_total", 1))
            repo = body.get("repo", "")
            issue = str(body.get("issue", ""))
            local_path = body.get("local_path", "")

            if not local_path and repo:
                project_id = ""
                for project in parse_registry():
                    if project.repo == repo:
                        project_id = project.id
                        break
                local_path = resolve_repo_path(project_id, repo)

            job = start_dispatch_job(
                mode=mode,
                repo=repo,
                issue=issue,
                max_total=max_total,
                local_path=local_path,
            )
            self._send_json(HTTPStatus.ACCEPTED, job)
        except ValueError as exc:
            self._send_json(HTTPStatus.BAD_REQUEST, {"error": str(exc)})
        except Exception as exc:  # noqa: BLE001
            self._send_json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": str(exc)})


def main() -> None:
    import sys

    if len(sys.argv) > 1 and sys.argv[1] == "--snapshot-json":
        refresh = "--refresh-verify" in sys.argv[2:]
        print(json.dumps(company_overview(refresh_verify=refresh), ensure_ascii=False))
        return

    host = os.environ.get("CEO_WORKBENCH_HOST", "127.0.0.1")
    port = DEFAULT_PORT
    server = ThreadingHTTPServer((host, port), Handler)
    print(f"CEO workbench: http://{host}:{port}")
    print("Press Ctrl+C to stop.")
    server.serve_forever()


if __name__ == "__main__":
    main()
