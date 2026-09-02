#!/usr/bin/env python3
"""Fail-closed canary, workspace GC audit, and transactional archiver.

The same process owns all three operations.  A production configuration starts
with ``archive_enabled=false`` and ``delete_source=false`` so the first run can
only prove the external-volume path and emit a GC dry-run report.
"""

from __future__ import annotations

import argparse
import concurrent.futures
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


def tree_manifest(
    root: Path,
    *,
    sample_limit: int = 16,
    excluded_relative_paths: Iterable[str] = (),
) -> Dict[str, Any]:
    """Describe a tree without following symlinks.

    The entry list freezes every file's size and mtime while hashes provide a
    deterministic content sample.  Both are compared before source deletion.
    """

    if not root.is_dir() or root.is_symlink():
        raise ArchiveError("archive source must be a real directory: %s" % root)
    excluded = set(excluded_relative_paths)
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
            if relative in excluded:
                continue
            if path.is_symlink():
                symlinks.append({"path": relative, "target": os.readlink(str(path))})
            else:
                directories.append(relative)
                kept_dirs.append(name)
        dirnames[:] = kept_dirs
        for name in sorted(filenames):
            path = current_path / name
            relative = path.relative_to(root).as_posix()
            if relative in excluded:
                continue
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
    content_hashes = {relative: hash_file(root / relative) for relative in sorted(regular_paths)}
    return {
        "file_count": len(files),
        "directory_count": len(directories),
        "symlink_count": len(symlinks),
        "total_bytes": sum(int(item["size"]) for item in files),
        "directories": directories,
        "files": files,
        "symlinks": symlinks,
        "content_hashes": content_hashes,
        "sample_hashes": {relative: content_hashes[relative] for relative in samples},
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
        volume_path: Optional[Path] = None,
        uuid_reader: Callable[[Path], str] = read_volume_uuid,
        free_bytes_reader: Callable[[Path], int] = available_bytes,
        postflight: Optional[Callable[[], Dict[str, Any]]] = None,
    ):
        self.destination = destination
        self.expected_uuid = expected_uuid.upper()
        self.min_free_bytes = min_free_bytes
        self.volume_path = volume_path or destination
        self.uuid_reader = uuid_reader
        self.free_bytes_reader = free_bytes_reader
        self.postflight = postflight

    def run(self) -> Dict[str, Any]:
        actual_uuid = self.uuid_reader(self.volume_path).upper()
        if actual_uuid != self.expected_uuid:
            raise ArchiveError(
                "external volume UUID mismatch: expected %s, got %s"
                % (self.expected_uuid, actual_uuid)
            )
        free_bytes = self.free_bytes_reader(self.volume_path)
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
        if self.postflight is not None:
            verified_volume = self.postflight()
            actual_uuid = str(verified_volume["volume_uuid"])
            free_bytes = int(verified_volume["free_bytes"])
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
        completed = sorted(
            (path for path in canary_root.iterdir() if path.is_dir() and not path.name.endswith(".partial")),
            key=lambda path: path.name,
            reverse=True,
        )
        for stale in completed[96:]:
            shutil.rmtree(str(stale))
        return result


class ExternalVolumeGuard:
    """Bind archive writes to one physical external volume at every phase."""

    def __init__(
        self,
        external_root: Path,
        archive_root: Path,
        *,
        expected_uuid: str,
        min_free_bytes: int,
        uuid_reader: Callable[[Path], str] = read_volume_uuid,
        free_bytes_reader: Callable[[Path], int] = available_bytes,
    ):
        self.external_root = external_root
        self.archive_root = archive_root
        self.expected_uuid = expected_uuid.upper()
        self.min_free_bytes = min_free_bytes
        self.uuid_reader = uuid_reader
        self.free_bytes_reader = free_bytes_reader

    def check(self, required_bytes: int = 0) -> Dict[str, Any]:
        if self.external_root.is_symlink() or self.archive_root.is_symlink():
            raise ArchiveError("external or archive root must not be a symlink")
        external = self.external_root.resolve(strict=True)
        archive = self.archive_root.resolve(strict=True)
        try:
            archive.relative_to(external)
        except ValueError as error:
            raise ArchiveError("archive root is outside the verified external root") from error
        if external.stat().st_dev != archive.stat().st_dev:
            raise ArchiveError("archive root is not on the verified external device")
        actual_uuid = self.uuid_reader(external).upper()
        if actual_uuid != self.expected_uuid:
            raise ArchiveError("archive volume UUID changed before commit")
        free_bytes = self.free_bytes_reader(archive)
        reserve = self.min_free_bytes + int(required_bytes * 1.10)
        if free_bytes < reserve:
            raise ArchiveError(
                "archive volume lacks candidate budget: %d < %d bytes" % (free_bytes, reserve)
            )
        return {"volume_uuid": actual_uuid, "free_bytes": free_bytes, "required_reserve_bytes": reserve}


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
    def __init__(
        self,
        destination: Path,
        *,
        fail_at: Optional[str] = None,
        preflight: Optional[Callable[[int], Any]] = None,
    ):
        self.destination = destination
        self.fail_at = fail_at
        self.preflight = preflight

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
        approval_token: Optional[str] = None,
        post_commit_hook: Optional[Callable[[Path], None]] = None,
    ) -> Dict[str, Any]:
        if delete_source:
            raise ArchiveError("source deletion requires a producer lease and is disabled")
        source = source.absolute()
        if not source.is_dir() or source.is_symlink():
            raise ArchiveError("refusing unsafe source: %s" % source)
        destination = self.destination.absolute()
        if source == destination or source in destination.parents or destination in source.parents:
            raise ArchiveError("source and archive destination must not contain one another")
        self.destination.mkdir(parents=True, exist_ok=True)

        frozen = tree_manifest(source)
        if self.preflight is not None:
            self.preflight(int(frozen["total_bytes"]))
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

            if self.preflight is not None:
                self.preflight(0)
            os.replace(str(partial), str(final))
            fsync_directory(self.destination)
            self._fail("after_rename")

            if post_commit_hook is not None:
                post_commit_hook(final)
            committed = tree_manifest(final)
            if committed != frozen:
                raise ArchiveError("committed archive changed before COMPLETE marker")

            marker = {
                "schema": "multica.transactional-archive.v1",
                "completed_at": utc_now().isoformat(),
                "candidate_id": candidate_id,
                "source_path": str(source),
                "archive_path": str(final),
                "source_manifest": frozen,
                "archive_manifest": committed,
                "approval_token": approval_token,
                "source_delete_enabled": False,
            }
            atomic_write_json(final / "COMPLETE.json", marker)
            fsync_directory(final)
            self._fail("after_complete")

            source_before_delete = tree_manifest(source)
            persisted = json.loads((final / "COMPLETE.json").read_text(encoding="utf-8"))
            if source_before_delete != frozen or persisted != marker:
                raise ArchiveError("source or COMPLETE marker changed before delete gate")
            return {
                "status": "complete",
                "source_path": str(source),
                "archive_path": str(final),
                "source_deleted": False,
                "manifest": frozen,
            }
        except ArchiveError:
            raise
        except Exception as error:
            raise ArchiveError("transactional archive failed: %s" % error) from error


class MulticaIssueClient:
    def _json(self, argv: List[str]) -> Any:
        try:
            process = subprocess.run(argv, capture_output=True, text=True, check=False, timeout=15)
        except subprocess.TimeoutExpired as error:
            raise ArchiveError("multica query timed out") from error
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
        if not isinstance(value, dict) or "stages" not in value or "unstaged" not in value or "total" not in value:
            raise ArchiveError("children response schema is unknown")
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
        if int(value["total"]) != len(children):
            raise ArchiveError("children response count does not match parsed entries")
        return children


def default_open_file_checker(path: Path) -> bool:
    process = subprocess.run(
        ["/usr/sbin/lsof", "+D", str(path)],
        capture_output=True,
        text=True,
        check=False,
        timeout=30,
    )
    if process.returncode not in (0, 1):
        raise ArchiveError("lsof failed while checking %s" % path)
    lines = [line for line in process.stdout.splitlines() if line.strip()]
    return len(lines) > 1


def _latest_mtime_size_and_unsafe_symlinks(root: Path) -> Tuple[float, int, List[str]]:
    latest = root.lstat().st_mtime
    total_bytes = 0
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
            if not path.is_symlink():
                total_bytes += stat.st_size
            if path.is_symlink():
                try:
                    path.resolve(strict=False).relative_to(resolved_root)
                except ValueError:
                    unsafe.append(path.relative_to(root).as_posix())
    return latest, total_bytes, sorted(unsafe)


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
        if any(run.get("status") not in TERMINAL_RUN_STATUSES for run in runs):
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
            latest_mtime, total_bytes, unsafe_symlinks = _latest_mtime_size_and_unsafe_symlinks(candidate)
            details["latest_mtime"] = datetime.fromtimestamp(latest_mtime, timezone.utc).isoformat()
            details["size_bytes"] = total_bytes
            if self.now().timestamp() - latest_mtime < self.recent_write_seconds:
                reasons.append("recent write exists under candidate")
            if unsafe_symlinks:
                reasons.append("out-of-bound symlink present (not followed): %s" % ", ".join(unsafe_symlinks))
        except OSError as error:
            reasons.append("filesystem scan failed closed: %s" % error)

        result = {
            "path": str(candidate),
            "issue_id": issue_id,
            "workspace_id": workspace_id,
            "eligible": not reasons,
            "reasons": reasons,
            "details": details,
        }
        if not reasons:
            try:
                manifest = tree_manifest(candidate)
                digest = hashlib.sha256(
                    json.dumps(manifest, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
                ).hexdigest()
                matching = matching_runs[0]
                approval_identity = {
                    "source_path": str(candidate.resolve()),
                    "workspace_id": workspace_id,
                    "issue_id": issue_id,
                    "run_id": str(matching.get("id")),
                    "completed_at": str(meta.get("completed_at")),
                    "manifest_sha256": digest,
                }
                result["run_id"] = approval_identity["run_id"]
                result["manifest_sha256"] = digest
                result["approval_token"] = hashlib.sha256(
                    json.dumps(approval_identity, sort_keys=True, separators=(",", ":")).encode("utf-8")
                ).hexdigest()
                result["details"]["size_bytes"] = int(manifest["total_bytes"])
            except Exception as error:
                reasons.append("approval manifest failed closed: %s" % error)
                result["eligible"] = False
        return result


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


def _inspect_stale_workdir(
    candidate: Path,
    *,
    checked_at: datetime,
    minimum_age_seconds: int,
) -> Dict[str, Any]:
    """Measure an old task directory without granting archive/delete eligibility."""

    try:
        directory_mtime = candidate.lstat().st_mtime
        age_seconds = checked_at.timestamp() - directory_mtime
        if age_seconds < minimum_age_seconds:
            return {"path": str(candidate), "stale": False}
        latest_mtime, size_bytes, unsafe_symlinks = _latest_mtime_size_and_unsafe_symlinks(candidate)
        return {
            "path": str(candidate),
            "stale": True,
            "size_bytes": size_bytes,
            "directory_mtime": datetime.fromtimestamp(directory_mtime, timezone.utc).isoformat(),
            "directory_age_seconds": age_seconds,
            "latest_tree_mtime": datetime.fromtimestamp(latest_mtime, timezone.utc).isoformat(),
            "latest_tree_age_seconds": checked_at.timestamp() - latest_mtime,
            "unsafe_symlink_count": len(unsafe_symlinks),
            "observation_only": True,
            "delete_authorized": False,
        }
    except OSError as error:
        return {"path": str(candidate), "stale": False, "error": str(error)}


def build_workspace_stale_report(
    config: Dict[str, Any],
    *,
    now: Callable[[], datetime] = utc_now,
) -> Dict[str, Any]:
    checked_at = now().astimezone(timezone.utc)
    minimum_age_seconds = int(config.get("workspace_stale_seconds", 86400))
    paths = discover_candidates(Path(str(item)) for item in config.get("workspace_roots", []))
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        inspected = list(
            executor.map(
                lambda path: _inspect_stale_workdir(
                    path,
                    checked_at=checked_at,
                    minimum_age_seconds=minimum_age_seconds,
                ),
                paths,
            )
        )
    errors = [value for value in inspected if value.get("error")]
    stale = sorted(
        (value for value in inspected if value.get("stale")),
        key=lambda value: str(value["path"]),
    )
    return {
        "schema": "multica.workspace-stale-dry-run.v1",
        "status": "red" if errors else "green",
        "recorded_at": checked_at.isoformat(),
        "gc_mode": "dry-run-observation-only",
        "basis": "task directory mtime",
        "minimum_age_seconds": minimum_age_seconds,
        "candidate_count": len(paths),
        "stale_workdir_candidate_count": len(stale),
        "stale_workdir_candidate_bytes": sum(int(value.get("size_bytes") or 0) for value in stale),
        "stale_workdir_candidates": stale,
        "errors": errors,
        "delete_authorized": False,
        "deletion_performed": False,
    }


def run_workspace_stale_dry_run(config: Dict[str, Any]) -> Dict[str, Any]:
    report = build_workspace_stale_report(config)
    report_path = Path(
        str(
            config.get("workspace_stale_report_path")
            or Path(str(config["report_path"])).with_name("workspace-stale-dry-run.json")
        )
    )
    atomic_write_json(report_path, report)
    report["report_path"] = str(report_path)
    return report


def consumed_approval_tokens(archive_root: Path) -> set[str]:
    tokens: set[str] = set()
    if not archive_root.is_dir():
        return tokens
    for marker_path in archive_root.glob("*/COMPLETE.json"):
        try:
            marker = json.loads(marker_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        token = marker.get("approval_token")
        if token:
            tokens.add(str(token))
    return tokens


def has_message_id(value: Any) -> bool:
    if isinstance(value, dict):
        return bool(value.get("message_id")) or any(has_message_id(child) for child in value.values())
    if isinstance(value, list):
        return any(has_message_id(child) for child in value)
    return False


def send_alert(config: Dict[str, Any], message: str) -> None:
    alert_path = Path(str(config["alert_log_path"]))
    try:
        append_jsonl(alert_path, {"recorded_at": utc_now().isoformat(), "message": message})
    except OSError:
        pass
    open_id = str(config.get("lark_open_id") or "")
    if not open_id:
        return
    try:
        process = subprocess.run(
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
            timeout=15,
        )
        response = json.loads(process.stdout) if process.stdout else {}
        delivered = process.returncode == 0 and has_message_id(response)
    except (OSError, subprocess.TimeoutExpired, json.JSONDecodeError):
        delivered = False
        process = None
    if not delivered:
        exit_code = process.returncode if process is not None else None
        try:
            append_jsonl(
                alert_path,
                {"recorded_at": utc_now().isoformat(), "message": "lark alert delivery failed", "exit_code": exit_code},
            )
        except OSError:
            pass


def process_ancestry(start_pid: Optional[int] = None, limit: int = 8) -> List[Dict[str, Any]]:
    pid = os.getpid() if start_pid is None else start_pid
    values: List[Dict[str, Any]] = []
    for _ in range(limit):
        process = subprocess.run(
            ["/bin/ps", "-p", str(pid), "-o", "ppid=", "-o", "comm="],
            capture_output=True,
            text=True,
            check=False,
        )
        fields = process.stdout.strip().split(None, 1)
        if process.returncode != 0 or len(fields) != 2:
            break
        parent = int(fields[0])
        command = fields[1]
        values.append({"pid": pid, "parent_pid": parent, "command": command})
        if parent <= 1 or parent == pid:
            break
        pid = parent
    return values


def verify_cron_bridge(config: Dict[str, Any]) -> Tuple[Dict[str, Any], List[Dict[str, Any]]]:
    if os.environ.get("MULTICA_STORAGE_CRON_BRIDGE") != "1":
        raise ArchiveError("formal cron bridge marker is missing")
    trigger_path = Path(str(config["cron_bridge_trigger_path"]))
    try:
        trigger = json.loads(trigger_path.read_text(encoding="utf-8"))
        bridge_pid = int(trigger["bridge_pid"])
        created_at = parse_timestamp(str(trigger["created_at"]))
    except (FileNotFoundError, OSError, ValueError, TypeError, KeyError, json.JSONDecodeError) as error:
        raise ArchiveError("formal cron bridge trigger is invalid") from error
    if trigger.get("schema") != "multica.storage-cron-trigger.v1" or not trigger.get("token"):
        raise ArchiveError("formal cron bridge trigger schema is invalid")
    try:
        receipt = json.loads(Path(str(config["cron_bridge_receipt_path"])).read_text(encoding="utf-8"))
    except (FileNotFoundError, OSError, json.JSONDecodeError):
        receipt = {}
    if receipt.get("token") == trigger["token"]:
        raise ArchiveError("formal cron bridge token was already consumed")
    age = (utc_now() - created_at).total_seconds()
    if age < 0 or age > 120:
        raise ArchiveError("formal cron bridge trigger is stale")
    command = subprocess.run(
        ["/bin/ps", "-p", str(bridge_pid), "-o", "command="],
        capture_output=True,
        text=True,
        check=False,
    )
    if command.returncode != 0 or "retention_cron_bridge.py" not in command.stdout:
        raise ArchiveError("formal cron bridge process is not alive")
    lineage = process_ancestry(bridge_pid)
    if not any(Path(str(item["command"])).name == "cron" for item in lineage):
        raise ArchiveError("formal cron bridge has no live cron ancestor")
    return trigger, lineage


def write_cron_bridge_receipt(
    config: Dict[str, Any],
    *,
    token: Optional[str],
    status: str,
    error: Optional[str] = None,
) -> None:
    if not token:
        return
    atomic_write_json(
        Path(str(config["cron_bridge_receipt_path"])),
        {"token": token, "status": status, "recorded_at": utc_now().isoformat(), "error": error},
    )


def atomic_write_failure_report(config: Dict[str, Any], message: str) -> None:
    path = Path(str(config["report_path"]))
    last_success_at: Optional[str] = None
    try:
        previous = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(previous, dict):
            if previous.get("status") == "green":
                last_success_at = str(previous.get("recorded_at") or "") or None
            else:
                last_success_at = previous.get("last_success_at")
    except (FileNotFoundError, OSError, json.JSONDecodeError):
        pass
    atomic_write_json(
        path,
        {
            "schema": "multica.storage-retention-run.v1",
            "status": "red",
            "failed_at": utc_now().isoformat(),
            "last_success_at": last_success_at,
            "error": message,
        },
    )


def run_worker(config: Dict[str, Any]) -> Dict[str, Any]:
    ancestry = process_ancestry()
    trigger: Optional[Dict[str, Any]] = None
    cron_lineage: List[Dict[str, Any]] = []
    if config.get("require_cron_lineage", True):
        trigger, cron_lineage = verify_cron_bridge(config)
        config["_verified_cron_token"] = str(trigger["token"])
        write_cron_bridge_receipt(config, token=str(trigger["token"]), status="running")
    if config.get("delete_source", False):
        raise ArchiveError("source deletion requires a producer lease and remains disabled")
    external_path = Path(str(config["external_path"]))
    archive_root = Path(str(config["archive_root"]))
    archive_root.mkdir(parents=True, exist_ok=True)
    volume_guard = ExternalVolumeGuard(
        external_path,
        archive_root,
        expected_uuid=str(config["external_volume_uuid"]),
        min_free_bytes=int(float(config.get("external_min_free_gib", 100)) * GIB),
    )
    volume_guard.check()
    canary_root = Path(str(config.get("canary_root") or external_path))
    canary_root.mkdir(parents=True, exist_ok=True)
    canary_guard = ExternalVolumeGuard(
        external_path,
        canary_root,
        expected_uuid=str(config["external_volume_uuid"]),
        min_free_bytes=int(float(config.get("external_min_free_gib", 100)) * GIB),
    )
    canary_guard.check()
    canary = Canary(
        canary_root,
        expected_uuid=str(config["external_volume_uuid"]),
        min_free_bytes=int(float(config.get("external_min_free_gib", 100)) * GIB),
        volume_path=external_path,
        postflight=canary_guard.check,
    ).run()
    evaluator = GCEvaluator(
        MulticaIssueClient(),
        retention_seconds=int(float(config.get("retention_days", 7)) * 86400),
        recent_write_seconds=int(config.get("recent_write_seconds", 86400)),
    )
    candidate_paths = discover_candidates(Path(str(item)) for item in config.get("workspace_roots", []))
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        candidates = list(executor.map(evaluator.evaluate, candidate_paths))
    stale_report = run_workspace_stale_dry_run(config)
    report: Dict[str, Any] = {
        "schema": "multica.storage-retention-run.v1",
        "status": "green",
        "recorded_at": utc_now().isoformat(),
        "pid": os.getpid(),
        "parent_pid": os.getppid(),
        "invocation_source": "verified-cron-launchd-bridge" if trigger else "manual",
        "process_ancestry": ancestry,
        "cron_bridge_ancestry": cron_lineage,
        "cron_trigger_token": trigger.get("token") if trigger else None,
        "canary": canary,
        "gc_mode": "dry-run",
        "gc_candidates": candidates,
        "eligible_count": sum(1 for candidate in candidates if candidate["eligible"]),
        "stale_workdir_observation": stale_report,
        "archive_enabled": bool(config.get("archive_enabled", False)),
        "delete_source": bool(config.get("delete_source", False)),
        "archives": [],
    }
    if config.get("archive_enabled", False):
        manager = ArchiveManager(archive_root, preflight=volume_guard.check)
        approved = {str(value) for value in config.get("approved_candidates", [])}
        consumed = consumed_approval_tokens(archive_root)
        for candidate in candidates:
            candidate_path = Path(str(candidate["path"]))
            token = str(candidate.get("approval_token") or "")
            if not candidate["eligible"] or token not in approved or token in consumed:
                continue
            fresh = evaluator.evaluate(candidate_path)
            if not fresh["eligible"] or fresh.get("approval_token") != candidate.get("approval_token"):
                continue
            report["archives"].append(
                manager.archive(
                    candidate_path,
                    candidate_path.name,
                    delete_source=False,
                    approval_token=token,
                )
            )
            consumed.add(token)
    atomic_write_json(Path(str(config["report_path"])), report)
    if trigger:
        write_cron_bridge_receipt(config, token=str(trigger["token"]), status="green")
    return report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument(
        "--workspace-stale-dry-run",
        action="store_true",
        help="only list old task directories and bytes; never archive or delete",
    )
    args = parser.parse_args()
    stale_only_report: Optional[Dict[str, Any]] = None
    try:
        config = json.loads(Path(args.config).read_text(encoding="utf-8"))
        with SingleInstanceLock(Path(str(config["lock_path"]))):
            if args.workspace_stale_dry_run:
                stale_only_report = run_workspace_stale_dry_run(config)
            else:
                report = run_worker(config)
    except BlockingIOError:
        message = "storage retention worker skipped: another owner holds the single-instance lock"
        for action in (
            lambda: atomic_write_failure_report(config, message),
            lambda: write_cron_bridge_receipt(
                config,
                token=str(config.get("_verified_cron_token") or "") or None,
                status="red",
                error=message,
            ),
            lambda: send_alert(config, message),
        ):
            try:
                action()
            except Exception:
                pass
        print(json.dumps({"status": "locked", "error": message}), file=sys.stderr)
        return 75
    except Exception as error:
        message = "storage retention worker failed closed: %s" % error
        for action in (
            lambda: atomic_write_failure_report(config, message),
            lambda: write_cron_bridge_receipt(
                config,
                token=str(config.get("_verified_cron_token") or "") or None,
                status="red",
                error=message,
            ),
            lambda: send_alert(config, message),
        ):
            try:
                action()
            except Exception:
                pass
        print(json.dumps({"status": "red", "error": message}, ensure_ascii=False), file=sys.stderr)
        return 1
    if stale_only_report is not None:
        print(
            json.dumps(
                {
                    "status": stale_only_report["status"],
                    "recorded_at": stale_only_report["recorded_at"],
                    "stale_workdir_candidate_count": stale_only_report["stale_workdir_candidate_count"],
                    "stale_workdir_candidate_bytes": stale_only_report["stale_workdir_candidate_bytes"],
                    "report_path": stale_only_report["report_path"],
                    "delete_authorized": False,
                    "deletion_performed": False,
                },
                ensure_ascii=False,
                sort_keys=True,
            )
        )
        return 1 if stale_only_report["status"] == "red" else 0
    print(
        json.dumps(
            {
                "status": report["status"],
                "recorded_at": report["recorded_at"],
                "eligible_count": report["eligible_count"],
                "archive_count": len(report["archives"]),
                "report_path": str(config["report_path"]),
            },
            ensure_ascii=False,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
