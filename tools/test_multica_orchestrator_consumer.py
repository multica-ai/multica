from __future__ import annotations

import argparse
import importlib.util
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).with_name("multica_orchestrator_consumer.py")
SPEC = importlib.util.spec_from_file_location("multica_orchestrator_consumer", SCRIPT_PATH)
consumer = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(consumer)


ISSUE_ID = "issue-1"
RUN_ID = "run-1"


def issue(
    *,
    title: str = "Fix deterministic thing",
    status: str = "todo",
    project_id: str = consumer.MULTICA_PROJECT_ID,
    assignee_id: str = consumer.MULTICA_BUILD_AGENT_ID,
    parent_issue_id: str | None = None,
    created_at: str | None = None,
    updated_at: str | None = None,
) -> dict[str, object]:
    payload: dict[str, object] = {
        "id": ISSUE_ID,
        "identifier": "FEL-1",
        "title": title,
        "status": status,
        "project_id": project_id,
        "assignee_id": assignee_id,
        "assignee_type": "agent",
        "parent_issue_id": parent_issue_id,
    }
    if created_at is not None:
        payload["created_at"] = created_at
    if updated_at is not None:
        payload["updated_at"] = updated_at
    return payload


def event(
    tmp: Path,
    *,
    agent_id: str,
    summary: str,
    title: str = "Fix deterministic thing",
    status: str = "completed",
    run_id: str = RUN_ID,
    created_at: str | None = None,
    completed_at: str | None = None,
    extra: dict[str, object] | None = None,
) -> Path:
    event_dir = tmp / "events"
    event_dir.mkdir(exist_ok=True)
    inbox = tmp / f"{run_id}-inbox.md"
    inbox.write_text(f"# Multica Run {status}: FEL-1\n\n## Result\n\n{summary}\n", encoding="utf-8")
    path = event_dir / f"{run_id}-{status}.json"
    payload = {
        "type": "multica.run.terminal",
        "idempotency_key": f"{run_id}:{status}",
        "issue_id": ISSUE_ID,
        "issue_identifier": "FEL-1",
        "issue_title": title,
        "run_id": run_id,
        "agent_id": agent_id,
        "terminal_status": status,
        "summary": summary,
        "inbox_markdown_path": str(inbox),
    }
    if created_at is not None:
        payload["created_at"] = created_at
    if completed_at is not None:
        payload["completed_at"] = completed_at
    if extra:
        payload.update(extra)
    path.write_text(json.dumps(payload), encoding="utf-8")
    return path


def outcome_marker(
    *,
    status: str,
    next_stage: str,
    reason: str,
    human_decision_needed: str = "none",
) -> str:
    return "\n".join(
        [
            "Orchestrator outcome:",
            f"- status: {status}",
            f"- next_stage: {next_stage}",
            f"- reason: {reason}",
            f"- human_decision_needed: {human_decision_needed}",
        ]
    )


def args_for(tmp: Path) -> argparse.Namespace:
    return argparse.Namespace(
        event_dir=tmp / "events",
        state_path=tmp / "state" / "consumer.json",
        multica="/fake/multica",
        max_events=10,
        newest_first=True,
        dry_run=False,
        now="2026-04-13T21:00:00Z",
    )


class FakeMultica:
    def __init__(
        self,
        issue_payload: dict[str, object],
        issue_list_payload: list[dict[str, object]] | None = None,
        unfiltered_issue_list_payload: list[dict[str, object]] | None = None,
    ):
        self.issue_payload = issue_payload
        self.issue_list_payload = issue_list_payload or []
        self.unfiltered_issue_list_payload = (
            self.issue_list_payload if unfiltered_issue_list_payload is None else unfiltered_issue_list_payload
        )
        self.commands: list[list[str]] = []

    def run_json(self, command: list[str], *, timeout: int = 30) -> object:
        self.commands.append(command)
        if command == ["/fake/multica", "issue", "get", ISSUE_ID, "--output", "json"]:
            return self.issue_payload
        if command == [
            "/fake/multica",
            "issue",
            "list",
            "--project",
            str(self.issue_payload.get("project_id") or ""),
            "--limit",
            str(consumer.MAX_ISSUE_SCAN_LIMIT),
            "--output",
            "json",
        ]:
            return self.issue_list_payload
        if command == [
            "/fake/multica",
            "issue",
            "list",
            "--limit",
            str(consumer.MAX_ISSUE_SCAN_LIMIT),
            "--output",
            "json",
        ]:
            return self.unfiltered_issue_list_payload
        raise AssertionError(f"unexpected read command: {command}")

    def run_command(self, command: list[str], *, timeout: int = 30) -> None:
        self.commands.append(command)


class ConsumerTests(unittest.TestCase):
    def run_consumer(self, tmp: Path, fake: FakeMultica, *, now: str | None = None) -> int:
        args = args_for(tmp)
        if now is not None:
            args.now = now
        with mock.patch.object(consumer, "run_json", fake.run_json), mock.patch.object(
            consumer, "run_command", fake.run_command
        ):
            return consumer.consume_once(args)

    def comments(self, fake: FakeMultica) -> list[str]:
        return [
            command[-1]
            for command in fake.commands
            if command[:4] == ["/fake/multica", "issue", "comment", "add"]
        ]

    def assert_no_agent_assign(self, fake: FakeMultica) -> None:
        self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "assign"] for command in fake.commands))

    def assert_no_issue_create_or_update(self, fake: FakeMultica) -> None:
        self.assertFalse(
            any(
                command[:3] in (["/fake/multica", "issue", "create"], ["/fake/multica", "issue", "update"])
                for command in fake.commands
            )
        )

    def test_implementation_pass_moves_to_review_and_assigns_review_agent(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Validation passed.")
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])
            self.assertNotIn("Next-agent prompt:", comments[0])

    def test_manual_user_requested_allows_review_agent_assignment(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                summary="Implemented the fix. Validation passed.",
                extra={"manual_user_requested": True},
            )
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assertIn(["/fake/multica", "issue", "assign", ISSUE_ID, "--to", "Multica Review Agent"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn("Next-agent prompt:", comments[0])
            state = json.loads((tmp / "state" / "consumer.json").read_text(encoding="utf-8"))
            self.assertEqual(
                state["handled_events"][f"{RUN_ID}:completed"]["admission_reason"],
                consumer.ADMISSION_MANUAL_USER_REQUESTED,
            )

    def test_build_agent_no_blocking_findings_still_routes_to_review(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                summary="Implemented the fix. No blocking findings. Validation passed.",
            )
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])
            self.assertNotIn("Next-agent prompt:", comments[0])

    def test_build_agent_no_blocking_findings_on_review_title_still_routes_to_review(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                title="Review FEL-1",
                summary="Implemented the fix. No blocking findings. Validation passed.",
            )
            fake = FakeMultica(issue(title="Review FEL-1"))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])
            self.assertNotIn("Next-agent prompt:", comments[0])

    def test_marker_clean_implementation_moves_to_review(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                summary=outcome_marker(
                    status="clean",
                    next_stage="in_review",
                    reason="implementation complete with focused validation",
                ),
            )
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn("structured outcome marker: implementation complete", comments[0])
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])

    def test_marker_clean_build_done_routes_to_review_not_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                summary=outcome_marker(
                    status="clean",
                    next_stage="done",
                    reason="implementation complete with focused validation",
                ),
            )
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn("overrides clean `implementation` next_stage `done` to review handoff", comments[0])
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])

    def test_marker_clean_review_moves_to_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="clean",
                    next_stage="done",
                    reason="review passed with no blocking findings",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)

    def test_marker_clean_review_in_review_does_not_loop(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="clean",
                    next_stage="in_review",
                    reason="review passed with no blocking findings",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("role-inconsistent for `review`", comments[0])

    def test_marker_clean_product_routes_to_review_not_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.TRAINER_PRODUCT_AGENT_ID,
                title="Define Trainer scope",
                summary=outcome_marker(
                    status="clean",
                    next_stage="done",
                    reason="product decision packet is complete",
                ),
            )
            fake = FakeMultica(
                issue(
                    title="Define Trainer scope",
                    status="todo",
                    project_id=consumer.TRAINER_PROJECT_ID,
                    assignee_id=consumer.TRAINER_PRODUCT_AGENT_ID,
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_autonomous_product_followup_is_denied(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.TRAINER_BUILD_AGENT_ID,
                title="Build Trainer feature",
                summary="Implementation paused. Product decision needed for acceptance criteria.",
            )
            fake = FakeMultica(
                issue(
                    title="Build Trainer feature",
                    status="in_progress",
                    project_id=consumer.TRAINER_PROJECT_ID,
                    assignee_id=consumer.TRAINER_BUILD_AGENT_ID,
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assert_no_issue_create_or_update(fake)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_PRODUCT_PLANNING_DENIED, comments[0])

    def test_marker_clean_ship_moves_to_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.TRAINER_SHIP_AGENT_ID,
                title="Ship FEL-1 live smoke",
                summary=outcome_marker(
                    status="clean",
                    next_stage="done",
                    reason="ship smoke passed and no caveat remains",
                ),
            )
            fake = FakeMultica(
                issue(
                    title="Ship FEL-1 live smoke",
                    status="in_review",
                    project_id=consumer.TRAINER_PROJECT_ID,
                    assignee_id=consumer.TRAINER_SHIP_AGENT_ID,
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_marker_blocked_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="blocked",
                    next_stage="blocked",
                    reason="review found a blocker in validation coverage",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)

    def test_marker_human_review_preserves_decision_question(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="human_review",
                    next_stage="human_review",
                    reason="production deploy needs explicit approval",
                    human_decision_needed="Approve production deploy for FEL-1?",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("Approve production deploy for FEL-1?", comments[0])

    def test_marker_human_review_requires_concrete_ask(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="human_review",
                    next_stage="human_review",
                    reason="needs another look",
                    human_decision_needed="Please review",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED, comments[0])

    def test_malformed_marker_does_not_fall_back_to_heuristics(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "No blocking findings. Validation passed.\n\n"
                    + outcome_marker(
                        status="victory",
                        next_stage="done",
                        reason="review passed",
                    )
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("structured outcome marker has unknown status", comments[0])

    def test_contradictory_marker_does_not_advance(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=outcome_marker(
                    status="clean",
                    next_stage="blocked",
                    reason="review passed but route says blocked",
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("structured outcome marker is contradictory", comments[0])

    def test_failed_validation_does_not_advance(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Validation failed.")
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "assign"] for command in fake.commands))
            comments = [command for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("no board transition", comments[0][-1])

    def test_failed_validation_word_order_does_not_advance(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Failed validation.")
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "assign"] for command in fake.commands))
            comments = [command for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("no board transition", comments[0][-1])

    def test_failed_tests_do_not_advance(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Tests failed.")
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "assign"] for command in fake.commands))
            comments = [command for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("no board transition", comments[0][-1])

    def test_child_transition_adds_parent_rollup(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Validation passed.")
            fake = FakeMultica(issue(parent_issue_id="parent-1"))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            parent_comments = [
                command[-1]
                for command in fake.commands
                if command[:5] == ["/fake/multica", "issue", "comment", "add", "parent-1"]
            ]
            self.assertEqual(len(parent_comments), 1)
            self.assertIn("Child `FEL-1` orchestrator rollup", parent_comments[0])
            self.assertIn("Status moved to `in_review`", parent_comments[0])
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, parent_comments[0])
            self.assertNotIn("Next owner:", parent_comments[0])

    def test_fel58_in_review_assigned_to_orchestrator_reassigns_review_agent(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Validation passed.")
            fake = FakeMultica(issue(status="in_review", assignee_id=consumer.MULTICA_ORCHESTRATOR_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_AUTONOMOUS_AGENT_LAUNCH_DENIED, comments[0])
            self.assertNotIn("Next-agent prompt:", comments[0])

    def test_review_pass_moves_to_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_REVIEW_AGENT_ID, summary="No blocking findings. Validation passed.")
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_review_pass_with_human_gate_moves_to_human_review_not_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="No blocking findings.\n\nHuman gate:\n- Required: yes\n- Human decision needed: approve deploy.",
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn("Human-review decision packet:", comments[0])
            self.assertIn("- Decision needed: approve deploy.", comments[0])
            self.assertIn("- Expiry / revisit:", comments[0])

    def test_review_pass_with_negated_safety_statement_moves_to_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "No blocking findings. Validation passed. "
                    "No deploy, publish, credentials, or production mutation touched."
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)

    def test_review_pass_with_negated_production_mutation_moves_to_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "No blocking findings. Validation passed. Did not run production mutation. "
                    "Did not mutate production. No credentials touched."
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)

    def test_review_pass_with_true_production_gate_moves_to_human_review(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="No blocking findings. Validation passed. Production mutation required before close.",
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            self.assertIn(consumer.ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED, self.comments(fake)[0])

    def test_review_pass_with_negated_required_gate_phrases_moves_to_done(self) -> None:
        phrases = [
            "No production mutation required.",
            "No deploy required.",
            "No publish required.",
            "No credential rotation required.",
            "No credentials need rotation.",
            "No OAuth renewal required.",
        ]
        for phrase in phrases:
            with self.subTest(phrase=phrase), tempfile.TemporaryDirectory() as td:
                tmp = Path(td)
                event(
                    tmp,
                    agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                    summary=f"No blocking findings. Validation passed. {phrase}",
                )
                fake = FakeMultica(
                    issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID)
                )

                self.assertEqual(self.run_consumer(tmp, fake), 0)

                self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
                self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)

    def test_review_pass_with_true_gate_phrases_moves_to_human_review(self) -> None:
        phrases = [
            "Production mutation required.",
            "Deploy required.",
            "Publish required.",
            "Credentials need rotation.",
            "Manual OAuth required.",
        ]
        for phrase in phrases:
            with self.subTest(phrase=phrase), tempfile.TemporaryDirectory() as td:
                tmp = Path(td)
                event(
                    tmp,
                    agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                    summary=f"No blocking findings. Validation passed. {phrase}",
                )
                fake = FakeMultica(
                    issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID)
                )

                self.assertEqual(self.run_consumer(tmp, fake), 0)

                self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
                self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
                self.assertIn(consumer.ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED, self.comments(fake)[0])

    def test_review_pass_with_upstream_wait_stays_in_review_not_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "No blocking findings. Next handoff state: not active agent review; "
                    "keep FEL-19 in review/upstream-waiting on PR #758 plus stacked PR #1."
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-19", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("External/upstream waiting packet:", comments[0])
            self.assertIn("Nearest board status: `in_review`", comments[0])

    def test_dependency_block_moves_to_blocked_with_packet(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.TRAINER_SHIP_AGENT_ID,
                title="Ship FEL-50 gate",
                summary="Keep this gate blocked until FEL-59 restores body-comp availability. No publish was run.",
            )
            fake = FakeMultica(
                issue(
                    title="Ship FEL-50 gate",
                    status="in_review",
                    project_id=consumer.TRAINER_PROJECT_ID,
                    assignee_id=consumer.TRAINER_SHIP_AGENT_ID,
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("Dependency-blocked packet:", comments[0])
            self.assertIn("Nearest board status: `blocked`", comments[0])

    def test_clean_review_with_dependency_gate_moves_to_blocked_not_done(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-50 gate",
                summary="No blocking findings. Keep this gate blocked until FEL-59 lands, then rerun Ship Ops.",
            )
            fake = FakeMultica(issue(title="Review FEL-50 gate", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("Dependency-blocked packet:", comments[0])
            self.assertIn("Nearest board status: `blocked`", comments[0])

    def test_review_blocker_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_REVIEW_AGENT_ID, summary="Blocking finding: validation does not cover the fix.")
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)

    def test_review_blocking_colon_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="Blocking: this review found a real issue. Validation passed.",
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_review_blocking_review_finding_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="Blocking review finding: do not land. Validation passed.",
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_stale_review_blocker_records_noop_and_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="Blocking finding: old review blocker superseded by a newer fix handoff.",
                created_at="2026-04-13T06:05:05Z",
                completed_at="2026-04-13T06:04:00Z",
            )
            fake = FakeMultica(
                issue(
                    title="Review FEL-1",
                    status="in_progress",
                    assignee_id=consumer.MULTICA_BUILD_AGENT_ID,
                    updated_at="2026-04-13T06:30:00Z",
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)
            command_count = len(fake.commands)
            self.assertEqual(self.run_consumer(tmp, fake), 0)

            write_commands = [
                command
                for command in fake.commands[command_count:]
                if command[:3] in (
                    ["/fake/multica", "issue", "status"],
                    ["/fake/multica", "issue", "assign"],
                    ["/fake/multica", "issue", "comment"],
                    ["/fake/multica", "issue", "create"],
                    ["/fake/multica", "issue", "update"],
                )
            ]
            self.assertEqual(write_commands, [])
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("stale bridge event skipped", comments[0])
            state = json.loads((tmp / "state" / "consumer.json").read_text(encoding="utf-8"))
            self.assertEqual(state["handled_events"][f"{RUN_ID}:completed"]["action"], "noop")
            self.assertIn("stale bridge event skipped", state["handled_events"][f"{RUN_ID}:completed"]["reason"])

    def test_created_after_issue_update_but_completed_before_is_stale(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="Blocking finding: event file was written after a newer handoff, but run completion was old.",
                created_at="2026-04-13T06:31:00Z",
                completed_at="2026-04-13T06:29:00Z",
            )
            fake = FakeMultica(
                issue(
                    title="Review FEL-1",
                    status="in_progress",
                    assignee_id=consumer.MULTICA_BUILD_AGENT_ID,
                    updated_at="2026-04-13T06:30:00Z",
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            comments = [command[-1] for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("event timestamp 2026-04-13T06:29:00Z", comments[0])

    def test_current_review_blocker_still_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary="Blocking finding: current review blocker still applies.",
                created_at="2026-04-13T06:30:05Z",
                completed_at="2026-04-13T06:30:00Z",
            )
            fake = FakeMultica(
                issue(
                    title="Review FEL-1",
                    status="in_review",
                    assignee_id=consumer.MULTICA_REVIEW_AGENT_ID,
                    updated_at="2026-04-13T06:29:30Z",
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)

    def test_current_clean_build_event_still_routes_to_review(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                summary="Implemented the fix. Validation passed.",
                created_at="2026-04-13T06:30:05Z",
                completed_at="2026-04-13T06:30:00Z",
            )
            fake = FakeMultica(issue(status="in_progress", updated_at="2026-04-13T06:29:30Z"))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "in_review"], fake.commands)
            self.assert_no_agent_assign(fake)

    def test_review_blocker_with_quoted_no_blockers_probe_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "**Findings**\n"
                    "Blocking: production mutation was treated as a human gate when negated. "
                    "Minimal probe: `No blocking findings. Validation passed.`"
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "done"], fake.commands)

    def test_review_blocker_with_quoted_human_gate_probe_moves_to_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                summary=(
                    "**Findings**\n"
                    "Blocking: false positive in gate parsing remains. "
                    "Probe: `Production mutation required.` should route to human_review."
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertIn(["/fake/multica", "issue", "status", ISSUE_ID, "blocked"], fake.commands)
            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)

    def test_fixed_string_probe_searches_backticked_op_signin_without_spawning_op(self) -> None:
        grep = shutil.which("grep")
        if grep is None:
            self.skipTest("grep is not available")
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake_bin = tmp / "bin"
            fake_bin.mkdir()
            marker = tmp / "op-was-run"
            fake_op = fake_bin / "op"
            fake_op.write_text(
                "#!/usr/bin/env python3\n"
                "from pathlib import Path\n"
                f"Path({str(marker)!r}).write_text('executed', encoding='utf-8')\n",
                encoding="utf-8",
            )
            fake_op.chmod(0o755)

            prompt = tmp / "prompt.md"
            prompt.write_text("Readback probe: literal `op signin` must stay inert.\n", encoding="utf-8")
            pattern = tmp / "pattern.txt"
            pattern.write_text("op signin\n", encoding="utf-8")
            env = dict(os.environ, PATH=f"{fake_bin}{os.pathsep}{os.environ.get('PATH', '')}")

            completed = subprocess.run(
                [grep, "-F", "-f", str(pattern), str(prompt)],
                capture_output=True,
                text=True,
                timeout=10,
                check=False,
                env=env,
            )

            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertIn("`op signin`", completed.stdout)
            self.assertFalse(marker.exists(), "literal prompt readback search spawned the fake op executable")

    def test_ship_ready_creates_ship_ops_card_prompt_without_direct_deploy(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-1",
                summary=(
                    "No blocking findings. Ready to deploy with Ship Ops. "
                    "PR: https://github.com/example/repo/pull/123. Branch: release/fel-1. "
                    "Commit abcdef123456. Validation run: /usr/bin/python3 -m unittest tests."
                ),
            )
            fake = FakeMultica(issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assert_no_issue_create_or_update(fake)
            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_CHILD_CARD_CREATION_DENIED, comments[0])
            self.assertNotIn("Next-agent prompt:", comments[0])

    def test_ship_ready_updates_existing_ship_ops_card_for_same_pr(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-1",
                summary=(
                    "No blocking findings. Ready to deploy with Ship Ops. "
                    "PR #123. Branch: release/fel-1. Commit abcdef123456. "
                    "Validation already run: git diff --check. Residual risk: smoke still needed."
                ),
            )
            existing = {
                "id": "ship-1",
                "identifier": "FEL-99",
                "title": "Ship Ops: FEL-1 [PR #123]",
                "status": "todo",
                "parent_issue_id": ISSUE_ID,
                "project_id": consumer.MULTICA_PROJECT_ID,
            }
            fake = FakeMultica(
                issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID),
                issue_list_payload=[existing],
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assert_no_issue_create_or_update(fake)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_CHILD_CARD_CREATION_DENIED, comments[0])

    def test_ship_ready_reuses_null_project_ship_ops_card_after_project_lookup_miss(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-1",
                summary="No blocking findings. Ready to deploy with Ship Ops. PR #123. Validation passed.",
            )
            existing = {
                "id": "ship-null-project-1",
                "identifier": "FEL-102",
                "title": "Ship Ops: FEL-1 [PR #123]",
                "status": "todo",
                "parent_issue_id": ISSUE_ID,
                "project_id": None,
            }
            fake = FakeMultica(
                issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID),
                issue_list_payload=[],
                unfiltered_issue_list_payload=[existing],
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(
                [
                    "/fake/multica",
                    "issue",
                    "list",
                    "--project",
                    consumer.MULTICA_PROJECT_ID,
                    "--limit",
                    str(consumer.MAX_ISSUE_SCAN_LIMIT),
                    "--output",
                    "json",
                ],
                fake.commands,
            )
            self.assert_no_issue_create_or_update(fake)
            self.assertIn(consumer.ADMISSION_CHILD_CARD_CREATION_DENIED, self.comments(fake)[0])

    def test_ship_ready_without_ref_reuses_source_issue_fallback_card(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-1",
                summary="No blocking findings. Ready to publish with Ship Ops. Validation passed.",
            )
            existing = {
                "id": "ship-source-1",
                "identifier": "FEL-100",
                "title": "Ship Ops: FEL-1 [source issue-1]",
                "status": "todo",
                "parent_issue_id": ISSUE_ID,
                "project_id": consumer.MULTICA_PROJECT_ID,
            }
            fake = FakeMultica(
                issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID),
                issue_list_payload=[existing],
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assert_no_issue_create_or_update(fake)
            self.assertIn(consumer.ADMISSION_CHILD_CARD_CREATION_DENIED, self.comments(fake)[0])

    def test_ship_ready_reuses_done_ship_ops_card_instead_of_duplicate(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.MULTICA_REVIEW_AGENT_ID,
                title="Review FEL-1",
                summary="No blocking findings. Ready to deploy with Ship Ops. PR #123. Validation passed.",
            )
            existing = {
                "id": "ship-done-1",
                "identifier": "FEL-101",
                "title": "Ship Ops: FEL-1 [PR #123]",
                "status": "done",
                "parent_issue_id": ISSUE_ID,
                "project_id": consumer.MULTICA_PROJECT_ID,
            }
            fake = FakeMultica(
                issue(title="Review FEL-1", status="in_review", assignee_id=consumer.MULTICA_REVIEW_AGENT_ID),
                issue_list_payload=[existing],
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assert_no_issue_create_or_update(fake)
            self.assertIn(consumer.ADMISSION_CHILD_CARD_CREATION_DENIED, self.comments(fake)[0])

    def test_ship_caveat_moves_to_human_review_with_decision_packet(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(
                tmp,
                agent_id=consumer.TRAINER_SHIP_AGENT_ID,
                title="Ship FEL-1 live smoke",
                summary="Validation passed. Residual risk: I did not run a production answer mutation.",
            )
            fake = FakeMultica(
                issue(
                    title="Ship FEL-1 live smoke",
                    status="in_review",
                    project_id=consumer.TRAINER_PROJECT_ID,
                    assignee_id=consumer.TRAINER_SHIP_AGENT_ID,
                )
            )

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertNotIn(["/fake/multica", "issue", "status", ISSUE_ID, "human_review"], fake.commands)
            comments = self.comments(fake)
            self.assertEqual(len(comments), 1)
            self.assertIn(consumer.ADMISSION_HUMAN_REVIEW_MISSING_ASK_DENIED, comments[0])
            self.assertNotIn("Human-review decision packet:", comments[0])

    def test_ambiguous_output_records_noop_comment(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id="unknown-agent", summary="I looked at the issue and wrote a note.")
            fake = FakeMultica(issue(title="Untyped task", assignee_id="unknown-agent"))

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "status"] for command in fake.commands))
            comments = [command for command in fake.commands if command[:4] == ["/fake/multica", "issue", "comment", "add"]]
            self.assertEqual(len(comments), 1)
            self.assertIn("no board transition", comments[0][-1])

    def test_workflow_analyst_baseline_creates_one_read_only_audit_and_suppresses_duplicate(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica(issue(), issue_list_payload=[issue(status="in_progress")])

            self.assertEqual(self.run_consumer(tmp, fake), 0)
            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))
            state = json.loads((tmp / "state" / "consumer.json").read_text(encoding="utf-8"))
            self.assertEqual(len(state["workflow_analyst_audits"]), 0)
            self.assertIsNotNone(state["last_checked_at"])

    def test_workflow_analyst_active_audit_gates_new_audit(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            active_audit = issue(
                title="Workflow Analyst audit: sprint baseline [audit:active123]",
                status="todo",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T20:00:00Z",
                updated_at="2026-04-13T20:00:00Z",
            )
            fake = FakeMultica(issue(), issue_list_payload=[issue(status="in_progress"), active_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_mid_sprint_cadence_waits_24_hours(self) -> None:
        for now in ["2026-04-13T23:00:00Z", "2026-04-14T01:30:01Z"]:
            with self.subTest(now=now), tempfile.TemporaryDirectory() as td:
                tmp = Path(td)
                previous_audit = issue(
                    title="Analyze current Multica workflow bottlenecks",
                    status="done",
                    assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                    created_at="2026-04-13T01:30:00Z",
                    updated_at="2026-04-13T01:30:00Z",
                )
                fake = FakeMultica(issue(), issue_list_payload=[issue(status="in_progress"), previous_audit])

                self.assertEqual(self.run_consumer(tmp, fake, now=now), 0)

                self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_terminal_event_burst_triggers_after_three_handled_events(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            for index in range(3):
                event(
                    tmp,
                    agent_id=consumer.MULTICA_BUILD_AGENT_ID,
                    summary="Implemented the fix. Validation passed.",
                    run_id=f"run-{index}",
                    completed_at=f"2026-04-13T20:0{index}:00Z",
                    created_at=f"2026-04-13T20:0{index}:30Z",
                )
            state = {
                "handled_events": {
                    f"run-{index}:completed": {
                        "handled_at": f"2026-04-13T20:0{index}:40Z",
                        "issue_id": ISSUE_ID,
                        "target_status": None,
                    }
                    for index in range(3)
                },
                "workflow_analyst_audits": {},
                "last_checked_at": None,
            }
            (tmp / "state").mkdir()
            (tmp / "state" / "consumer.json").write_text(json.dumps(state), encoding="utf-8")
            previous_audit = issue(
                title="Analyze current Multica workflow bottlenecks",
                status="done",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T19:00:00Z",
                updated_at="2026-04-13T19:00:00Z",
            )
            fake = FakeMultica(issue(), issue_list_payload=[issue(status="in_progress"), previous_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_repeated_blocked_trigger_creates_audit(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            state = {
                "handled_events": {
                    "run-a:completed": {
                        "handled_at": "2026-04-13T20:01:00Z",
                        "issue_id": ISSUE_ID,
                        "target_status": "blocked",
                    },
                    "run-b:completed": {
                        "handled_at": "2026-04-13T20:02:00Z",
                        "issue_id": ISSUE_ID,
                        "target_status": "blocked",
                    },
                },
                "workflow_analyst_audits": {},
                "last_checked_at": None,
            }
            (tmp / "state").mkdir()
            (tmp / "state" / "consumer.json").write_text(json.dumps(state), encoding="utf-8")
            previous_audit = issue(
                title="Analyze current Multica workflow bottlenecks",
                status="done",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T19:00:00Z",
                updated_at="2026-04-13T19:00:00Z",
            )
            fake = FakeMultica(issue(status="blocked"), issue_list_payload=[issue(status="blocked"), previous_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_repeated_reopened_trigger_creates_audit(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            state = {
                "handled_events": {
                    "run-a:completed": {
                        "handled_at": "2026-04-13T20:01:00Z",
                        "issue_id": ISSUE_ID,
                        "target_status": "in_progress",
                    },
                    "run-b:completed": {
                        "handled_at": "2026-04-13T20:02:00Z",
                        "issue_id": ISSUE_ID,
                        "target_status": "in_review",
                    },
                },
                "workflow_analyst_audits": {},
                "last_checked_at": None,
            }
            (tmp / "state").mkdir()
            (tmp / "state" / "consumer.json").write_text(json.dumps(state), encoding="utf-8")
            previous_audit = issue(
                title="Analyze current Multica workflow bottlenecks",
                status="done",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T19:00:00Z",
                updated_at="2026-04-13T19:00:00Z",
            )
            fake = FakeMultica(issue(status="in_progress"), issue_list_payload=[issue(status="in_progress"), previous_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_stale_review_card_triggers_audit(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            previous_audit = issue(
                title="Analyze current Multica workflow bottlenecks",
                status="done",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T20:00:00Z",
                updated_at="2026-04-13T20:00:00Z",
            )
            stale_review = issue(
                title="Review stale Multica handoff",
                status="in_review",
                updated_at="2026-04-12T20:59:59Z",
            )
            fake = FakeMultica(issue(), issue_list_payload=[stale_review, previous_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_workflow_analyst_sprint_close_triggers_when_no_active_work_remains(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            previous_audit = issue(
                title="Analyze current Multica workflow bottlenecks",
                status="done",
                assignee_id=consumer.WORKFLOW_ANALYST_AGENT_ID,
                created_at="2026-04-13T20:00:00Z",
                updated_at="2026-04-13T20:00:00Z",
            )
            completed_work = issue(status="done", created_at="2026-04-13T18:00:00Z", updated_at="2026-04-13T18:30:00Z")
            fake = FakeMultica(issue(), issue_list_payload=[completed_work, previous_audit])

            self.assertEqual(self.run_consumer(tmp, fake), 0)

            self.assertFalse(any(command[:3] == ["/fake/multica", "issue", "create"] for command in fake.commands))

    def test_rerun_is_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            event(tmp, agent_id=consumer.MULTICA_BUILD_AGENT_ID, summary="Implemented the fix. Validation passed.")
            fake = FakeMultica(issue())

            self.assertEqual(self.run_consumer(tmp, fake), 0)
            command_count = len(fake.commands)
            self.assertEqual(self.run_consumer(tmp, fake), 0)

            write_commands = [
                command
                for command in fake.commands[command_count:]
                if command[:3] in (
                    ["/fake/multica", "issue", "status"],
                    ["/fake/multica", "issue", "assign"],
                    ["/fake/multica", "issue", "comment"],
                    ["/fake/multica", "issue", "create"],
                    ["/fake/multica", "issue", "update"],
                )
            ]
            self.assertEqual(write_commands, [])
            state = json.loads((tmp / "state" / "consumer.json").read_text(encoding="utf-8"))
            self.assertIn(f"{RUN_ID}:completed", state["handled_events"])


if __name__ == "__main__":
    unittest.main()
