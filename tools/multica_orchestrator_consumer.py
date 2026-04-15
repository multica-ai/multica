#!/usr/bin/env python3
"""Consume Multica watcher bridge events and apply bounded board transitions."""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any

DEFAULT_EVENT_DIR = Path.home() / ".local/share/multica-orchestrator/events"
DEFAULT_STATE_PATH = Path.home() / ".local/state/multica-orchestrator/orchestrator_consumer_state.json"
FALLBACK_MULTICA = "/opt/homebrew/bin/multica"
MAX_COMMENT_CHARS = 1800
MAX_ISSUE_SCAN_LIMIT = 1000
TERMINAL_ISSUE_STATUSES = {"done", "cancelled", "canceled"}
ACTIVE_WORK_STATUSES = {"todo", "in_progress", "in_review", "blocked", "human_review"}
TERMINAL_RUN_STATUSES = {"completed", "failed", "cancelled", "canceled"}
WORKFLOW_ANALYST_AGENT_ID = "c7302a77-3117-4af0-99d6-2a7ee7cf34fc"
WORKFLOW_ANALYST_AGENT_NAME = "Multica Workflow Analyst Agent"
WORKFLOW_ANALYST_TITLE_PREFIX = "Workflow Analyst audit"
WORKFLOW_ANALYST_TERMINAL_EVENT_THRESHOLD = 3
WORKFLOW_ANALYST_CADENCE = dt.timedelta(hours=24)
WORKFLOW_ANALYST_STALE_THRESHOLD = dt.timedelta(hours=24)
OUTCOME_MARKER_TITLE = "orchestrator outcome:"
OUTCOME_STATUSES = {"clean", "blocked", "human_review", "ambiguous"}
OUTCOME_NEXT_STAGES = {"in_review", "done", "blocked", "human_review", "no_op"}
OUTCOME_ALLOWED_NEXT_STAGES = {
    "clean": {"in_review", "done"},
    "blocked": {"blocked"},
    "human_review": {"human_review"},
    "ambiguous": {"no_op"},
}
NO_HUMAN_DECISION_VALUES = {"", "n/a", "na", "no", "none", "none.", "not needed"}
ADMISSION_MANUAL_USER_REQUESTED = "manual_user_requested"
ADMISSION_PROOF_RECORDING = "proof_recording"
ADMISSION_WATCHER_STATUS_SYNC = "watcher_status_sync"
ADMISSION_AUTONOMOUS_PRODUCT_PLANNING_DENIED = "autonomous_product_planning_denied"
ADMISSION_AUTONOMOUS_WORKFLOW_ANALYSIS_DENIED = "autonomous_workflow_analysis_denied"
ADMISSION_CHILD_CARD_CREATION_DENIED = "child_card_creation_denied"
ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED = "human_review_missing_ask_denied"
ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED = "autonomous_agent_launch_denied"

TRAINER_PROJECT_ID = "072b1862-109e-43c3-98d8-c18515961b93"
MULTICA_PROJECT_ID = "d02043a2-5fa2-463e-803d-a3e38133553a"

TRAINER_BUILD_AGENT_ID = "ae1ad471-7be3-488d-b2a5-9d14c2803d32"
TRAINER_REVIEW_AGENT_ID = "8d80b525-288f-4efe-b53d-5e3d4348d0e8"
TRAINER_PRODUCT_AGENT_ID = "d8344c33-897c-40bd-9053-5d95d085e59f"
TRAINER_AUDIT_AGENT_ID = "b8827733-f155-4fa1-8bc4-28df481b4c01"
TRAINER_VALIDATE_AGENT_ID = "c3c5a2d5-633d-494b-b41c-b024470212be"
TRAINER_SHIP_AGENT_ID = "306665c7-8b8f-47b6-a18b-f9f3d46e3682"

MULTICA_BUILD_AGENT_ID = "9b77e498-e6f2-4c96-8236-e245f7190f35"
MULTICA_REVIEW_AGENT_ID = "f482a295-1be7-4cf0-aa60-1f8e2737885d"
MULTICA_OPS_AGENT_ID = "910339ce-c499-4c1c-ac12-5f44936afba0"
MULTICA_ORCHESTRATOR_AGENT_ID = "9f5006bf-a505-4e87-8c28-4136c24bbe03"

BUILD_AGENT_IDS = {TRAINER_BUILD_AGENT_ID, MULTICA_BUILD_AGENT_ID}
REVIEW_AGENT_IDS = {TRAINER_REVIEW_AGENT_ID, MULTICA_REVIEW_AGENT_ID}
PRODUCT_AGENT_IDS = {TRAINER_PRODUCT_AGENT_ID}
AUDIT_AGENT_IDS = {TRAINER_AUDIT_AGENT_ID}
VALIDATION_AGENT_IDS = {TRAINER_VALIDATE_AGENT_ID}
SHIP_AGENT_IDS = {TRAINER_SHIP_AGENT_ID}

BUILD_AGENT_NAMES = {
    "trainer": "Trainer Build Agent",
    "multica": "Multica Build Agent",
}

REVIEW_AGENT_NAMES = {
    "trainer": "Trainer Review Agent",
    "multica": "Multica Review Agent",
}

PRODUCT_AGENT_NAMES = {
    "trainer": "Trainer Product Agent",
}

AUDIT_AGENT_NAMES = {
    "trainer": "Trainer Audit Agent",
}

VALIDATION_AGENT_NAMES = {
    "trainer": "Trainer Validate Agent",
    "multica": "Multica Ops Agent",
}

SHIP_AGENT_NAMES = {
    "trainer": "Trainer Ship Ops Agent",
    "multica": "Multica Ops Agent",
}

AGENT_IDS_BY_NAME = {
    "Trainer Build Agent": TRAINER_BUILD_AGENT_ID,
    "Trainer Review Agent": TRAINER_REVIEW_AGENT_ID,
    "Trainer Product Agent": TRAINER_PRODUCT_AGENT_ID,
    "Trainer Audit Agent": TRAINER_AUDIT_AGENT_ID,
    "Trainer Validate Agent": TRAINER_VALIDATE_AGENT_ID,
    "Trainer Ship Ops Agent": TRAINER_SHIP_AGENT_ID,
    "Multica Build Agent": MULTICA_BUILD_AGENT_ID,
    "Multica Review Agent": MULTICA_REVIEW_AGENT_ID,
    "Multica Ops Agent": MULTICA_OPS_AGENT_ID,
    "Multica Orchestrator Agent": MULTICA_ORCHESTRATOR_AGENT_ID,
    WORKFLOW_ANALYST_AGENT_NAME: WORKFLOW_ANALYST_AGENT_ID,
}


def utc_now_dt() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0)


def utc_now() -> str:
    return utc_now_dt().isoformat().replace("+00:00", "Z")


def parse_timestamp(value: Any) -> dt.datetime | None:
    if not isinstance(value, str) or not value.strip():
        return None
    normalized = value.strip()
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    try:
        parsed = dt.datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc)


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


def run_command(command: list[str], *, timeout: int = 30) -> None:
    completed = subprocess.run(command, capture_output=True, text=True, timeout=timeout, check=False)
    if completed.returncode != 0:
        raise RuntimeError(
            f"Command failed ({completed.returncode}): {' '.join(command)}\n"
            f"stdout={completed.stdout[-2000:]}\nstderr={completed.stderr[-2000:]}"
        )


def has_explicit_manual_allow(event: dict[str, Any]) -> bool:
    for key in ("manual_user_requested", "operator_requested", "human_opt_in_id", "allow_autonomous_llm"):
        value = event.get(key)
        if isinstance(value, bool) and value:
            return True
        if isinstance(value, str) and value.strip():
            return True
    return False


def denied_decision(reason: str, detail: str) -> dict[str, Any]:
    return {
        "action": "noop",
        "target_status": None,
        "assignee": None,
        "reason": f"{reason}: {detail}",
        "admission_reason": reason,
    }


def valid_human_review_ask(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    ask = value.strip()
    if not ask or ask.lower() in NO_HUMAN_DECISION_VALUES:
        return False
    normalized = normalize_text(ask).strip(" .")
    invalid_exact = {
        "please review",
        "review this",
        "need product input",
        "decide next steps",
        "human gate required",
        "human review required",
        "needs human review",
        "requires human review",
    }
    if normalized in invalid_exact or len(normalized) < 12:
        return False
    decision_terms = (
        "approve",
        "approval",
        "choose",
        "decide",
        "select",
        "confirm",
        "authorize",
        "accept",
        "reject",
        "defer",
        "which",
        "whether",
        "should ",
    )
    return ask.endswith("?") or any(term in normalized for term in decision_terms)


def extract_human_review_ask(terminal_text: str) -> str:
    patterns = [
        r"human[_ -]?decision[_ -]?needed\s*:\s*(.+)",
        r"decision needed\s*:\s*(.+)",
        r"human ask\s*:\s*(.+)",
        r"ask\s*:\s*(.+)",
    ]
    for pattern in patterns:
        match = re.search(pattern, terminal_text, flags=re.IGNORECASE)
        if match:
            return match.group(1).strip().strip("`'\"")
    return ""


def apply_admission_control(issue: dict[str, Any], event: dict[str, Any], decision: dict[str, Any]) -> dict[str, Any]:
    if has_explicit_manual_allow(event):
        decision.setdefault("admission_reason", ADMISSION_MANUAL_USER_REQUESTED)
        return decision

    action = str(decision.get("action") or "")
    assignee = str(decision.get("assignee") or "")
    target_status = str(decision.get("target_status") or "")

    if action in {"create_issue", "ensure_issue"}:
        role = normalize_text(str(decision.get("handoff_role") or decision.get("followup_kind") or ""))
        if "product" in role:
            return denied_decision(
                ADMISSION_AUTONOMOUS_PRODUCT_PLANNING_DENIED,
                "Product Agent follow-up creation requires an explicit manual operator request.",
            )
        return denied_decision(
            ADMISSION_CHILD_CARD_CREATION_DENIED,
            "Autonomous child-card or follow-up-card creation is disabled in cockpit mode.",
        )

    if target_status == "human_review":
        ask = str(decision.get("human_decision_needed") or extract_human_review_ask(load_terminal_text(event)) or "")
        if not valid_human_review_ask(ask):
            return denied_decision(
                ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED,
                "Human Review transition requires one concrete decision ask, not a generic review request.",
            )
        decision["human_decision_needed"] = ask
        decision.setdefault("admission_reason", ADMISSION_WATCHER_STATUS_SYNC)
        return decision

    if action == "status_assign" and target_status:
        return {
            "action": "status",
            "target_status": target_status,
            "assignee": None,
            "reason": (
                f"{decision.get('reason')}; {ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED}: "
                f"automatic assignment to `{assignee}` suppressed in cockpit mode"
            ),
            "admission_reason": ADMISSION_WATCHER_STATUS_SYNC,
        }

    if action == "assign":
        return denied_decision(
            ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED,
            f"Automatic assignment to `{assignee}` is disabled in cockpit mode.",
        )

    if action in {"status", "noop", "update_issue"}:
        decision.setdefault("admission_reason", ADMISSION_WATCHER_STATUS_SYNC if action == "status" else ADMISSION_PROOF_RECORDING)
    return decision


def load_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"handled_events": {}, "workflow_analyst_audits": {}, "last_checked_at": None}
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {"handled_events": {}, "workflow_analyst_audits": {}, "last_checked_at": None}
    if not isinstance(payload, dict):
        return {"handled_events": {}, "workflow_analyst_audits": {}, "last_checked_at": None}
    payload.setdefault("handled_events", {})
    payload.setdefault("workflow_analyst_audits", {})
    payload.setdefault("last_checked_at", None)
    return payload


def write_state(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    tmp.replace(path)


def load_events(event_dir: Path) -> list[tuple[Path, dict[str, Any]]]:
    if not event_dir.exists():
        return []
    events: list[tuple[Path, dict[str, Any]]] = []
    for path in sorted(event_dir.glob("*.json")):
        try:
            event = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            print(f"Warning: failed to parse bridge event {path}: {exc}", file=sys.stderr)
            continue
        if isinstance(event, dict):
            events.append((path, event))
    return events


def event_order_key(item: tuple[Path, dict[str, Any]]) -> tuple[str, str]:
    path, event = item
    return (str(event.get("created_at") or ""), path.name)


def event_key(event: dict[str, Any], path: Path) -> str:
    key = str(event.get("idempotency_key") or "").strip()
    if key:
        return key
    run_id = str(event.get("run_id") or "").strip()
    status = str(event.get("terminal_status") or "").strip()
    if run_id and status:
        return f"{run_id}:{status}"
    return path.stem


def event_reference_timestamp(event: dict[str, Any]) -> dt.datetime | None:
    for key in ("completed_at", "run_completed_at", "terminal_at", "created_at"):
        timestamp = parse_timestamp(event.get(key))
        if timestamp is not None:
            return timestamp
    return None


def stale_event_decision(issue: dict[str, Any], event: dict[str, Any]) -> dict[str, Any] | None:
    issue_updated_at = parse_timestamp(issue.get("updated_at"))
    event_at = event_reference_timestamp(event)
    if issue_updated_at is None or event_at is None or issue_updated_at <= event_at:
        return None
    issue_updated_text = issue_updated_at.isoformat().replace("+00:00", "Z")
    event_text = event_at.isoformat().replace("+00:00", "Z")
    return {
        "action": "noop",
        "target_status": None,
        "assignee": None,
        "reason": (
            "stale bridge event skipped because the issue was updated after this run event "
            f"(`issue.updated_at` {issue_updated_text} > event timestamp {event_text})"
        ),
    }


def issue_get(multica: str, issue_id: str) -> dict[str, Any]:
    issue = run_json([multica, "issue", "get", issue_id, "--output", "json"])
    if not isinstance(issue, dict):
        raise RuntimeError(f"issue get returned non-object for {issue_id}")
    return issue


def issue_status(multica: str, issue_id: str, status: str) -> None:
    run_command([multica, "issue", "status", issue_id, status])


def issue_assign(multica: str, issue_id: str, assignee: str) -> None:
    run_command([multica, "issue", "assign", issue_id, "--to", assignee])


def issue_comment(multica: str, issue_id: str, body: str) -> None:
    run_command([multica, "issue", "comment", "add", issue_id, "--content", body[:MAX_COMMENT_CHARS]])


def issue_create(
    multica: str,
    *,
    title: str,
    description: str,
    assignee: str,
    parent_id: str | None,
    project_id: str | None = None,
) -> None:
    command = [
        multica,
        "issue",
        "create",
        "--title",
        title,
        "--description",
        description,
        "--assignee",
        assignee,
        "--status",
        "todo",
    ]
    if project_id:
        command.extend(["--project", project_id])
    if parent_id:
        command.extend(["--parent", parent_id])
    run_command(command)


def issue_update(multica: str, issue_id: str, *, title: str, description: str, assignee: str) -> None:
    run_command(
        [
            multica,
            "issue",
            "update",
            issue_id,
            "--title",
            title,
            "--description",
            description,
            "--assignee",
            assignee,
            "--output",
            "json",
        ]
    )


def issue_list(multica: str, *, project_id: str | None) -> list[dict[str, Any]]:
    command = [multica, "issue", "list", "--limit", str(MAX_ISSUE_SCAN_LIMIT), "--output", "json"]
    if project_id:
        command[3:3] = ["--project", project_id]
    issues = run_json(command)
    if not isinstance(issues, list):
        return []
    return [issue for issue in issues if isinstance(issue, dict)]


def format_utc(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def current_time(args: argparse.Namespace) -> dt.datetime:
    configured = getattr(args, "now", None)
    parsed = parse_timestamp(configured)
    return parsed if parsed is not None else utc_now_dt()


def workflow_sprint_window(now: dt.datetime) -> tuple[str, dt.datetime, dt.datetime]:
    now = now.astimezone(dt.timezone.utc)
    start = now - dt.timedelta(days=now.weekday())
    start = start.replace(hour=0, minute=0, second=0, microsecond=0)
    end = start + dt.timedelta(days=7)
    iso_year, iso_week, _ = now.isocalendar()
    return f"{iso_year}-W{iso_week:02d}", start, end


def audit_digest(key: str) -> str:
    return hashlib.sha256(key.encode("utf-8")).hexdigest()[:12]


def workflow_audit_title(reason: str, key: str) -> str:
    label = reason.replace("_", " ")
    return f"{WORKFLOW_ANALYST_TITLE_PREFIX}: {label} [audit:{audit_digest(key)}]"


def is_workflow_analyst_issue(issue: dict[str, Any]) -> bool:
    assignee_id = str(issue.get("assignee_id") or "")
    title = str(issue.get("title") or "")
    return assignee_id == WORKFLOW_ANALYST_AGENT_ID or title.startswith(WORKFLOW_ANALYST_TITLE_PREFIX)


def is_terminal_issue(issue: dict[str, Any]) -> bool:
    return normalize_text(str(issue.get("status") or "")) in TERMINAL_ISSUE_STATUSES


def is_active_work_issue(issue: dict[str, Any], *, project_id: str) -> bool:
    if str(issue.get("project_id") or "") != project_id:
        return False
    if is_workflow_analyst_issue(issue):
        return False
    return normalize_text(str(issue.get("status") or "")) in ACTIVE_WORK_STATUSES


def issue_activity_timestamp(issue: dict[str, Any]) -> dt.datetime | None:
    return parse_timestamp(issue.get("updated_at")) or parse_timestamp(issue.get("created_at"))


def latest_workflow_audit_at(audits: list[dict[str, Any]]) -> dt.datetime | None:
    timestamps = [timestamp for audit in audits if (timestamp := issue_activity_timestamp(audit)) is not None]
    return max(timestamps) if timestamps else None


def audit_exists_in_window(audits: list[dict[str, Any]], start: dt.datetime, end: dt.datetime) -> bool:
    for audit in audits:
        timestamp = parse_timestamp(audit.get("created_at")) or issue_activity_timestamp(audit)
        if timestamp is not None and start <= timestamp < end:
            return True
    return False


def active_workflow_audit(audits: list[dict[str, Any]]) -> dict[str, Any] | None:
    for audit in audits:
        if not is_terminal_issue(audit):
            return audit
    return None


def event_project_id(event: dict[str, Any], issues_by_id: dict[str, dict[str, Any]]) -> str:
    project_id = str(event.get("project_id") or "")
    if project_id:
        return project_id
    issue_id = str(event.get("issue_id") or "")
    issue = issues_by_id.get(issue_id)
    return str(issue.get("project_id") or "") if issue else ""


def terminal_events_since_audit(
    events: list[tuple[Path, dict[str, Any]]],
    handled: dict[str, Any],
    issues_by_id: dict[str, dict[str, Any]],
    *,
    project_id: str,
    since: dt.datetime | None,
) -> list[dict[str, Any]]:
    matched: list[dict[str, Any]] = []
    for path, event in events:
        if event_key(event, path) not in handled:
            continue
        status = normalize_text(str(event.get("terminal_status") or ""))
        if status not in TERMINAL_RUN_STATUSES:
            continue
        if event_project_id(event, issues_by_id) != project_id:
            continue
        event_at = event_reference_timestamp(event)
        if since is not None and event_at is not None and event_at <= since:
            continue
        matched.append(event)
    return matched


def repeated_blocked_or_reopen_issue(handled: dict[str, Any], issues_by_id: dict[str, dict[str, Any]], *, project_id: str, since: dt.datetime | None) -> tuple[str, str, int] | None:
    blocked_counts: dict[str, int] = {}
    reopen_counts: dict[str, int] = {}
    for payload in handled.values():
        if not isinstance(payload, dict):
            continue
        issue_id = str(payload.get("issue_id") or "")
        issue = issues_by_id.get(issue_id)
        if not issue or str(issue.get("project_id") or "") != project_id:
            continue
        handled_at = parse_timestamp(payload.get("handled_at"))
        if since is not None and handled_at is not None and handled_at <= since:
            continue
        target_status = normalize_text(str(payload.get("target_status") or ""))
        if target_status == "blocked":
            blocked_counts[issue_id] = blocked_counts.get(issue_id, 0) + 1
        if target_status in {"todo", "in_progress", "in_review"}:
            reopen_counts[issue_id] = reopen_counts.get(issue_id, 0) + 1
    for issue_id, count in sorted(blocked_counts.items()):
        if count >= 2:
            return issue_id, "blocked_twice", count
    for issue_id, count in sorted(reopen_counts.items()):
        if count >= 2:
            return issue_id, "reopened_twice", count
    return None


def stale_workflow_issue(issues: list[dict[str, Any]], *, project_id: str, now: dt.datetime) -> dict[str, Any] | None:
    cutoff = now - WORKFLOW_ANALYST_STALE_THRESHOLD
    candidates: list[tuple[dt.datetime, dict[str, Any]]] = []
    for issue in issues:
        if str(issue.get("project_id") or "") != project_id or is_workflow_analyst_issue(issue):
            continue
        status = normalize_text(str(issue.get("status") or ""))
        if status not in {"in_review", "blocked"}:
            continue
        timestamp = issue_activity_timestamp(issue)
        if timestamp is not None and timestamp <= cutoff:
            candidates.append((timestamp, issue))
    candidates.sort(key=lambda item: item[0])
    return candidates[0][1] if candidates else None


def workflow_audit_key(project_id: str, sprint_id: str, window: str, reason: str) -> str:
    return f"workflow-analyst:{project_id}:{sprint_id}:{window}:{reason}"


def workflow_audit_decision(
    *,
    issues: list[dict[str, Any]],
    events: list[tuple[Path, dict[str, Any]]],
    handled: dict[str, Any],
    state: dict[str, Any],
    now: dt.datetime,
    project_id: str,
) -> dict[str, Any] | None:
    sprint_id, sprint_start, sprint_end = workflow_sprint_window(now)
    audits = [issue for issue in issues if is_workflow_analyst_issue(issue) and str(issue.get("project_id") or "") == project_id]
    active_audit = active_workflow_audit(audits)
    if active_audit is not None:
        return {
            "action": "noop",
            "reason": f"Workflow Analyst audit is already active on `{active_audit.get('identifier') or active_audit.get('id')}`",
        }

    active_work = [issue for issue in issues if is_active_work_issue(issue, project_id=project_id)]
    issues_by_id = {str(issue.get("id")): issue for issue in issues if issue.get("id")}
    latest_audit = latest_workflow_audit_at(audits)
    audit_state = state.setdefault("workflow_analyst_audits", {})

    def create_decision(reason: str, window: str, detail: str) -> dict[str, Any] | None:
        if not state.get("manual_user_requested"):
            return denied_decision(
                ADMISSION_AUTONOMOUS_WORKFLOW_ANALYSIS_DENIED,
                f"Workflow Analyst `{reason}` audit creation is disabled in cockpit mode.",
            )
        key = workflow_audit_key(project_id, sprint_id, window, reason)
        title = workflow_audit_title(reason, key)
        if audit_state.get(key) or any(str(issue.get("title") or "") == title for issue in audits):
            return None
        return {
            "action": "create_workflow_audit",
            "project_id": project_id,
            "sprint_id": sprint_id,
            "window": window,
            "reason": reason,
            "detail": detail,
            "idempotency_key": key,
            "title": title,
            "assignee": WORKFLOW_ANALYST_AGENT_NAME,
        }

    if active_work and not audit_exists_in_window(audits, sprint_start, sprint_end):
        return create_decision(
            "sprint_baseline",
            f"{format_utc(sprint_start)}..{format_utc(sprint_end)}",
            "No Workflow Analyst audit exists in the current ISO-week sprint window while active Multica work exists.",
        )

    repeated = repeated_blocked_or_reopen_issue(handled, issues_by_id, project_id=project_id, since=latest_audit)
    if repeated is not None:
        issue_id, reason, count = repeated
        issue = issues_by_id[issue_id]
        identifier = str(issue.get("identifier") or issue_id)
        return create_decision(
            reason,
            f"{identifier}:{count}",
            f"`{identifier}` matched `{reason}` with {count} routed state changes since the last audit.",
        )

    terminal_events = terminal_events_since_audit(events, handled, issues_by_id, project_id=project_id, since=latest_audit)
    if len(terminal_events) >= WORKFLOW_ANALYST_TERMINAL_EVENT_THRESHOLD:
        latest_event_at = max((event_reference_timestamp(event) for event in terminal_events), default=None)
        window = format_utc(latest_event_at or now)
        return create_decision(
            "terminal_event_burst",
            window,
            f"{len(terminal_events)} terminal run events have completed since the last Workflow Analyst audit.",
        )

    stale = stale_workflow_issue(issues, project_id=project_id, now=now)
    if stale is not None:
        identifier = str(stale.get("identifier") or stale.get("id") or "issue")
        status = str(stale.get("status") or "unknown")
        return create_decision(
            f"stale_{status}",
            identifier,
            f"`{identifier}` has been `{status}` for at least {int(WORKFLOW_ANALYST_STALE_THRESHOLD.total_seconds() // 3600)} hours.",
        )

    if active_work and latest_audit is not None and now - latest_audit >= WORKFLOW_ANALYST_CADENCE:
        return create_decision(
            "mid_sprint_cadence",
            format_utc(now.replace(hour=0, minute=0, second=0, microsecond=0)),
            f"Active Multica work exists and the last Workflow Analyst audit was at `{format_utc(latest_audit)}`.",
        )

    if not active_work and audits and audit_exists_in_window(audits, sprint_start, sprint_end):
        return create_decision(
            "sprint_close",
            f"{format_utc(sprint_start)}..{format_utc(sprint_end)}",
            "No active Multica work remains in the current sprint window; run a read-only close audit before declaring the sprint complete.",
        )

    return None


def build_workflow_audit_description(decision: dict[str, Any], now: dt.datetime) -> str:
    return "\n".join(
        [
            "Workflow Analyst audit request:",
            f"- Trigger reason: {decision['reason']}",
            f"- Trigger detail: {decision['detail']}",
            f"- Idempotency key: {decision['idempotency_key']}",
            f"- Project: {decision['project_id']}",
            f"- Sprint window: {decision['sprint_id']} / {decision['window']}",
            f"- Created at: {format_utc(now)}",
            "- Role: read-only Multica workflow diagnostics analyst.",
            "- Scope: Inspect issue lists, assignees, statuses, parent/subissue relationships, recent run events, and local orchestrator health for this project/window.",
            "- Hard boundaries: Do not change issue status, assign or unassign agents, create/delete/archive/edit issues, cancel tasks, edit code, or run deploy/publish/OAuth/credential/production operations.",
            "- Human gates: Report human-gated work as human-gated with the exact missing decision; do not auto-advance it.",
            "- Acceptance: Produce a concise Workflow Health Report with bottlenecks, stale or inconsistent state, repeated failure patterns, misrouting, human gates, and bounded recommendations only.",
        ]
    )


def apply_workflow_audit_decision(
    multica: str,
    decision: dict[str, Any] | None,
    state: dict[str, Any],
    state_path: Path,
    now: dt.datetime,
    *,
    dry_run: bool,
) -> bool:
    if not decision:
        return False
    if decision.get("action") == "noop":
        state["last_checked_at"] = format_utc(now)
        if not dry_run:
            write_state(state_path, state)
        print(f"workflow_analyst: noop ({decision.get('reason')})")
        return True
    if decision.get("action") != "create_workflow_audit":
        return False
    if dry_run:
        print(f"DRY-RUN workflow audit: would create `{decision['title']}` for `{decision['assignee']}`")
        return True
    issue_create(
        multica,
        title=str(decision["title"]),
        description=build_workflow_audit_description(decision, now),
        assignee=str(decision["assignee"]),
        parent_id=None,
        project_id=str(decision["project_id"]),
    )
    audits = state.setdefault("workflow_analyst_audits", {})
    audits[str(decision["idempotency_key"])] = {
        "created_at": format_utc(now),
        "project_id": decision.get("project_id"),
        "sprint_id": decision.get("sprint_id"),
        "window": decision.get("window"),
        "reason": decision.get("reason"),
        "title": decision.get("title"),
    }
    state["last_checked_at"] = format_utc(now)
    write_state(state_path, state)
    print(f"{decision['idempotency_key']}: create_workflow_audit ({decision['detail']})")
    return True


def normalize_text(value: str) -> str:
    lowered = value.lower()
    lowered = lowered.replace("`", "")
    return re.sub(r"\s+", " ", lowered)


def is_negated_context(text: str, start: int) -> bool:
    prefix = text[max(0, start - 40) : start]
    return bool(re.search(r"(?:^|[\s.;:!?([{])(?:no|not|without|did not|do not|does not)\s+$", prefix))


def load_terminal_text(event: dict[str, Any]) -> str:
    parts: list[str] = []
    summary = event.get("summary")
    if isinstance(summary, str) and summary.strip():
        parts.append(summary.strip())
    inbox_path = event.get("inbox_markdown_path")
    if isinstance(inbox_path, str) and inbox_path:
        path = Path(inbox_path)
        if path.exists():
            try:
                text = path.read_text(encoding="utf-8")
            except OSError:
                text = ""
            if text:
                marker = "\n## Result\n"
                if marker in text:
                    text = text.split(marker, 1)[1]
                parts.append(text.strip())
    return "\n\n".join(parts)


def parse_orchestrator_outcome(terminal_text: str) -> dict[str, str] | None:
    lines = terminal_text.splitlines()
    start_index = None
    for index, line in enumerate(lines):
        if line.strip().lower() == OUTCOME_MARKER_TITLE:
            start_index = index + 1
            break
    if start_index is None:
        return None

    fields: dict[str, str] = {"_present": "true"}
    field_pattern = re.compile(r"^\s*[-*]\s*(status|next_stage|reason|human_decision_needed)\s*:\s*(.*)\s*$", re.IGNORECASE)
    for line in lines[start_index:]:
        match = field_pattern.match(line)
        if match:
            fields[match.group(1).lower()] = match.group(2).strip()

    missing = [key for key in ["status", "next_stage", "reason", "human_decision_needed"] if key not in fields]
    if missing:
        fields["_error"] = f"structured outcome marker is missing {', '.join(missing)}"
        return fields

    status = fields["status"].strip().lower()
    next_stage = fields["next_stage"].strip().lower()
    fields["status"] = status
    fields["next_stage"] = next_stage

    if status not in OUTCOME_STATUSES:
        fields["_error"] = f"structured outcome marker has unknown status `{status}`"
        return fields
    if next_stage not in OUTCOME_NEXT_STAGES:
        fields["_error"] = f"structured outcome marker has unknown next_stage `{next_stage}`"
        return fields
    if next_stage not in OUTCOME_ALLOWED_NEXT_STAGES[status]:
        fields["_error"] = f"structured outcome marker is contradictory: status `{status}` cannot route to `{next_stage}`"
        return fields

    human_decision = fields["human_decision_needed"].strip()
    has_human_decision = human_decision.lower() not in NO_HUMAN_DECISION_VALUES
    if status == "human_review" and not has_human_decision:
        fields["_error"] = "structured outcome marker routes to human_review without human_decision_needed"
        return fields
    if status != "human_review" and has_human_decision:
        fields["_error"] = f"structured outcome marker is contradictory: status `{status}` includes a human decision"
        return fields

    if not fields["reason"].strip():
        fields["_error"] = "structured outcome marker is missing a reason"
    return fields


def first_event_value(event: dict[str, Any], keys: list[str]) -> str:
    for key in keys:
        value = event.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def first_regex_value(patterns: list[str], text: str) -> str:
    for pattern in patterns:
        match = re.search(pattern, text, flags=re.IGNORECASE | re.MULTILINE)
        if match:
            return match.group(1).strip().strip("`'\".,);]")
    return ""


def extract_ship_refs(event: dict[str, Any], terminal_text: str) -> dict[str, str]:
    pr_url = first_event_value(event, ["pr_url", "pull_request_url"])
    if not pr_url:
        pr_url = first_regex_value([r"(https?://[^\s)`]+/pull/\d+)"], terminal_text)
    pr_number = first_event_value(event, ["pr_number", "pull_request_number"])
    if not pr_number:
        pr_number = first_regex_value([r"\b(?:pr|pull request)\s*#?(\d+)\b"], terminal_text)
    if not pr_number and pr_url:
        pr_number = first_regex_value([r"/pull/(\d+)\b"], pr_url)

    branch = first_event_value(event, ["branch", "branch_name", "head_branch", "source_branch"])
    if not branch:
        branch = first_regex_value(
            [
                r"\bbranch\s*[:=]\s*`?([A-Za-z0-9._/-]+)",
                r"\bbranch\s+`([^`]+)`",
                r"\bhead branch\s*[:=]\s*`?([A-Za-z0-9._/-]+)",
            ],
            terminal_text,
        )

    commit = first_event_value(event, ["commit", "commit_sha", "sha"])
    if not commit:
        commit = first_regex_value([r"\bcommit\s*[:=]?\s*`?([0-9a-f]{7,40})\b"], terminal_text)

    return {"pr_url": pr_url, "pr_number": pr_number, "branch": branch, "commit": commit}


def ship_reference_label(source_issue_id: str, refs: dict[str, str]) -> str:
    if refs.get("pr_number"):
        return f"PR #{refs['pr_number']}"
    if refs.get("pr_url"):
        return refs["pr_url"]
    if refs.get("branch"):
        return f"branch {refs['branch']}"
    if refs.get("commit"):
        return f"commit {refs['commit'][:12]}"
    return f"source {source_issue_id}"


def ship_followup_title(issue: dict[str, Any], event: dict[str, Any], terminal_text: str) -> str:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("number") or issue.get("id") or "issue")
    source_issue_id = str(event.get("issue_id") or issue.get("id") or identifier)
    refs = extract_ship_refs(event, terminal_text)
    return f"Ship Ops: {identifier} [{ship_reference_label(source_issue_id, refs)}]"


def matching_ship_followup(issues: list[dict[str, Any]], *, source_issue_id: str, title: str, legacy_title: str) -> dict[str, Any] | None:
    exact_active_matches: list[dict[str, Any]] = []
    exact_terminal_matches: list[dict[str, Any]] = []
    legacy_active_matches: list[dict[str, Any]] = []
    legacy_terminal_matches: list[dict[str, Any]] = []
    for candidate in issues:
        if str(candidate.get("parent_issue_id") or "") != source_issue_id:
            continue
        status = normalize_text(str(candidate.get("status") or ""))
        is_terminal = status in {"done", "cancelled", "canceled"}
        candidate_title = str(candidate.get("title") or "")
        if candidate_title == title:
            if is_terminal:
                exact_terminal_matches.append(candidate)
            else:
                exact_active_matches.append(candidate)
        elif candidate_title == legacy_title:
            if is_terminal:
                legacy_terminal_matches.append(candidate)
            else:
                legacy_active_matches.append(candidate)
    for matches in (exact_active_matches, exact_terminal_matches, legacy_active_matches, legacy_terminal_matches):
        if matches:
            return matches[0]
    return None


def relevant_lines(text: str, keywords: list[str], *, limit: int = 5) -> list[str]:
    lines: list[str] = []
    for raw_line in text.splitlines():
        line = raw_line.strip().lstrip("-* ")
        if not line:
            continue
        lowered = line.lower()
        if any(keyword in lowered for keyword in keywords):
            lines.append(line)
        if len(lines) >= limit:
            break
    return lines


def lane_for(issue: dict[str, Any], event: dict[str, Any]) -> str:
    project_id = str(issue.get("project_id") or event.get("project_id") or "")
    agent_id = str(event.get("agent_id") or "")
    trainer_agent_ids = (
        {
            TRAINER_BUILD_AGENT_ID,
            TRAINER_REVIEW_AGENT_ID,
            TRAINER_PRODUCT_AGENT_ID,
            TRAINER_AUDIT_AGENT_ID,
            TRAINER_VALIDATE_AGENT_ID,
            TRAINER_SHIP_AGENT_ID,
        }
    )
    if project_id == TRAINER_PROJECT_ID or agent_id in trainer_agent_ids:
        return "trainer"
    if project_id == MULTICA_PROJECT_ID or agent_id in {
        MULTICA_BUILD_AGENT_ID,
        MULTICA_REVIEW_AGENT_ID,
        MULTICA_OPS_AGENT_ID,
        MULTICA_ORCHESTRATOR_AGENT_ID,
    }:
        return "multica"
    title = normalize_text(str(issue.get("title") or event.get("issue_title") or ""))
    if "trainer" in title or "whoop" in title or "mcp" in title:
        return "trainer"
    return "multica"


def has_human_gate(text: str, issue: dict[str, Any], event: dict[str, Any]) -> bool:
    title = normalize_text(str(issue.get("title") or event.get("issue_title") or ""))
    agent_id = str(event.get("agent_id") or "")
    if "human gate: - required: yes" in text:
        return True
    gate_terms = [
        "human decision needed",
        "production decision",
        "not safe/available",
        "no active current-day",
        "awaiting human",
        "requires human review",
    ]
    if any(term in text for term in gate_terms):
        return True
    required_gate_patterns = [
        r"\b(?:production mutation|deploy|deployment|publish|publication) (?:is )?(?:required|needed|blocked|pending|must run)\b",
        r"\b(?:requires|needs|need|must run) (?:a )?(?:production mutation|deploy|deployment|publish|publication)\b",
        r"\b(?:operator|credential|credentials|oauth|token) (?:renewal|rotation) (?:is )?(?:required|needed|pending)\b",
        r"\b(?:credentials?|oauth|token) (?:need|needs|require|requires) (?:manual )?(?:renewal|rotation|update|approval)\b",
        r"\bmanual oauth (?:is )?(?:required|needed|pending)\b",
    ]
    for pattern in required_gate_patterns:
        for match in re.finditer(pattern, text):
            if not is_negated_context(text, match.start()):
                return True
    if (agent_id in SHIP_AGENT_IDS or any(term in title for term in ["ship", "deploy", "smoke"])) and any(
        term in text for term in ["caveat", "residual risk", "manual decision", "blocked by production"]
    ):
        return True
    return False


def has_ship_need(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "ship needed",
            "shipping needed",
            "ready to ship",
            "ready for ship",
            "deploy needed",
            "deployment needed",
            "ready to deploy",
            "ready for deploy",
            "publish needed",
            "ready to publish",
            "needs ship ops",
            "needs shipping",
            "needs deploy",
            "needs deployment",
            "needs publish",
        ]
    )


def has_product_need(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "product agent",
            "product decision",
            "product approval",
            "sprint planning",
            "planning needed",
            "scope decision",
            "acceptance criteria needed",
        ]
    )


def has_audit_need(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "audit agent",
            "audit needed",
            "root cause needed",
            "root-cause needed",
            "provenance audit",
            "source precedence",
        ]
    )


def has_validation_need(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "validation agent",
            "validation needed",
            "needs validation",
            "smoke needed",
            "needs smoke",
            "read-only smoke",
            "run smoke",
        ]
    )


def has_upstream_waiting(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "upstream waiting",
            "upstream-waiting",
            "upstream/maintainer wait",
            "upstream/maintainer handling",
            "waiting on upstream",
            "waiting on maintainer",
            "waiting for maintainer",
            "maintainer handling",
            "maintainer acceptance",
            "maintainer review",
            "upstream pr",
            "fork-local stacked pr",
        ]
    )


def has_dependency_block(text: str) -> bool:
    explicit_dependency_gate = any(
        phrase in text
        for phrase in [
            "dependency blocked",
            "blocked by dependency",
            "blocked on dependency",
            "blocked until",
            "keep this card blocked",
            "keep the card blocked",
            "keep this gate blocked",
            "keep the gate blocked",
            "stays blocked",
            "remains blocked",
        ]
    )
    if explicit_dependency_gate:
        return True
    if has_no_blockers(text):
        return False
    return any(phrase in text for phrase in ["active blocker", "current blocker"])


def has_no_blockers(text: str) -> bool:
    return any(
        phrase in text
        for phrase in [
            "no blocking findings",
            "no blocking review finding",
            "findings: none",
            "no blockers",
            "no code-level blocker",
            "no blocking finding",
        ]
    )


def has_blockers(text: str) -> bool:
    strong_blocker_patterns = [
        r"(?:^|\bfindings\W+)blocking:",
        r"(?:^|\bfindings\W+)blocking finding:",
        r"(?:^|\bfindings\W+)blocking review finding:",
        r"(?:^|\bfindings\W+)blocker:",
    ]
    if any(re.search(pattern, text) for pattern in strong_blocker_patterns):
        return True
    if has_no_blockers(text):
        return False
    return any(
        phrase in text
        for phrase in [
            "blocking:",
            "blocking finding",
            "blocking findings",
            "blocking review finding",
            "blocking review findings",
            "review blocker",
            "blocker:",
            "request changes",
            "cannot accept",
            "must be fixed",
            "ready-to-land: no",
        ]
    )


def has_failed_validation_or_tests(text: str) -> bool:
    failure_phrases = [
        "validation failed",
        "failed validation",
        "tests failed",
        "test failed",
        "failed tests",
        "failed test",
        "unit tests failed",
        "checks failed",
        "check failed",
        "build failed",
    ]
    negated_prefixes = ("no ", "no known ", "without ", "zero ")
    for phrase in failure_phrases:
        start = text.find(phrase)
        while start != -1:
            prefix = text[max(0, start - 20) : start]
            if not prefix.endswith(negated_prefixes):
                return True
            start = text.find(phrase, start + 1)
    return False


def looks_like_review(issue: dict[str, Any], event: dict[str, Any], text: str) -> bool:
    agent_id = str(event.get("agent_id") or "")
    if agent_id in REVIEW_AGENT_IDS:
        return True
    if agent_id in BUILD_AGENT_IDS:
        return False
    title = normalize_text(str(issue.get("title") or event.get("issue_title") or ""))
    return title.startswith("review ") or "audit" in title


def looks_like_ship(issue: dict[str, Any], event: dict[str, Any]) -> bool:
    agent_id = str(event.get("agent_id") or "")
    title = normalize_text(str(issue.get("title") or event.get("issue_title") or ""))
    return agent_id in SHIP_AGENT_IDS or any(term in title for term in ["ship", "deploy", "smoke", "rollout"])


def looks_like_implementation(issue: dict[str, Any], event: dict[str, Any], text: str) -> bool:
    agent_id = str(event.get("agent_id") or "")
    title = normalize_text(str(issue.get("title") or event.get("issue_title") or ""))
    if agent_id in BUILD_AGENT_IDS:
        return True
    if any(title.startswith(prefix) for prefix in ["implement ", "fix ", "package ", "add ", "deliver "]):
        return True
    return "implemented" in text and "validation" in text and "passed" in text


def completed_cleanly(text: str) -> bool:
    if has_blockers(text) or has_failed_validation_or_tests(text):
        return False
    return any(
        phrase in text
        for phrase in [
            "validation passed",
            "tests passed",
            "all github checks passed",
            "implemented",
            "completed",
            "no blocking findings",
            "no code changes",
        ]
    )


def marker_reason(outcome: dict[str, str]) -> str:
    reason = outcome.get("reason", "").strip()
    human_decision = outcome.get("human_decision_needed", "").strip()
    if outcome.get("status") == "human_review" and human_decision.lower() not in NO_HUMAN_DECISION_VALUES:
        return f"structured outcome marker: {reason}; human decision needed: {human_decision}"
    return f"structured outcome marker: {reason}"


def marker_role(issue: dict[str, Any], event: dict[str, Any]) -> str:
    agent_id = str(event.get("agent_id") or "")
    if agent_id in BUILD_AGENT_IDS:
        return "implementation"
    if agent_id in REVIEW_AGENT_IDS:
        return "review"
    if agent_id in PRODUCT_AGENT_IDS:
        return "product"
    if agent_id in SHIP_AGENT_IDS or looks_like_ship(issue, event):
        return "ship"
    return "unknown"


def marker_role_noop(reason: str, role: str, next_stage: str, expected: str) -> dict[str, Any]:
    return {
        "action": "noop",
        "target_status": None,
        "assignee": None,
        "reason": (
            f"{reason}; structured outcome marker is role-inconsistent for `{role}`: "
            f"`next_stage` `{next_stage}` must be `{expected}`"
        ),
    }


def clean_marker_decision(issue: dict[str, Any], event: dict[str, Any], outcome: dict[str, str]) -> dict[str, Any]:
    status = str(issue.get("status") or "")
    next_stage = outcome["next_stage"]
    role = marker_role(issue, event)
    reason = marker_reason(outcome)

    if role in {"implementation", "product"}:
        if status in {"done", "human_review", "blocked"}:
            return {
                "action": "noop",
                "target_status": None,
                "assignee": None,
                "reason": f"{reason}; issue already has terminal board status {status}",
            }
        if next_stage == "done":
            reason = f"{reason}; role-aware policy overrides clean `{role}` next_stage `done` to review handoff"
        elif next_stage != "in_review":
            return marker_role_noop(reason, role, next_stage, "in_review")
        return assignment_decision(
            issue,
            event,
            target_status="in_review",
            assignee=desired_review_assignee(issue, event),
            reason=reason,
            handoff_role="Review Agent",
            handoff_goal="Review the completed implementation or product output for correctness, regression risk, scope control, and focused validation evidence.",
        )

    if role == "review":
        if next_stage != "done":
            return marker_role_noop(reason, role, next_stage, "done")
        if status == "done":
            return {"action": "noop", "target_status": None, "assignee": None, "reason": f"{reason}; `done` is already reflected"}
        return {
            "action": "status",
            "target_status": "done",
            "assignee": None,
            "reason": reason,
        }

    if role == "ship":
        if next_stage != "done":
            return marker_role_noop(reason, role, next_stage, "done")
        if status == "done":
            return {"action": "noop", "target_status": None, "assignee": None, "reason": f"{reason}; `done` is already reflected"}
        return {
            "action": "status",
            "target_status": "done",
            "assignee": None,
            "reason": reason,
        }

    return {
        "action": "noop",
        "target_status": None,
        "assignee": None,
        "reason": f"{reason}; structured outcome marker has no safe role-aware route for clean output from this agent",
    }


def marker_decision(issue: dict[str, Any], event: dict[str, Any], outcome: dict[str, str]) -> dict[str, Any]:
    status = str(issue.get("status") or "")
    if outcome.get("_error"):
        return {
            "action": "noop",
            "target_status": None,
            "assignee": None,
            "reason": str(outcome["_error"]),
        }

    if outcome["status"] == "clean":
        return clean_marker_decision(issue, event, outcome)

    next_stage = outcome["next_stage"]
    reason = marker_reason(outcome)
    if next_stage == "no_op":
        return {"action": "noop", "target_status": None, "assignee": None, "reason": reason}
    if next_stage == "in_review":
        if status in {"done", "human_review", "blocked"}:
            return {
                "action": "noop",
                "target_status": None,
                "assignee": None,
                "reason": f"structured outcome marker requested `in_review`, but issue already has terminal board status {status}",
            }
        return assignment_decision(
            issue,
            event,
            target_status="in_review",
            assignee=desired_review_assignee(issue, event),
            reason=reason,
            handoff_role="Review Agent",
            handoff_goal="Review the completed implementation for correctness, regression risk, scope control, and focused validation evidence.",
        )

    if status == next_stage:
        return {"action": "noop", "target_status": None, "assignee": None, "reason": f"{reason}; `{next_stage}` is already reflected"}

    decision: dict[str, Any] = {
        "action": "status",
        "target_status": next_stage,
        "assignee": None,
        "reason": reason,
    }
    if next_stage == "human_review":
        decision["packet"] = "human_review"
        decision["human_decision_needed"] = outcome.get("human_decision_needed", "").strip()
    return decision


def desired_review_assignee(issue: dict[str, Any], event: dict[str, Any]) -> str:
    return REVIEW_AGENT_NAMES[lane_for(issue, event)]


def lane_agent_name(agent_names: dict[str, str], issue: dict[str, Any], event: dict[str, Any]) -> str | None:
    return agent_names.get(lane_for(issue, event))


def is_assigned_to(issue: dict[str, Any], assignee: str | None) -> bool:
    if not assignee or str(issue.get("assignee_type") or "") != "agent":
        return False
    return str(issue.get("assignee_id") or "") == AGENT_IDS_BY_NAME.get(assignee)


def assignment_decision(
    issue: dict[str, Any],
    event: dict[str, Any],
    *,
    target_status: str | None,
    assignee: str,
    reason: str,
    handoff_role: str,
    handoff_goal: str,
) -> dict[str, Any]:
    status = str(issue.get("status") or "")
    if target_status and status == target_status and is_assigned_to(issue, assignee):
        return {
            "action": "noop",
            "target_status": None,
            "assignee": None,
            "reason": f"{handoff_role} handoff is already reflected",
        }
    if target_status and status != target_status:
        action = "status_assign"
    else:
        action = "assign"
    return {
        "action": action,
        "target_status": target_status if action == "status_assign" else None,
        "assignee": assignee,
        "reason": reason,
        "handoff_role": handoff_role,
        "handoff_goal": handoff_goal,
    }


def followup_decision(
    issue: dict[str, Any],
    event: dict[str, Any],
    *,
    assignee: str,
    reason: str,
    followup_kind: str,
    handoff_role: str,
    handoff_goal: str,
) -> dict[str, Any]:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("number") or issue.get("id") or "issue")
    return {
        "action": "create_issue",
        "target_status": None,
        "assignee": assignee,
        "reason": reason,
        "followup_kind": followup_kind,
        "followup_title": f"{followup_kind}: {identifier}",
        "handoff_role": handoff_role,
        "handoff_goal": handoff_goal,
    }


def ship_ops_followup_decision(
    issue: dict[str, Any],
    event: dict[str, Any],
    terminal_text: str,
    *,
    assignee: str,
    reason: str,
) -> dict[str, Any]:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("number") or issue.get("id") or "issue")
    source_issue_id = str(event.get("issue_id") or issue.get("id") or "")
    title = ship_followup_title(issue, event, terminal_text)
    return {
        "action": "ensure_issue",
        "target_status": None,
        "assignee": assignee,
        "reason": reason,
        "followup_kind": "Ship Ops",
        "followup_title": title,
        "legacy_followup_title": f"Ship Ops: {identifier}",
        "source_issue_id": source_issue_id,
        "handoff_role": "Ship Ops Agent",
        "handoff_goal": "Package and perform only the explicitly authorized ship, deploy, publish, or smoke step for the completed work.",
    }


def decide(issue: dict[str, Any], event: dict[str, Any], terminal_text: str) -> dict[str, Any]:
    status = str(issue.get("status") or "")
    terminal_status = str(event.get("terminal_status") or "")
    text = normalize_text(terminal_text)
    if terminal_status != "completed":
        return {"action": "noop", "target_status": None, "assignee": None, "reason": f"terminal status is {terminal_status}"}
    if not text:
        return {"action": "noop", "target_status": None, "assignee": None, "reason": "terminal output is empty"}

    structured_outcome = parse_orchestrator_outcome(terminal_text)
    if structured_outcome is not None:
        return marker_decision(issue, event, structured_outcome)

    if looks_like_review(issue, event, text) and has_blockers(text):
        if status == "blocked":
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "blocking review is already reflected"}
        return {
            "action": "status",
            "target_status": "blocked",
            "assignee": None,
            "reason": "review output includes blocking findings",
        }

    human_gate = has_human_gate(text, issue, event)
    if human_gate:
        if status == "human_review":
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "human gate is already reflected"}
        return {
            "action": "status",
            "target_status": "human_review",
            "assignee": None,
            "reason": "terminal output requires a human gate or production/credential decision",
            "packet": "human_review",
        }

    if has_upstream_waiting(text):
        if status == "in_review":
            return {
                "action": "noop",
                "target_status": None,
                "assignee": None,
                "reason": "external/upstream wait is already represented by in_review",
                "packet": "upstream_waiting",
            }
        return {
            "action": "status",
            "target_status": "in_review",
            "assignee": None,
            "reason": "terminal output says external/upstream or maintainer handling is the next owner",
            "packet": "upstream_waiting",
        }

    if has_dependency_block(text):
        if status == "blocked":
            return {
                "action": "noop",
                "target_status": None,
                "assignee": None,
                "reason": "dependency block is already reflected",
                "packet": "dependency_block",
            }
        return {
            "action": "status",
            "target_status": "blocked",
            "assignee": None,
            "reason": "terminal output says a dependency or external prerequisite blocks local progress",
            "packet": "dependency_block",
        }

    if not looks_like_ship(issue, event) and has_ship_need(text):
        assignee = lane_agent_name(SHIP_AGENT_NAMES, issue, event)
        if assignee is None:
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "ship/deploy handoff has no configured lane agent"}
        return ship_ops_followup_decision(
            issue,
            event,
            terminal_text,
            assignee=assignee,
            reason="terminal output says ship/deploy/publish work is the next owner",
        )

    if has_product_need(text):
        assignee = lane_agent_name(PRODUCT_AGENT_NAMES, issue, event)
        if assignee is None:
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "product handoff has no configured lane agent"}
        return followup_decision(
            issue,
            event,
            assignee=assignee,
            reason="terminal output says product/sprint planning is needed next",
            followup_kind="Product",
            handoff_role="Product Agent",
            handoff_goal="Clarify the product decision, scope boundary, and acceptance criteria before additional implementation.",
        )

    if has_audit_need(text):
        assignee = lane_agent_name(AUDIT_AGENT_NAMES, issue, event)
        if assignee is None:
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "audit handoff has no configured lane agent"}
        return followup_decision(
            issue,
            event,
            assignee=assignee,
            reason="terminal output says audit/root-cause work is needed next",
            followup_kind="Audit",
            handoff_role="Audit Agent",
            handoff_goal="Audit the cited data, provenance, or root-cause question without production mutations.",
        )

    if has_validation_need(text):
        assignee = lane_agent_name(VALIDATION_AGENT_NAMES, issue, event)
        if assignee is None:
            return {"action": "noop", "target_status": None, "assignee": None, "reason": "validation handoff has no configured lane agent"}
        return followup_decision(
            issue,
            event,
            assignee=assignee,
            reason="terminal output says validation/smoke work is needed next",
            followup_kind="Validation",
            handoff_role="Validation Agent",
            handoff_goal="Run the requested validation or smoke checks and report exact evidence without shipping or production mutation unless explicitly authorized.",
        )

    if looks_like_review(issue, event, text):
        if has_blockers(text):
            if status == "blocked":
                return {"action": "noop", "target_status": None, "assignee": None, "reason": "blocking review is already reflected"}
            return {
                "action": "status",
                "target_status": "blocked",
                "assignee": None,
                "reason": "review output includes blocking findings",
            }
        if has_no_blockers(text) or completed_cleanly(text):
            if status == "done":
                return {"action": "noop", "target_status": None, "assignee": None, "reason": "passing review is already done"}
            return {
                "action": "status",
                "target_status": "done",
                "assignee": None,
                "reason": "review output has no blocking findings and no human gate",
            }

    if looks_like_ship(issue, event):
        if completed_cleanly(text):
            if status == "done":
                return {"action": "noop", "target_status": None, "assignee": None, "reason": "ship output is already done"}
            return {
                "action": "status",
                "target_status": "done",
                "assignee": None,
                "reason": "ship output completed without a detected human gate",
            }

    if looks_like_implementation(issue, event, text) and completed_cleanly(text):
        assignee = desired_review_assignee(issue, event)
        if status in {"done", "human_review", "blocked"}:
            return {"action": "noop", "target_status": None, "assignee": None, "reason": f"issue already has terminal board status {status}"}
        return assignment_decision(
            issue,
            event,
            target_status="in_review",
            assignee=assignee,
            reason="implementation/fix run completed cleanly",
            handoff_role="Review Agent",
            handoff_goal="Review the completed implementation for correctness, regression risk, scope control, and focused validation evidence.",
        )

    return {
        "action": "noop",
        "target_status": None,
        "assignee": None,
        "reason": "classification was ambiguous under deterministic policy",
    }


def build_next_agent_prompt(issue: dict[str, Any], event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("id") or "issue")
    run_id = str(event.get("run_id") or "unknown-run")
    role = str(decision.get("handoff_role") or "Next Agent")
    goal = str(decision.get("handoff_goal") or decision.get("reason") or "Handle the next bounded board step.")
    return "\n".join(
        [
            "Next-agent prompt:",
            f"- Role: {role}",
            f"- Goal: {goal}",
            f"- Context: Orchestrator classified completed run `{run_id}` for `{identifier}` and selected `{decision.get('assignee')}` as next owner.",
            "- Scope: Use the completed run output, current issue description, comments, and local repo state needed for this issue only.",
            "- Non-goals: Do not broaden scope, deploy, publish, merge, rotate credentials, or run production mutations unless the issue explicitly authorizes that action.",
            f"- Inputs: `{identifier}`, run `{run_id}`, watcher bridge event, issue comments, and changed files or validation cited by the previous agent.",
            "- Prompt/readback checks: Treat Markdown code spans as data; never place backticked command text inside double-quoted shell strings. Prefer single-quoted fixed strings, heredocs/temp pattern files, or Python JSON parsing.",
            "- Acceptance: Produce a clear outcome that supports the next board transition, with blockers called out explicitly.",
            "- Validation: Run focused checks appropriate for the touched area or explain why validation is not applicable.",
            "- Output: Comment with findings/outcome, files changed or no code changes, validation, residual risks, and the next handoff state.",
            "- State semantics: Build done routes to review; review done can close only with no blocker, human gate, upstream wait, or Ship Ops gate; Ship done requires explicit Ship Ops evidence.",
            "- Handoff: Return to the Orchestrator for board-flow classification after completion.",
        ]
    )


def build_decision_packet(event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or event.get("issue_id") or "issue")
    reason = str(decision.get("reason") or "human decision required")
    decision_needed = str(
        decision.get("human_decision_needed") or f"Decide the next approved action for `{identifier}`."
    )
    return "\n".join(
        [
            "Human-review decision packet:",
            f"- Decision needed: {decision_needed}",
            f"- Why human: {reason}; the Orchestrator must not make production, credential, product, or risk-acceptance decisions.",
            "- Options with impact: Approve the next action and let the appropriate agent proceed; request a narrower follow-up; or reject/defer and leave the issue blocked.",
            "- Recommended option: No recommendation; choose based on human review of the evidence and operational risk.",
            f"- Evidence: Issue `{identifier}`, run `{event.get('run_id') or 'unknown-run'}`, and the watcher bridge event that produced this packet.",
            "- Blocked until: A human records the selected option in an issue comment.",
            "- Next action after approval: Orchestrator assigns the appropriate Product, Build, Review, Validate, Audit, or Ship Ops agent with a next-agent prompt.",
            "- Expiry / revisit: Revisit when the blocking evidence changes or close/defer if no decision is recorded in the current work window.",
            "- State semantics: `human_review` is for a human decision; use `in_review` with an upstream-waiting packet for maintainer/PR wait, and `blocked` for unmet dependencies.",
        ]
    )


def build_upstream_waiting_packet(event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or event.get("issue_id") or "issue")
    reason = str(decision.get("reason") or "external/upstream wait")
    return "\n".join(
        [
            "External/upstream waiting packet:",
            f"- Waiting issue: `{identifier}`.",
            f"- Why waiting: {reason}.",
            "- Nearest board status: `in_review`, because local build/review work is complete enough for maintainer or upstream handling and no native `external_waiting` stage exists.",
            "- Required tracking: keep the PR, branch, upstream issue, or maintainer handoff reference in the issue comments.",
            "- Allowed local action: recheck the external reference or respond to maintainer feedback; do not duplicate completed packaging/review work unless maintainers reject it.",
            "- Done condition: upstream/maintainer outcome is accepted and any required local rollout or supersession evidence is recorded.",
        ]
    )


def build_dependency_block_packet(event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or event.get("issue_id") or "issue")
    reason = str(decision.get("reason") or "dependency block")
    return "\n".join(
        [
            "Dependency-blocked packet:",
            f"- Blocked issue: `{identifier}`.",
            f"- Why blocked: {reason}.",
            "- Nearest board status: `blocked`, because a known dependency or prerequisite prevents useful local progress.",
            "- Required tracking: cite the dependency issue, PR, deployment, data artifact, credential proof, or external condition that unblocks the card.",
            "- Allowed local action: only targeted unblock work or periodic recheck; do not mark Done until unblock evidence and required follow-up validation are recorded.",
        ]
    )


def build_ship_ops_packet(issue: dict[str, Any], event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("id") or "issue")
    source_issue_id = str(event.get("issue_id") or issue.get("id") or "")
    run_id = str(event.get("run_id") or "unknown-run")
    terminal_text = load_terminal_text(event)
    refs = extract_ship_refs(event, terminal_text)
    normalized = normalize_text(terminal_text)
    if has_no_blockers(normalized):
        review_result = "No blocking findings; ship/deploy/publish is the next requested owner."
    elif has_blockers(normalized):
        review_result = "Blocking findings were mentioned; Ship Ops must verify source issue state before taking action."
    else:
        review_result = "Ship/deploy/publish was requested by the completed run output."

    validation_lines = relevant_lines(terminal_text, ["validation", "test", "check", "py_compile", "unittest", "git diff"])
    residual_lines = relevant_lines(terminal_text, ["residual risk", "risk", "caveat", "not run", "did not run"])

    validation = "\n".join(f"  - {line}" for line in validation_lines) if validation_lines else "  - Not stated in the completed run output."
    residual = "\n".join(f"  - {line}" for line in residual_lines) if residual_lines else "  - Not stated; Ship Ops must report any remaining risk before closure."
    pr_value = refs.get("pr_url") or (f"PR #{refs['pr_number']}" if refs.get("pr_number") else "Not available")
    branch_value = refs.get("branch") or "Not available"
    commit_value = refs.get("commit") or "Not available"

    return "\n".join(
        [
            "Ship Ops handoff packet:",
            f"- Source issue: `{identifier}` (`{source_issue_id or 'unknown-source-issue'}`).",
            f"- Source run: `{run_id}`.",
            f"- PR: {pr_value}.",
            f"- Branch: `{branch_value}`.",
            f"- Commit: `{commit_value}`.",
            f"- Review result: {review_result}",
            "- Validation already run:",
            validation,
            "- Required merge/deploy/publish/smoke steps:",
            "  - Confirm the source issue explicitly authorizes the requested merge, deploy, publish, or smoke action.",
            "  - Merge only the cited PR/branch/commit when merge is in scope; otherwise leave merge untouched.",
            "  - Deploy or publish only the cited target when deploy/publish is in scope; otherwise leave live systems untouched.",
            "  - Run the requested post-ship smoke or focused validation and record exact evidence.",
            "  - Return to the Orchestrator with outcome, files changed or no code changes, validation, residual risks, and next handoff state.",
            "- Residual risks:",
            residual,
        ]
    )


def build_followup_description(issue: dict[str, Any], event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or issue.get("identifier") or issue.get("id") or "issue")
    if decision.get("followup_kind") == "Ship Ops":
        return "\n\n".join(
            [
                f"Created or updated by the Orchestrator from completed run `{event.get('run_id') or 'unknown-run'}` on `{identifier}`.",
                build_ship_ops_packet(issue, event, decision),
                build_next_agent_prompt(issue, event, decision),
            ]
        )
    return "\n\n".join(
        [
            f"Created by the Orchestrator from completed run `{event.get('run_id') or 'unknown-run'}` on `{identifier}`.",
            build_next_agent_prompt(issue, event, decision),
        ]
    )


def build_parent_rollup(event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or event.get("issue_id") or "issue")
    action = str(decision.get("action") or "noop")
    target = str(decision.get("target_status") or "")
    assignee = str(decision.get("assignee") or "")
    reason = str(decision.get("reason") or "no reason recorded")
    fragments = [f"Child `{identifier}` orchestrator rollup: {reason}."]
    if target:
        fragments.append(f"Status moved to `{target}`.")
    if assignee:
        fragments.append(f"Next owner: `{assignee}`.")
    if action == "create_issue":
        fragments.append(f"Created `{decision.get('followup_title')}`.")
    if action == "update_issue":
        fragments.append(f"Updated `{decision.get('followup_title')}`.")
    return " ".join(fragments)


def build_comment(issue: dict[str, Any], event: dict[str, Any], decision: dict[str, Any]) -> str:
    identifier = str(event.get("issue_identifier") or event.get("issue_id") or "issue")
    key = str(event.get("idempotency_key") or event.get("run_id") or "unknown-event")
    reason = str(decision.get("reason") or "no reason recorded")
    action = decision.get("action")
    target_status = decision.get("target_status")
    assignee = decision.get("assignee")
    if decision.get("packet") == "human_review":
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and moved this issue to `{target_status}`: {reason}.\n\n"
            f"{build_decision_packet(event, decision)}"
        )
    if decision.get("packet") == "upstream_waiting":
        transition = (
            f"moved this issue to `{target_status}`"
            if target_status
            else "left this issue in its current upstream-waiting board state"
        )
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and {transition}: {reason}.\n\n"
            f"{build_upstream_waiting_packet(event, decision)}"
        )
    if decision.get("packet") == "dependency_block":
        transition = (
            f"moved this issue to `{target_status}`"
            if target_status
            else "left this issue in its current blocked board state"
        )
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and {transition}: {reason}.\n\n"
            f"{build_dependency_block_packet(event, decision)}"
        )
    if action == "noop":
        return f"Orchestrator consumed event `{key}` for `{identifier}` with no board transition: {reason}."
    if action == "create_issue":
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and created `{decision.get('followup_title')}` "
            f"for `{assignee}`: {reason}.\n\n{build_next_agent_prompt(issue, event, decision)}"
        )
    if action == "update_issue":
        followup_id = str(decision.get("followup_issue_id") or "existing follow-up")
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and updated existing `{decision.get('followup_title')}` "
            f"(`{followup_id}`) for `{assignee}`: {reason}."
        )
    if action == "ensure_issue":
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and would create or update `{decision.get('followup_title')}` "
            f"for `{assignee}`: {reason}.\n\n{build_next_agent_prompt(issue, event, decision)}"
        )
    if action == "assign":
        return (
            f"Orchestrator consumed event `{key}` for `{identifier}` and assigned `{assignee}`: {reason}.\n\n"
            f"{build_next_agent_prompt(issue, event, decision)}"
        )
    if action == "status":
        return f"Orchestrator consumed event `{key}` for `{identifier}` and moved this issue to `{target_status}`: {reason}."
    return (
        f"Orchestrator consumed event `{key}` for `{identifier}`, moved this issue to `{target_status}`, "
        f"and assigned `{assignee}`: {reason}.\n\n{build_next_agent_prompt(issue, event, decision)}"
    )


def apply_decision(
    multica: str, issue: dict[str, Any], issue_id: str, event: dict[str, Any], decision: dict[str, Any], *, dry_run: bool
) -> dict[str, Any]:
    decision = apply_admission_control(issue, event, decision)
    action = decision.get("action")
    if not dry_run:
        followup_ensured = False
        if action == "ensure_issue":
            project_id = str(issue.get("project_id") or event.get("project_id") or "")
            title = str(decision.get("followup_title") or "Ship Ops follow-up")
            existing = matching_ship_followup(
                issue_list(multica, project_id=project_id or None),
                source_issue_id=issue_id,
                title=title,
                legacy_title=str(decision.get("legacy_followup_title") or title),
            )
            if not existing and project_id:
                existing = matching_ship_followup(
                    issue_list(multica, project_id=None),
                    source_issue_id=issue_id,
                    title=title,
                    legacy_title=str(decision.get("legacy_followup_title") or title),
                )
            description = build_followup_description(issue, event, decision)
            if existing:
                existing_id = str(existing.get("id") or "")
                if existing_id:
                    issue_update(
                        multica,
                        existing_id,
                        title=title,
                        description=description,
                        assignee=str(decision["assignee"]),
                    )
                    decision["action"] = "update_issue"
                    decision["followup_issue_id"] = existing_id
                    action = "update_issue"
                    followup_ensured = True
            if action == "ensure_issue":
                issue_create(
                    multica,
                    title=title,
                    description=description,
                    assignee=str(decision["assignee"]),
                    parent_id=issue_id,
                )
                decision["action"] = "create_issue"
                action = "create_issue"
                followup_ensured = True
        if action == "create_issue" and not followup_ensured:
            issue_create(
                multica,
                title=str(decision.get("followup_title") or "Orchestrator follow-up"),
                description=build_followup_description(issue, event, decision),
                assignee=str(decision["assignee"]),
                parent_id=issue_id,
            )
        if action in {"status", "status_assign"} and decision.get("target_status"):
            issue_status(multica, issue_id, str(decision["target_status"]))
        if action in {"assign", "status_assign"} and decision.get("assignee"):
            issue_assign(multica, issue_id, str(decision["assignee"]))
        issue_comment(multica, issue_id, build_comment(issue, event, decision))
        parent_id = str(issue.get("parent_issue_id") or "")
        if parent_id and action != "noop":
            issue_comment(multica, parent_id, build_parent_rollup(event, decision))
    else:
        print(f"DRY-RUN {issue_id}: {build_comment(issue, event, decision)}")
    return decision


def consume_once(args: argparse.Namespace) -> int:
    state = load_state(args.state_path)
    handled = dict(state.get("handled_events") or {})
    events = sorted(load_events(args.event_dir), key=event_order_key)
    if args.newest_first:
        events = list(reversed(events))

    processed = 0
    for path, event in events:
        key = event_key(event, path)
        if handled.get(key):
            continue
        issue_id = str(event.get("issue_id") or "")
        if not issue_id:
            print(f"Skipping {path}: missing issue_id", file=sys.stderr)
            continue
        issue = issue_get(args.multica, issue_id)
        terminal_text = load_terminal_text(event)
        decision = stale_event_decision(issue, event) or decide(issue, event, terminal_text)
        decision = apply_decision(args.multica, issue, issue_id, event, decision, dry_run=args.dry_run)
        handled[key] = {
            "handled_at": utc_now(),
            "issue_id": issue_id,
            "issue_identifier": event.get("issue_identifier") or issue.get("identifier"),
            "run_id": event.get("run_id"),
            "action": decision.get("action"),
            "target_status": decision.get("target_status"),
            "assignee": decision.get("assignee"),
            "reason": decision.get("reason"),
            "admission_reason": decision.get("admission_reason"),
        }
        state["handled_events"] = handled
        state["last_checked_at"] = utc_now()
        if not args.dry_run:
            write_state(args.state_path, state)
        print(f"{key}: {decision.get('action')} ({decision.get('reason')})")
        processed += 1
        if processed >= args.max_events:
            break

    if processed == 0:
        now = current_time(args)
        issues = issue_list(args.multica, project_id=MULTICA_PROJECT_ID)
        audit_decision = workflow_audit_decision(
            issues=issues,
            events=events,
            handled=handled,
            state=state,
            now=now,
            project_id=MULTICA_PROJECT_ID,
        )
        if apply_workflow_audit_decision(
            args.multica,
            audit_decision,
            state,
            args.state_path,
            now,
            dry_run=args.dry_run,
        ):
            return 0
        print("No unhandled Multica bridge events.")
    elif args.dry_run:
        print("Dry run only; state was not updated.")
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event-dir", type=Path, default=DEFAULT_EVENT_DIR)
    parser.add_argument("--state-path", type=Path, default=DEFAULT_STATE_PATH)
    parser.add_argument("--multica", "--cli-path", dest="multica", default=default_multica())
    parser.add_argument("--max-events", type=int, default=1, help="Maximum unhandled events to consume in this pass.")
    parser.add_argument("--oldest-first", dest="newest_first", action="store_false", help="Process the oldest unhandled event first.")
    parser.add_argument("--dry-run", action="store_true", help="Print intended actions without writing board state or consumer state.")
    parser.add_argument("--now", default=None, help=argparse.SUPPRESS)
    parser.set_defaults(newest_first=True)
    args = parser.parse_args(argv)
    if args.max_events < 1:
        parser.error("--max-events must be at least 1")
    return consume_once(args)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
