from __future__ import annotations

import argparse
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).with_name("multica_orchestrator_watcher.py")
SPEC = importlib.util.spec_from_file_location("multica_orchestrator_watcher", SCRIPT_PATH)
watcher = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(watcher)


PROJECT_ID = "072b1862-109e-43c3-98d8-c18515961b93"
ISSUE_ID = "issue-1"
RUN_ID = "run-1"


def issue(project_id: str = PROJECT_ID) -> dict[str, object]:
    return {
        "id": ISSUE_ID,
        "project_id": project_id,
        "identifier": "FT-1",
        "title": "Do the deterministic thing",
    }


def run(status: str, *, result: dict[str, object] | None = None) -> dict[str, object]:
    return {
        "id": RUN_ID,
        "issue_id": ISSUE_ID,
        "agent_id": "agent-1",
        "runtime_id": "runtime-1",
        "status": status,
        "result": result if result is not None else {"output": "done"},
        "started_at": "2026-04-12T00:00:00Z",
        "completed_at": "2026-04-12T00:01:00Z" if status in watcher.TERMINAL_STATUSES else None,
    }


class FakeMultica:
    def __init__(
        self,
        runs: list[dict[str, object]],
        messages: list[dict[str, object]] | None = None,
        messages_error: Exception | None = None,
    ):
        self.runs = runs
        self.messages = messages or []
        self.messages_error = messages_error
        self.commands: list[list[str]] = []

    def run_json(self, command: list[str], *, timeout: int = 30) -> object:
        self.commands.append(command)
        if command[:6] == ["/fake/multica", "issue", "list", "--project", PROJECT_ID, "--output"]:
            return [issue()]
        if command == ["/fake/multica", "issue", "runs", ISSUE_ID, "--output", "json"]:
            return self.runs
        if command == ["/fake/multica", "issue", "run-messages", RUN_ID, "--output", "json"]:
            if self.messages_error is not None:
                raise self.messages_error
            return self.messages
        raise AssertionError(f"unexpected command: {command}")


def args_for(tmp: Path, *, no_notify: bool = True) -> argparse.Namespace:
    return argparse.Namespace(
        project_id=PROJECT_ID,
        state_path=tmp / "state" / "watcher_state.json",
        inbox_dir=tmp / "inbox",
        multica="/fake/multica",
        no_notify=no_notify,
        once=True,
    )


def inbox_items(tmp: Path) -> list[Path]:
    inbox = tmp / "inbox"
    if not inbox.exists():
        return []
    return sorted(inbox.glob("*.md"))


class WatcherTests(unittest.TestCase):
    def test_first_terminal_run_creates_exactly_one_inbox_item(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("completed")])
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(watcher, "notify"):
                self.assertEqual(watcher.check_once(args_for(tmp)), 0)

            items = inbox_items(tmp)
            self.assertEqual(len(items), 1)
            self.assertIn("Run status: `completed`", items[0].read_text(encoding="utf-8"))
            self.assertIn("done", items[0].read_text(encoding="utf-8"))
            state = json.loads((tmp / "state" / "watcher_state.json").read_text(encoding="utf-8"))
            self.assertEqual(state["notified_runs"], {RUN_ID: "completed"})

    def test_rerun_does_not_duplicate_notification_for_same_run_status(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("completed")])
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(watcher, "notify"):
                self.assertEqual(watcher.check_once(args_for(tmp)), 0)
                self.assertEqual(watcher.check_once(args_for(tmp)), 0)

            self.assertEqual(len(inbox_items(tmp)), 1)

    def test_running_task_is_tracked_but_does_not_notify(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("running")])
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(watcher, "notify") as notify:
                self.assertEqual(watcher.check_once(args_for(tmp)), 0)

            self.assertEqual(inbox_items(tmp), [])
            notify.assert_not_called()
            state = json.loads((tmp / "state" / "watcher_state.json").read_text(encoding="utf-8"))
            self.assertEqual(state["active_runs"][RUN_ID]["status"], "running")

    def test_terminal_statuses_notify(self) -> None:
        for status in ["failed", "cancelled", "completed"]:
            with self.subTest(status=status), tempfile.TemporaryDirectory() as td:
                tmp = Path(td)
                fake = FakeMultica([run(status)])
                with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(watcher, "notify") as notify:
                    self.assertEqual(watcher.check_once(args_for(tmp, no_notify=False)), 0)

                self.assertEqual(len(inbox_items(tmp)), 1)
                notify.assert_called_once()

    def test_run_message_fallback_when_result_output_missing(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica(
                [run("completed", result={})],
                messages=[{"content": ""}, {"output": "fallback from message output"}],
            )
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(watcher, "notify"):
                self.assertEqual(watcher.check_once(args_for(tmp)), 0)

            items = inbox_items(tmp)
            self.assertEqual(len(items), 1)
            self.assertIn("fallback from message output", items[0].read_text(encoding="utf-8"))

    def test_missing_result_output_and_run_messages_failure_does_not_notify(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("completed", result={})], messages_error=RuntimeError("message fetch failed"))
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(
                watcher, "notify", side_effect=AssertionError("notify should not be called")
            ), mock.patch("sys.stderr") as stderr:
                self.assertEqual(watcher.check_once(args_for(tmp, no_notify=False)), 0)

            self.assertEqual(inbox_items(tmp), [])
            state = json.loads((tmp / "state" / "watcher_state.json").read_text(encoding="utf-8"))
            self.assertNotIn(RUN_ID, state["notified_runs"])
            self.assertIn("failed to fetch messages", "".join(call.args[0] for call in stderr.write.call_args_list))

    def test_result_output_and_run_messages_failure_still_notifies(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("completed")], messages_error=RuntimeError("message fetch failed"))
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(
                watcher, "notify"
            ) as notify, mock.patch("sys.stderr") as stderr:
                self.assertEqual(watcher.check_once(args_for(tmp, no_notify=False)), 0)

            items = inbox_items(tmp)
            self.assertEqual(len(items), 1)
            self.assertIn("done", items[0].read_text(encoding="utf-8"))
            notify.assert_called_once()
            self.assertIn("failed to fetch messages", "".join(call.args[0] for call in stderr.write.call_args_list))
            state = json.loads((tmp / "state" / "watcher_state.json").read_text(encoding="utf-8"))
            self.assertEqual(state["notified_runs"], {RUN_ID: "completed"})

    def test_no_notify_avoids_osascript(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tmp = Path(td)
            fake = FakeMultica([run("completed")])
            with mock.patch.object(watcher, "run_json", fake.run_json), mock.patch.object(
                watcher, "notify", side_effect=AssertionError("notify should not be called")
            ):
                self.assertEqual(watcher.check_once(args_for(tmp, no_notify=True)), 0)

            self.assertEqual(len(inbox_items(tmp)), 1)


if __name__ == "__main__":
    unittest.main()
