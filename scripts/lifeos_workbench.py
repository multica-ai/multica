#!/usr/bin/env python3
"""LifeOS 工作台本机运行入口。"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import plistlib
import re
import secrets
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import unicodedata
import urllib.error
import urllib.request
import webbrowser
from pathlib import Path
from typing import Dict, Iterable, List, Mapping, Optional, Sequence


REPO = Path(__file__).resolve().parents[1]
ENV_FILE = REPO / ".env.lifeos"
COMPOSE_FILES = (
    REPO / "docker-compose.selfhost.yml",
    REPO / "docker-compose.selfhost.build.yml",
    REPO / "docker-compose.lifeos.yml",
)
LOCAL_APP_ORIGIN = "http://127.0.0.1:3000"
APP_URL = LOCAL_APP_ORIGIN + "/lifeos/issues"
BACKEND_URL = "http://127.0.0.1:8080"
PROFILE = "lifeos"
LAUNCH_LABEL = "ai.lifeos.workbench"
INDEX_LABEL = "ai.lifeos.workbench.index"
BACKUP_LABEL = "ai.lifeos.workbench.backup"
CHATGPT_WAKE_LABEL = "ai.lifeos.chatgpt-wake"
CLOUDFLARE_TUNNEL_LABEL = "ai.lifeos.cloudflare-tunnel"
INDEX_SYNC_INTERVAL_SECONDS = 2 * 60 * 60
LOCAL_AUTH_VERSION = "2"
LOCAL_PASSWORD_ITERATIONS = 600_000
LOCAL_USERNAME_PATTERN = re.compile(r"^[A-Za-z0-9._-]{3,64}$")
CLI_ALIAS = REPO / "server" / "bin" / "multica"
STATE_ROOT = Path.home() / "Library/Application Support/LifeOS"
CONTEXT_DB = STATE_ROOT / "data/lifeos-workbench.sqlite3"
AUTOMATION_TOKEN_FILE = STATE_ROOT / "secrets/automation-token"
WORKBENCH_DEPLOYED_HEAD_ENV = "LIFEOS_WORKBENCH_DEPLOYED_GIT_HEAD"
WORKBENCH_DEPLOYED_SHA_ENV = "LIFEOS_WORKBENCH_DEPLOYED_SCRIPT_SHA256"
CONTROLLER_DEPLOYED_HEAD_ENV = "LIFEOS_CONTROLLER_DEPLOYED_GIT_HEAD"
CONTROLLER_DEPLOYED_SHA_ENV = "LIFEOS_CONTROLLER_DEPLOYED_SCRIPT_SHA256"


class WorkbenchError(RuntimeError):
    pass


def _candidate_codex_paths() -> List[Path]:
    paths: List[Path] = []
    found = shutil.which("codex")
    if found:
        paths.append(Path(found))
    paths.extend(sorted((Path.home() / ".nvm/versions/node").glob("*/bin/codex"), reverse=True))
    paths.extend(
        [
            Path.home() / ".local/bin/codex",
            Path("/opt/homebrew/bin/codex"),
            Path("/usr/local/bin/codex"),
        ]
    )
    return [path for path in paths if path.is_file()]


def runtime_environment(lifeos_root: Path, controller_root: Path) -> Dict[str, str]:
    environment = os.environ.copy()
    path_parts = [
        str((REPO / "server" / "bin").resolve()),
        "/opt/homebrew/bin",
        "/usr/local/bin",
        "/usr/bin",
        "/bin",
    ]
    candidates = _candidate_codex_paths()
    if candidates:
        path_parts.insert(0, str(candidates[0].parent))
    current_path = environment.get("PATH", "")
    if current_path:
        path_parts.append(current_path)
    environment.update(
        {
            "PATH": ":".join(dict.fromkeys(path_parts)),
            "LIFEOS_ROOT": str(lifeos_root),
            "LIFEOS_CONTROLLER_ROOT": str(controller_root),
            "LIFEOS_CODEX_HOME": str(Path.home() / ".codex"),
            "LIFEOS_SERVER_URL": BACKEND_URL,
            "LIFEOS_WORKBENCH_DB": str(CONTEXT_DB),
            "LIFEOS_AUTOMATION_TOKEN_FILE": str(AUTOMATION_TOKEN_FILE),
        }
    )
    return environment


def ensure_context_database(lifeos_root: Path) -> Path:
    """Keep writable background state outside macOS-protected Documents."""
    CONTEXT_DB.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    os.chmod(STATE_ROOT, 0o700)
    os.chmod(CONTEXT_DB.parent, 0o700)
    if CONTEXT_DB.is_file():
        os.chmod(CONTEXT_DB, 0o600)
        return CONTEXT_DB

    legacy = lifeos_root / "inbox/lifeos-workbench.sqlite3"
    if legacy.is_file():
        source = sqlite3.connect("file:%s?mode=ro" % legacy, uri=True)
        destination = sqlite3.connect(str(CONTEXT_DB))
        try:
            source.backup(destination)
        finally:
            destination.close()
            source.close()
    if CONTEXT_DB.exists():
        os.chmod(CONTEXT_DB, 0o600)
    return CONTEXT_DB


def _find_executable(name: str, environment: Optional[Mapping[str, str]] = None) -> str:
    path = shutil.which(name, path=(environment or os.environ).get("PATH"))
    if not path:
        raise WorkbenchError("缺少本机依赖：%s" % name)
    return path


def _run(
    command: Sequence[str],
    *,
    environment: Mapping[str, str],
    cwd: Path = REPO,
    check: bool = True,
    capture: bool = False,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(command),
        cwd=str(cwd),
        env=dict(environment),
        text=True,
        capture_output=capture,
        check=check,
    )


def parse_env(path: Path = ENV_FILE) -> Dict[str, str]:
    result: Dict[str, str] = {}
    if not path.is_file():
        return result
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        result[key] = value
    return result


def _normalize_public_origin(value: str) -> str:
    origin = value.strip().rstrip("/")
    if not origin.startswith("https://"):
        raise WorkbenchError("公网入口必须使用 https://")
    remainder = origin[len("https://") :]
    if not remainder or "/" in remainder or "@" in remainder:
        raise WorkbenchError("公网入口必须是只包含域名的 HTTPS origin")
    return origin


def configured_app_url(path: Path = ENV_FILE) -> str:
    values = parse_env(path)
    origin = (
        values.get("MULTICA_APP_URL")
        or values.get("FRONTEND_ORIGIN")
        or LOCAL_APP_ORIGIN
    )
    return origin.rstrip("/") + "/lifeos/issues"


def ensure_env(
    path: Path = ENV_FILE, public_origin: Optional[str] = None
) -> Dict[str, str]:
    existing = parse_env(path)
    jwt_secret = existing.get("JWT_SECRET")
    if existing.get("LIFEOS_AUTH_VERSION") != LOCAL_AUTH_VERSION:
        # The strong-login rollout must invalidate cookies issued by the old
        # passwordless local session. Rotate once, then keep the secret stable.
        jwt_secret = secrets.token_urlsafe(64)
    requested_origin = public_origin or os.environ.get("LIFEOS_PUBLIC_ORIGIN", "")
    requested_origin = (
        _normalize_public_origin(requested_origin) if requested_origin.strip() else ""
    )
    frontend_origin = (
        requested_origin or existing.get("FRONTEND_ORIGIN") or LOCAL_APP_ORIGIN
    )
    required = {
        "POSTGRES_DB": "lifeos",
        "POSTGRES_USER": "lifeos",
        "POSTGRES_PASSWORD": existing.get("POSTGRES_PASSWORD") or secrets.token_urlsafe(36),
        "JWT_SECRET": jwt_secret or secrets.token_urlsafe(64),
        "LIFEOS_AUTH_VERSION": LOCAL_AUTH_VERSION,
        "LIFEOS_AUTOMATION_TOKEN": existing.get("LIFEOS_AUTOMATION_TOKEN")
        or secrets.token_urlsafe(48),
        "LIFEOS_LOCAL_MODE": "true",
        "ALLOW_SIGNUP": "false",
        "DISABLE_WORKSPACE_CREATION": "true",
        "FRONTEND_ORIGIN": frontend_origin,
        "MULTICA_APP_URL": requested_origin
        or existing.get("MULTICA_APP_URL")
        or frontend_origin,
        "CORS_ALLOWED_ORIGINS": requested_origin
        or existing.get("CORS_ALLOWED_ORIGINS")
        or frontend_origin,
        "ALLOWED_ORIGINS": requested_origin
        or existing.get("ALLOWED_ORIGINS")
        or frontend_origin,
        "BACKEND_PORT": "8080",
        "FRONTEND_PORT": "3000",
        "APP_ENV": "production",
    }
    values = dict(existing)
    values.update(required)
    lines = [
        "# LifeOS 工作台本机配置。包含凭据，禁止提交或分享。",
        *["%s=%s" % (key, values[key]) for key in sorted(values)],
        "",
    ]
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=str(path.parent), prefix=".env.lifeos.", delete=False
    )
    temp_path = Path(handle.name)
    try:
        os.chmod(temp_path, 0o600)
        with handle:
            handle.write("\n".join(lines))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_path, path)
        os.chmod(path, 0o600)
    finally:
        if temp_path.exists():
            temp_path.unlink()
    AUTOMATION_TOKEN_FILE.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    os.chmod(AUTOMATION_TOKEN_FILE.parent, 0o700)
    token_handle = tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        dir=str(AUTOMATION_TOKEN_FILE.parent),
        prefix="automation-token.",
        delete=False,
    )
    token_path = Path(token_handle.name)
    try:
        os.chmod(token_path, 0o600)
        with token_handle:
            token_handle.write(values["LIFEOS_AUTOMATION_TOKEN"] + "\n")
            token_handle.flush()
            os.fsync(token_handle.fileno())
        os.replace(token_path, AUTOMATION_TOKEN_FILE)
        os.chmod(AUTOMATION_TOKEN_FILE, 0o600)
    finally:
        if token_path.exists():
            token_path.unlink()
    return values


def compose_command(environment: Mapping[str, str]) -> List[str]:
    docker = _find_executable("docker", environment)
    command = [docker, "compose", "--env-file", str(ENV_FILE)]
    for compose_file in COMPOSE_FILES:
        command.extend(["-f", str(compose_file)])
    return command


def docker_ready(environment: Mapping[str, str]) -> bool:
    try:
        completed = _run(
            [_find_executable("docker", environment), "info"],
            environment=environment,
            check=False,
            capture=True,
        )
    except WorkbenchError:
        return False
    return completed.returncode == 0


def ensure_docker(environment: Mapping[str, str]) -> None:
    if docker_ready(environment):
        return
    colima = _find_executable("colima", environment)
    print("正在启动 LifeOS 本机运行底座…")
    _run(
        [
            colima,
            "start",
            "--cpu",
            "4",
            "--memory",
            "6",
            "--disk",
            "60",
            "--dns",
            "223.5.5.5",
            "--dns",
            "114.114.114.114",
        ],
        environment=environment,
    )
    if not docker_ready(environment):
        raise WorkbenchError("本机容器运行底座启动失败")


def wait_for_url(url: str, timeout: float = 180.0) -> None:
    deadline = time.monotonic() + timeout
    last_error = ""
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=5) as response:
                if 200 <= response.status < 500:
                    return
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            last_error = str(error)
        time.sleep(2)
    raise WorkbenchError("等待本地服务超时：%s（%s）" % (url, last_error))


def cli_binary() -> Path:
    return REPO / "server" / "bin" / "lifeos-multica"


def build_cli(environment: Mapping[str, str]) -> Path:
    go = _find_executable("go", environment)
    target = cli_binary()
    target.parent.mkdir(parents=True, exist_ok=True)
    _run(
        [go, "build", "-o", str(target), "./cmd/multica"],
        environment=environment,
        cwd=REPO / "server",
    )
    os.chmod(target, 0o755)
    ensure_cli_alias(target)
    return target


def ensure_cli_alias(target: Optional[Path] = None) -> Path:
    """Expose the task-scoped CLI name expected by Multica's Agent workflow."""
    source = (target or cli_binary()).resolve()
    if not source.is_file():
        raise WorkbenchError("LifeOS Multica CLI 尚未构建")
    temp_alias = CLI_ALIAS.with_name(".multica.lifeos-link")
    try:
        temp_alias.unlink(missing_ok=True)
        os.symlink(source.name, temp_alias)
        os.replace(temp_alias, CLI_ALIAS)
    finally:
        temp_alias.unlink(missing_ok=True)
    return CLI_ALIAS


def resolve_controller_root(value: Optional[Path]) -> Path:
    if value:
        root = value.expanduser().resolve()
    elif os.environ.get("LIFEOS_CONTROLLER_ROOT"):
        root = Path(os.environ["LIFEOS_CONTROLLER_ROOT"]).expanduser().resolve()
    else:
        sibling = REPO.parent / "lifeos-ai-workbench-control"
        root = sibling if (sibling / "scripts/lifeos_mcp_server.py").is_file() else Path.home() / "Documents/Life OS AI"
        root = root.resolve()
    if not (root / "scripts/lifeos_mcp_server.py").is_file():
        raise WorkbenchError("找不到 LifeOS 控制器：%s" % root)
    return root


def resolve_lifeos_root(value: Optional[Path]) -> Path:
    root = (value or Path(os.environ.get("LIFEOS_ROOT", str(Path.home() / "Documents/Life OS AI")))).expanduser().resolve()
    if not (root / "meta/00-charter.md").is_file():
        raise WorkbenchError("找不到 LifeOS AI 正本：%s" % root)
    return root


def configure_agents(
    environment: Mapping[str, str], lifeos_root: Path, controller_root: Path
) -> None:
    binary = cli_binary()
    if not binary.is_file():
        raise WorkbenchError("LifeOS Multica CLI 尚未构建")
    _run(
        [
            str(binary),
            "setup",
            "lifeos",
            "--profile",
            PROFILE,
            "--server-url",
            BACKEND_URL,
            "--app-url",
            "http://127.0.0.1:3000",
            "--lifeos-root",
            str(lifeos_root),
            "--controller-root",
            str(controller_root),
        ],
        environment=environment,
        cwd=REPO / "server",
    )


def sync_codex_index(
    environment: Mapping[str, str], lifeos_root: Path, controller_root: Path
) -> None:
    database = ensure_context_database(lifeos_root)
    _run(
        [
            sys.executable,
            str(controller_root / "scripts/lifeos_controller.py"),
            "--root",
            str(lifeos_root),
            "--database",
            str(database),
            "--server-url",
            BACKEND_URL,
            "--codex-home",
            str(Path.home() / ".codex"),
            "sync",
        ],
        environment=environment,
        cwd=controller_root,
    )


def start(
    lifeos_root: Path,
    controller_root: Path,
    *,
    build: bool = True,
    open_browser: bool = True,
    start_executor: bool = True,
) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    ensure_env()
    ensure_docker(environment)
    compose = compose_command(environment)
    print("正在启动 LifeOS 工作台…")
    command = compose + ["up", "-d"]
    if build:
        command.append("--build")
    _run(command, environment=environment)
    wait_for_url(BACKEND_URL + "/readyz", timeout=240)
    wait_for_url("http://127.0.0.1:3000/api/config", timeout=240)
    if build or not cli_binary().is_file():
        build_cli(environment)
    else:
        ensure_cli_alias()
    if start_executor:
        configure_agents(environment, lifeos_root, controller_root)
    sync_codex_index(environment, lifeos_root, controller_root)
    app_url = configured_app_url()
    if start_executor:
        print("LifeOS 工作台已就绪：%s" % app_url)
    else:
        print("LifeOS 看板与索引已就绪；AI 执行器由本机后台管家按需恢复。")
    if open_browser:
        webbrowser.open(app_url)


def ensure_running(lifeos_root: Path, controller_root: Path) -> None:
    """由本机后台管家调用；幂等恢复看板、执行器和全局索引。"""
    start(
        lifeos_root,
        controller_root,
        build=False,
        open_browser=False,
        start_executor=True,
    )


def _controller_json(
    lifeos_root: Path,
    controller_root: Path,
    *arguments: str,
) -> Dict[str, object]:
    environment = runtime_environment(lifeos_root, controller_root)
    database = ensure_context_database(lifeos_root)
    completed = _run(
        [
            sys.executable,
            str(controller_root / "scripts/lifeos_controller.py"),
            "--root",
            str(lifeos_root),
            "--database",
            str(database),
            "--server-url",
            BACKEND_URL,
            "--codex-home",
            str(Path.home() / ".codex"),
            "--role",
            "ceo",
            *arguments,
        ],
        environment=environment,
        capture=True,
    )
    try:
        payload = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise WorkbenchError("LifeOS 后台控制器没有返回有效收据") from error
    if not isinstance(payload, dict):
        raise WorkbenchError("LifeOS 后台控制器收据格式不正确")
    return payload


def _committed_implementation_revision(
    repository: Path,
    protected_path: str,
    *,
    deployed_head: Optional[str] = None,
    deployed_sha256: Optional[str] = None,
) -> str:
    """Bind a runtime implementation file to a durable Git commit."""
    environment = os.environ.copy()
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    if bool(deployed_head) != bool(deployed_sha256):
        raise WorkbenchError("LifeOS 后台实现部署绑定不完整")
    try:
        head = _run(
            ["git", "rev-parse", "HEAD"],
            environment=environment,
            cwd=repository,
            capture=True,
        ).stdout.strip()
        dirty = _run(
            ["git", "status", "--porcelain", "--", protected_path],
            environment=environment,
            cwd=repository,
            capture=True,
        ).stdout.strip()
    except (OSError, subprocess.CalledProcessError) as error:
        if not deployed_head or not deployed_sha256:
            raise WorkbenchError("LifeOS 后台实现无法绑定 Git 提交") from error
        if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", deployed_head):
            raise WorkbenchError("LifeOS 后台实现部署提交格式无效") from error
        if not re.fullmatch(r"[0-9a-f]{64}", deployed_sha256):
            raise WorkbenchError("LifeOS 后台实现部署哈希格式无效") from error
        implementation_path = repository / protected_path
        if (
            not implementation_path.is_file()
            or _sha256_file(implementation_path) != deployed_sha256
        ):
            raise WorkbenchError("LifeOS 后台实现与已部署提交绑定不一致") from error
        return deployed_head
    if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", head):
        raise WorkbenchError("LifeOS 后台实现 Git 提交格式无效")
    if dirty:
        raise WorkbenchError("LifeOS 后台实现存在未提交改动，已停止同步")
    if deployed_head and deployed_sha256:
        implementation_path = repository / protected_path
        if (
            head != deployed_head
            or not implementation_path.is_file()
            or _sha256_file(implementation_path) != deployed_sha256
        ):
            raise WorkbenchError("LifeOS 后台实现与已部署提交绑定不一致")
    return head


def implementation_deployment_environment(
    controller_root: Path,
) -> Dict[str, str]:
    """Create a non-secret commit binding while Git metadata is accessible."""
    workbench_path = REPO / "scripts/lifeos_workbench.py"
    controller_path = controller_root / "scripts/lifeos_controller.py"
    return {
        WORKBENCH_DEPLOYED_HEAD_ENV: _committed_implementation_revision(
            REPO,
            "scripts/lifeos_workbench.py",
        ),
        WORKBENCH_DEPLOYED_SHA_ENV: _sha256_file(workbench_path),
        CONTROLLER_DEPLOYED_HEAD_ENV: _committed_implementation_revision(
            controller_root,
            "scripts/lifeos_controller.py",
        ),
        CONTROLLER_DEPLOYED_SHA_ENV: _sha256_file(controller_path),
    }


def implementation_provenance(controller_root: Path) -> Dict[str, object]:
    workbench_path = REPO / "scripts/lifeos_workbench.py"
    controller_path = controller_root / "scripts/lifeos_controller.py"
    return {
        "status": "committed",
        "workbench_git_head": _committed_implementation_revision(
            REPO,
            "scripts/lifeos_workbench.py",
            deployed_head=os.environ.get(WORKBENCH_DEPLOYED_HEAD_ENV),
            deployed_sha256=os.environ.get(WORKBENCH_DEPLOYED_SHA_ENV),
        ),
        "workbench_script_sha256": _sha256_file(workbench_path),
        "controller_git_head": _committed_implementation_revision(
            controller_root,
            "scripts/lifeos_controller.py",
            deployed_head=os.environ.get(CONTROLLER_DEPLOYED_HEAD_ENV),
            deployed_sha256=os.environ.get(CONTROLLER_DEPLOYED_SHA_ENV),
        ),
        "controller_script_sha256": _sha256_file(controller_path),
    }


def persist_background_sync_receipt(result: Mapping[str, object]) -> Path:
    """Persist a private, source-free runtime receipt for commit binding."""
    receipt_root = STATE_ROOT / "receipts/background-sync"
    receipt_root.mkdir(parents=True, mode=0o700, exist_ok=True)
    os.chmod(receipt_root.parent, 0o700)
    os.chmod(receipt_root, 0o700)
    completed_at = dt.datetime.now(dt.timezone.utc)
    payload = {
        "schema_version": 1,
        "kind": "lifeos_background_sync_receipt",
        "completed_at": completed_at.isoformat(),
        "result": dict(result),
    }
    target = receipt_root / (
        completed_at.strftime("%Y%m%dT%H%M%S.%fZ") + ".json"
    )
    descriptor, temporary_name = tempfile.mkstemp(
        dir=receipt_root,
        prefix=".background-sync-",
        suffix=".tmp",
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary_name, 0o600)
        os.replace(temporary_name, target)
    except BaseException:
        Path(temporary_name).unlink(missing_ok=True)
        raise
    return target


def background_sync(
    lifeos_root: Path,
    controller_root: Path,
    *,
    summary_limit: int = 5,
    triage_limit: int = 20,
    max_summaries: int = 25,
) -> Dict[str, object]:
    """Run the bounded steward loop without creating a Codex task or thread."""
    if summary_limit < 1 or triage_limit < 1 or max_summaries < 1:
        raise WorkbenchError("LifeOS 后台同步批次参数必须大于零")

    start_provenance = implementation_provenance(controller_root)
    ensure_running(lifeos_root, controller_root)
    summaries_processed = 0
    reviews_processed = 0
    iterations = 0
    coverage: Dict[str, object] = {}

    while iterations < 20:
        iterations += 1
        remaining = max_summaries - summaries_processed
        processed: object = []
        if remaining > 0:
            summaries = _controller_json(
                lifeos_root,
                controller_root,
                "process-summaries",
                "--limit",
                str(min(summary_limit, remaining)),
            )
            processed = summaries.get("processed")
            summaries_processed += len(processed) if isinstance(processed, list) else 0
            coverage_value = summaries.get("coverage")
            coverage = coverage_value if isinstance(coverage_value, dict) else {}
        else:
            coverage = _controller_json(
                lifeos_root,
                controller_root,
                "coverage",
            )

        triage = _controller_json(
            lifeos_root,
            controller_root,
            "triage-ceo",
            "--limit",
            str(triage_limit),
            "--allow-board-fallback",
        )
        triaged = triage.get("processed")
        reviews_processed += len(triaged) if isinstance(triaged, list) else 0
        coverage_value = triage.get("coverage")
        coverage = coverage_value if isinstance(coverage_value, dict) else coverage

        summaries_pending = int(coverage.get("summaries_pending") or 0)
        reviews_pending = int(coverage.get("ceo_reviews_pending") or 0)
        if summaries_pending == 0 and reviews_pending == 0:
            break
        if summaries_pending > 0 and summaries_processed >= max_summaries:
            break
        if not processed and not triaged:
            raise WorkbenchError("LifeOS 后台同步队列未取得进展，请检查本机日志")

    end_provenance = implementation_provenance(controller_root)
    if end_provenance != start_provenance:
        raise WorkbenchError("LifeOS 后台实现提交在同步期间发生变化")
    result: Dict[str, object] = {
        "status": (
            "completed"
            if int(coverage.get("summaries_pending") or 0) == 0
            and int(coverage.get("ceo_reviews_pending") or 0) == 0
            else "deferred"
        ),
        "summaries_processed": summaries_processed,
        "ceo_reviews_processed": reviews_processed,
        "coverage": coverage,
        "raw_content_stored": False,
        "codex_task_created": False,
        "action_projection": "deferred_to_visible_sync",
        "implementation_provenance": end_provenance,
    }
    receipt_path = persist_background_sync_receipt(result)
    result["receipt_ref"] = str(receipt_path)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


def stop(lifeos_root: Path, controller_root: Path) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    binary = cli_binary()
    if binary.is_file():
        _run(
            [str(binary), "daemon", "stop", "--profile", PROFILE],
            environment=environment,
            cwd=REPO / "server",
            check=False,
        )
    if ENV_FILE.is_file() and docker_ready(environment):
        _run(compose_command(environment) + ["stop"], environment=environment, check=False)
    print("LifeOS 工作台已停止；数据卷和任务记录已保留。")


def status(lifeos_root: Path, controller_root: Path) -> int:
    environment = runtime_environment(lifeos_root, controller_root)
    if not ENV_FILE.is_file() or not docker_ready(environment):
        print("LifeOS 工作台未运行。")
        return 1
    compose_status = _run(
        compose_command(environment) + ["ps", "--format", "json"],
        environment=environment,
        check=False,
        capture=True,
    )
    services = []
    for line in compose_status.stdout.splitlines():
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        services.append(
            {
                "service": item.get("Service"),
                "state": item.get("State"),
                "health": item.get("Health"),
            }
        )
    daemon = None
    if cli_binary().is_file():
        daemon_result = _run(
            [str(cli_binary()), "daemon", "status", "--profile", PROFILE, "--output", "json"],
            environment=environment,
            cwd=REPO / "server",
            check=False,
            capture=True,
        )
        try:
            daemon = json.loads(daemon_result.stdout) if daemon_result.stdout.strip() else None
        except json.JSONDecodeError:
            daemon = {"status": "unknown", "detail": daemon_result.stdout.strip()[:500]}
    payload = {"app_url": configured_app_url(), "services": services, "daemon": daemon}
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    healthy = bool(services) and all(item.get("state") == "running" for item in services)
    return 0 if healthy else 1


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def backup(lifeos_root: Path, controller_root: Path) -> Path:
    environment = runtime_environment(lifeos_root, controller_root)
    ensure_env()
    if not docker_ready(environment):
        raise WorkbenchError("备份前需要启动本机容器运行底座")
    timestamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    root = Path.home() / "Library/Application Support/LifeOS/backups" / timestamp
    root.mkdir(parents=True, mode=0o700)
    os.chmod(root, 0o700)
    postgres_dump = root / "workbench-postgres.dump"
    with postgres_dump.open("wb") as output:
        completed = subprocess.run(
            compose_command(environment)
            + ["exec", "-T", "postgres", "pg_dump", "-U", "lifeos", "-d", "lifeos", "--format=custom"],
            cwd=str(REPO),
            env=dict(environment),
            stdout=output,
            stderr=subprocess.PIPE,
            check=False,
        )
    if completed.returncode != 0:
        raise WorkbenchError("PostgreSQL 备份失败")
    os.chmod(postgres_dump, 0o600)

    files = [postgres_dump]
    source_db = ensure_context_database(lifeos_root)
    if source_db.is_file():
        sqlite_dump = root / "lifeos-context.sqlite3"
        source = sqlite3.connect("file:%s?mode=ro" % source_db, uri=True)
        destination = sqlite3.connect(str(sqlite_dump))
        try:
            source.backup(destination)
        finally:
            destination.close()
            source.close()
        os.chmod(sqlite_dump, 0o600)
        files.append(sqlite_dump)
    manifest = {
        "created_at": dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat(),
        "lifeos_root": str(lifeos_root),
        "contains_codex_chat_raw": False,
        "files": [
            {"name": path.name, "bytes": path.stat().st_size, "sha256": _sha256_file(path)}
            for path in files
        ],
    }
    manifest_path = root / "manifest.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    os.chmod(manifest_path, 0o600)
    print("LifeOS 工作台备份已完成：%s" % root)
    return root


def doctor(lifeos_root: Path, controller_root: Path) -> int:
    environment = runtime_environment(lifeos_root, controller_root)
    database = ensure_context_database(lifeos_root)
    completed = _run(
        [
            sys.executable,
            str(controller_root / "scripts/lifeos_controller.py"),
            "--root",
            str(lifeos_root),
            "--database",
            str(database),
            "--server-url",
            BACKEND_URL,
            "--codex-home",
            str(Path.home() / ".codex"),
            "doctor",
        ],
        environment=environment,
        cwd=controller_root,
        check=False,
    )
    return completed.returncode


def _validate_local_password(username: str, password: str) -> str:
    normalized_username = username.strip().lower()
    if not LOCAL_USERNAME_PATTERN.fullmatch(normalized_username):
        raise WorkbenchError("用户名需为 3–64 位字母、数字、点、下划线或连字符")
    if not 12 <= len(password) <= 128:
        raise WorkbenchError("密码需为 12–128 个字符")
    categories = (
        any(value.islower() for value in password),
        any(value.isupper() for value in password),
        any(value.isdigit() for value in password),
        any(unicodedata.category(value)[0] in {"P", "S"} for value in password),
    )
    if sum(categories) < 3:
        raise WorkbenchError("密码至少包含小写、大写、数字、符号中的三类")
    if normalized_username in password.lower():
        raise WorkbenchError("密码不能包含用户名")
    return normalized_username


def reset_login(
    lifeos_root: Path,
    controller_root: Path,
    *,
    username: str,
    password_file: Path,
) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    values = ensure_env()
    if not docker_ready(environment):
        raise WorkbenchError("重置登录前需要启动本机容器运行底座")
    password_file = password_file.expanduser().resolve()
    if not password_file.is_file() or password_file.stat().st_mode & 0o077:
        raise WorkbenchError("密码文件必须存在且权限为 0600")
    raw_password = password_file.read_text(encoding="utf-8")
    password = raw_password[:-1] if raw_password.endswith("\n") else raw_password
    if "\n" in password or "\r" in password:
        raise WorkbenchError("密码文件只能包含一行密码")
    normalized_username = _validate_local_password(username, password)
    salt = secrets.token_bytes(32)
    digest = hashlib.pbkdf2_hmac(
        "sha256",
        password.encode("utf-8"),
        salt,
        LOCAL_PASSWORD_ITERATIONS,
        dklen=32,
    )
    statement = (
        "INSERT INTO lifeos_local_credential("
        "singleton, username, password_salt, password_hash, password_iterations, "
        "failed_attempts, locked_until) VALUES ("
        "1, '%s', decode('%s', 'hex'), decode('%s', 'hex'), %d, 0, NULL) "
        "ON CONFLICT (singleton) DO UPDATE SET "
        "username = EXCLUDED.username, password_salt = EXCLUDED.password_salt, "
        "password_hash = EXCLUDED.password_hash, "
        "password_iterations = EXCLUDED.password_iterations, failed_attempts = 0, "
        "locked_until = NULL, updated_at = now();\n"
        % (
            normalized_username,
            salt.hex(),
            digest.hex(),
            LOCAL_PASSWORD_ITERATIONS,
        )
    )
    completed = subprocess.run(
        compose_command(environment)
        + [
            "exec",
            "-T",
            "postgres",
            "psql",
            "-v",
            "ON_ERROR_STOP=1",
            "-U",
            values["POSTGRES_USER"],
            "-d",
            values["POSTGRES_DB"],
        ],
        cwd=str(REPO),
        env=dict(environment),
        input=statement,
        text=True,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise WorkbenchError("本机登录凭据更新失败")
    print("LifeOS 本机登录凭据已安全重置。")


def install_cloudflare_tunnel(
    lifeos_root: Path,
    controller_root: Path,
    *,
    token_file: Path,
    public_origin: str,
) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    origin = _normalize_public_origin(public_origin)
    token_file = token_file.expanduser().resolve()
    if not token_file.is_file() or token_file.stat().st_mode & 0o077:
        raise WorkbenchError("Cloudflare Tunnel token 文件必须存在且权限为 0600")
    cloudflared = _find_executable("cloudflared", environment)
    ensure_env(public_origin=origin)
    launch_dir = Path.home() / "Library/LaunchAgents"
    log_dir = Path.home() / "Library/Logs/LifeOS"
    log_dir.mkdir(parents=True, exist_ok=True)
    path = launch_dir / (CLOUDFLARE_TUNNEL_LABEL + ".plist")
    payload = {
        "Label": CLOUDFLARE_TUNNEL_LABEL,
        "ProgramArguments": [
            cloudflared,
            "tunnel",
            "--no-autoupdate",
            "--metrics",
            "127.0.0.1:20249",
            "run",
            "--token-file",
            str(token_file),
            "--url",
            LOCAL_APP_ORIGIN,
        ],
        "RunAtLoad": True,
        "KeepAlive": True,
        "ThrottleInterval": 15,
        "StandardOutPath": str(log_dir / "cloudflare-tunnel.log"),
        "StandardErrorPath": str(log_dir / "cloudflare-tunnel-error.log"),
    }
    launchctl = _find_executable("launchctl", environment)
    domain = "gui/%s" % os.getuid()
    _run(
        [launchctl, "bootout", domain + "/" + CLOUDFLARE_TUNNEL_LABEL],
        environment=environment,
        check=False,
        capture=True,
    )
    _write_plist(path, payload)
    _run([launchctl, "bootstrap", domain, str(path)], environment=environment)
    print("LifeOS 公网隧道已启用，并将在登录本机后自动恢复。")


def _write_plist(path: Path, payload: Mapping[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(mode="wb", dir=str(path.parent), prefix=path.name + ".", delete=False)
    temp_path = Path(handle.name)
    try:
        os.chmod(temp_path, 0o600)
        with handle:
            plistlib.dump(dict(payload), handle)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_path, path)
        os.chmod(path, 0o600)
    finally:
        if temp_path.exists():
            temp_path.unlink()


def install_autostart(lifeos_root: Path, controller_root: Path) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    database = ensure_context_database(lifeos_root)
    deployment_environment = implementation_deployment_environment(
        controller_root
    )
    launch_dir = Path.home() / "Library/LaunchAgents"
    log_dir = Path.home() / "Library/Logs/LifeOS"
    log_dir.mkdir(parents=True, exist_ok=True)
    python = sys.executable
    common_env = {
        "PATH": environment["PATH"],
        "LIFEOS_ROOT": str(lifeos_root),
        "LIFEOS_CONTROLLER_ROOT": str(controller_root),
        "LIFEOS_CODEX_HOME": str(Path.home() / ".codex"),
        "LIFEOS_SERVER_URL": BACKEND_URL,
        "LIFEOS_WORKBENCH_DB": str(database),
        "LIFEOS_AUTOMATION_TOKEN_FILE": str(AUTOMATION_TOKEN_FILE),
        **deployment_environment,
    }
    plists = {
        LAUNCH_LABEL: {
            "Label": LAUNCH_LABEL,
            "ProgramArguments": [
                python,
                str(Path(__file__).resolve()),
                "start",
                "--no-open",
                "--no-build",
                "--board-only",
            ],
            "RunAtLoad": True,
            "ThrottleInterval": 60,
            "EnvironmentVariables": common_env,
            "StandardOutPath": str(log_dir / "workbench.log"),
            "StandardErrorPath": str(log_dir / "workbench-error.log"),
        },
        INDEX_LABEL: {
            "Label": INDEX_LABEL,
            "ProgramArguments": [
                python,
                str(Path(__file__).resolve()),
                "--lifeos-root",
                str(lifeos_root),
                "--controller-root",
                str(controller_root),
                "background-sync",
                "--summary-limit",
                "5",
                "--triage-limit",
                "20",
                "--max-summaries",
                "25",
            ],
            "RunAtLoad": True,
            "StartInterval": INDEX_SYNC_INTERVAL_SECONDS,
            "ThrottleInterval": 60,
            "EnvironmentVariables": common_env,
            "StandardOutPath": str(log_dir / "index.log"),
            "StandardErrorPath": str(log_dir / "index-error.log"),
        },
        BACKUP_LABEL: {
            "Label": BACKUP_LABEL,
            "ProgramArguments": [python, str(Path(__file__).resolve()), "backup"],
            "StartCalendarInterval": {"Hour": 3, "Minute": 15},
            "ThrottleInterval": 300,
            "EnvironmentVariables": common_env,
            "StandardOutPath": str(log_dir / "backup.log"),
            "StandardErrorPath": str(log_dir / "backup-error.log"),
        },
        CHATGPT_WAKE_LABEL: {
            "Label": CHATGPT_WAKE_LABEL,
            "ProgramArguments": ["/usr/bin/open", "-gj", "-a", "ChatGPT"],
            "RunAtLoad": True,
            "ThrottleInterval": 300,
            "EnvironmentVariables": {"PATH": environment["PATH"]},
            "StandardOutPath": str(log_dir / "chatgpt-wake.log"),
            "StandardErrorPath": str(log_dir / "chatgpt-wake-error.log"),
        },
    }
    launchctl = _find_executable("launchctl", environment)
    domain = "gui/%s" % os.getuid()
    for label, payload in plists.items():
        path = launch_dir / (label + ".plist")
        _run([launchctl, "bootout", domain + "/" + label], environment=environment, check=False, capture=True)
        _write_plist(path, payload)
        _run([launchctl, "bootstrap", domain, str(path)], environment=environment)
    print(
        "已启用：登录后自动启动看板并唤醒 Codex，本机后台管家恢复 AI 执行器，"
        "每 2 小时静默同步 LifeOS、每日本地备份。"
    )


def uninstall_autostart(lifeos_root: Path, controller_root: Path) -> None:
    environment = runtime_environment(lifeos_root, controller_root)
    launchctl = _find_executable("launchctl", environment)
    domain = "gui/%s" % os.getuid()
    launch_dir = Path.home() / "Library/LaunchAgents"
    for label in (LAUNCH_LABEL, INDEX_LABEL, BACKUP_LABEL, CHATGPT_WAKE_LABEL):
        _run([launchctl, "bootout", domain + "/" + label], environment=environment, check=False, capture=True)
        path = launch_dir / (label + ".plist")
        if path.exists():
            path.unlink()
    print("LifeOS 工作台自动启动与定时任务已移除；工作台数据未删除。")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="LifeOS 工作台本机运行入口")
    parser.add_argument("--lifeos-root", type=Path)
    parser.add_argument("--controller-root", type=Path)
    commands = parser.add_subparsers(dest="command", required=True)
    start_parser = commands.add_parser("start", help="启动并打开工作台")
    start_parser.add_argument("--no-build", action="store_true")
    start_parser.add_argument("--no-open", action="store_true")
    start_parser.add_argument(
        "--board-only",
        action="store_true",
        help="只启动看板与索引；AI 执行器由本机后台管家按需恢复",
    )
    commands.add_parser("stop", help="停止工作台但保留数据")
    commands.add_parser("status", help="查看本地服务状态")
    commands.add_parser("backup", help="创建受限本地备份")
    commands.add_parser("doctor", help="检查完整执行闭环")
    commands.add_parser("ensure", help="幂等恢复看板、AI 执行器和索引")
    background = commands.add_parser(
        "background-sync",
        help="不创建 Codex 任务的两小时静默管家同步",
    )
    background.add_argument("--summary-limit", type=int, default=5)
    background.add_argument("--triage-limit", type=int, default=20)
    background.add_argument("--max-summaries", type=int, default=25)
    commands.add_parser("install-autostart", help="启用自动启动、索引和备份")
    commands.add_parser("uninstall-autostart", help="移除自动任务但保留数据")
    reset_parser = commands.add_parser("reset-login", help="通过本机受限文件重置唯一登录凭据")
    reset_parser.add_argument("--username", required=True)
    reset_parser.add_argument("--password-file", type=Path, required=True)
    tunnel_parser = commands.add_parser(
        "install-tunnel", help="启用 Cloudflare Tunnel 公网入口"
    )
    tunnel_parser.add_argument("--token-file", type=Path, required=True)
    tunnel_parser.add_argument(
        "--public-origin",
        required=True,
        help="HTTPS 公网 origin，例如 https://lifeos.example.com",
    )
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        lifeos_root = resolve_lifeos_root(args.lifeos_root)
        controller_root = resolve_controller_root(args.controller_root)
        if args.command == "start":
            start(
                lifeos_root,
                controller_root,
                build=not args.no_build,
                open_browser=not args.no_open,
                start_executor=not args.board_only,
            )
            return 0
        if args.command == "stop":
            stop(lifeos_root, controller_root)
            return 0
        if args.command == "status":
            return status(lifeos_root, controller_root)
        if args.command == "backup":
            backup(lifeos_root, controller_root)
            return 0
        if args.command == "doctor":
            return doctor(lifeos_root, controller_root)
        if args.command == "ensure":
            ensure_running(lifeos_root, controller_root)
            return 0
        if args.command == "background-sync":
            background_sync(
                lifeos_root,
                controller_root,
                summary_limit=args.summary_limit,
                triage_limit=args.triage_limit,
                max_summaries=args.max_summaries,
            )
            return 0
        if args.command == "install-autostart":
            install_autostart(lifeos_root, controller_root)
            return 0
        if args.command == "uninstall-autostart":
            uninstall_autostart(lifeos_root, controller_root)
            return 0
        if args.command == "reset-login":
            reset_login(
                lifeos_root,
                controller_root,
                username=args.username,
                password_file=args.password_file,
            )
            return 0
        if args.command == "install-tunnel":
            install_cloudflare_tunnel(
                lifeos_root,
                controller_root,
                token_file=args.token_file,
                public_origin=args.public_origin,
            )
            return 0
        raise WorkbenchError("未知命令")
    except (WorkbenchError, OSError, subprocess.SubprocessError, sqlite3.Error) as error:
        print("LifeOS 工作台操作失败：%s" % error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
