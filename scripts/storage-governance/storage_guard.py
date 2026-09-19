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
import shlex
import signal
import subprocess
import tempfile
import time
from dataclasses import asdict, dataclass, replace
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


GIB = 1024**3


@dataclass(frozen=True)
class DaemonTarget:
    pid: int
    executable: str
    profile: Optional[str] = None

    def command(self, action: str, *arguments: str) -> List[str]:
        command = [self.executable]
        if self.profile:
            command.extend(["--profile", self.profile])
        command.extend(["daemon", action])
        command.extend(arguments)
        return command


@dataclass(frozen=True)
class Sample:
    recorded_at: str
    internal_free_bytes: int
    external_free_bytes: Optional[int]
    swap_used_bytes: int
    active_task_count: int
    daemon_pid: Optional[int]
    daemon_status: str
    daemon_id: Optional[str] = None
    daemon_instances: Tuple[Dict[str, Any], ...] = ()
    daemon_discovery_errors: Tuple[str, ...] = ()
    admission_pause_owners: Optional[Tuple[str, ...]] = None
    shadow_runs_bytes: Optional[int] = None
    workspace_total_bytes: Optional[int] = None
    workspace_inflight_bytes: Optional[int] = None
    workspace_gc_eligible_bytes: Optional[int] = None
    workspace_gc_deletion_eligible_bytes: Optional[int] = None
    workspace_gc_backlog_bytes: Optional[int] = None
    workspace_unclassified_bytes: Optional[int] = None
    cursor_bytes: Optional[int] = None
    logs_bytes: Optional[int] = None


class CommandRunner:
    def run(self, argv: List[str], *, tolerate_failure: bool = False) -> Tuple[int, str, str]:
        environment = None
        if argv and Path(argv[0]).name == "multica" and "daemon" in argv:
            environment = os.environ.copy()
            for key in (
                "MULTICA_AGENT_ID",
                "MULTICA_AGENT_NAME",
                "MULTICA_DAEMON_PORT",
                "MULTICA_SERVER_URL",
                "MULTICA_TASK_CONFIG_ROOT",
                "MULTICA_TASK_ID",
                "MULTICA_TASK_SLOT",
                "MULTICA_TASK_WORKSPACES_ROOT",
                "MULTICA_TOKEN",
                "MULTICA_WORKSPACE_ID",
            ):
                environment.pop(key, None)
        try:
            proc = subprocess.run(argv, capture_output=True, text=True, check=False, timeout=10, env=environment)
        except subprocess.TimeoutExpired as error:
            if tolerate_failure:
                return 124, "", "command timed out after 10 seconds"
            raise RuntimeError("command timed out: %s" % argv[0]) from error
        if proc.returncode != 0 and not tolerate_failure:
            detail = (proc.stderr or proc.stdout).strip()
            raise RuntimeError("command failed (%d): %s: %s" % (proc.returncode, argv[0], detail))
        return proc.returncode, proc.stdout, proc.stderr

    def signal(self, pid: int, sig: int) -> None:
        os.kill(pid, sig)


def parse_daemon_targets(processes: str, *, uid: int) -> List[DaemonTarget]:
    """Find local Multica daemon processes without trusting names alone."""

    targets: List[DaemonTarget] = []
    seen: set[int] = set()
    for line in processes.splitlines():
        fields = line.strip().split(None, 2)
        if len(fields) != 3:
            continue
        try:
            process_uid, pid = int(fields[0]), int(fields[1])
        except ValueError:
            continue
        if process_uid != uid or pid in seen:
            continue
        try:
            argv = shlex.split(fields[2])
        except ValueError:
            continue
        if len(argv) < 3 or not os.path.isabs(argv[0]) or Path(argv[0]).name != "multica":
            continue
        if not any(argv[index : index + 2] == ["daemon", "start"] for index in range(len(argv) - 1)):
            continue
        profile: Optional[str] = None
        for index, argument in enumerate(argv):
            if argument == "--profile" and index + 1 < len(argv):
                profile = argv[index + 1]
            elif argument.startswith("--profile="):
                profile = argument.split("=", 1)[1]
        targets.append(DaemonTarget(pid=pid, executable=argv[0], profile=profile))
        seen.add(pid)
    return sorted(targets, key=lambda item: item.pid)


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

    def _daemon_status_for(self, target: DaemonTarget) -> Tuple[Optional[Dict[str, Any]], Optional[str]]:
        code, stdout, stderr = self.runner.run(
            target.command("status", "--output", "json"), tolerate_failure=True
        )
        if code != 0:
            return None, "pid %d status failed: %s" % (target.pid, (stderr or stdout).strip()[:200])
        try:
            value = json.loads(stdout)
        except json.JSONDecodeError:
            return None, "pid %d status returned invalid JSON" % target.pid
        if not isinstance(value, dict):
            return None, "pid %d status returned a non-object" % target.pid
        resolved_pid = int(value.get("pid") or 0)
        if resolved_pid != target.pid:
            return None, "pid %d profile resolved to pid %d" % (target.pid, resolved_pid)
        return dict(value), None

    def discover_daemons(self) -> Tuple[List[Tuple[DaemonTarget, Dict[str, Any]]], List[str]]:
        code, stdout, stderr = self.runner.run(
            ["/bin/ps", "-axo", "uid=,pid=,command="], tolerate_failure=True
        )
        if code != 0:
            return [], ["daemon process discovery failed: %s" % (stderr or stdout).strip()[:200]]
        targets = parse_daemon_targets(stdout, uid=int(self.config.get("uid", os.getuid())))
        instances: List[Tuple[DaemonTarget, Dict[str, Any]]] = []
        errors: List[str] = []
        for target in targets:
            status, error = self._daemon_status_for(target)
            if status is not None:
                instances.append((target, status))
            elif error:
                errors.append(error)
        if not targets:
            errors.append("no local multica daemon process was discovered")
        return instances, errors

    @staticmethod
    def _instance_summary(target: DaemonTarget, status: Dict[str, Any]) -> Dict[str, Any]:
        owners = status.get("admission_pause_owners")
        return {
            "pid": target.pid,
            "daemon_id": str(status.get("daemon_id") or ""),
            "profile": target.profile,
            "cli_version": str(status.get("cli_version") or ""),
            "active_task_count": int(status.get("active_task_count") or 0),
            "admission_paused": status.get("admission_paused"),
            "admission_pause_owners": [str(value) for value in owners] if isinstance(owners, list) else [],
        }

    def apply_admission_group(self, action: str, *, daemon_id: str, owner: str) -> Dict[str, Any]:
        if action not in {"pause", "resume"}:
            raise ValueError("daemon admission action must be pause or resume")
        discovered, errors = self.discover_daemons()
        matches = [(target, status) for target, status in discovered if status.get("daemon_id") == daemon_id]
        attempted_pids: List[int] = []
        for target, _ in matches:
            code, stdout, stderr = self.runner.run(
                target.command(action, "--owner", owner, "--output", "json"), tolerate_failure=True
            )
            attempted_pids.append(target.pid)
            try:
                response = json.loads(stdout)
            except json.JSONDecodeError:
                response = {}
            if code != 0 or response.get("owner_paused") is not (action == "pause"):
                errors.append("pid %d %s failed: %s" % (target.pid, action, (stderr or stdout).strip()[:200]))

        verified, verify_errors = self.discover_daemons()
        errors.extend(verify_errors)
        verified_matches = {
            target.pid: status for target, status in verified if status.get("daemon_id") == daemon_id
        }
        expected_pids = {target.pid for target, _ in matches}
        if not matches:
            errors.append("no daemon process matched daemon_id %s" % daemon_id)
        if set(verified_matches) != expected_pids:
            errors.append(
                "daemon group changed during %s: before=%s after=%s"
                % (action, sorted(expected_pids), sorted(verified_matches))
            )
        for pid, status in verified_matches.items():
            owners = {str(value) for value in status.get("admission_pause_owners") or []}
            if action == "pause" and (status.get("admission_paused") is not True or owner not in owners):
                errors.append("pid %d did not verify paused for owner %s" % (pid, owner))
            if action == "resume" and owner in owners:
                errors.append("pid %d retained pause owner %s" % (pid, owner))
        return {
            "ok": not errors,
            "daemon_id": daemon_id,
            "pids": sorted(verified_matches),
            "attempted_pids": sorted(attempted_pids),
            "errors": errors,
        }

    @staticmethod
    def directory_size(path: Path, *, deadline: Optional[float] = None) -> Optional[int]:
        if deadline is not None and time.monotonic() >= deadline:
            return None
        if not path.is_dir() or path.is_symlink():
            return None
        total = 0
        try:
            for current, dirnames, filenames in os.walk(str(path), topdown=True, followlinks=False):
                if deadline is not None and time.monotonic() >= deadline:
                    return None
                current_path = Path(current)
                dirnames[:] = [name for name in dirnames if not (current_path / name).is_symlink()]
                for name in filenames:
                    if deadline is not None and time.monotonic() >= deadline:
                        return None
                    candidate = current_path / name
                    if not candidate.is_symlink():
                        total += candidate.stat().st_size
        except OSError:
            return None
        return total

    def growth_metrics(self) -> Dict[str, Optional[int]]:
        deadline = time.monotonic() + max(0.0, float(self.config.get("growth_scan_budget_seconds", 5)))

        def scan(path: object) -> Optional[int]:
            return self.directory_size(Path(str(path)), deadline=deadline)

        shadow = scan(self.config["shadow_runs_path"]) if self.config.get("shadow_runs_path") else None
        cursor = scan(self.config["cursor_path"]) if self.config.get("cursor_path") else None
        log_values = [scan(path) for path in self.config.get("logs_paths", [])]
        logs = sum(value for value in log_values if value is not None) if log_values and all(value is not None for value in log_values) else None
        workspace_values = [scan(path) for path in self.config.get("workspace_roots", [])]
        workspace_total = (
            sum(value for value in workspace_values if value is not None)
            if workspace_values and all(value is not None for value in workspace_values)
            else None
        )

        eligible = deletion_eligible = inflight = backlog = None
        stale_paths: Dict[str, int] = {}
        stale_report_path = self.config.get("workspace_stale_report_path")
        if stale_report_path:
            try:
                stale_report = json.loads(Path(str(stale_report_path)).read_text(encoding="utf-8"))
                recorded_at = parse_timestamp(str(stale_report["recorded_at"]))
                maximum_age = float(self.config.get("retention_report_max_age_seconds", 1800))
                report_age = (datetime.now(timezone.utc) - recorded_at).total_seconds()
                if stale_report.get("status") == "green" and 0 <= report_age <= maximum_age:
                    for candidate in stale_report.get("stale_workdir_candidates") or []:
                        stale_paths[str(candidate.get("path") or "")] = int(candidate.get("size_bytes") or 0)
            except (KeyError, OSError, ValueError, TypeError, json.JSONDecodeError):
                pass
        report_path = self.config.get("retention_report_path")
        if report_path:
            try:
                report = json.loads(Path(str(report_path)).read_text(encoding="utf-8"))
                recorded_at = parse_timestamp(str(report["recorded_at"]))
                maximum_age = float(self.config.get("retention_report_max_age_seconds", 1800))
                report_age = (datetime.now(timezone.utc) - recorded_at).total_seconds()
                if report.get("status") == "green" and 0 <= report_age <= maximum_age:
                    strict_eligible_value = inflight_value = backlog_value = 0
                    for candidate in report.get("gc_candidates") or []:
                        size = int((candidate.get("details") or {}).get("size_bytes") or 0)
                        path = str(candidate.get("path") or "")
                        reasons = " ".join(str(value) for value in candidate.get("reasons") or [])
                        if candidate.get("eligible"):
                            strict_eligible_value += size
                        if path in stale_paths:
                            continue
                        elif any(
                            marker in reasons
                            for marker in ("not terminal", "nonterminal child", "active run", "open file", "recent write")
                        ):
                            inflight_value += size
                        elif float((candidate.get("details") or {}).get("age_seconds") or 0) >= float(self.config.get("retention_days", 7)) * 86400:
                            backlog_value += size
                    eligible = sum(stale_paths.values()) if stale_paths else strict_eligible_value
                    deletion_eligible = strict_eligible_value
                    inflight, backlog = inflight_value, backlog_value
            except (KeyError, OSError, ValueError, TypeError, json.JSONDecodeError):
                pass
        unclassified = None
        if workspace_total is not None and eligible is not None and inflight is not None and backlog is not None:
            categorized = eligible + inflight + backlog
            if categorized <= workspace_total:
                unclassified = workspace_total - categorized
            else:
                eligible = inflight = backlog = None
        return {
            "shadow_runs_bytes": shadow,
            "workspace_total_bytes": workspace_total,
            "workspace_inflight_bytes": inflight,
            "workspace_gc_eligible_bytes": eligible,
            "workspace_gc_deletion_eligible_bytes": deletion_eligible,
            "workspace_gc_backlog_bytes": backlog,
            "workspace_unclassified_bytes": unclassified,
            "cursor_bytes": cursor,
            "logs_bytes": logs,
        }

    def collect(self) -> Sample:
        daemon = self.daemon_status()
        daemon_id = str(daemon.get("daemon_id") or "") or None
        discovered, discovery_errors = self.discover_daemons()
        group = [
            (target, status)
            for target, status in discovered
            if daemon_id and status.get("daemon_id") == daemon_id
        ]
        summaries = tuple(self._instance_summary(target, status) for target, status in group)
        if group:
            active_task_count = sum(int(status.get("active_task_count") or 0) for _, status in group)
            owner_sets = [
                {str(value) for value in status.get("admission_pause_owners") or []}
                for _, status in group
            ]
            shared_owners = tuple(sorted(set.intersection(*owner_sets))) if owner_sets else ()
        else:
            active_task_count = int(daemon.get("active_task_count") or 0)
            owners_value = daemon.get("admission_pause_owners")
            shared_owners = tuple(str(value) for value in owners_value) if isinstance(owners_value, list) else ()
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
            active_task_count=active_task_count,
            daemon_pid=int(pid) if pid else None,
            daemon_status=str(daemon.get("status") or "unknown"),
            daemon_id=daemon_id,
            daemon_instances=summaries,
            daemon_discovery_errors=tuple(discovery_errors),
            admission_pause_owners=shared_owners,
        )

    def enrich(self, sample: Sample) -> Sample:
        cache_path = Path(
            str(
                self.config.get("growth_cache_path")
                or Path(str(self.config["state_path"])).with_name("growth-metrics-cache.json")
            )
        )
        interval = float(self.config.get("growth_scan_interval_seconds", 900))
        now = datetime.now(timezone.utc)
        cached = load_json(cache_path, {})
        try:
            cached_age = (now - parse_timestamp(str(cached["recorded_at"]))).total_seconds()
            cached_values = cached["values"]
        except (KeyError, TypeError, ValueError):
            cached_age = float("inf")
            cached_values = {}
        if cached_age < interval:
            return replace(sample, **cached_values)
        values = self.growth_metrics()
        if any(value is not None for value in values.values()):
            atomic_write_json(cache_path, {"recorded_at": now.isoformat(), "values": values})
            return replace(sample, **values)
        if cached_age < interval * 2:
            return replace(sample, **cached_values)
        return replace(sample, **values)


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
    expected_interval_seconds: float = 3600,
    maximum_window_hours: float = 72,
    minimum_coverage: float = 0.8,
    required_growth_fields: Iterable[str] = (),
    required_field_max_gap_seconds: float = 1800,
    discarded_sample_count: int = 0,
    external_safety_floor_bytes: int = 100 * GIB,
) -> Dict[str, Any]:
    ordered_all = sorted(samples, key=lambda item: parse_timestamp(str(item["recorded_at"])))
    if ordered_all:
        cutoff = parse_timestamp(str(ordered_all[-1]["recorded_at"])) - timedelta(hours=maximum_window_hours)
        ordered = [item for item in ordered_all if parse_timestamp(str(item["recorded_at"])) >= cutoff]
    else:
        ordered = []
    report: Dict[str, Any] = {
        "schema": "multica.storage-capacity.v1",
        "status": "INCONCLUSIVE",
        "sample_count": len(ordered),
        "discarded_sample_count": discarded_sample_count,
        "observation_hours": 0.0,
        "coverage_ratio": 0.0,
        "maximum_gap_seconds": None,
        "p95_growth_bytes_per_hour": None,
        "peak_growth_bytes_per_hour": None,
        "category_p95_growth_bytes_per_hour": {},
        "field_coverage_ratio": {},
        "field_maximum_gap_seconds": {},
        "safety_floor_bytes": safety_floor_bytes,
        "burst_reserve_bytes": burst_reserve_bytes,
        "days_remaining": None,
        "external_safety_floor_bytes": external_safety_floor_bytes,
        "external_days_remaining": None,
    }
    if len(ordered) < 2:
        return report
    start = parse_timestamp(str(ordered[0]["recorded_at"]))
    end = parse_timestamp(str(ordered[-1]["recorded_at"]))
    observation_hours = max(0.0, (end - start).total_seconds() / 3600)
    report["observation_hours"] = observation_hours
    gaps = [
        (parse_timestamp(str(current["recorded_at"])) - parse_timestamp(str(previous["recorded_at"]))).total_seconds()
        for previous, current in zip(ordered, ordered[1:])
    ]
    maximum_gap = max(gaps) if gaps else None
    coverage = min(1.0, ((len(ordered) - 1) * expected_interval_seconds) / max(1.0, (end - start).total_seconds()))
    report["coverage_ratio"] = coverage
    report["maximum_gap_seconds"] = maximum_gap
    minimum_samples = math.ceil(minimum_hours * 3600 / expected_interval_seconds * minimum_coverage) + 1
    required_fields = list(required_growth_fields)
    fields_ready = True
    for field in required_fields:
        available = [item for item in ordered if item.get(field) is not None]
        coverage_value = len(available) / len(ordered)
        report["field_coverage_ratio"][field] = coverage_value
        field_times = [parse_timestamp(str(item["recorded_at"])) for item in available]
        field_gaps = [(current - previous).total_seconds() for previous, current in zip(field_times, field_times[1:])]
        field_maximum_gap = max(field_gaps) if field_gaps else None
        report["field_maximum_gap_seconds"][field] = field_maximum_gap
        latest_age = (end - field_times[-1]).total_seconds() if field_times else float("inf")
        if (
            coverage_value < minimum_coverage
            or field_maximum_gap is None
            or field_maximum_gap > required_field_max_gap_seconds
            or latest_age > required_field_max_gap_seconds
        ):
            fields_ready = False
    if (
        observation_hours < minimum_hours
        or len(ordered) < minimum_samples
        or coverage < minimum_coverage
        or maximum_gap is None
        or maximum_gap > expected_interval_seconds * 3
        or not fields_ready
    ):
        return report

    hourly_growth: Dict[str, float] = {}
    hourly_categories: Dict[str, Dict[str, float]] = {field: {} for field in required_fields}
    for previous, current in zip(ordered, ordered[1:]):
        previous_at = parse_timestamp(str(previous["recorded_at"]))
        current_at = parse_timestamp(str(current["recorded_at"]))
        elapsed_seconds = (current_at - previous_at).total_seconds()
        if elapsed_seconds <= 0 or elapsed_seconds > expected_interval_seconds * 3:
            continue
        bucket = current_at.replace(minute=0, second=0, microsecond=0).isoformat()
        delta = int(previous["internal_free_bytes"]) - int(current["internal_free_bytes"])
        hourly_growth[bucket] = hourly_growth.get(bucket, 0.0) + delta
        for field in required_fields:
            if previous.get(field) is None or current.get(field) is None:
                continue
            if field.endswith("_free_bytes"):
                category_delta = int(previous[field]) - int(current[field])
            else:
                category_delta = int(current[field]) - int(previous[field])
            values = hourly_categories[field]
            values[bucket] = values.get(bucket, 0.0) + category_delta
    growth_rates = [max(0.0, value) for value in hourly_growth.values()]
    if not growth_rates:
        return report

    sorted_rates = sorted(growth_rates)
    p95 = sorted_rates[max(0, math.ceil(0.95 * len(sorted_rates)) - 1)]
    report["status"] = "READY"
    report["p95_growth_bytes_per_hour"] = p95
    report["peak_growth_bytes_per_hour"] = max(growth_rates)
    for field, buckets in hourly_categories.items():
        values = sorted(max(0.0, value) for value in buckets.values())
        report["category_p95_growth_bytes_per_hour"][field] = (
            values[max(0, math.ceil(0.95 * len(values)) - 1)] if values else None
        )
    latest_free = int(ordered[-1]["internal_free_bytes"])
    budget = max(0, latest_free - safety_floor_bytes - burst_reserve_bytes)
    report["days_remaining"] = (budget / p95 / 24) if p95 > 0 else None
    external_growth = report["category_p95_growth_bytes_per_hour"].get("external_free_bytes")
    if external_growth is not None and ordered[-1].get("external_free_bytes") is not None:
        external_budget = max(0, int(ordered[-1]["external_free_bytes"]) - external_safety_floor_bytes)
        report["external_days_remaining"] = (
            external_budget / float(external_growth) / 24 if float(external_growth) > 0 else None
        )
    return report


def load_json(path: Path, default: Dict[str, Any]) -> Dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return dict(default)
    return value if isinstance(value, dict) else dict(default)


def read_jsonl_with_errors(path: Path) -> Tuple[List[Dict[str, Any]], int]:
    values: List[Dict[str, Any]] = []
    discarded = 0
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except FileNotFoundError:
        return values, discarded
    for line in lines:
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            discarded += 1
            continue
        if isinstance(value, dict) and "recorded_at" in value and "internal_free_bytes" in value:
            values.append(value)
        else:
            discarded += 1
    return values, discarded


def read_jsonl(path: Path) -> List[Dict[str, Any]]:
    values, _ = read_jsonl_with_errors(path)
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

    def stop_launchagent(self, label: str, actions: List[str]) -> bool:
        target = "gui/%d/%s" % (self.uid, label)
        disable_code, _, disable_error = self.runner.run(["launchctl", "disable", target], tolerate_failure=True)
        bootout_code, _, bootout_error = self.runner.run(["launchctl", "bootout", target], tolerate_failure=True)
        print_code, _, print_error = self.runner.run(["launchctl", "print", target], tolerate_failure=True)
        print_not_found = print_code != 0 and any(
            marker in (print_error or "").lower()
            for marker in ("not found", "could not find service", "no such process")
        )
        if disable_code == 0 and print_not_found:
            actions.append("launchagent_stopped:" + label)
            return True
        detail = (print_error or bootout_error or disable_error).strip()[:120]
        actions.append("launchagent_stop_failed:%s:%s" % (label, detail))
        return False

    def pause_admission(self, sample: Sample, state: Dict[str, Any], actions: List[str]) -> bool:
        if not sample.daemon_id or not hasattr(self.collector, "apply_admission_group"):
            actions.append("daemon_admission_pause_failed:daemon group discovery unavailable")
            actions.append("legacy_sigstop_disabled:unsafe_without_shared_claim_barrier")
            return False
        response = self.collector.apply_admission_group(
            "pause", daemon_id=sample.daemon_id, owner="storage-guard"
        )
        if response.get("ok") is True:
            state["legacy_daemon_sigstopped"] = False
            state["legacy_daemon_pid"] = None
            state["legacy_daemon_command"] = None
            state["admission_pause_owned"] = True
            state["admission_group_id"] = sample.daemon_id
            state["admission_group_pids"] = response.get("pids") or []
            actions.append("daemon_admission_paused")
            actions.append(
                "daemon_admission_group_verified:%s"
                % ",".join(str(value) for value in response.get("pids") or [])
            )
            return True
        actions.append(
            "daemon_admission_pause_failed:"
            + "; ".join(str(value) for value in response.get("errors") or ["group verification failed"])[:400]
        )
        actions.append("legacy_sigstop_disabled:unsafe_without_shared_claim_barrier")
        return False

    def resume_admission(self, sample: Sample, state: Dict[str, Any], actions: List[str]) -> bool:
        released_legacy_fallback = False
        if (state.get("legacy_daemon_sigstopped") or state.get("legacy_daemon_sigstop_intent")) and state.get("legacy_daemon_pid"):
            pid = int(state["legacy_daemon_pid"])
            ps_code, command, _ = self.runner.run(
                ["/bin/ps", "-p", str(pid), "-o", "command="], tolerate_failure=True
            )
            expected = str(state.get("legacy_daemon_command") or "")
            if ps_code != 0 or not expected or command.strip() != expected:
                actions.append("legacy_daemon_sigcontinue_failed:pid_identity_mismatch")
                state["resume_pending"] = True
                return False
            self.runner.signal(pid, signal.SIGCONT)
            state["legacy_daemon_sigstopped"] = False
            state["legacy_daemon_sigstop_intent"] = False
            state["legacy_daemon_pid"] = None
            state["legacy_daemon_command"] = None
            atomic_write_json(Path(str(self.config["state_path"])), state)
            actions.append("legacy_daemon_sigcontinued")
            released_legacy_fallback = True
        if released_legacy_fallback and not state.get("admission_pause_owned"):
            state["resume_pending"] = False
            return True
        daemon_id = str(state.get("admission_group_id") or sample.daemon_id or "")
        if not daemon_id or not hasattr(self.collector, "apply_admission_group"):
            state["resume_pending"] = True
            actions.append("daemon_admission_resume_failed:daemon group discovery unavailable")
            return False
        response = self.collector.apply_admission_group(
            "resume", daemon_id=daemon_id, owner="storage-guard"
        )
        if response.get("ok") is not True:
            state["resume_pending"] = True
            actions.append(
                "daemon_admission_resume_failed:"
                + "; ".join(str(value) for value in response.get("errors") or ["group verification failed"])[:400]
            )
            return False
        state["admission_pause_owned"] = False
        state["resume_pending"] = False
        state["admission_group_id"] = daemon_id
        state["admission_group_pids"] = response.get("pids") or []
        actions.append("daemon_admission_resumed")
        actions.append(
            "daemon_admission_group_verified:%s"
            % ",".join(str(value) for value in response.get("pids") or [])
        )
        return True

    def alert(
        self,
        sample: Sample,
        level: int,
        state: Dict[str, Any],
        actions: List[str],
        reason: str,
        *,
        alert_key: str,
    ) -> None:
        open_id = str(self.config.get("lark_open_id") or "").strip()
        if not open_id:
            actions.append("lark_alert_skipped:no_recipient")
            return
        now = parse_timestamp(sample.recorded_at)
        alert_times = state.setdefault("last_alert_at_by_key", {})
        previous_raw = alert_times.get(alert_key)
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
            alert_times[alert_key] = sample.recorded_at
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
            {
                "level": 0,
                "legacy_daemon_sigstopped": False,
                "legacy_daemon_sigstop_intent": False,
                "legacy_daemon_pid": None,
                "admission_pause_owned": False,
                "resume_pending": False,
            },
        )
        if sample.admission_pause_owners is not None:
            state["admission_pause_owned"] = "storage-guard" in sample.admission_pause_owners
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
        enforcement_ok = True

        if level >= 1:
            for label in self.config.get("observer_labels", []):
                enforcement_ok = self.stop_launchagent(str(label), actions) and enforcement_ok
            enforcement_ok = self.pause_admission(sample, state, actions) and enforcement_ok
            if not enforcement_ok:
                self.alert(
                    sample,
                    level,
                    state,
                    actions,
                    "一级低水位执行失败，任务入场可能仍开放",
                    alert_key="level1-enforcement-failed",
                )
        elif previous >= 1 or state.get("legacy_daemon_sigstopped") or state.get("legacy_daemon_sigstop_intent") or state.get("resume_pending") or state.get("admission_pause_owned"):
            enforcement_ok = self.resume_admission(sample, state, actions)
            if not enforcement_ok:
                level = max(previous, 1)
                self.alert(
                    sample,
                    level,
                    state,
                    actions,
                    "低水位恢复失败，将持续重试 storage-guard 自有入场屏障",
                    alert_key="level1-resume-failed",
                )

        if level >= 2:
            level2_enforcement_ok = True
            for label in self.config.get("nonproduction_launchagents", []):
                level2_enforcement_ok = self.stop_launchagent(str(label), actions) and level2_enforcement_ok
            enforcement_ok = level2_enforcement_ok and enforcement_ok
            self.alert(
                sample,
                level,
                state,
                actions,
                (
                    "内置盘进入二级低水位，已暂停显式列出的非生产任务"
                    if level2_enforcement_ok
                    else "内置盘进入二级低水位，但非生产任务暂停未完全生效"
                ),
                alert_key="level2-internal-low-water",
            )

        external_min = int(float(self.config.get("external_min_free_gib", 100)) * GIB)
        if sample.external_free_bytes is None:
            self.alert(
                sample,
                max(level, 1),
                state,
                actions,
                "外置归档卷不可用",
                alert_key="external-unavailable",
            )
        elif sample.external_free_bytes < external_min:
            self.alert(
                sample,
                max(level, 1),
                state,
                actions,
                "外置归档卷低于配置水位 (%.2f GiB)" % (sample.external_free_bytes / GIB),
                alert_key="external-low-water",
            )

        if hasattr(self.collector, "enrich"):
            sample = self.collector.enrich(sample)
        state.update(
            {
                "level": level,
                "previous_level": previous,
                "last_sample": asdict(sample),
                "last_actions": actions,
                "enforcement_status": "verified" if enforcement_ok else "failed",
            }
        )
        append_jsonl(metrics_path, asdict(sample))
        samples, discarded = read_jsonl_with_errors(metrics_path)
        report = build_capacity_report(
            samples,
            safety_floor_bytes=int(float(self.config.get("safety_floor_gib", 25)) * GIB),
            burst_reserve_bytes=int(float(self.config.get("burst_reserve_gib", 10)) * GIB),
            minimum_hours=float(self.config.get("minimum_observation_hours", 48)),
            expected_interval_seconds=float(self.config.get("expected_interval_seconds", 60)),
            maximum_window_hours=float(self.config.get("maximum_observation_hours", 72)),
            minimum_coverage=float(self.config.get("minimum_sample_coverage", 0.8)),
            required_growth_fields=self.config.get("required_growth_fields", []),
            required_field_max_gap_seconds=float(self.config.get("required_field_max_gap_seconds", 1800)),
            discarded_sample_count=discarded,
            external_safety_floor_bytes=int(float(self.config.get("external_min_free_gib", 100)) * GIB),
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
