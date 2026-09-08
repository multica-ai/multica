#!/usr/bin/env python3
"""CEO approval bridge: GitHub BLOCKED ↔ Multica issue ↔ Feishu cards/commands."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
AI_COMPANY_ROOT = SCRIPT_DIR.parent.parent
DEFAULT_REGISTRY = Path.home() / ".multica" / "ceo-approvals" / "registry.json"
DEFAULT_CONFIG = Path.home() / ".multica" / "config.json"
APPROVAL_META_REPO = "ceo_approval.github_repo"
APPROVAL_META_NUMBER = "ceo_approval.github_number"
APPROVAL_META_KIND = "ceo_approval.kind"
FEISHU_NO_PROXY = "open.feishu.cn,feishu.cn,larksuite.com,larkoffice.com"


@dataclass
class PendingApproval:
    project_id: str
    repo: str
    number: int
    title: str
    url: str
    kind: str  # blocked | merge
    multica_issue_id: str | None = None
    multica_identifier: str | None = None


def load_json(path: Path, default: Any) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return default


def save_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def load_multica_config(config_path: Path = DEFAULT_CONFIG) -> dict[str, str]:
    if not config_path.is_file():
        raise SystemExit(f"error: Multica config not found: {config_path} (run multica login)")
    raw = json.loads(config_path.read_text(encoding="utf-8"))
    api = str(raw.get("server_url", "")).rstrip("/")
    token = str(raw.get("token", ""))
    wsid = str(raw.get("workspace_id", ""))
    if not api or not token or not wsid:
        raise SystemExit("error: server_url, token, workspace_id required in multica config")
    return {"api": api, "token": token, "wsid": wsid}


def multica_request(
    cfg: dict[str, str],
    method: str,
    path: str,
    body: dict[str, Any] | None = None,
) -> Any:
    url = cfg["api"] + path
    headers = {
        "Authorization": f"Bearer {cfg['token']}",
        "X-Workspace-ID": cfg["wsid"],
        "Content-Type": "application/json",
    }
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read()
            if not raw:
                return None
            return json.loads(raw.decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise SystemExit(f"multica {method} {path} failed ({exc.code}): {detail}") from exc


def gh_json(args: list[str]) -> Any:
    cmd = ["gh", *args]
    proc = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        raise SystemExit(f"gh failed ({proc.returncode}): {proc.stderr.strip() or proc.stdout.strip()}")
    if not proc.stdout.strip():
        return None
    return json.loads(proc.stdout)


def registry_key(repo: str, number: int) -> str:
    return f"{repo}#{number}"


def load_registry(path: Path = DEFAULT_REGISTRY) -> dict[str, Any]:
    return load_json(path, {})


def save_registry(data: dict[str, Any], path: Path = DEFAULT_REGISTRY) -> None:
    save_json(path, data)


def parse_project_registry(registry_path: Path) -> list[dict[str, Any]]:
    if not registry_path.is_file():
        raise SystemExit(f"error: project registry not found: {registry_path}")
    text = registry_path.read_text(encoding="utf-8")
    projects: list[dict[str, Any]] = []
    current: dict[str, Any] | None = None
    for line in text.splitlines():
        if line.startswith("  - id:"):
            if current:
                projects.append(current)
            current = {"id": line.split(":", 1)[1].strip()}
            continue
        if current is None:
            continue
        if line.startswith("    repo:"):
            current["repo"] = line.split(":", 1)[1].strip()
        elif line.startswith("    paused:"):
            current["paused"] = line.split(":", 1)[1].strip().lower() == "true"
    if current:
        projects.append(current)
    return [p for p in projects if p.get("repo") and not p.get("paused")]


def repo_short_name(repo: str) -> str:
    return repo.split("/")[-1] if "/" in repo else repo


def normalize_repo(repo: str, github_org: str) -> str:
    repo = repo.strip()
    if repo.startswith("github.com/"):
        repo = repo.removeprefix("github.com/")
    if "/" not in repo:
        return f"{github_org}/{repo}"
    return repo


def list_blocked(repo: str) -> list[PendingApproval]:
    rows = gh_json(
        [
            "issue",
            "list",
            "-R",
            repo,
            "-l",
            "agent-blocked",
            "-s",
            "open",
            "--json",
            "number,title,url",
        ]
    )
    project_id = repo_short_name(repo)
    return [
        PendingApproval(
            project_id=project_id,
            repo=repo,
            number=int(row["number"]),
            title=str(row["title"]),
            url=str(row["url"]),
            kind="blocked",
        )
        for row in rows or []
    ]


def list_pending_merge(repo: str) -> list[PendingApproval]:
    rows = gh_json(
        [
            "pr",
            "list",
            "-R",
            repo,
            "-s",
            "open",
            "-L",
            "20",
            "--json",
            "number,title,url,isDraft,statusCheckRollup",
        ]
    )
    out: list[PendingApproval] = []
    for pr in rows or []:
        if pr.get("isDraft"):
            continue
        checks = pr.get("statusCheckRollup") or []
        if not checks:
            continue
        if not all(
            c.get("status") == "COMPLETED" and c.get("conclusion") == "SUCCESS"
            for c in checks
        ):
            continue
        out.append(
            PendingApproval(
                project_id=repo_short_name(repo),
                repo=repo,
                number=int(pr["number"]),
                title=str(pr["title"]),
                url=str(pr["url"]),
                kind="merge",
            )
        )
    return out


def collect_pending(
    registry_path: Path,
    github_org: str,
    *,
    include_merge: bool = True,
) -> list[PendingApproval]:
    pending: list[PendingApproval] = []
    for project in parse_project_registry(registry_path):
        repo = normalize_repo(str(project["repo"]), github_org)
        pending.extend(list_blocked(repo))
        if include_merge:
            pending.extend(list_pending_merge(repo))
    reg = load_registry()
    for item in pending:
        key = registry_key(item.repo, item.number)
        entry = reg.get(key) or {}
        item.multica_issue_id = entry.get("multica_issue_id")
        item.multica_identifier = entry.get("multica_identifier")
    return pending


def ensure_multica_issue(
    cfg: dict[str, str],
    item: PendingApproval,
    *,
    dry_run: bool = False,
) -> tuple[str, str]:
    reg = load_registry()
    key = registry_key(item.repo, item.number)
    entry = reg.get(key) or {}
    if entry.get("multica_issue_id") and entry.get("multica_identifier"):
        return str(entry["multica_issue_id"]), str(entry["multica_identifier"])

    title_prefix = "CEO审批·BLOCKED" if item.kind == "blocked" else "CEO审批·待merge"
    title = f"{title_prefix} {item.repo}#{item.number} — {item.title[:80]}"
    description = "\n".join(
        [
            f"GitHub: {item.url}",
            f"类型: {item.kind}",
            "",
            "在 Multica 或飞书任一侧审批，状态会同步到 GitHub。",
            "",
            f"飞书命令: `/批 {item.project_id} {item.number} <说明>`",
        ]
    )
    if dry_run:
        print(f"[dry-run] would create multica issue for {key}")
        return "dry-run-id", "DRY-1"

    created = multica_request(
        cfg,
        "POST",
        "/api/issues",
        {
            "title": title,
            "description": description,
            "status": "blocked" if item.kind == "blocked" else "in_review",
            "priority": "high",
        },
    )
    issue_id = str(created["id"])
    identifier = str(created.get("identifier") or created.get("number") or issue_id)
    multica_request(
        cfg,
        "PUT",
        f"/api/issues/{issue_id}/metadata/{APPROVAL_META_REPO}",
        {"value": item.repo},
    )
    multica_request(
        cfg,
        "PUT",
        f"/api/issues/{issue_id}/metadata/{APPROVAL_META_NUMBER}",
        {"value": item.number},
    )
    multica_request(
        cfg,
        "PUT",
        f"/api/issues/{issue_id}/metadata/{APPROVAL_META_KIND}",
        {"value": item.kind},
    )
    reg[key] = {
        "multica_issue_id": issue_id,
        "multica_identifier": identifier,
        "github_repo": item.repo,
        "github_number": item.number,
        "kind": item.kind,
        "status": "pending",
        "updated_at": datetime.now(timezone.utc).isoformat(),
    }
    save_registry(reg)
    return issue_id, identifier


def sync_pending(
    registry_path: Path,
    github_org: str,
    *,
    dry_run: bool = False,
    include_merge: bool = True,
) -> list[PendingApproval]:
    cfg = load_multica_config()
    pending = collect_pending(registry_path, github_org, include_merge=include_merge)
    for item in pending:
        issue_id, identifier = ensure_multica_issue(cfg, item, dry_run=dry_run)
        item.multica_issue_id = issue_id
        item.multica_identifier = identifier
    return pending


def github_approve_blocked(repo: str, number: int, comment: str, *, dry_run: bool = False) -> None:
    body = "\n".join(
        [
            "## CEO 批准 @agent",
            "",
            comment.strip() or "已批准，请继续执行。",
            "",
            "来源: Multica/飞书审批桥",
        ]
    )
    if dry_run:
        print(f"[dry-run] gh issue comment {repo}#{number}")
        print(body)
        return
    subprocess.run(
        ["gh", "issue", "comment", str(number), "-R", repo, "-b", body],
        check=True,
    )
    subprocess.run(
        [
            "gh",
            "issue",
            "edit",
            str(number),
            "-R",
            repo,
            "--remove-label",
            "agent-blocked",
            "--add-label",
            "agent-safe",
        ],
        check=True,
    )


def github_reject_blocked(repo: str, number: int, comment: str, *, dry_run: bool = False) -> None:
    body = "\n".join(
        [
            "## CEO 打回",
            "",
            comment.strip() or "需要补充信息后再继续。",
            "",
            "来源: Multica/飞书审批桥",
        ]
    )
    if dry_run:
        print(f"[dry-run] gh issue comment {repo}#{number} (reject)")
        return
    subprocess.run(["gh", "issue", "comment", str(number), "-R", repo, "-b", body], check=True)


def multica_record_decision(
    cfg: dict[str, str],
    issue_id: str,
    decision: str,
    comment: str,
    source: str,
    *,
    dry_run: bool = False,
) -> None:
    body = "\n".join(
        [
            f"## CEO {decision}（{source}）",
            "",
            comment.strip() or "（无附加说明）",
        ]
    )
    if dry_run:
        print(f"[dry-run] multica comment on {issue_id}: {decision}")
        return
    multica_request(
        cfg,
        "POST",
        f"/api/issues/{issue_id}/comments",
        {"content": body},
    )
    status = "done" if decision == "批准" else "blocked"
    multica_request(cfg, "PUT", f"/api/issues/{issue_id}", {"status": status})


def resolve_repo(project_or_repo: str, registry_path: Path, github_org: str) -> str:
    needle = project_or_repo.strip().lower()
    for project in parse_project_registry(registry_path):
        repo = normalize_repo(str(project["repo"]), github_org)
        if needle in {str(project["id"]).lower(), repo.lower(), repo_short_name(repo).lower()}:
            return repo
    if "/" in project_or_repo:
        return normalize_repo(project_or_repo, github_org)
    return f"{github_org}/{project_or_repo}"


def apply_approval(
    repo: str,
    number: int,
    comment: str,
    *,
    source: str = "multica",
    dry_run: bool = False,
) -> dict[str, Any]:
    cfg = load_multica_config()
    key = registry_key(repo, number)
    reg = load_registry()
    entry = reg.get(key) or {}
    kind = str(entry.get("kind") or "blocked")
    issue_id = entry.get("multica_issue_id")

    if kind == "blocked":
        github_approve_blocked(repo, number, comment, dry_run=dry_run)
    elif kind == "merge":
        if dry_run:
            print(f"[dry-run] would merge PR #{number} on {repo}")
        else:
            subprocess.run(["gh", "pr", "merge", str(number), "-R", repo, "--merge"], check=True)
    else:
        raise SystemExit(f"unknown approval kind: {kind}")

    if issue_id:
        multica_record_decision(cfg, str(issue_id), "批准", comment, source, dry_run=dry_run)

    entry.update(
        {
            "status": "approved",
            "last_decision": "approve",
            "last_source": source,
            "updated_at": datetime.now(timezone.utc).isoformat(),
        }
    )
    reg[key] = entry
    if not dry_run:
        save_registry(reg)
    return {"repo": repo, "number": number, "decision": "approve", "kind": kind}


def apply_rejection(
    repo: str,
    number: int,
    comment: str,
    *,
    source: str = "multica",
    dry_run: bool = False,
) -> dict[str, Any]:
    cfg = load_multica_config()
    key = registry_key(repo, number)
    reg = load_registry()
    entry = reg.get(key) or {}
    kind = str(entry.get("kind") or "blocked")
    issue_id = entry.get("multica_issue_id")

    if kind == "blocked":
        github_reject_blocked(repo, number, comment, dry_run=dry_run)
    elif kind == "merge":
        if dry_run:
            print(f"[dry-run] reject merge PR #{number} — comment only")
        else:
            subprocess.run(
                [
                    "gh",
                    "pr",
                    "comment",
                    str(number),
                    "-R",
                    repo,
                    "-b",
                    comment.strip() or "CEO 暂不 merge",
                ],
                check=True,
            )

    if issue_id:
        multica_record_decision(cfg, str(issue_id), "打回", comment, source, dry_run=dry_run)

    entry.update(
        {
            "status": "rejected",
            "last_decision": "reject",
            "last_source": source,
            "updated_at": datetime.now(timezone.utc).isoformat(),
        }
    )
    reg[key] = entry
    if not dry_run:
        save_registry(reg)
    return {"repo": repo, "number": number, "decision": "reject", "kind": kind}


def feishu_http(
    method: str,
    url: str,
    *,
    headers: dict[str, str] | None = None,
    body: bytes | None = None,
    timeout: int = 30,
) -> dict[str, Any]:
    """Call Feishu Open API via curl --noproxy (matches lib/notify.sh; avoids proxy MITM TLS)."""
    cmd = [
        "curl",
        "-sS",
        "--fail",
        "--noproxy",
        FEISHU_NO_PROXY,
        "--max-time",
        str(timeout),
        "-X",
        method,
        url,
    ]
    for key, value in (headers or {}).items():
        cmd.extend(["-H", f"{key}: {value}"])
    if body is not None:
        cmd.extend(["--data-binary", "@-"])
    proc = subprocess.run(cmd, input=body, capture_output=True, check=False)
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).decode("utf-8", errors="replace").strip()
        raise SystemExit(f"feishu curl failed ({proc.returncode}): {detail or url}")
    raw = proc.stdout.decode("utf-8", errors="replace").strip()
    if not raw:
        return {}
    out = json.loads(raw)
    if out.get("code") not in (0, None):
        raise SystemExit(f"feishu api error: {out.get('msg')} — {out}")
    return out


def feishu_token(app_id: str, app_secret: str) -> str:
    out = feishu_http(
        "POST",
        "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
        headers={"Content-Type": "application/json"},
        body=json.dumps({"app_id": app_id, "app_secret": app_secret}).encode(),
    )
    return str(out["tenant_access_token"])


def feishu_curl_post(url: str, token: str, payload: dict[str, Any]) -> dict[str, Any]:
    return feishu_http(
        "POST",
        url,
        headers={
            "Content-Type": "application/json; charset=utf-8",
            "Authorization": f"Bearer {token}",
        },
        body=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
    )


def build_approval_card(item: PendingApproval, frontend_url: str) -> str:
    kind_label = "BLOCKED" if item.kind == "blocked" else "待 merge"
    multica_link = ""
    if item.multica_identifier and frontend_url:
        multica_link = f"{frontend_url.rstrip('/')}/issues/{item.multica_identifier}"
    lines = [
        f"**{kind_label}** · `{item.repo}#{item.number}`",
        item.title,
        f"[GitHub 打开]({item.url})",
    ]
    if multica_link:
        lines.append(f"[Multica 打开]({multica_link})")
    card = {
        "config": {"wide_screen_mode": True},
        "header": {
            "template": "orange" if item.kind == "blocked" else "green",
            "title": {"tag": "plain_text", "content": "CEO 待审批"},
        },
        "elements": [
            {"tag": "div", "text": {"tag": "lark_md", "content": "\n".join(lines)}},
            {
                "tag": "action",
                "actions": [
                    {
                        "tag": "button",
                        "text": {"tag": "plain_text", "content": "批准"},
                        "type": "primary",
                        "value": {
                            "action": "approve",
                            "key": registry_key(item.repo, item.number),
                        },
                    },
                    {
                        "tag": "button",
                        "text": {"tag": "plain_text", "content": "打回"},
                        "type": "danger",
                        "value": {
                            "action": "reject",
                            "key": registry_key(item.repo, item.number),
                        },
                    },
                ],
            },
        ],
    }
    if multica_link:
        card["elements"][1]["actions"].append(
            {
                "tag": "button",
                "text": {"tag": "plain_text", "content": "Multica"},
                "type": "default",
                "url": multica_link,
            }
        )
    return json.dumps(card, ensure_ascii=False)


def push_feishu_cards(
    pending: list[PendingApproval],
    *,
    open_id: str,
    app_id: str,
    app_secret: str,
    frontend_url: str,
    dry_run: bool = False,
) -> int:
    if not pending:
        return 0
    if dry_run:
        for item in pending:
            print(f"[dry-run] card -> {item.repo}#{item.number} ({item.kind})")
        return len(pending)

    token = feishu_token(app_id, app_secret)
    sent = 0
    reg = load_registry()
    for item in pending:
        card_json = build_approval_card(item, frontend_url)
        payload = {
            "receive_id": open_id,
            "msg_type": "interactive",
            "content": card_json,
        }
        out = feishu_curl_post(
            "https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=open_id",
            token,
            payload,
        )
        message_id = str(out.get("data", {}).get("message_id") or "")
        key = registry_key(item.repo, item.number)
        entry = reg.get(key) or {}
        entry["feishu_card_message_id"] = message_id
        reg[key] = entry
        sent += 1
    save_registry(reg)
    return sent


COMMAND_RE = re.compile(
    r"^/(?:批|approve|打回|reject)\s+(\S+)\s+#?(\d+)\s*(.*)$",
    re.IGNORECASE,
)


def parse_text_command(text: str) -> tuple[str, str, int, str] | None:
    text = text.strip()
    match = COMMAND_RE.match(text)
    if not match:
        return None
    verb_raw, project, number_raw, comment = match.groups()
    verb = verb_raw.lower()
    if verb in {"打回", "reject"}:
        action = "reject"
    else:
        action = "approve"
    return action, project, int(number_raw), comment.strip()


def handle_feishu_card_action(value: dict[str, Any]) -> dict[str, Any]:
    action = str(value.get("action") or "")
    key = str(value.get("key") or "")
    if not action or "#" not in key:
        return {"toast": {"type": "warning", "content": "无效卡片操作"}}
    repo, number_raw = key.rsplit("#", 1)
    number = int(number_raw)
    comment = "飞书卡片批准" if action == "approve" else "飞书卡片打回"
    if action == "approve":
        apply_approval(repo, number, comment, source="feishu")
        toast = "已批准，GitHub 与 Multica 已同步"
    elif action == "reject":
        apply_rejection(repo, number, comment, source="feishu")
        toast = "已打回，GitHub 与 Multica 已同步"
    else:
        return {"toast": {"type": "warning", "content": f"未知操作: {action}"}}
    return {"toast": {"type": "info", "content": toast}}


def handle_feishu_text_message(
    text: str,
    registry_path: Path,
    github_org: str,
) -> str | None:
    parsed = parse_text_command(text)
    if not parsed:
        return None
    action, project, number, comment = parsed
    repo = resolve_repo(project, registry_path, github_org)
    if action == "approve":
        apply_approval(repo, number, comment, source="feishu")
        return f"已批准 {repo}#{number}"
    apply_rejection(repo, number, comment, source="feishu")
    return f"已打回 {repo}#{number}"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="CEO approval bridge (GitHub ↔ Multica ↔ Feishu)")
    parser.add_argument(
        "--registry",
        default=str(AI_COMPANY_ROOT / "templates" / "project-registry.yaml"),
    )
    parser.add_argument("--github-org", default=os.environ.get("GITHUB_ORG", "chenzh"))
    parser.add_argument("--frontend-url", default=os.environ.get("MULTICA_FRONTEND_URL", "http://localhost:3000"))
    parser.add_argument("--dry-run", action="store_true")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("list", help="List pending approvals")

    sync_p = sub.add_parser("sync", help="Sync pending GitHub items to Multica issues")
    sync_p.add_argument("--no-merge", action="store_true")

    push_p = sub.add_parser("push", help="Send Feishu approval cards")
    push_p.add_argument("--no-merge", action="store_true")

    approve_p = sub.add_parser("approve", help="Approve one item")
    approve_p.add_argument("project")
    approve_p.add_argument("number", type=int)
    approve_p.add_argument("comment", nargs="*", default=[])

    reject_p = sub.add_parser("reject", help="Reject one item")
    reject_p.add_argument("project")
    reject_p.add_argument("number", type=int)
    reject_p.add_argument("comment", nargs="*", default=[])

    args = parser.parse_args(argv)
    registry_path = Path(args.registry)

    if args.cmd == "list":
        pending = collect_pending(registry_path, args.github_org)
        if not pending:
            print("无待审批项")
            return 0
        for item in pending:
            mid = item.multica_identifier or "-"
            print(f"[{item.kind}] {item.repo}#{item.number} — {item.title} (Multica {mid})")
        return 0

    if args.cmd == "sync":
        pending = sync_pending(
            registry_path,
            args.github_org,
            dry_run=args.dry_run,
            include_merge=not args.no_merge,
        )
        print(f"synced {len(pending)} item(s)")
        return 0

    if args.cmd == "push":
        app_id = os.environ.get("FEISHU_BOT_APP_ID", "")
        app_secret = os.environ.get("FEISHU_BOT_APP_SECRET", "")
        open_id = os.environ.get("FEISHU_BOT_NOTIFY_OPEN_ID", "")
        if not app_id or not app_secret or not open_id:
            raise SystemExit("error: set FEISHU_BOT_APP_ID/SECRET and FEISHU_BOT_NOTIFY_OPEN_ID")
        pending = sync_pending(
            registry_path,
            args.github_org,
            dry_run=args.dry_run,
            include_merge=not args.no_merge,
        )
        sent = push_feishu_cards(
            pending,
            open_id=open_id,
            app_id=app_id,
            app_secret=app_secret,
            frontend_url=args.frontend_url,
            dry_run=args.dry_run,
        )
        print(f"sent {sent} feishu card(s)")
        return 0

    if args.cmd == "approve":
        repo = resolve_repo(args.project, registry_path, args.github_org)
        apply_approval(repo, args.number, " ".join(args.comment), source="cli", dry_run=args.dry_run)
        print(f"approved {repo}#{args.number}")
        return 0

    if args.cmd == "reject":
        repo = resolve_repo(args.project, registry_path, args.github_org)
        apply_rejection(repo, args.number, " ".join(args.comment), source="cli", dry_run=args.dry_run)
        print(f"rejected {repo}#{args.number}")
        return 0

    return 1


if __name__ == "__main__":
    raise SystemExit(main())
