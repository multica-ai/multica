from __future__ import annotations

import fcntl
import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))

from retention_worker import (  # noqa: E402
    ArchiveError,
    ArchiveManager,
    Canary,
    ExternalVolumeGuard,
    GCEvaluator,
    SingleInstanceLock,
    _latest_mtime_size_and_unsafe_symlinks,
    audit_electron_updaters,
    build_workspace_stale_report,
    consumed_approval_tokens,
    main,
    maybe_run_electron_updater_audit,
    run_worker,
    send_alert,
    tree_manifest,
    verify_cron_bridge,
    write_cron_bridge_receipt,
)


class FakeIssueClient:
    def __init__(self, issue: dict, runs: list[dict], children: list[dict] | None = None):
        self.issue = issue
        self.runs = runs
        self.children = children or []

    def get_issue(self, issue_id: str) -> dict:
        return dict(self.issue)

    def get_runs(self, issue_id: str) -> list[dict]:
        return list(self.runs)

    def get_children(self, issue_id: str) -> list[dict]:
        return list(self.children)


def make_tree(root: Path) -> None:
    (root / "nested" / "deeper").mkdir(parents=True)
    (root / "root.txt").write_text("root payload\n", encoding="utf-8")
    (root / "nested" / "data.bin").write_bytes(bytes(range(64)))
    (root / "nested" / "deeper" / "empty").write_bytes(b"")


class CanaryTest(unittest.TestCase):
    def test_uuid_is_checked_before_writing_representative_tree(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp)
            canary = Canary(
                destination,
                expected_uuid="expected",
                min_free_bytes=1,
                uuid_reader=lambda _: "wrong",
                free_bytes_reader=lambda _: 10_000,
            )
            with self.assertRaises(ArchiveError):
                canary.run()
            self.assertFalse((destination / ".multica-storage-canary").exists())

    def test_external_low_water_is_red_before_writing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp)
            with self.assertRaises(ArchiveError):
                Canary(
                    destination,
                    expected_uuid="volume-uuid",
                    min_free_bytes=100,
                    uuid_reader=lambda _: "volume-uuid",
                    free_bytes_reader=lambda _: 99,
                ).run()
            self.assertFalse((destination / ".multica-storage-canary").exists())

    def test_writes_and_verifies_nested_tree_with_counts_bytes_and_hashes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp)
            result = Canary(
                destination,
                expected_uuid="volume-uuid",
                min_free_bytes=1,
                uuid_reader=lambda _: "volume-uuid",
                free_bytes_reader=lambda _: 10_000,
            ).run()
            self.assertEqual(result["status"], "green")
            self.assertGreaterEqual(result["manifest"]["file_count"], 3)
            self.assertGreater(result["manifest"]["total_bytes"], 0)
            self.assertGreaterEqual(len(result["manifest"]["sample_hashes"]), 2)
            self.assertTrue(Path(result["path"], "nested", "deeper", "empty").is_file())

    def test_postflight_volume_rebind_failure_prevents_green_marker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp)

            def rebound() -> dict:
                raise ArchiveError("archive volume UUID changed before commit")

            with self.assertRaisesRegex(ArchiveError, "UUID changed"):
                Canary(
                    destination,
                    expected_uuid="volume-uuid",
                    min_free_bytes=1,
                    uuid_reader=lambda _: "volume-uuid",
                    free_bytes_reader=lambda _: 10_000,
                    postflight=rebound,
                ).run()
            self.assertEqual(list(destination.glob("**/CANARY.json")), [])


class ArchiveTransactionTest(unittest.TestCase):
    def test_manifest_hashes_every_file_not_only_samples(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for index in range(17):
                (root / ("f%02d.bin" % index)).write_bytes(bytes([index]) * 32)
            before = tree_manifest(root)
            target = root / "f08.bin"
            stat = target.stat()
            target.write_bytes(b"x" * 32)
            import os

            os.utime(target, ns=(stat.st_atime_ns, stat.st_mtime_ns))
            after = tree_manifest(root)
            self.assertEqual(before["files"], after["files"])
            self.assertNotEqual(before["content_hashes"], after["content_hashes"])

    def test_source_delete_is_refused_without_a_shared_producer_lease(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            destination = root / "archive"
            source.mkdir()
            make_tree(source)

            with self.assertRaisesRegex(ArchiveError, "producer lease"):
                ArchiveManager(destination).archive(source, "candidate", delete_source=True)
            self.assertTrue(source.exists())

    def test_every_injected_failure_preserves_source(self) -> None:
        for phase in ("after_copy", "after_verify", "after_rename", "after_complete"):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                source = root / "source"
                source.mkdir()
                make_tree(source)
                with self.assertRaises(ArchiveError):
                    ArchiveManager(root / "archive", fail_at=phase).archive(
                        source, "candidate", delete_source=False
                    )
                self.assertTrue(source.exists())
                self.assertEqual(tree_manifest(source)["file_count"], 3)

    def test_committed_archive_is_rehashed_after_complete_marker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            source.mkdir()
            make_tree(source)

            def corrupt_committed(final: Path) -> None:
                (final / "root.txt").write_text("corrupt\n", encoding="utf-8")

            with self.assertRaisesRegex(ArchiveError, "committed archive changed"):
                ArchiveManager(root / "archive").archive(
                    source,
                    "candidate",
                    delete_source=False,
                    post_commit_hook=corrupt_committed,
                )
            self.assertTrue(source.exists())
            self.assertEqual(list((root / "archive").glob("*/COMPLETE.json")), [])
            self.assertEqual(consumed_approval_tokens(root / "archive"), set())

    def test_single_instance_lock_is_nonblocking(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            lock_path = Path(tmp) / "worker.lock"
            with SingleInstanceLock(lock_path):
                descriptor = lock_path.open("a+")
                try:
                    with self.assertRaises(BlockingIOError):
                        fcntl.flock(descriptor.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                finally:
                    descriptor.close()


class GCGateTest(unittest.TestCase):
    def make_candidate(self, root: Path, *, completed: datetime) -> tuple[Path, str, str]:
        workspace_id = "workspace-uuid"
        issue_id = "issue-uuid"
        task_id = "abcdef12-0000-0000-0000-000000000000"
        candidate = root / workspace_id / task_id[:8]
        (candidate / "workdir" / ".multica").mkdir(parents=True)
        (candidate / ".gc_meta.json").write_text(
            json.dumps(
                {
                    "kind": "issue",
                    "issue_id": issue_id,
                    "workspace_id": workspace_id,
                    "completed_at": completed.isoformat(),
                }
            ),
            encoding="utf-8",
        )
        (candidate / "workdir" / ".multica" / "daemon_task_context.json").write_text(
            json.dumps({"managed_by": "multica-daemon-task", "issue_id": issue_id}),
            encoding="utf-8",
        )
        (candidate / "payload.txt").write_text("payload", encoding="utf-8")
        old = completed.timestamp()
        for path in sorted(candidate.rglob("*"), reverse=True):
            path.touch() if path.is_file() else None
            if path.exists():
                import os

                os.utime(path, (old, old), follow_symlinks=False)
        import os

        os.utime(candidate, (old, old))
        return candidate, issue_id, task_id

    def test_all_gates_must_pass_for_dry_run_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            now = datetime(2026, 8, 10, tzinfo=timezone.utc)
            completed = now - timedelta(days=8)
            candidate, issue_id, task_id = self.make_candidate(Path(tmp), completed=completed)
            client = FakeIssueClient(
                {"id": issue_id, "workspace_id": "workspace-uuid", "status": "done", "metadata": {}},
                [
                    {
                        "id": task_id,
                        "issue_id": issue_id,
                        "workspace_id": "workspace-uuid",
                        "status": "completed",
                        "completed_at": completed.isoformat(),
                        "work_dir": str(candidate / "workdir"),
                    }
                ],
                [{"status": "done"}],
            )
            result = GCEvaluator(
                client,
                now=lambda: now,
                open_file_checker=lambda _: False,
                retention_seconds=7 * 86400,
                recent_write_seconds=3600,
            ).evaluate(candidate)
            self.assertTrue(result["eligible"])
            self.assertEqual(result["reasons"], [])
            self.assertEqual(result["run_id"], task_id)
            self.assertEqual(len(result["approval_token"]), 64)

            client.runs.append({"id": "other", "status": "dispatched", "work_dir": "/other"})
            rejected = GCEvaluator(
                client,
                now=lambda: now,
                open_file_checker=lambda _: False,
                retention_seconds=7 * 86400,
                recent_write_seconds=3600,
            ).evaluate(candidate)
            self.assertFalse(rejected["eligible"])
            self.assertIn("active run", " ".join(rejected["reasons"]))

    def test_nonterminal_pin_open_file_recent_write_and_identity_mismatch_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            now = datetime(2026, 8, 10, tzinfo=timezone.utc)
            completed = now - timedelta(days=8)
            candidate, issue_id, task_id = self.make_candidate(Path(tmp), completed=completed)
            client = FakeIssueClient(
                {
                    "id": issue_id,
                    "workspace_id": "different-workspace",
                    "status": "in_review",
                    "metadata": {"gc_pin": True},
                },
                [{"id": task_id, "status": "running", "work_dir": str(candidate / "wrong")}],
                [{"status": "todo"}],
            )
            (candidate / "recent.txt").write_text("new", encoding="utf-8")
            import os

            os.utime(candidate / "recent.txt", (now.timestamp(), now.timestamp()))
            os.symlink(str(Path(tmp).parent / "outside"), candidate / "outside-link")
            result = GCEvaluator(
                client,
                now=lambda: now,
                open_file_checker=lambda _: True,
                retention_seconds=7 * 86400,
                recent_write_seconds=3600,
            ).evaluate(candidate)
            self.assertFalse(result["eligible"])
            reasons = " ".join(result["reasons"])
            for expected in (
                "terminal",
                "pin",
                "child",
                "open file",
                "recent write",
                "identity",
                "active run",
                "out-of-bound symlink",
            ):
                self.assertIn(expected, reasons)


class ExternalVolumeGuardTest(unittest.TestCase):
    def test_binds_archive_root_device_uuid_and_candidate_budget(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            external = Path(tmp) / "external"
            archive = external / "archive"
            archive.mkdir(parents=True)
            guard = ExternalVolumeGuard(
                external,
                archive,
                expected_uuid="volume-uuid",
                min_free_bytes=100,
                uuid_reader=lambda _: "volume-uuid",
                free_bytes_reader=lambda _: 1210,
            )
            self.assertEqual(guard.check(100)["required_reserve_bytes"], 210)

            outside = Path(tmp) / "outside"
            outside.mkdir()
            with self.assertRaises(ArchiveError):
                ExternalVolumeGuard(
                    external,
                    outside,
                    expected_uuid="volume-uuid",
                    min_free_bytes=1,
                    uuid_reader=lambda _: "volume-uuid",
                    free_bytes_reader=lambda _: 100,
                ).check()

    def test_size_scan_counts_regular_file_payload(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "payload.bin").write_bytes(b"x" * 4096)
            _, total_bytes, _ = _latest_mtime_size_and_unsafe_symlinks(root)
            self.assertGreaterEqual(total_bytes, 4096)

    def test_consumed_approval_tokens_are_read_from_complete_markers(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp)
            completed = archive / "candidate-1"
            completed.mkdir()
            (completed / "COMPLETE.json").write_text(
                json.dumps({"approval_token": "one-time-token"}),
                encoding="utf-8",
            )
            self.assertEqual(consumed_approval_tokens(archive), {"one-time-token"})


class CronBridgeVerificationTest(unittest.TestCase):
    def make_config(self, root: Path, token: str = "fresh-token") -> dict:
        trigger = root / "trigger.json"
        trigger.write_text(
            json.dumps(
                {
                    "schema": "multica.storage-cron-trigger.v1",
                    "token": token,
                    "created_at": datetime.now(timezone.utc).isoformat(),
                    "bridge_pid": 4321,
                }
            ),
            encoding="utf-8",
        )
        return {
            "cron_bridge_trigger_path": str(trigger),
            "cron_bridge_receipt_path": str(root / "receipt.json"),
        }

    @mock.patch("retention_worker.process_ancestry")
    @mock.patch("retention_worker.subprocess.run")
    def test_accepts_fresh_token_with_live_cron_ancestry(self, run: mock.Mock, ancestry: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(
            "os.environ", {"MULTICA_STORAGE_CRON_BRIDGE": "1"}
        ):
            config = self.make_config(Path(tmp))
            run.return_value = mock.Mock(
                returncode=0,
                stdout="/usr/bin/python3 /opt/multica/retention_cron_bridge.py --trigger ...\n",
            )
            ancestry.return_value = [
                {"pid": 4321, "parent_pid": 123, "command": "python3"},
                {"pid": 123, "parent_pid": 1, "command": "/usr/sbin/cron"},
            ]

            trigger, lineage = verify_cron_bridge(config)

            self.assertEqual(trigger["token"], "fresh-token")
            self.assertEqual(lineage[-1]["command"], "/usr/sbin/cron")

    @mock.patch("retention_worker.process_ancestry")
    @mock.patch("retention_worker.subprocess.run")
    def test_rejects_token_already_consumed_by_a_receipt(self, run: mock.Mock, ancestry: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(
            "os.environ", {"MULTICA_STORAGE_CRON_BRIDGE": "1"}
        ):
            root = Path(tmp)
            config = self.make_config(root, token="replayed-token")
            Path(config["cron_bridge_receipt_path"]).write_text(
                json.dumps({"token": "replayed-token", "status": "green"}),
                encoding="utf-8",
            )
            run.return_value = mock.Mock(
                returncode=0,
                stdout="/usr/bin/python3 /opt/multica/retention_cron_bridge.py --trigger ...\n",
            )
            ancestry.return_value = [
                {"pid": 4321, "parent_pid": 123, "command": "python3"},
                {"pid": 123, "parent_pid": 1, "command": "/usr/sbin/cron"},
            ]

            with self.assertRaisesRegex(ArchiveError, "already consumed"):
                verify_cron_bridge(config)

    def test_receipt_is_bound_to_verified_token_not_reread_trigger(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config = self.make_config(root, token="newer-trigger")
            write_cron_bridge_receipt(config, token="verified-token", status="green")
            receipt = json.loads(Path(config["cron_bridge_receipt_path"]).read_text())
            self.assertEqual(receipt["token"], "verified-token")


class AlertDeliveryTest(unittest.TestCase):
    @mock.patch("retention_worker.subprocess.run")
    def test_success_exit_without_message_id_is_recorded_as_delivery_failure(self, run: mock.Mock) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            run.return_value = mock.Mock(returncode=0, stdout="{}", stderr="")
            config = {
                "lark_open_id": "ou_test",
                "alert_log_path": str(root / "alerts.jsonl"),
            }
            send_alert(config, "disk full")
            alert = json.loads((root / "alerts.jsonl").read_text().splitlines()[-1])
            self.assertEqual(alert["message"], "lark alert delivery failed")


class WorkspaceStaleDryRunTest(unittest.TestCase):
    def test_reports_old_task_directory_without_authorizing_deletion(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "workspaces"
            candidate = root / "workspace" / "task"
            workdir = candidate / "workdir"
            workdir.mkdir(parents=True)
            (candidate / ".gc_meta.json").write_text("{}", encoding="utf-8")
            (workdir / "recent.txt").write_bytes(b"x" * 17)
            checked_at = datetime(2026, 9, 2, tzinfo=timezone.utc)
            old = (checked_at - timedelta(days=2)).timestamp()
            os.utime(candidate, (old, old))

            report = build_workspace_stale_report(
                {"workspace_roots": [str(root)], "workspace_stale_seconds": 86400},
                now=lambda: checked_at,
            )

            self.assertEqual(report["status"], "green")
            self.assertEqual(report["stale_workdir_candidate_count"], 1)
            self.assertGreaterEqual(report["stale_workdir_candidate_bytes"], 17)
            self.assertFalse(report["delete_authorized"])
            self.assertFalse(report["deletion_performed"])
            self.assertTrue(report["stale_workdir_candidates"][0]["observation_only"])


class ElectronUpdaterAuditTest(unittest.TestCase):
    def make_config(self, root: Path) -> tuple[dict, Path]:
        home = root / "home"
        home.mkdir()
        return (
            {
                "electron_updater_audit": {
                    "enabled": True,
                    "timezone": "Asia/Shanghai",
                    "day_of_month": 15,
                    "home_path": str(home),
                    "report_dir": str(root / "reports"),
                    "patterns": [
                        {"label": "electron_updater_cache", "glob": "Library/Caches/*-updater"},
                        {"label": "pending_update_zip", "glob": "Library/Caches/*-updater/*.zip"},
                    ],
                    "warn_total_gib": 5,
                    "warn_candidate_gib": 1,
                    "stale_days": 45,
                    "stale_min_mib": 100,
                }
            },
            home,
        )

    def test_discovers_deduplicates_and_does_not_modify_updater_residue(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config, home = self.make_config(root)
            updater = home / "Library" / "Caches" / "demo-updater"
            updater.mkdir(parents=True)
            (updater / "update.zip").write_bytes(b"z" * 128)
            (updater / "metadata.json").write_bytes(b"m" * 64)
            before = {
                path.relative_to(updater).as_posix(): (path.stat().st_size, path.stat().st_mtime_ns)
                for path in updater.iterdir()
            }

            report = audit_electron_updaters(
                config,
                now=lambda: datetime(2026, 8, 5, tzinfo=timezone.utc),
                invocation_source="test",
            )

            self.assertEqual(report["schema"], "multica.st1-electron-updater-audit.v1")
            self.assertEqual(report["audit_month"], "2026-08")
            self.assertEqual(report["status"], "green")
            self.assertEqual(report["candidate_count"], 1)
            self.assertEqual(report["total_bytes"], 192)
            self.assertEqual(report["candidates"][0]["file_count"], 2)
            self.assertEqual(
                report["candidates"][0]["matched_labels"],
                ["electron_updater_cache", "pending_update_zip"],
            )
            after = {
                path.relative_to(updater).as_posix(): (path.stat().st_size, path.stat().st_mtime_ns)
                for path in updater.iterdir()
            }
            self.assertEqual(after, before)
            self.assertEqual(json.loads(Path(report["report_path"]).read_text()), report)

    def test_records_a_symlink_match_without_following_or_counting_its_target(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config, home = self.make_config(root)
            cache = home / "Library" / "Caches"
            cache.mkdir(parents=True)
            outside = root / "outside"
            outside.mkdir()
            (outside / "payload.bin").write_bytes(b"x" * 4096)
            (cache / "escape-updater").symlink_to(outside, target_is_directory=True)

            report = audit_electron_updaters(
                config,
                now=lambda: datetime(2026, 8, 5, tzinfo=timezone.utc),
                invocation_source="test",
            )

            self.assertEqual(report["status"], "green")
            self.assertEqual(report["candidate_count"], 0)
            self.assertEqual(report["total_bytes"], 0)
            self.assertEqual(report["errors"], [])
            self.assertEqual(len(report["excluded_symlink_matches"]), 1)
            self.assertEqual(
                report["excluded_symlink_matches"][0]["path"],
                str(cache.resolve() / "escape-updater"),
            )
            self.assertIn("not traversed", report["excluded_symlink_matches"][0]["reason"])
            self.assertEqual((outside / "payload.bin").read_bytes(), b"x" * 4096)

    def test_rejects_unbounded_recursive_globs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            config, _ = self.make_config(Path(tmp))
            config["electron_updater_audit"]["patterns"] = [
                {"label": "too_broad", "glob": "Library/**/update.zip"}
            ]

            with self.assertRaisesRegex(ArchiveError, "recursive"):
                audit_electron_updaters(config)

    def test_marks_large_or_stale_candidates_for_attention_without_cleanup(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config, home = self.make_config(root)
            audit_config = config["electron_updater_audit"]
            audit_config["warn_total_gib"] = 0.0000001
            audit_config["warn_candidate_gib"] = 0.0000001
            audit_config["stale_min_mib"] = 0.0001
            updater = home / "Library" / "Caches" / "old-updater"
            updater.mkdir(parents=True)
            payload = updater / "update.zip"
            payload.write_bytes(b"x" * 512)
            old = datetime(2026, 5, 1, tzinfo=timezone.utc).timestamp()
            import os

            os.utime(payload, (old, old))
            os.utime(updater, (old, old))

            report = audit_electron_updaters(
                config,
                now=lambda: datetime(2026, 8, 5, tzinfo=timezone.utc),
                invocation_source="test",
            )

            self.assertEqual(report["status"], "attention")
            reasons = " ".join(report["attention_reasons"])
            self.assertIn("total", reasons)
            self.assertIn("candidate", reasons)
            self.assertIn("stale", reasons)
            self.assertTrue(payload.exists())

    def test_month_gate_runs_once_and_force_bypasses_day_and_existing_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config, home = self.make_config(root)
            updater = home / "Library" / "Caches" / "demo-updater"
            updater.mkdir(parents=True)
            (updater / "update.zip").write_bytes(b"z")
            before_due = lambda: datetime(2026, 8, 5, tzinfo=timezone.utc)
            on_due = lambda: datetime(2026, 8, 15, tzinfo=timezone.utc)

            skipped = maybe_run_electron_updater_audit(config, now=before_due)
            self.assertEqual(skipped["status"], "skipped")
            self.assertIn("before day", skipped["reason"])

            forced = maybe_run_electron_updater_audit(config, now=before_due, force=True)
            self.assertEqual(forced["status"], "green")
            report_path = Path(forced["report_path"])
            self.assertTrue(report_path.is_file())
            # The production gate deliberately verifies the evidence file's
            # calendar-month mtime. Pin it to the test clock so this time-travel
            # test remains deterministic after August 2026.
            os.utime(report_path, (before_due().timestamp(), before_due().timestamp()))

            already_recorded = maybe_run_electron_updater_audit(config, now=on_due)
            self.assertEqual(already_recorded["status"], "skipped")
            self.assertIn("already exists", already_recorded["reason"])

            rerun = maybe_run_electron_updater_audit(config, now=on_due, force=True)
            self.assertEqual(rerun["status"], "green")
            self.assertEqual(rerun["audit_month"], "2026-08")

    def test_formal_worker_audits_before_external_volume_checks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            archive = root / "external" / "archive"
            archive.mkdir(parents=True)
            config = {
                "require_cron_lineage": False,
                "delete_source": False,
                "external_path": str(root / "external"),
                "archive_root": str(archive),
                "external_volume_uuid": "volume-uuid",
                "external_min_free_gib": 1,
                "workspace_roots": [],
            }
            events: list[str] = []

            def audit(*args: object, **kwargs: object) -> dict:
                events.append("audit")
                return {"status": "skipped", "reason": "test"}

            def external_check(*args: object, **kwargs: object) -> dict:
                events.append("external")
                raise ArchiveError("stop after ordering proof")

            with mock.patch(
                "retention_worker.maybe_run_electron_updater_audit", side_effect=audit
            ), mock.patch.object(ExternalVolumeGuard, "check", side_effect=external_check):
                with self.assertRaisesRegex(ArchiveError, "ordering proof"):
                    run_worker(config)

            self.assertEqual(events, ["audit", "external"])

    def test_audit_only_cli_bypasses_cron_lineage_and_external_volume(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            config, home = self.make_config(root)
            config.update(
                {
                    "require_cron_lineage": True,
                    "lock_path": str(root / "worker.lock"),
                    "external_path": str(root / "missing-external"),
                    "archive_root": str(root / "missing-external" / "archive"),
                }
            )
            updater = home / "Library" / "Caches" / "demo-updater"
            updater.mkdir(parents=True)
            (updater / "update.zip").write_bytes(b"z")
            config_path = root / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            output = io.StringIO()

            with mock.patch.object(
                sys,
                "argv",
                ["retention_worker.py", "--config", str(config_path), "--electron-audit-only"],
            ), redirect_stdout(output):
                exit_code = main()

            self.assertEqual(exit_code, 0)
            summary = json.loads(output.getvalue())
            self.assertEqual(summary["status"], "green")
            self.assertTrue(Path(summary["report_path"]).is_file())


if __name__ == "__main__":
    unittest.main()
