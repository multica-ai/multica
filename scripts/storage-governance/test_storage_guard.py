from __future__ import annotations

import json
import os
import plistlib
import signal
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))

from storage_guard import (  # noqa: E402
    Guard,
    Sample,
    build_capacity_report,
    classify_level,
    parse_daemon_targets,
    SystemCollector,
)


GIB = 1024**3


class LaunchdContractTest(unittest.TestCase):
    def test_guard_ignores_ambient_python_environment(self) -> None:
        plist_path = Path(__file__).with_name("com.multica.storage-guard.plist.example")
        with plist_path.open("rb") as handle:
            program_arguments = plistlib.load(handle)["ProgramArguments"]

        self.assertEqual(program_arguments[:2], ["/usr/bin/python3", "-E"])


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
    def __init__(self, sample: Sample, runner: "FakeRunner"):
        self.sample = sample
        self.runner = runner

    def collect(self) -> Sample:
        return self.sample

    def apply_admission_group(self, action: str, *, daemon_id: str, owner: str) -> dict:
        code, stdout, stderr = self.runner.run(
            ["multica", "daemon", action, "--owner", owner, "--output", "json"],
            tolerate_failure=True,
        )
        response = json.loads(stdout) if stdout else {}
        expected = action == "pause"
        ok = code == 0 and response.get("owner_paused") is expected
        return {
            "ok": ok,
            "daemon_id": daemon_id,
            "pids": [self.sample.daemon_pid] if self.sample.daemon_pid else [],
            "errors": [] if ok else [(stderr or stdout or "admission failed")],
        }


class FakeRunner:
    def __init__(
        self,
        *,
        admission_supported: bool = True,
        resume_supported: bool = True,
        launchagent_stops: bool = True,
    ):
        self.admission_supported = admission_supported
        self.resume_supported = resume_supported
        self.launchagent_stops = launchagent_stops
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
        if argv[:2] == ["launchctl", "print"]:
            return (1, "", "not found") if self.launchagent_stops else (0, "running", "")
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
    def make_sample(
        self,
        free_gib: int,
        timestamp: datetime,
        active_tasks: int = 2,
        admission_pause_owners: tuple[str, ...] = (),
    ) -> Sample:
        return Sample(
            recorded_at=timestamp.isoformat(),
            internal_free_bytes=free_gib * GIB,
            external_free_bytes=1500 * GIB,
            swap_used_bytes=9 * GIB,
            active_task_count=active_tasks,
            daemon_pid=4321,
            daemon_status="running",
            daemon_id="daemon-test",
            admission_pause_owners=admission_pause_owners,
        )

    def test_level1_stops_observer_and_pauses_admission_without_killing_tasks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            runner = FakeRunner()
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            result = Guard(base_config(root), runner, FakeCollector(self.make_sample(24, now), runner)).run_once()

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
            Guard(cfg, runner, FakeCollector(self.make_sample(24, now), runner)).run_once()
            self.assertEqual(runner.signals, [])
            self.assertTrue(
                any(
                    "legacy_sigstop_disabled:unsafe" in action
                    for action in json.loads(Path(cfg["state_path"]).read_text())["last_actions"]
                )
            )

    def test_old_daemon_never_uses_sigstop_even_when_idle(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            runner = FakeRunner(admission_supported=False)
            result = Guard(cfg, runner, FakeCollector(self.make_sample(24, now, active_tasks=0), runner)).run_once()
            self.assertEqual(runner.signals, [])
            self.assertEqual(result["level"], 1)
            self.assertEqual(json.loads(Path(cfg["state_path"]).read_text())["enforcement_status"], "failed")

    def test_persisted_sigstop_intent_is_reconciled_after_space_recovers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            Path(cfg["state_path"]).write_text(
                json.dumps(
                    {
                        "level": 0,
                        "legacy_daemon_sigstop_intent": True,
                        "legacy_daemon_sigstopped": False,
                        "legacy_daemon_pid": 4321,
                        "legacy_daemon_command": "/opt/homebrew/bin/multica daemon start --foreground",
                    }
                ),
                encoding="utf-8",
            )
            runner = FakeRunner(admission_supported=False)
            result = Guard(
                cfg,
                runner,
                FakeCollector(self.make_sample(30, datetime(2026, 8, 3, tzinfo=timezone.utc)), runner),
            ).run_once()
            self.assertIn((4321, signal.SIGCONT), runner.signals)
            self.assertEqual(result["level"], 0)

    def test_daemon_owner_reconciles_when_local_pause_state_was_not_committed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            runner = FakeRunner()
            result = Guard(
                cfg,
                runner,
                FakeCollector(
                    self.make_sample(
                        30,
                        datetime(2026, 8, 3, tzinfo=timezone.utc),
                        admission_pause_owners=("storage-guard",),
                    ),
                    runner,
                ),
            ).run_once()
            self.assertIn(
                ("multica", "daemon", "resume", "--owner", "storage-guard", "--output", "json"),
                runner.calls,
            )
            self.assertEqual(result["level"], 0)

    def test_level2_launchagent_failure_is_not_reported_as_verified(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            runner = FakeRunner(launchagent_stops=False)
            Guard(
                cfg,
                runner,
                FakeCollector(self.make_sample(17, datetime(2026, 8, 3, tzinfo=timezone.utc)), runner),
            ).run_once()
            state = json.loads(Path(cfg["state_path"]).read_text())
            self.assertEqual(state["enforcement_status"], "failed")
            self.assertTrue(any("launchagent_stop_failed" in action for action in state["last_actions"]))

    def test_resume_failure_remains_pending_and_retries(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            first = FakeRunner()
            Guard(cfg, first, FakeCollector(self.make_sample(24, now), first)).run_once()

            failing = FakeRunner(resume_supported=False)
            result = Guard(cfg, failing, FakeCollector(self.make_sample(30, now + timedelta(minutes=1)), failing)).run_once()
            self.assertEqual(result["level"], 1)
            state = json.loads(Path(cfg["state_path"]).read_text())
            self.assertTrue(state["resume_pending"])
            self.assertEqual(state["enforcement_status"], "failed")

    def test_level2_pauses_explicit_nonproduction_jobs_and_sends_lark_alert(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            runner = FakeRunner()
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            result = Guard(base_config(root), runner, FakeCollector(self.make_sample(17, now), runner)).run_once()

            self.assertEqual(result["level"], 2)
            self.assertIn(("launchctl", "disable", "gui/501/com.example.nonproduction"), runner.calls)
            self.assertTrue(any(call and call[0] == "lark-cli" for call in runner.calls))

    def test_every_run_appends_one_machine_readable_sample(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg = base_config(root)
            now = datetime(2026, 8, 3, tzinfo=timezone.utc)
            runner = FakeRunner()
            Guard(cfg, runner, FakeCollector(self.make_sample(30, now), runner)).run_once()
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

    def test_one_missing_category_sample_uses_field_coverage_instead_of_poisoning_window(self) -> None:
        start = datetime(2026, 8, 3, tzinfo=timezone.utc)
        samples = []
        for hour in range(50):
            samples.append(
                {
                    "recorded_at": (start + timedelta(hours=hour)).isoformat(),
                    "internal_free_bytes": (80 - hour) * GIB,
                    "cursor_bytes": None if hour == 25 else hour * GIB,
                }
            )
        report = build_capacity_report(
            samples,
            safety_floor_bytes=25 * GIB,
            burst_reserve_bytes=10 * GIB,
            minimum_hours=48,
            expected_interval_seconds=3600,
            required_growth_fields=["cursor_bytes"],
            required_field_max_gap_seconds=3 * 3600,
        )
        self.assertEqual(report["status"], "READY")
        self.assertGreater(report["field_coverage_ratio"]["cursor_bytes"], 0.9)


class SystemCollectorTest(unittest.TestCase):
    def test_growth_metrics_respect_shared_scan_budget(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workspace = Path(tmp) / "workspaces"
            workspace.mkdir()
            (workspace / "payload.bin").write_bytes(b"payload")
            collector = SystemCollector(
                {
                    "growth_scan_budget_seconds": 0,
                    "workspace_roots": [str(workspace)],
                    "logs_paths": [],
                },
                mock.Mock(),
            )

            self.assertIsNone(collector.growth_metrics()["workspace_total_bytes"])

    def test_fast_collect_does_not_scan_growth_directories(self) -> None:
        runner = mock.Mock()
        runner.run.side_effect = [
            (0, json.dumps({"status": "running", "pid": 1, "active_task_count": 0}), ""),
            (0, "vm.swapusage: total = 0.00M  used = 0.00M  free = 0.00M", ""),
        ]
        collector = SystemCollector({"internal_path": "/", "external_path": "/"}, runner)
        with mock.patch.object(collector, "discover_daemons", return_value=([], [])), mock.patch.object(
            collector, "growth_metrics", side_effect=AssertionError("slow scan on fast path")
        ):
            sample = collector.collect()
        self.assertEqual(sample.active_task_count, 0)

    def test_stale_green_retention_report_is_not_capacity_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            report = root / "report.json"
            report.write_text(
                json.dumps(
                    {
                        "status": "green",
                        "recorded_at": "2000-01-01T00:00:00+00:00",
                        "gc_candidates": [{"eligible": True, "details": {"size_bytes": 123}}],
                    }
                ),
                encoding="utf-8",
            )
            collector = SystemCollector(
                {
                    "retention_report_path": str(report),
                    "retention_report_max_age_seconds": 1800,
                    "workspace_roots": [],
                    "logs_paths": [],
                },
                mock.Mock(),
            )
            metrics = collector.growth_metrics()
            self.assertIsNone(metrics["workspace_gc_eligible_bytes"])

    def test_future_green_retention_report_is_not_capacity_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            report = root / "report.json"
            report.write_text(
                json.dumps(
                    {
                        "status": "green",
                        "recorded_at": "2099-01-01T00:00:00+00:00",
                        "gc_candidates": [{"eligible": True, "details": {"size_bytes": 123}}],
                    }
                ),
                encoding="utf-8",
            )
            collector = SystemCollector(
                {
                    "retention_report_path": str(report),
                    "retention_report_max_age_seconds": 1800,
                    "workspace_roots": [],
                    "logs_paths": [],
                },
                mock.Mock(),
            )
            self.assertIsNone(collector.growth_metrics()["workspace_gc_eligible_bytes"])

    def test_workspace_categories_conserve_file_payload_bytes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            workspace = root / "workspaces"
            workspace.mkdir()
            (workspace / "payload.bin").write_bytes(b"x" * 100)
            report = root / "report.json"
            report.write_text(
                json.dumps(
                    {
                        "status": "green",
                        "recorded_at": datetime.now(timezone.utc).isoformat(),
                        "gc_candidates": [{"eligible": True, "details": {"size_bytes": 40}}],
                    }
                ),
                encoding="utf-8",
            )
            collector = SystemCollector(
                {
                    "retention_report_path": str(report),
                    "retention_report_max_age_seconds": 1800,
                    "workspace_roots": [str(workspace)],
                    "logs_paths": [],
                },
                mock.Mock(),
            )
            metrics = collector.growth_metrics()
            total = metrics["workspace_total_bytes"]
            categories = sum(
                int(metrics[key] or 0)
                for key in (
                    "workspace_gc_eligible_bytes",
                    "workspace_inflight_bytes",
                    "workspace_gc_backlog_bytes",
                    "workspace_unclassified_bytes",
                )
            )
            self.assertEqual(categories, total)

    def test_stale_observation_is_reported_separately_from_delete_eligibility(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            workspace = root / "workspaces"
            workspace.mkdir()
            (workspace / "payload.bin").write_bytes(b"x" * 100)
            recorded_at = datetime.now(timezone.utc).isoformat()
            retention = root / "retention.json"
            retention.write_text(
                json.dumps(
                    {
                        "status": "green",
                        "recorded_at": recorded_at,
                        "gc_candidates": [
                            {
                                "path": "/tmp/stale",
                                "eligible": False,
                                "details": {"size_bytes": 40, "age_seconds": 8 * 86400},
                                "reasons": ["issue is not terminal"],
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            stale = root / "stale.json"
            stale.write_text(
                json.dumps(
                    {
                        "status": "green",
                        "recorded_at": recorded_at,
                        "stale_workdir_candidates": [{"path": "/tmp/stale", "size_bytes": 40}],
                    }
                ),
                encoding="utf-8",
            )
            metrics = SystemCollector(
                {
                    "retention_report_path": str(retention),
                    "workspace_stale_report_path": str(stale),
                    "retention_report_max_age_seconds": 1800,
                    "workspace_roots": [str(workspace)],
                    "logs_paths": [],
                },
                mock.Mock(),
            ).growth_metrics()
            self.assertEqual(metrics["workspace_gc_eligible_bytes"], 40)
            self.assertEqual(metrics["workspace_gc_deletion_eligible_bytes"], 0)


class DaemonGroupTest(unittest.TestCase):
    class Runner:
        def __init__(self) -> None:
            uid = os.getuid()
            self.processes = "\n".join(
                [
                    f"{uid} 100 /opt/old/multica daemon start --foreground",
                    f"{uid} 200 /Applications/Multica.app/bin/multica daemon start --foreground --profile desktop-api.multica.ai",
                    f"{uid} 300 /opt/old/multica daemon start --foreground --profile trace",
                ]
            )
            self.statuses = {
                ("/opt/old/multica", None): {
                    "pid": 100,
                    "daemon_id": "shared",
                    "active_task_count": 0,
                    "admission_paused": True,
                    "admission_pause_owners": ["post-reboot-canary"],
                },
                ("/Applications/Multica.app/bin/multica", "desktop-api.multica.ai"): {
                    "pid": 200,
                    "daemon_id": "shared",
                    "active_task_count": 4,
                    "admission_paused": False,
                    "admission_pause_owners": [],
                },
                ("/opt/old/multica", "trace"): {
                    "pid": 300,
                    "daemon_id": "other",
                    "active_task_count": 1,
                    "admission_paused": False,
                    "admission_pause_owners": [],
                },
            }
            self.actions: list[tuple[tuple[str, str | None], str]] = []

        @staticmethod
        def key(argv: list[str]) -> tuple[str, str | None]:
            profile = argv[argv.index("--profile") + 1] if "--profile" in argv else None
            return argv[0], profile

        def run(self, argv: list[str], *, tolerate_failure: bool = False) -> tuple[int, str, str]:
            if argv == ["/bin/ps", "-axo", "uid=,pid=,command="]:
                return 0, self.processes, ""
            key = self.key(argv)
            action = argv[argv.index("daemon") + 1]
            if action == "status":
                return 0, json.dumps(self.statuses[key]), ""
            self.actions.append((key, action))
            owners = set(self.statuses[key].get("admission_pause_owners") or [])
            if action == "pause":
                owners.add("storage-guard")
            else:
                owners.discard("storage-guard")
            self.statuses[key]["admission_pause_owners"] = sorted(owners)
            self.statuses[key]["admission_paused"] = bool(owners)
            return 0, json.dumps({"owner_paused": action == "pause"}), ""

    def test_broadcasts_only_to_processes_with_matching_daemon_id(self) -> None:
        runner = self.Runner()
        result = SystemCollector({"uid": os.getuid()}, runner).apply_admission_group(
            "pause", daemon_id="shared", owner="storage-guard"
        )
        self.assertTrue(result["ok"], result["errors"])
        self.assertEqual(result["pids"], [100, 200])
        self.assertEqual([key for key, _ in runner.actions], [("/opt/old/multica", None), ("/Applications/Multica.app/bin/multica", "desktop-api.multica.ai")])
        self.assertNotIn("storage-guard", runner.statuses[("/opt/old/multica", "trace")]["admission_pause_owners"])

    def test_parser_accepts_profile_after_start(self) -> None:
        targets = parse_daemon_targets(self.Runner().processes, uid=os.getuid())
        self.assertEqual([(target.pid, target.profile) for target in targets], [(100, None), (200, "desktop-api.multica.ai"), (300, "trace")])


if __name__ == "__main__":
    unittest.main()
