from __future__ import annotations

import fcntl
import json
import sys
import tempfile
import unittest
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
    tree_manifest,
    verify_cron_bridge,
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

    def test_complete_marker_precedes_opt_in_source_delete(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            destination = root / "archive"
            source.mkdir()
            make_tree(source)

            result = ArchiveManager(destination).archive(source, "candidate", delete_source=True)

            final = Path(result["archive_path"])
            self.assertFalse(source.exists())
            marker = json.loads((final / "COMPLETE.json").read_text(encoding="utf-8"))
            self.assertEqual(marker["schema"], "multica.transactional-archive.v1")
            self.assertEqual(marker["source_manifest"], marker["archive_manifest"])
            self.assertTrue(marker["verified_before_source_delete"])

    def test_every_injected_failure_preserves_source(self) -> None:
        for phase in ("after_copy", "after_verify", "after_rename", "after_complete"):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                source = root / "source"
                source.mkdir()
                make_tree(source)
                with self.assertRaises(ArchiveError):
                    ArchiveManager(root / "archive", fail_at=phase).archive(
                        source, "candidate", delete_source=True
                    )
                self.assertTrue(source.exists())
                self.assertEqual(tree_manifest(source)["file_count"], 3)

    def test_fresh_delete_gate_failure_preserves_source_after_complete_marker(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            source.mkdir()
            make_tree(source)
            with self.assertRaises(ArchiveError):
                ArchiveManager(root / "archive").archive(
                    source,
                    "candidate",
                    delete_source=True,
                    delete_gate=lambda: False,
                )
            self.assertTrue(source.exists())
            self.assertEqual(len(list((root / "archive").glob("*/COMPLETE.json"))), 1)

    def test_write_racing_in_delete_gate_is_not_deleted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            source.mkdir()
            make_tree(source)

            def late_writer() -> bool:
                (source / "late.txt").write_text("not archived", encoding="utf-8")
                return True

            with self.assertRaises(ArchiveError):
                ArchiveManager(root / "archive").archive(
                    source, "candidate", delete_source=True, delete_gate=late_writer
                )
            self.assertEqual((source / "late.txt").read_text(encoding="utf-8"), "not archived")

    def test_failure_after_atomic_isolation_rolls_source_path_back(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            source.mkdir()
            make_tree(source)
            with self.assertRaises(ArchiveError):
                ArchiveManager(root / "archive", fail_at="after_isolation").archive(
                    source, "candidate", delete_source=True
                )
            self.assertTrue(source.is_dir())
            self.assertEqual(tree_manifest(source)["file_count"], 3)

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


if __name__ == "__main__":
    unittest.main()
