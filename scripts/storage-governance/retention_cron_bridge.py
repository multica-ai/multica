#!/usr/bin/env python3
"""Synchronous cron-to-LaunchAgent bridge for macOS Full Disk Access."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import subprocess
import tempfile
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from retention_worker import atomic_write_failure_report, send_alert


class SingleInstanceLock:
    def __init__(self, path: Path):
        self.path = path
        self.handle = None

    def __enter__(self) -> "SingleInstanceLock":
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.handle = self.path.open("a+")
        fcntl.flock(self.handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        if self.handle is not None:
            fcntl.flock(self.handle.fileno(), fcntl.LOCK_UN)
            self.handle.close()


def launchctl_kickstart_command(label: str, *, uid: int) -> list[str]:
    return ["/bin/launchctl", "kickstart", "gui/%d/%s" % (uid, label)]


def receipt_exit_code(value: dict, token: str) -> Optional[int]:
    if value.get("token") != token:
        return None
    if value.get("status") == "green":
        return 0
    if value.get("status") == "red":
        return 1
    return None


def record_lock_collision(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(
            json.dumps(
                {
                    "recorded_at": datetime.now(timezone.utc).isoformat(),
                    "status": "locked",
                    "message": "previous bridge is still running; skipped overlapping retention launch",
                },
                sort_keys=True,
            )
            + "\n"
        )
        handle.flush()
        os.fsync(handle.fileno())


def report_bridge_failure(config_path: Optional[str], message: str) -> None:
    if not config_path:
        return
    try:
        config = json.loads(Path(config_path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return
    for action in (
        lambda: atomic_write_failure_report(config, message),
        lambda: send_alert(config, message),
    ):
        try:
            action()
        except Exception:
            pass


def atomic_write(path: Path, value: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".%s." % path.name, dir=str(path.parent))
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def ancestry(pid: int, limit: int = 8) -> list[dict]:
    values = []
    for _ in range(limit):
        parent_result = subprocess.run(
            ["/bin/ps", "-p", str(pid), "-o", "ppid="], capture_output=True, text=True, check=False
        )
        command_result = subprocess.run(
            ["/bin/ps", "-p", str(pid), "-o", "comm="], capture_output=True, text=True, check=False
        )
        if parent_result.returncode != 0 or command_result.returncode != 0:
            break
        parent = int(parent_result.stdout.strip())
        command = command_result.stdout.strip()
        values.append({"pid": pid, "parent_pid": parent, "command": command})
        if parent <= 1 or parent == pid:
            break
        pid = parent
    return values


def run_bridge(args: argparse.Namespace) -> int:
    lineage = ancestry(os.getpid())
    if not any(Path(str(item["command"])).name == "cron" for item in lineage):
        raise SystemExit("retention bridge refuses non-cron ancestry")
    token = uuid.uuid4().hex
    trigger = Path(args.trigger)
    receipt = Path(args.receipt)
    atomic_write(
        trigger,
        {
            "schema": "multica.storage-cron-trigger.v1",
            "token": token,
            "created_at": datetime.now(timezone.utc).isoformat(),
            "bridge_pid": os.getpid(),
            "cron_ancestry": lineage,
        },
    )
    process = subprocess.run(
        launchctl_kickstart_command(args.label, uid=os.getuid()),
        capture_output=True,
        text=True,
        check=False,
    )
    if process.returncode != 0:
        raise SystemExit("launchctl kickstart failed: " + (process.stderr or process.stdout).strip())
    deadline = time.monotonic() + args.timeout
    while time.monotonic() < deadline:
        try:
            value = json.loads(receipt.read_text(encoding="utf-8"))
        except (FileNotFoundError, OSError, json.JSONDecodeError):
            time.sleep(2)
            continue
        result = receipt_exit_code(value, token)
        if result is not None:
            return result
        time.sleep(2)
    raise SystemExit("retention worker receipt timeout")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--trigger", required=True)
    parser.add_argument("--receipt", required=True)
    parser.add_argument("--label", default="com.multica.storage-retention")
    parser.add_argument("--timeout", type=int, default=7200)
    parser.add_argument("--lock")
    parser.add_argument("--alert-log")
    parser.add_argument("--config")
    args = parser.parse_args()
    lock_path = Path(args.lock) if args.lock else Path(args.trigger).with_name("retention-cron-bridge.lock")
    try:
        with SingleInstanceLock(lock_path):
            try:
                return run_bridge(args)
            except SystemExit as error:
                report_bridge_failure(args.config, "storage retention cron bridge failed: %s" % error)
                raise
    except BlockingIOError:
        alert_path = Path(args.alert_log) if args.alert_log else Path(args.trigger).with_name("retention-alerts.jsonl")
        record_lock_collision(alert_path)
        report_bridge_failure(args.config, "storage retention cron bridge skipped: previous bridge is still running")
        return 75


if __name__ == "__main__":
    raise SystemExit(main())
