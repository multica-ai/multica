from __future__ import annotations

import fcntl
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from retention_cron_bridge import (  # noqa: E402
    SingleInstanceLock,
    launchctl_kickstart_command,
    receipt_exit_code,
    record_lock_collision,
)


class CronBridgeTest(unittest.TestCase):
    def test_kickstart_does_not_force_kill_an_existing_retention_run(self) -> None:
        command = launchctl_kickstart_command("com.multica.storage-retention", uid=501)
        self.assertEqual(
            command,
            ["/bin/launchctl", "kickstart", "gui/501/com.multica.storage-retention"],
        )
        self.assertNotIn("-k", command)

    def test_bridge_single_instance_lock_is_nonblocking(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "bridge.lock"
            with SingleInstanceLock(path):
                descriptor = path.open("a+")
                try:
                    with self.assertRaises(BlockingIOError):
                        fcntl.flock(descriptor.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                finally:
                    descriptor.close()

    def test_running_receipt_keeps_bridge_waiting(self) -> None:
        self.assertIsNone(receipt_exit_code({"token": "t", "status": "running"}, "t"))
        self.assertEqual(receipt_exit_code({"token": "t", "status": "green"}, "t"), 0)
        self.assertEqual(receipt_exit_code({"token": "t", "status": "red"}, "t"), 1)

    def test_lock_collision_writes_machine_readable_alert(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "alerts.jsonl"
            record_lock_collision(path)
            self.assertIn("previous bridge is still running", path.read_text())


if __name__ == "__main__":
    unittest.main()
