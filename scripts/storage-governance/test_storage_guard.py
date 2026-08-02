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
    def __init__(self, *, admission_supported: bool = True):
        self.admission_supported = admission_supported
        self.calls: list[tuple[str, ...]] = []
        self.signals: list[tuple[int, int]] = []

    def run(self, argv: list[str], *, tolerate_failure: bool = False) -> tuple[int, str, str]:
        self.calls.append(tuple(argv))
        if argv[:3] == ["multica", "daemon", "pause"] and not self.admission_supported:
            return 1, "", "endpoint returned 404"
        if argv[:3] == ["multica", "daemon", "status"]:
            return 0, json.dumps({"pid": 4321, "status": "running", "active_task_count": 2}), ""
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
    def make_sample(self, free_gib: int, timestamp: datetime) -> Sample:
        return Sample(
            recorded_at=timestamp.isoformat(),
            internal_free_bytes=free_gib * GIB,
            external_free_bytes=1500 * GIB,
            swap_used_bytes=9 * GIB,
            active_task_count=2,
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
            self.assertIn(("multica", "daemon", "pause", "--output", "json"), runner.calls)
            self.assertEqual(runner.signals, [])
            state = json.loads(Path(base_config(root)["state_path"]).read_text())
            self.assertEqual(state["level"], 1)
            self.assertFalse(state["legacy_daemon_sigstopped"])

    def test_old_daemon_falls_back_to_sigstop_and_recovery_continues_it(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            runner = FakeRunner(admission_supported=False)
            Guard(cfg, runner, FakeCollector(self.make_sample(24, now))).run_once()
            self.assertEqual(runner.signals, [(4321, signal.SIGSTOP)])

            runner.admission_supported = True
            Guard(cfg, runner, FakeCollector(self.make_sample(30, now + timedelta(minutes=1)))).run_once()
            self.assertIn((4321, signal.SIGCONT), runner.signals)
            self.assertIn(("multica", "daemon", "resume", "--output", "json"), runner.calls)

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
        self.assertEqual(report["days_remaining"], 0.0)


if __name__ == "__main__":
    unittest.main()
