from __future__ import annotations

import fcntl
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from retention_cron_bridge import SingleInstanceLock, launchctl_kickstart_command  # noqa: E402


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


if __name__ == "__main__":
    unittest.main()
