#!/usr/bin/env python3
"""Minute-level storage guard for a Multica runtime host.

The guard is deliberately small: sample, classify with hysteresis, pause new
Multica admissions, stop explicitly listed non-critical jobs, and alert. It
never deletes or archives data.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import signal
import subprocess
import tempfile
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


GIB = 1024**3


@dataclass(frozen=True)
class Sample:
    recorded_at: str
    internal_free_bytes: int
    external_free_bytes: Optional[int]
    swap_used_bytes: int
    active_task_count: int
    daemon_pid: Optional[int]
    daemon_status: str


class CommandRunner:
    def run(self, argv: List[str], *, tolerate_failure: bool = False) -> Tuple[int, str, str]:
        proc = subprocess.run(argv, capture_output=True, text=True, check=False)
        if proc.returncode != 0 and not tolerate_failure:
            detail = (proc.stderr or proc.stdout).strip()
            raise RuntimeError("command failed (%d): %s: %s" % (proc.returncode, argv[0], detail))
        return proc.returncode, proc.stdout, proc.stderr

    def signal(self, pid: int, sig: int) -> None:
        os.kill(pid, sig)


class SystemCollector:
    def __init__(self, config: Dict[str, Any], runner: CommandRunner):
        self.config = config
        self.runner = runner

    @staticmethod
    def free_bytes(path: str) -> int:
        stat = os.statvfs(path)
        return stat.f_bavail * stat.f_frsize

    def swap_used_bytes(self) -> int:
        code, stdout, stderr = self.runner.run(["/usr/sbin/sysctl", "vm.swapusage"])
        if code != 0:
            raise RuntimeError("sysctl vm.swapusage failed: " + (stderr or stdout).strip())
        match = re.search(r"\bused\s*=\s*([0-9.]+)([KMGT])", stdout)
        if not match:
            raise RuntimeError("could not parse vm.swapusage")
        units = {"K": 1024, "M": 1024**2, "G": 1024**3, "T": 1024**4}
        return int(float(match.group(1)) * units[match.group(2)])

    def daemon_status(self) -> Dict[str, Any]:
        code, stdout, _ = self.runner.run(
            ["multica", "daemon", "status", "--output", "json"],
            tolerate_failure=True,
        )
        if code != 0:
            return {"status": "stopped", "active_task_count": 0, "pid": None}
        try:
            value = json.loads(stdout)
        except json.JSONDecodeError:
            return {"status": "unknown", "active_task_count": 0, "pid": None}
        return value if isinstance(value, dict) else {"status": "unknown"}

    def collect(self) -> Sample:
        daemon = self.daemon_status()
        external_free: Optional[int]
        try:
            external_free = self.free_bytes(str(self.config["external_path"]))
        except OSError:
            external_free = None
        pid = daemon.get("pid")
        return Sample(
            recorded_at=datetime.now(timezone.utc).isoformat(),
            internal_free_bytes=self.free_bytes(str(self.config["internal_path"])),
            external_free_bytes=external_free,
            swap_used_bytes=self.swap_used_bytes(),
            active_task_count=int(daemon.get("active_task_count") or 0),
            daemon_pid=int(pid) if pid else None,
            daemon_status=str(daemon.get("status") or "unknown"),
        )


def classify_level(
    free_bytes: int,
    *,
    previous: int,
    level1: int,
    level1_clear: int,
    level2: int,
    level2_clear: int,
) -> int:
    if free_bytes <= level2:
        return 2
    if previous >= 2 and free_bytes < level2_clear:
        return 2
    if free_bytes <= level1:
        return 1
    if previous >= 1 and free_bytes < level1_clear:
        return 1
    return 0


def atomic_write_json(path: Path, value: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temp_name = tempfile.mkstemp(prefix=".%s." % path.name, dir=str(path.parent))
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, ensure_ascii=False, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_name, path)
    finally:
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass


def append_jsonl(path: Path, value: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(value, ensure_ascii=False, sort_keys=True) + "\n")
        handle.flush()
        os.fsync(handle.fileno())


def parse_timestamp(raw: str) -> datetime:
    return datetime.fromisoformat(raw.replace("Z", "+00:00"))


def build_capacity_report(
    samples: Iterable[Dict[str, Any]],
    *,
    safety_floor_bytes: int,
    burst_reserve_bytes: int,
    minimum_hours: float,
) -> Dict[str, Any]:
    ordered = sorted(samples, key=lambda item: parse_timestamp(str(item["recorded_at"])))
    report: Dict[str, Any] = {
        "schema": "multica.storage-capacity.v1",
        "status": "INCONCLUSIVE",
        "sample_count": len(ordered),
        "observation_hours": 0.0,
        "p95_growth_bytes_per_hour": None,
        "safety_floor_bytes": safety_floor_bytes,
        "burst_reserve_bytes": burst_reserve_bytes,
        "days_remaining": None,
    }
    if len(ordered) < 2:
        return report
    start = parse_timestamp(str(ordered[0]["recorded_at"]))
    end = parse_timestamp(str(ordered[-1]["recorded_at"]))
    observation_hours = max(0.0, (end - start).total_seconds() / 3600)
    report["observation_hours"] = observation_hours
    if observation_hours < minimum_hours:
        return report

    growth_rates: List[float] = []
    for previous, current in zip(ordered, ordered[1:]):
        previous_at = parse_timestamp(str(previous["recorded_at"]))
        current_at = parse_timestamp(str(current["recorded_at"]))
        elapsed_hours = (current_at - previous_at).total_seconds() / 3600
        if elapsed_hours <= 0:
            continue
        delta = int(previous["internal_free_bytes"]) - int(current["internal_free_bytes"])
        growth_rates.append(max(0.0, delta / elapsed_hours))
    if not growth_rates:
        return report

    sorted_rates = sorted(growth_rates)
    p95 = sorted_rates[max(0, math.ceil(0.95 * len(sorted_rates)) - 1)]
    report["status"] = "READY"
    report["p95_growth_bytes_per_hour"] = p95
    latest_free = int(ordered[-1]["internal_free_bytes"])
    budget = max(0, latest_free - safety_floor_bytes - burst_reserve_bytes)
    report["days_remaining"] = (budget / p95 / 24) if p95 > 0 else None
    return report


def load_json(path: Path, default: Dict[str, Any]) -> Dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return dict(default)
    return value if isinstance(value, dict) else dict(default)


def read_jsonl(path: Path) -> List[Dict[str, Any]]:
    values: List[Dict[str, Any]] = []
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except FileNotFoundError:
        return values
    for line in lines:
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict) and "recorded_at" in value and "internal_free_bytes" in value:
            values.append(value)
    return values


def validate_config(config: Dict[str, Any]) -> None:
    required = [
        "internal_path",
        "external_path",
        "level1_free_gib",
        "level1_clear_gib",
        "level2_free_gib",
        "level2_clear_gib",
        "state_path",
        "metrics_path",
        "capacity_report_path",
    ]
    missing = [key for key in required if key not in config]
    if missing:
        raise ValueError("missing config keys: " + ", ".join(missing))
    level1 = float(config["level1_free_gib"])
    level1_clear = float(config["level1_clear_gib"])
    level2 = float(config["level2_free_gib"])
    level2_clear = float(config["level2_clear_gib"])
    if not (0 < level2 < level2_clear <= level1 < level1_clear):
        raise ValueError("watermarks must satisfy level2 < level2_clear <= level1 < level1_clear")


def has_message_id(value: Any) -> bool:
    if isinstance(value, dict):
        if value.get("message_id"):
            return True
        return any(has_message_id(child) for child in value.values())
    if isinstance(value, list):
        return any(has_message_id(child) for child in value)
    return False


class Guard:
    def __init__(self, config: Dict[str, Any], runner: CommandRunner, collector: Any):
        validate_config(config)
        self.config = config
        self.runner = runner
        self.collector = collector
        self.uid = int(config.get("uid", os.getuid()))

    def stop_launchagent(self, label: str, actions: List[str]) -> None:
        target = "gui/%d/%s" % (self.uid, label)
        self.runner.run(["launchctl", "disable", target], tolerate_failure=True)
        self.runner.run(["launchctl", "bootout", target], tolerate_failure=True)
        actions.append("launchagent_stopped:" + label)

    def pause_admission(self, sample: Sample, state: Dict[str, Any], actions: List[str]) -> None:
        code, _, stderr = self.runner.run(
            ["multica", "daemon", "pause", "--output", "json"],
            tolerate_failure=True,
        )
        if code == 0:
            state["legacy_daemon_sigstopped"] = False
            state["legacy_daemon_pid"] = None
            actions.append("daemon_admission_paused")
            return
        if not bool(self.config.get("legacy_sigstop_fallback", False)):
            actions.append("daemon_admission_pause_failed:" + stderr.strip()[:200])
            return
        pid = sample.daemon_pid
        if not pid:
            code, stdout, _ = self.runner.run(
                ["multica", "daemon", "status", "--output", "json"],
                tolerate_failure=True,
            )
            if code == 0:
                try:
                    pid = int(json.loads(stdout).get("pid") or 0)
                except (ValueError, TypeError, json.JSONDecodeError):
                    pid = None
        if pid:
            self.runner.signal(pid, signal.SIGSTOP)
            state["legacy_daemon_sigstopped"] = True
            state["legacy_daemon_pid"] = pid
            actions.append("legacy_daemon_sigstopped")
        else:
            actions.append("daemon_admission_pause_failed:no_pid")

    def resume_admission(self, state: Dict[str, Any], actions: List[str]) -> None:
        if state.get("legacy_daemon_sigstopped") and state.get("legacy_daemon_pid"):
            self.runner.signal(int(state["legacy_daemon_pid"]), signal.SIGCONT)
            actions.append("legacy_daemon_sigcontinued")
        self.runner.run(
            ["multica", "daemon", "resume", "--output", "json"],
            tolerate_failure=True,
        )
        state["legacy_daemon_sigstopped"] = False
        state["legacy_daemon_pid"] = None
        actions.append("daemon_admission_resumed")

    def alert(self, sample: Sample, level: int, state: Dict[str, Any], actions: List[str], reason: str) -> None:
        open_id = str(self.config.get("lark_open_id") or "").strip()
        if not open_id:
            actions.append("lark_alert_skipped:no_recipient")
            return
        now = parse_timestamp(sample.recorded_at)
        previous_raw = state.get("last_alert_at")
        if previous_raw:
            previous = parse_timestamp(str(previous_raw))
            cooldown = float(self.config.get("alert_cooldown_seconds", 3600))
            if (now - previous).total_seconds() < cooldown:
                actions.append("lark_alert_suppressed:cooldown")
                return
        text = (
            "[主机储存熔断 L%d] %s；内置盘可用 %.2f GiB，swap %.2f GiB，活跃任务 %d。"
            % (
                level,
                reason,
                sample.internal_free_bytes / GIB,
                sample.swap_used_bytes / GIB,
                sample.active_task_count,
            )
        )
        code, stdout, _ = self.runner.run(
            [
                "lark-cli",
                "im",
                "+messages-send",
                "--as",
                "bot",
                "--user-id",
                open_id,
                "--text",
                text,
                "--format",
                "json",
            ],
            tolerate_failure=True,
        )
        try:
            response = json.loads(stdout)
        except json.JSONDecodeError:
            response = {}
        if code == 0 and has_message_id(response):
            state["last_alert_at"] = sample.recorded_at
            actions.append("lark_alert_sent")
        else:
            actions.append("lark_alert_failed")

    def run_once(self) -> Dict[str, Any]:
        sample = self.collector.collect()
        state_path = Path(str(self.config["state_path"]))
        metrics_path = Path(str(self.config["metrics_path"]))
        report_path = Path(str(self.config["capacity_report_path"]))
        state = load_json(
            state_path,
            {"level": 0, "legacy_daemon_sigstopped": False, "legacy_daemon_pid": None},
        )
        previous = int(state.get("level") or 0)
        level = classify_level(
            sample.internal_free_bytes,
            previous=previous,
            level1=int(float(self.config["level1_free_gib"]) * GIB),
            level1_clear=int(float(self.config["level1_clear_gib"]) * GIB),
            level2=int(float(self.config["level2_free_gib"]) * GIB),
            level2_clear=int(float(self.config["level2_clear_gib"]) * GIB),
        )
        actions: List[str] = []

        if level >= 1:
            for label in self.config.get("observer_labels", []):
                self.stop_launchagent(str(label), actions)
            self.pause_admission(sample, state, actions)
        elif previous >= 1 or state.get("legacy_daemon_sigstopped"):
            self.resume_admission(state, actions)

        if level >= 2:
            for label in self.config.get("nonproduction_launchagents", []):
                self.stop_launchagent(str(label), actions)
            self.alert(sample, level, state, actions, "内置盘进入二级低水位，已暂停显式列出的非生产任务")

        external_min = int(float(self.config.get("external_min_free_gib", 100)) * GIB)
        if sample.external_free_bytes is None:
            self.alert(sample, max(level, 1), state, actions, "外置归档卷不可用")
        elif sample.external_free_bytes < external_min:
            self.alert(sample, max(level, 1), state, actions, "外置归档卷低于配置水位")

        state.update({"level": level, "previous_level": previous, "last_sample": asdict(sample), "last_actions": actions})
        append_jsonl(metrics_path, asdict(sample))
        report = build_capacity_report(
            read_jsonl(metrics_path),
            safety_floor_bytes=int(float(self.config.get("safety_floor_gib", 25)) * GIB),
            burst_reserve_bytes=int(float(self.config.get("burst_reserve_gib", 10)) * GIB),
            minimum_hours=float(self.config.get("minimum_observation_hours", 48)),
        )
        atomic_write_json(report_path, report)
        atomic_write_json(state_path, state)
        return {"level": level, "previous_level": previous, "sample": asdict(sample), "actions": actions, "capacity": report}


def main() -> int:
    parser = argparse.ArgumentParser(description="Sample and protect a Multica runtime host")
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args()
    config = json.loads(args.config.read_text(encoding="utf-8"))
    runner = CommandRunner()
    result = Guard(config, runner, SystemCollector(config, runner)).run_once()
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
