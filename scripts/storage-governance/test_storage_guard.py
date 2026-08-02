from __future__ import annotations

import json
import signal
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from storage_guard import (  # noqa: E402
    Guard,
    Sample,
    build_capacity_report,
    classify_level,
)


GIB = 1024**3


def base_config(root: Path) -> dict:
    return {
        "internal_path": "/",
        "external_path": "/Volumes/MacMini-HotSSD",
        "level1_free_gib": 25,
        "level1_clear_gib": 28,
        "level2_free_gib": 18,
        "level2_clear_gib": 21,
        "external_min_free_gib": 100,
        "safety_floor_gib": 25,
        "burst_reserve_gib": 10,
        "minimum_observation_hours": 48,
        "observer_labels": ["ai.multica.ws2512.m0-shadow-observer"],
        "nonproduction_launchagents": ["com.example.nonproduction"],
        "lark_open_id": "ou_test",
        "alert_cooldown_seconds": 3600,
        "legacy_sigstop_fallback": True,
        "state_path": str(root / "state.json"),
        "metrics_path": str(root / "metrics.jsonl"),
        "capacity_report_path": str(root / "capacity.json"),
    }


class FakeCollector:
    def __init__(self, sample: Sample):
        self.sample = sample

    def collect(self) -> Sample:
        return self.sample


class FakeRunner:
    def __init__(self, *, admission_supported: bool = True, resume_supported: bool = True):
        self.admission_supported = admission_supported
        self.resume_supported = resume_supported
        self.calls: list[tuple[str, ...]] = []
        self.signals: list[tuple[int, int]] = []

    def run(self, argv: list[str], *, tolerate_failure: bool = False) -> tuple[int, str, str]:
        self.calls.append(tuple(argv))
        if argv[:3] == ["multica", "daemon", "pause"]:
            if not self.admission_supported:
                return 1, "", "endpoint returned 404"
            return 0, json.dumps({"owner_paused": True, "admission_paused": True}), ""
        if argv[:3] == ["multica", "daemon", "resume"]:
            if not self.resume_supported:
                return 1, "", "endpoint unavailable"
            return 0, json.dumps({"owner_paused": False, "admission_paused": False}), ""
        if argv[:3] == ["multica", "daemon", "status"]:
            return 0, json.dumps({"pid": 4321, "status": "running", "active_task_count": 2}), ""
        if argv[:3] == ["/bin/ps", "-p", "4321"]:
            return 0, "/opt/homebrew/bin/multica daemon start --foreground\n", ""
        if argv and argv[0] == "lark-cli":
            return 0, json.dumps({"data": {"message_id": "om_test"}}), ""
        return 0, "{}", ""

    def signal(self, pid: int, sig: int) -> None:
        self.signals.append((pid, sig))


class LevelClassificationTest(unittest.TestCase):
    def test_hysteresis_prevents_flapping(self) -> None:
        self.assertEqual(classify_level(30 * GIB, previous=0, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 0)
        self.assertEqual(classify_level(24 * GIB, previous=0, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 1)
        self.assertEqual(classify_level(26 * GIB, previous=1, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 1)
        self.assertEqual(classify_level(29 * GIB, previous=1, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 0)
        self.assertEqual(classify_level(17 * GIB, previous=1, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 2)
        self.assertEqual(classify_level(19 * GIB, previous=2, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 2)
        self.assertEqual(classify_level(22 * GIB, previous=2, level1=25 * GIB, level1_clear=28 * GIB, level2=18 * GIB, level2_clear=21 * GIB), 1)


class GuardActionTest(unittest.TestCase):
    def make_sample(self, free_gib: int, timestamp: datetime, active_tasks: int = 2) -> Sample:
        return Sample(
            recorded_at=timestamp.isoformat(),
            internal_free_bytes=free_gib * GIB,
            external_free_bytes=1500 * GIB,
            swap_used_bytes=9 * GIB,
            active_task_count=active_tasks,
            daemon_pid=4321,
            daemon_status="running",
        )

    def test_level1_stops_observer_and_pauses_admission_without_killing_tasks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            runner = FakeRunner()
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            result = Guard(base_config(root), runner, FakeCollector(self.make_sample(24, now))).run_once()

            self.assertEqual(result["level"], 1)
            self.assertIn(("launchctl", "disable", "gui/501/ai.multica.ws2512.m0-shadow-observer"), runner.calls)
            self.assertIn(("launchctl", "bootout", "gui/501/ai.multica.ws2512.m0-shadow-observer"), runner.calls)
            self.assertIn(("multica", "daemon", "pause", "--owner", "storage-guard", "--output", "json"), runner.calls)
            self.assertEqual(runner.signals, [])
            state = json.loads(Path(base_config(root)["state_path"]).read_text())
            self.assertEqual(state["level"], 1)
            self.assertFalse(state["legacy_daemon_sigstopped"])

    def test_old_daemon_refuses_sigstop_with_active_tasks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            runner = FakeRunner(admission_supported=False)
            Guard(cfg, runner, FakeCollector(self.make_sample(24, now))).run_once()
            self.assertEqual(runner.signals, [])
            self.assertTrue(any("legacy_sigstop_refused:active_tasks" in action for action in json.loads(Path(cfg["state_path"]).read_text())["last_actions"]))

    def test_idle_old_daemon_persists_intent_before_sigstop_and_recovers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            runner = FakeRunner(admission_supported=False)
            Guard(cfg, runner, FakeCollector(self.make_sample(24, now, active_tasks=0))).run_once()
            self.assertEqual(runner.signals, [(4321, signal.SIGSTOP)])

            result = Guard(cfg, runner, FakeCollector(self.make_sample(30, now + timedelta(minutes=1)))).run_once()
            self.assertIn((4321, signal.SIGCONT), runner.signals)
            self.assertNotIn(("multica", "daemon", "resume", "--owner", "storage-guard", "--output", "json"), runner.calls)
            self.assertEqual(result["level"], 0)

    def test_resume_failure_remains_pending_and_retries(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            first = FakeRunner()
            Guard(cfg, first, FakeCollector(self.make_sample(24, now))).run_once()

            failing = FakeRunner(resume_supported=False)
            result = Guard(cfg, failing, FakeCollector(self.make_sample(30, now + timedelta(minutes=1)))).run_once()
            self.assertEqual(result["level"], 1)
            state = json.loads(Path(cfg["state_path"]).read_text())
            self.assertTrue(state["resume_pending"])
            self.assertEqual(state["enforcement_status"], "failed")

    def test_level2_pauses_explicit_nonproduction_jobs_and_sends_lark_alert(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            runner = FakeRunner()
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            result = Guard(base_config(root), runner, FakeCollector(self.make_sample(17, now))).run_once()

            self.assertEqual(result["level"], 2)
            self.assertIn(("launchctl", "disable", "gui/501/com.example.nonproduction"), runner.calls)
            self.assertTrue(any(call and call[0] == "lark-cli" for call in runner.calls))

    def test_every_run_appends_one_machine_readable_sample(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            Guard(cfg, FakeRunner(), FakeCollector(self.make_sample(30, now))).run_once()
            lines = Path(cfg["metrics_path"]).read_text().splitlines()
            self.assertEqual(len(lines), 1)
            self.assertEqual(json.loads(lines[0])["internal_free_bytes"], 30 * GIB)


class CapacityReportTest(unittest.TestCase):
    def test_report_is_inconclusive_before_48_hours(self) -> None:
        start = datetime(2026, 8, 3, tzinfo=timezone.utc)
        samples = [
            {"recorded_at": start.isoformat(), "internal_free_bytes": 40 * GIB},
            {"recorded_at": (start + timedelta(hours=24)).isoformat(), "internal_free_bytes": 36 * GIB},
        ]
        report = build_capacity_report(samples, safety_floor_bytes=25 * GIB, burst_reserve_bytes=10 * GIB, minimum_hours=48)
        self.assertEqual(report["status"], "INCONCLUSIVE")
        self.assertIsNone(report["days_remaining"])

    def test_two_samples_across_48_hours_do_not_fake_ready_coverage(self) -> None:
        start = datetime(2026, 8, 3, tzinfo=timezone.utc)
        report = build_capacity_report(
            [
                {"recorded_at": start.isoformat(), "internal_free_bytes": 40 * GIB},
                {"recorded_at": (start + timedelta(hours=48)).isoformat(), "internal_free_bytes": 39 * GIB},
            ],
            safety_floor_bytes=25 * GIB,
            burst_reserve_bytes=10 * GIB,
            minimum_hours=48,
            expected_interval_seconds=3600,
        )
        self.assertEqual(report["status"], "INCONCLUSIVE")
        self.assertLess(report["coverage_ratio"], 0.1)

    def test_report_uses_p95_observed_hourly_growth_and_reserves(self) -> None:
        start = datetime(2026, 8, 3, tzinfo=timezone.utc)
        samples = []
        free = 80 * GIB
        for hour in range(50):
            samples.append({"recorded_at": (start + timedelta(hours=hour)).isoformat(), "internal_free_bytes": free})
            free -= 1 * GIB
        report = build_capacity_report(samples, safety_floor_bytes=25 * GIB, burst_reserve_bytes=10 * GIB, minimum_hours=48)
        self.assertEqual(report["status"], "READY")
        self.assertAlmostEqual(report["p95_growth_bytes_per_hour"], float(GIB))
        self.assertAlmostEqual(report["peak_growth_bytes_per_hour"], float(GIB))
        self.assertEqual(report["days_remaining"], 0.0)


if __name__ == "__main__":
    unittest.main()
