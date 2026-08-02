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
        if value.get("token") == token:
            return 0 if value.get("status") == "green" else 1
        time.sleep(2)
    raise SystemExit("retention worker receipt timeout")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--trigger", required=True)
    parser.add_argument("--receipt", required=True)
    parser.add_argument("--label", default="com.multica.storage-retention")
    parser.add_argument("--timeout", type=int, default=7200)
    parser.add_argument("--lock")
    args = parser.parse_args()
    lock_path = Path(args.lock) if args.lock else Path(args.trigger).with_name("retention-cron-bridge.lock")
    try:
        with SingleInstanceLock(lock_path):
            return run_bridge(args)
    except BlockingIOError:
        return 75


if __name__ == "__main__":
    raise SystemExit(main())
