#!/usr/bin/env python3
"""Fail-closed canary, workspace GC audit, and transactional archiver.

The same process owns all three operations.  A production configuration starts
with ``archive_enabled=false`` and ``delete_source=false`` so the first run can
only prove the external-volume path and emit a GC dry-run report.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Dict, Iterable, List, Optional, Tuple


GIB = 1024**3
TERMINAL_ISSUE_STATUSES = {"done", "cancelled"}
TERMINAL_RUN_STATUSES = {"completed", "failed", "cancelled"}
ACTIVE_RUN_STATUSES = {"queued", "claimed", "preparing", "running", "in_progress"}


class ArchiveError(RuntimeError):
    pass


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def parse_timestamp(value: str) -> datetime:
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def atomic_write_json(path: Path, value: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".%s." % path.name, dir=str(path.parent))
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, ensure_ascii=False, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        fsync_directory(path.parent)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def append_jsonl(path: Path, value: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(value, ensure_ascii=False, sort_keys=True) + "\n")
        handle.flush()
        os.fsync(handle.fileno())


def fsync_directory(path: Path) -> None:
    descriptor = os.open(str(path), os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def fsync_tree(root: Path) -> None:
    """Make copied regular-file contents durable before the commit rename."""

    directories: List[Path] = []
    for current, dirnames, filenames in os.walk(str(root), topdown=True, followlinks=False):
        current_path = Path(current)
        directories.append(current_path)
        dirnames[:] = [name for name in dirnames if not (current_path / name).is_symlink()]
        for name in filenames:
            path = current_path / name
            if path.is_symlink():
                continue
            descriptor = os.open(str(path), os.O_RDONLY)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
    for directory in reversed(directories):
        fsync_directory(directory)


def hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while True:
            block = handle.read(1024 * 1024)
            if not block:
                break
            digest.update(block)
    return digest.hexdigest()


def _select_samples(paths: List[str], limit: int) -> List[str]:
    if len(paths) <= limit:
        return paths
    if limit <= 1:
        return [paths[0]]
    indexes = {round(index * (len(paths) - 1) / (limit - 1)) for index in range(limit)}
    return [paths[index] for index in sorted(indexes)]


def tree_manifest(root: Path, *, sample_limit: int = 16) -> Dict[str, Any]:
    """Describe a tree without following symlinks.

    The entry list freezes every file's size and mtime while hashes provide a
    deterministic content sample.  Both are compared before source deletion.
    """

    if not root.is_dir() or root.is_symlink():
        raise ArchiveError("archive source must be a real directory: %s" % root)
    directories: List[str] = []
    files: List[Dict[str, Any]] = []
    symlinks: List[Dict[str, str]] = []
    regular_paths: List[str] = []
    for current, dirnames, filenames in os.walk(str(root), topdown=True, followlinks=False):
        current_path = Path(current)
        kept_dirs: List[str] = []
        for name in sorted(dirnames):
            path = current_path / name
            relative = path.relative_to(root).as_posix()
            if path.is_symlink():
                symlinks.append({"path": relative, "target": os.readlink(str(path))})
            else:
                directories.append(relative)
                kept_dirs.append(name)
        dirnames[:] = kept_dirs
        for name in sorted(filenames):
            path = current_path / name
            relative = path.relative_to(root).as_posix()
            if path.is_symlink():
                symlinks.append({"path": relative, "target": os.readlink(str(path))})
                continue
            stat = path.lstat()
            if not path.is_file():
                raise ArchiveError("unsupported non-regular entry: %s" % path)
            files.append({"path": relative, "size": stat.st_size, "mtime_ns": stat.st_mtime_ns})
            regular_paths.append(relative)
    files.sort(key=lambda item: str(item["path"]))
    directories.sort()
    symlinks.sort(key=lambda item: item["path"])
    samples = _select_samples(sorted(regular_paths), sample_limit)
    return {
        "file_count": len(files),
        "directory_count": len(directories),
        "symlink_count": len(symlinks),
        "total_bytes": sum(int(item["size"]) for item in files),
        "directories": directories,
        "files": files,
        "symlinks": symlinks,
        "sample_hashes": {relative: hash_file(root / relative) for relative in samples},
    }


def read_volume_uuid(path: Path) -> str:
    process = subprocess.run(
        ["/usr/sbin/diskutil", "info", "-plist", str(path)],
        capture_output=True,
        check=False,
    )
    if process.returncode != 0:
        raise ArchiveError("diskutil could not inspect external volume")
    try:
        value = plistlib.loads(process.stdout)
    except (plistlib.InvalidFileException, ValueError) as error:
        raise ArchiveError("diskutil returned invalid volume metadata") from error
    volume_uuid = value.get("VolumeUUID")
    if not volume_uuid:
        raise ArchiveError("external volume has no VolumeUUID")
    return str(volume_uuid).upper()


def available_bytes(path: Path) -> int:
    stat = os.statvfs(str(path))
    return stat.f_bavail * stat.f_frsize


class Canary:
    def __init__(
        self,
        destination: Path,
        *,
        expected_uuid: str,
        min_free_bytes: int,
        uuid_reader: Callable[[Path], str] = read_volume_uuid,
        free_bytes_reader: Callable[[Path], int] = available_bytes,
    ):
        self.destination = destination
        self.expected_uuid = expected_uuid.upper()
        self.min_free_bytes = min_free_bytes
        self.uuid_reader = uuid_reader
        self.free_bytes_reader = free_bytes_reader

    def run(self) -> Dict[str, Any]:
        actual_uuid = self.uuid_reader(self.destination).upper()
        if actual_uuid != self.expected_uuid:
            raise ArchiveError(
                "external volume UUID mismatch: expected %s, got %s"
                % (self.expected_uuid, actual_uuid)
            )
        free_bytes = self.free_bytes_reader(self.destination)
        if free_bytes < self.min_free_bytes:
            raise ArchiveError(
                "external volume below low-water mark: %d < %d bytes"
                % (free_bytes, self.min_free_bytes)
            )

        canary_root = self.destination / ".multica-storage-canary"
        canary_root.mkdir(parents=True, exist_ok=True)
        run_name = "%s-%s" % (utc_now().strftime("%Y%m%dT%H%M%S.%fZ"), uuid.uuid4().hex[:8])
        partial = canary_root / (run_name + ".partial")
        final = canary_root / run_name
        (partial / "nested" / "deeper").mkdir(parents=True)
        (partial / "root.txt").write_text("multica storage canary\n", encoding="utf-8")
        (partial / "nested" / "payload.bin").write_bytes(bytes(range(128)))
        (partial / "nested" / "deeper" / "empty").write_bytes(b"")
        fsync_tree(partial)
        expected = tree_manifest(partial)
        os.replace(str(partial), str(final))
        fsync_directory(canary_root)
        actual = tree_manifest(final)
        if expected != actual:
            raise ArchiveError("canary verification failed after atomic rename")
        result = {
            "schema": "multica.external-volume-canary.v1",
            "status": "green",
            "checked_at": utc_now().isoformat(),
            "volume_uuid": actual_uuid,
            "free_bytes": free_bytes,
            "minimum_free_bytes": self.min_free_bytes,
            "path": str(final),
            "manifest": actual,
        }
        atomic_write_json(final / "CANARY.json", result)
        return result


class SingleInstanceLock:
    def __init__(self, path: Path):
        self.path = path
        self.handle: Optional[Any] = None

    def __enter__(self) -> "SingleInstanceLock":
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.handle = self.path.open("a+")
        try:
            fcntl.flock(self.handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            self.handle.close()
            self.handle = None
            raise
        self.handle.seek(0)
        self.handle.truncate()
        self.handle.write("%d\n" % os.getpid())
        self.handle.flush()
        return self

    def __exit__(self, exc_type: Any, exc: Any, traceback: Any) -> None:
        if self.handle is not None:
            fcntl.flock(self.handle.fileno(), fcntl.LOCK_UN)
            self.handle.close()
            self.handle = None


class ArchiveManager:
    def __init__(self, destination: Path, *, fail_at: Optional[str] = None):
        self.destination = destination
        self.fail_at = fail_at

    def _fail(self, phase: str) -> None:
        if self.fail_at == phase:
            raise ArchiveError("injected failure at %s" % phase)

    def archive(
        self,
        source: Path,
        candidate_id: str,
        *,
        delete_source: bool = False,
        delete_gate: Optional[Callable[[], bool]] = None,
    ) -> Dict[str, Any]:
        source = source.absolute()
        if not source.is_dir() or source.is_symlink():
            raise ArchiveError("refusing unsafe source: %s" % source)
        destination = self.destination.absolute()
        if source == destination or source in destination.parents or destination in source.parents:
            raise ArchiveError("source and archive destination must not contain one another")
        self.destination.mkdir(parents=True, exist_ok=True)

        frozen = tree_manifest(source)
        suffix = utc_now().strftime("%Y%m%dT%H%M%S.%fZ")
        final = self.destination / (candidate_id + "-" + suffix)
        partial = self.destination / (final.name + ".partial")
        if partial.exists() or final.exists():
            raise ArchiveError("archive destination already exists")

        try:
            shutil.copytree(str(source), str(partial), symlinks=True, copy_function=shutil.copy2)
            fsync_tree(partial)
            fsync_directory(self.destination)
            self._fail("after_copy")

            copied = tree_manifest(partial)
            source_after_copy = tree_manifest(source)
            if frozen != source_after_copy:
                raise ArchiveError("source changed while archive copy was in progress")
            if frozen != copied:
                raise ArchiveError("archive count/bytes/entry/sample verification failed")
            self._fail("after_verify")

            os.replace(str(partial), str(final))
            fsync_directory(self.destination)
            self._fail("after_rename")

            marker = {
                "schema": "multica.transactional-archive.v1",
                "completed_at": utc_now().isoformat(),
                "candidate_id": candidate_id,
                "source_path": str(source),
                "archive_path": str(final),
                "source_manifest": frozen,
                "archive_manifest": copied,
                "verified_before_source_delete": True,
            }
            atomic_write_json(final / "COMPLETE.json", marker)
            fsync_directory(final)
            self._fail("after_complete")

            source_before_delete = tree_manifest(source)
            persisted = json.loads((final / "COMPLETE.json").read_text(encoding="utf-8"))
            if source_before_delete != frozen or persisted != marker:
                raise ArchiveError("source or COMPLETE marker changed before delete gate")
            if delete_source:
                if delete_gate is not None and not delete_gate():
                    raise ArchiveError("fresh control-plane delete gate rejected candidate")
                shutil.rmtree(str(source))
                fsync_directory(source.parent)
            return {
                "status": "complete",
                "source_path": str(source),
                "archive_path": str(final),
                "source_deleted": delete_source,
                "manifest": frozen,
            }
        except ArchiveError:
            raise
        except Exception as error:
            raise ArchiveError("transactional archive failed: %s" % error) from error


class MulticaIssueClient:
    def _json(self, argv: List[str]) -> Any:
        process = subprocess.run(argv, capture_output=True, text=True, check=False)
        if process.returncode != 0:
            detail = (process.stderr or process.stdout).strip()
            raise ArchiveError("multica query failed: %s" % detail)
        try:
            return json.loads(process.stdout)
        except json.JSONDecodeError as error:
            raise ArchiveError("multica query returned invalid JSON") from error

    def get_issue(self, issue_id: str) -> Dict[str, Any]:
        value = self._json(["multica", "issue", "get", issue_id, "--output", "json"])
        if not isinstance(value, dict):
            raise ArchiveError("issue response is not an object")
        return value

    def get_runs(self, issue_id: str) -> List[Dict[str, Any]]:
        value = self._json(["multica", "issue", "runs", issue_id, "--output", "json"])
        return [item for item in value if isinstance(item, dict)] if isinstance(value, list) else []

    def get_children(self, issue_id: str) -> List[Dict[str, Any]]:
        value = self._json(["multica", "issue", "children", issue_id, "--output", "json"])
        if not isinstance(value, dict):
            return []
        children: List[Dict[str, Any]] = []
        for item in value.get("unstaged") or []:
            if isinstance(item, dict):
                children.append(item)
        for stage in value.get("stages") or []:
            if not isinstance(stage, dict):
                continue
            for item in stage.get("issues") or stage.get("children") or []:
                if isinstance(item, dict):
                    children.append(item)
        return children


def default_open_file_checker(path: Path) -> bool:
    process = subprocess.run(
        ["/usr/sbin/lsof", "+D", str(path)],
        capture_output=True,
        text=True,
        check=False,
    )
    if process.returncode not in (0, 1):
        raise ArchiveError("lsof failed while checking %s" % path)
    lines = [line for line in process.stdout.splitlines() if line.strip()]
    return len(lines) > 1


def _latest_mtime_and_unsafe_symlinks(root: Path) -> Tuple[float, List[str]]:
    latest = root.lstat().st_mtime
    unsafe: List[str] = []
    resolved_root = root.resolve()
    for current, dirnames, filenames in os.walk(str(root), topdown=True, followlinks=False):
        current_path = Path(current)
        kept_dirs: List[str] = []
        for name in dirnames:
            path = current_path / name
            stat = path.lstat()
            latest = max(latest, stat.st_mtime)
            if path.is_symlink():
                try:
                    path.resolve(strict=False).relative_to(resolved_root)
                except ValueError:
                    unsafe.append(path.relative_to(root).as_posix())
            else:
                kept_dirs.append(name)
        dirnames[:] = kept_dirs
        for name in filenames:
            path = current_path / name
            stat = path.lstat()
            latest = max(latest, stat.st_mtime)
            if path.is_symlink():
                try:
                    path.resolve(strict=False).relative_to(resolved_root)
                except ValueError:
                    unsafe.append(path.relative_to(root).as_posix())
    return latest, sorted(unsafe)


class GCEvaluator:
    def __init__(
        self,
        client: Any,
        *,
        now: Callable[[], datetime] = utc_now,
        open_file_checker: Callable[[Path], bool] = default_open_file_checker,
        retention_seconds: int = 7 * 86400,
        recent_write_seconds: int = 24 * 3600,
    ):
        self.client = client
        self.now = now
        self.open_file_checker = open_file_checker
        self.retention_seconds = retention_seconds
        self.recent_write_seconds = recent_write_seconds

    def evaluate(self, candidate: Path) -> Dict[str, Any]:
        reasons: List[str] = []
        details: Dict[str, Any] = {}
        meta_path = candidate / ".gc_meta.json"
        context_path = candidate / "workdir" / ".multica" / "daemon_task_context.json"
        try:
            meta = json.loads(meta_path.read_text(encoding="utf-8"))
            context = json.loads(context_path.read_text(encoding="utf-8"))
        except (FileNotFoundError, OSError, json.JSONDecodeError) as error:
            return {
                "path": str(candidate),
                "eligible": False,
                "reasons": ["identity metadata unreadable: %s" % error],
            }
        if not isinstance(meta, dict) or not isinstance(context, dict):
            return {"path": str(candidate), "eligible": False, "reasons": ["identity metadata invalid"]}

        issue_id = str(meta.get("issue_id") or "")
        workspace_id = str(meta.get("workspace_id") or "")
        if (
            meta.get("kind") != "issue"
            or not issue_id
            or not workspace_id
            or candidate.parent.name != workspace_id
            or context.get("managed_by") != "multica-daemon-task"
            or context.get("issue_id") != issue_id
        ):
            reasons.append("identity metadata does not match directory")

        try:
            completed_at = parse_timestamp(str(meta.get("completed_at") or ""))
            age = (self.now() - completed_at).total_seconds()
            details["completed_at"] = completed_at.isoformat()
            details["age_seconds"] = age
            if age < self.retention_seconds:
                reasons.append("retention window has not elapsed")
        except (ValueError, TypeError):
            completed_at = None
            reasons.append("completed_at is invalid")

        try:
            issue = self.client.get_issue(issue_id)
            runs = self.client.get_runs(issue_id)
            children = self.client.get_children(issue_id)
        except Exception as error:
            reasons.append("control-plane lookup failed closed: %s" % error)
            issue, runs, children = {}, [], []

        if issue.get("status") not in TERMINAL_ISSUE_STATUSES:
            reasons.append("issue is not in an irreversible terminal status")
        if issue.get("id") != issue_id or issue.get("workspace_id") != workspace_id:
            reasons.append("identity differs from control plane")
        metadata = issue.get("metadata") if isinstance(issue.get("metadata"), dict) else {}
        if any(metadata.get(key) for key in ("gc_pin", "pinned", "retention_pin")) or (candidate / ".gc-pin").exists():
            reasons.append("candidate has a retention pin")
        if any(child.get("status") not in TERMINAL_ISSUE_STATUSES for child in children):
            reasons.append("nonterminal child or supplement verification exists")
        if any(run.get("status") in ACTIVE_RUN_STATUSES for run in runs):
            reasons.append("active run or lease exists")

        matching_runs: List[Dict[str, Any]] = []
        for run in runs:
            run_id = str(run.get("id") or "")
            work_dir = Path(str(run.get("work_dir") or ""))
            if run_id.startswith(candidate.name) and work_dir == candidate / "workdir":
                matching_runs.append(run)
        if len(matching_runs) != 1:
            reasons.append("identity has no unique matching task run")
        else:
            run = matching_runs[0]
            if (
                run.get("issue_id") not in (None, issue_id)
                or run.get("workspace_id") not in (None, workspace_id)
                or run.get("status") not in TERMINAL_RUN_STATUSES
            ):
                reasons.append("identity or terminal state differs from task run")
            if completed_at is not None:
                try:
                    run_completed = parse_timestamp(str(run.get("completed_at") or ""))
                    if abs((run_completed - completed_at).total_seconds()) > 1:
                        reasons.append("identity completed_at differs from task run")
                except ValueError:
                    reasons.append("task run completed_at is invalid")

        try:
            if self.open_file_checker(candidate):
                reasons.append("open file exists under candidate")
        except Exception as error:
            reasons.append("open file check failed closed: %s" % error)
        try:
            latest_mtime, unsafe_symlinks = _latest_mtime_and_unsafe_symlinks(candidate)
            details["latest_mtime"] = datetime.fromtimestamp(latest_mtime, timezone.utc).isoformat()
            if self.now().timestamp() - latest_mtime < self.recent_write_seconds:
                reasons.append("recent write exists under candidate")
            if unsafe_symlinks:
                reasons.append("out-of-bound symlink present (not followed): %s" % ", ".join(unsafe_symlinks))
        except OSError as error:
            reasons.append("filesystem scan failed closed: %s" % error)

        return {
            "path": str(candidate),
            "issue_id": issue_id,
            "workspace_id": workspace_id,
            "eligible": not reasons,
            "reasons": reasons,
            "details": details,
        }


def discover_candidates(roots: Iterable[Path]) -> List[Path]:
    candidates: List[Path] = []
    for root in roots:
        if not root.is_dir() or root.is_symlink():
            continue
        for workspace in sorted(root.iterdir()):
            if not workspace.is_dir() or workspace.is_symlink() or workspace.name.startswith("."):
                continue
            for candidate in sorted(workspace.iterdir()):
                if candidate.is_dir() and not candidate.is_symlink() and (candidate / ".gc_meta.json").is_file():
                    candidates.append(candidate)
    return candidates


def send_alert(config: Dict[str, Any], message: str) -> None:
    alert_path = Path(str(config["alert_log_path"]))
    append_jsonl(alert_path, {"recorded_at": utc_now().isoformat(), "message": message})
    open_id = str(config.get("lark_open_id") or "")
    if not open_id:
        return
    subprocess.run(
        [
            "lark-cli",
            "im",
            "+messages-send",
            "--as",
            "bot",
            "--user-id",
            open_id,
            "--text",
            message,
            "--format",
            "json",
        ],
        capture_output=True,
        text=True,
        check=False,
    )


def run_worker(config: Dict[str, Any]) -> Dict[str, Any]:
    if config.get("require_cron_lineage", True) and os.environ.get("MULTICA_STORAGE_CRON") != "1":
        raise ArchiveError("formal cron lineage marker is missing")
    external_path = Path(str(config["external_path"]))
    canary = Canary(
        external_path,
        expected_uuid=str(config["external_volume_uuid"]),
        min_free_bytes=int(float(config.get("external_min_free_gib", 100)) * GIB),
    ).run()
    evaluator = GCEvaluator(
        MulticaIssueClient(),
        retention_seconds=int(float(config.get("retention_days", 7)) * 86400),
        recent_write_seconds=int(config.get("recent_write_seconds", 86400)),
    )
    candidates = [
        evaluator.evaluate(path)
        for path in discover_candidates(Path(str(item)) for item in config.get("workspace_roots", []))
    ]
    report: Dict[str, Any] = {
        "schema": "multica.storage-retention-run.v1",
        "recorded_at": utc_now().isoformat(),
        "pid": os.getpid(),
        "parent_pid": os.getppid(),
        "invocation_source": "formal-cron" if os.environ.get("MULTICA_STORAGE_CRON") == "1" else "manual",
        "canary": canary,
        "gc_mode": "dry-run",
        "gc_candidates": candidates,
        "eligible_count": sum(1 for candidate in candidates if candidate["eligible"]),
        "archive_enabled": bool(config.get("archive_enabled", False)),
        "delete_source": bool(config.get("delete_source", False)),
        "archives": [],
    }
    if config.get("archive_enabled", False):
        manager = ArchiveManager(Path(str(config["archive_root"])))
        approved = {str(value) for value in config.get("approved_candidate_ids", [])}
        for candidate in candidates:
            candidate_path = Path(str(candidate["path"]))
            if not candidate["eligible"] or candidate_path.name not in approved:
                continue
            fresh = evaluator.evaluate(candidate_path)
            if not fresh["eligible"]:
                continue
            report["archives"].append(
                manager.archive(
                    candidate_path,
                    candidate_path.name,
                    delete_source=bool(config.get("delete_source", False)),
                    delete_gate=lambda path=candidate_path: evaluator.evaluate(path)["eligible"],
                )
            )
    atomic_write_json(Path(str(config["report_path"])), report)
    return report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    args = parser.parse_args()
    try:
        config = json.loads(Path(args.config).read_text(encoding="utf-8"))
        with SingleInstanceLock(Path(str(config["lock_path"]))):
            report = run_worker(config)
    except BlockingIOError:
        message = "storage retention worker skipped: another owner holds the single-instance lock"
        try:
            send_alert(config, message)
        except Exception:
            pass
        print(json.dumps({"status": "locked", "error": message}), file=sys.stderr)
        return 75
    except Exception as error:
        message = "storage retention worker failed closed: %s" % error
        try:
            send_alert(config, message)
        except Exception:
            pass
        print(json.dumps({"status": "red", "error": message}, ensure_ascii=False), file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
