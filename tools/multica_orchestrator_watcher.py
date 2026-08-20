#!/usr/bin/env python3
"""Poll Multica issue runs and write orchestrator inbox items on completion."""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any

DEFAULT_PROJECT_ID = "072b1862-109e-43c3-98d8-c18515961b93"
DEFAULT_STATE_PATH = Path("/Volumes/itsmefelix SSD/appdata/multica/orchestrator-state/watcher_state.json")
DEFAULT_INBOX_DIR = Path("/Volumes/itsmefelix SSD/appdata/multica/orchestrator-inbox")
FALLBACK_MULTICA = "/opt/homebrew/bin/multica"
# The Multica CLI exposes issue-list --limit but not --offset; use a high,
# explicit bound so the watcher does not inherit the CLI default page window.
DEFAULT_ISSUE_SCAN_LIMIT = 1000

TERMINAL_STATUSES = {"completed", "failed", "cancelled", "canceled"}
ACTIVE_STATUSES = {"queued", "running", "dispatched", "started"}


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def default_multica() -> str:
    return os.environ.get("MULTICA_BIN") or shutil.which("multica") or FALLBACK_MULTICA


def run_json(command: list[str], *, timeout: int = 30) -> Any:
    completed = subprocess.run(command, capture_output=True, text=True, timeout=timeout, check=False)
    if completed.returncode != 0:
        raise RuntimeError(
            f"Command failed ({completed.returncode}): {' '.join(command)}\n"
            f"stdout={completed.stdout[-2000:]}\nstderr={completed.stderr[-2000:]}"
        )
    try:
        return json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Command did not return JSON: {' '.join(command)}\n{completed.stdout[-2000:]}") from exc


def load_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"notified_runs": {}, "active_runs": {}, "last_checked_at": None}
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {"notified_runs": {}, "active_runs": {}, "last_checked_at": None}
    if not isinstance(payload, dict):
        return {"notified_runs": {}, "active_runs": {}, "last_checked_at": None}
    payload.setdefault("notified_runs", {})
    payload.setdefault("active_runs", {})
    payload.setdefault("last_checked_at", None)
    return payload


def write_state(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    tmp.replace(path)


def slug(value: str) -> str:
    lowered = value.strip().lower()
    lowered = re.sub(r"[^a-z0-9._-]+", "-", lowered)
    return lowered.strip("-") or "run"


def issue_map(multica: str, project_id: str, issue_scan_limit: int) -> dict[str, dict[str, Any]]:
    issues = run_json(
        [
            multica,
            "issue",
            "list",
            "--project",
            project_id,
            "--limit",
            str(issue_scan_limit),
            "--output",
            "json",
        ]
    )
    result: dict[str, dict[str, Any]] = {}
    if not isinstance(issues, list):
        return result
    for issue in issues:
        if not isinstance(issue, dict):
            continue
        if issue.get("project_id") != project_id:
            continue
        issue_id = str(issue.get("id") or "")
        if issue_id:
            result[issue_id] = issue
    return result


def issue_runs(multica: str, issue_id: str) -> list[dict[str, Any]]:
    runs = run_json([multica, "issue", "runs", issue_id, "--output", "json"])
    if not isinstance(runs, list):
        return []
    return [run for run in runs if isinstance(run, dict)]


def run_messages(multica: str, run_id: str) -> list[dict[str, Any]]:
    messages = run_json([multica, "issue", "run-messages", run_id, "--output", "json"], timeout=45)
    if not isinstance(messages, list):
        return []
    return [message for message in messages if isinstance(message, dict)]


def has_result_output(run: dict[str, Any]) -> bool:
    result = run.get("result")
    return isinstance(result, dict) and isinstance(result.get("output"), str) and bool(result["output"].strip())


def concise_result(run: dict[str, Any], messages: list[dict[str, Any]]) -> str:
    result = run.get("result")
    if has_result_output(run):
        return result["output"].strip()
    text_messages = [m.get("content") for m in messages if isinstance(m.get("content"), str) and m["content"].strip()]
    if text_messages:
        return str(text_messages[-1]).strip()
    output_messages = [m.get("output") for m in messages if isinstance(m.get("output"), str) and m["output"].strip()]
    if output_messages:
        return str(output_messages[-1]).strip()
    error = run.get("error")
    if error:
        return str(error)
    return "No result text available."


def write_inbox_item(
    inbox_dir: Path,
    *,
    issue: dict[str, Any],
    run: dict[str, Any],
    messages: list[dict[str, Any]],
) -> Path:
    inbox_dir.mkdir(parents=True, exist_ok=True)
    identifier = str(issue.get("identifier") or issue.get("number") or issue.get("id") or "issue")
    title = str(issue.get("title") or "Untitled issue")
    status = str(run.get("status") or "unknown")
    run_id = str(run.get("id") or "unknown-run")
    created = utc_now()
    filename = f"{created.replace(':', '').replace('-', '')}-{slug(identifier)}-{slug(status)}-{slug(run_id[:8])}.md"
    path = inbox_dir / filename
    output = concise_result(run, messages)
    result = run.get("result") if isinstance(run.get("result"), dict) else {}
    workdir = str(result.get("work_dir") or "") if isinstance(result, dict) else ""
    pr_url = str(result.get("pr_url") or "") if isinstance(result, dict) else ""

    body = [
        f"# Multica Run {status}: {identifier}",
        "",
        f"- Checked at: `{created}`",
        f"- Issue: `{identifier}`",
        f"- Issue ID: `{issue.get('id', '')}`",
        f"- Title: {title}",
        f"- Run ID: `{run_id}`",
        f"- Run status: `{status}`",
        f"- Agent ID: `{run.get('agent_id', '')}`",
        f"- Runtime ID: `{run.get('runtime_id', '')}`",
        f"- Started at: `{run.get('started_at') or ''}`",
        f"- Completed at: `{run.get('completed_at') or ''}`",
    ]
    if workdir:
        body.append(f"- Workdir: `{workdir}`")
    if pr_url:
        body.append(f"- PR: {pr_url}")
    if run.get("error"):
        body.append(f"- Error: `{run.get('error')}`")
    body.extend(["", "## Result", "", output, ""])
    path.write_text("\n".join(body), encoding="utf-8")
    return path


def notify(title: str, message: str) -> None:
    escaped_title = title.replace('"', '\\"')
    escaped_message = message.replace('"', '\\"')
    subprocess.run(
        [
            "/usr/bin/osascript",
            "-e",
            f'display notification "{escaped_message}" with title "{escaped_title}"',
        ],
        capture_output=True,
        text=True,
        check=False,
    )


def check_once(args: argparse.Namespace) -> int:
    state = load_state(args.state_path)
    notified = dict(state.get("notified_runs") or {})
    active = dict(state.get("active_runs") or {})
    issues = issue_map(args.multica, args.project_id, args.issue_scan_limit)
    events: list[Path] = []

    for issue_id, issue in issues.items():
        for run in issue_runs(args.multica, issue_id):
            run_id = str(run.get("id") or "")
            status = str(run.get("status") or "unknown")
            if not run_id:
                continue
            if status in ACTIVE_STATUSES:
                active[run_id] = {
                    "issue_id": issue_id,
                    "identifier": issue.get("identifier"),
                    "title": issue.get("title"),
                    "status": status,
                    "seen_at": utc_now(),
                }
                continue
            if status not in TERMINAL_STATUSES:
                continue
            if notified.get(run_id) == status:
                active.pop(run_id, None)
                continue
            try:
                messages = run_messages(args.multica, run_id)
            except Exception as exc:
                print(f"Warning: failed to fetch messages for run {run_id}: {exc}", file=sys.stderr)
                if not has_result_output(run):
                    continue
                messages = []
            path = write_inbox_item(args.inbox_dir, issue=issue, run=run, messages=messages)
            events.append(path)
            notified[run_id] = status
            active.pop(run_id, None)
            if not args.no_notify:
                identifier = str(issue.get("identifier") or issue.get("number") or "issue")
                notify("Multica run finished", f"{identifier} {status}; inbox item written")

    state["notified_runs"] = notified
    state["active_runs"] = active
    state["last_checked_at"] = utc_now()
    write_state(args.state_path, state)
    for event in events:
        print(event)
    if not events:
        print("No new completed Multica runs.")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project-id", default=DEFAULT_PROJECT_ID)
    parser.add_argument("--state-path", type=Path, default=DEFAULT_STATE_PATH)
    parser.add_argument("--inbox-dir", type=Path, default=DEFAULT_INBOX_DIR)
    parser.add_argument("--multica", "--cli-path", dest="multica", default=default_multica())
    parser.add_argument(
        "--issue-scan-limit",
        type=int,
        default=DEFAULT_ISSUE_SCAN_LIMIT,
        help="Explicit issue-list scan limit; the Multica CLI has no offset flag.",
    )
    parser.add_argument("--no-notify", action="store_true")
    parser.add_argument("--once", action="store_true", help="Run one polling pass and exit; this is the default behavior.")
    args = parser.parse_args(argv)
    try:
        return check_once(args)
    except Exception as exc:
        args.inbox_dir.mkdir(parents=True, exist_ok=True)
        error_path = args.inbox_dir / f"{utc_now().replace(':', '').replace('-', '')}-watcher-error.md"
        error_path.write_text(f"# Multica Watcher Error\n\n`{utc_now()}`\n\n```\n{exc}\n```\n", encoding="utf-8")
        print(f"Watcher error: {exc}", file=sys.stderr)
        print(error_path)
        return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
