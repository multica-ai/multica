#!/usr/bin/env python3
"""LifeOS 工作台本机运行入口的最小回归测试。"""

from __future__ import annotations

import json
import hashlib
import io
import plistlib
import sqlite3
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import lifeos_workbench


def write_backup_fixture(
    root: Path,
    *,
    missing_asset: str = "",
    release_schema_version: int = 1,
    include_attachments: bool = True,
    backup_schema_version: int = lifeos_workbench.BACKUP_SCHEMA_VERSION,
    directory_asset: str = "",
) -> None:
    with sqlite3.connect(root / "lifeos-context.sqlite3") as connection:
        for table in lifeos_workbench.CONTEXT_DATABASE_TABLES:
            connection.execute("CREATE TABLE %s(value TEXT)" % table)
    with sqlite3.connect(root / "lifeos-interaction.sqlite3") as connection:
        for table in lifeos_workbench.INTERACTION_DATABASE_TABLES:
            connection.execute("CREATE TABLE %s(value TEXT)" % table)
    (root / "workbench-postgres.dump").write_bytes(b"postgres-dump")
    (root / "release-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": release_schema_version,
                "release_id": "a" * 64,
                "contains_secrets": False,
            }
        ),
        encoding="utf-8",
    )
    state = b"# State\n<!-- LIFEOS_WORKPLACE_STATE_JSON\n{}\nLIFEOS_WORKPLACE_STATE_JSON -->\n"
    commit = {
        "schema_version": 2,
        "state_version": 7,
        "commit_status": "committed",
        "snapshot_hash": "fixture-snapshot",
        "artifact_hashes": {
            lifeos_workbench.OPERATING_STATE_PATH: hashlib.sha256(state).hexdigest(),
        },
    }
    assets = {
        lifeos_workbench.OPERATING_STATE_PATH: state,
        lifeos_workbench.OPERATING_COMMIT_PATH: json.dumps(commit).encode(),
        "departments/workplace/department.md": b"# Workplace\n",
        "meta/00-charter.md": b"# Charter\n",
        "workflows/interaction-loop.md": b"# Interaction\n",
        "logs/transactions/workplace/fixture.json": b"{}\n",
        "logs/runs/fixture/run.json": b"{}\n",
        "logs/recovery/workplace/fixture/manifest.json": b"{}\n",
        "projects/project-index.md": b"# Projects\n",
        "projects/project-map.json": b"{}\n",
        "projects/automation-registry.md": b"# Automations\n",
        "memory/preferences.md": b"# Preferences\n",
        "memory/writing-rules.json": b"{}\n",
    }
    with tarfile.open(root / "lifeos-operating-assets.tar.gz", "w:gz") as archive:
        for relative in lifeos_workbench.LIFEOS_BACKUP_ASSETS["required"]:
            if relative == missing_asset:
                continue
            if "." not in Path(relative).name:
                info = tarfile.TarInfo(relative)
                info.type = tarfile.DIRTYPE
                archive.addfile(info)
        for relative, content in assets.items():
            if relative == missing_asset or relative.startswith(missing_asset.rstrip("/") + "/"):
                continue
            info = tarfile.TarInfo(relative)
            if relative == directory_asset:
                info.type = tarfile.DIRTYPE
                archive.addfile(info)
                continue
            info.size = len(content)
            archive.addfile(info, io.BytesIO(content))
    if include_attachments:
        with tarfile.open(root / "workbench-attachments.tar", "w") as archive:
            info = tarfile.TarInfo("uploads")
            info.type = tarfile.DIRTYPE
            archive.addfile(info)
    archive_tree = hashlib.sha256()
    with tarfile.open(root / "lifeos-operating-assets.tar.gz", "r:gz") as archive:
        for member in sorted(archive.getmembers(), key=lambda item: item.name):
            if not member.isfile():
                continue
            handle = archive.extractfile(member)
            assert handle is not None
            archive_tree.update(member.name.encode())
            archive_tree.update(b"\0")
            archive_tree.update(hashlib.sha256(handle.read()).hexdigest().encode())
            archive_tree.update(b"\n")
    files = [path for path in root.iterdir() if path.name != "manifest.json"]
    (root / "manifest.json").write_text(
        json.dumps(
            {
                "backup_schema_version": backup_schema_version,
                "operating_commit": {
                    "commit_sha256": hashlib.sha256(
                        json.dumps(commit).encode()
                    ).hexdigest(),
                    "state_version": 7,
                    "snapshot_hash": "fixture-snapshot",
                    "asset_tree_sha256": archive_tree.hexdigest(),
                },
                "files": [
                    {
                        "name": path.name,
                        "sha256": lifeos_workbench._sha256_file(path),
                    }
                    for path in files
                ],
            }
        ),
        encoding="utf-8",
    )


class ContextDatabaseTests(unittest.TestCase):
    def test_default_controller_root_is_the_canonical_lifeos_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            home = Path(tempdir)
            canonical = home / "Documents/Life OS AI"
            (canonical / "scripts").mkdir(parents=True)
            (canonical / "scripts/lifeos_mcp_server.py").write_text(
                "# canonical controller\n", encoding="utf-8"
            )
            with mock.patch.object(lifeos_workbench.Path, "home", return_value=home), mock.patch.dict(
                lifeos_workbench.os.environ,
                {"LIFEOS_CONTROLLER_ROOT": ""},
            ):
                resolved = lifeos_workbench.resolve_controller_root(None)

        self.assertEqual(resolved, canonical.resolve())

    def test_codex_index_sync_runs_every_two_hours(self) -> None:
        self.assertEqual(lifeos_workbench.INDEX_SYNC_INTERVAL_SECONDS, 7200)

    def test_lifeos_asset_archive_excludes_restricted_meeting_source(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir) / "lifeos"
            meeting = root / "sources/meetings/2026/example"
            (meeting / "source").mkdir(parents=True)
            (meeting / "minutes.md").write_text("结论\n", encoding="utf-8")
            (meeting / "source/transcript.txt").write_text(
                "受限原文\n", encoding="utf-8"
            )
            target = Path(tempdir) / "assets.tar.gz"

            lifeos_workbench.create_lifeos_asset_archive(root, target)

            with tarfile.open(target, "r:gz") as archive:
                names = archive.getnames()
            self.assertIn("sources/meetings/2026/example/minutes.md", names)
            self.assertNotIn(
                "sources/meetings/2026/example/source/transcript.txt", names
            )

    def test_verify_backup_checks_hashes_and_sqlite_integrity(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root)
            database = root / "lifeos-context.sqlite3"

            result = lifeos_workbench.verify_backup(root)
            database.write_bytes(b"corrupt")

            self.assertTrue(result["verified"])
            self.assertEqual(result["verification_level"], "recovery_package")
            self.assertTrue(result["recovery_package_verified"])
            self.assertFalse(result["business_recovery_verified"])
            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "哈希不一致"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_rejects_missing_required_operating_asset(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, missing_asset="projects/project-map.json")

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "经营资产必须是文件"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_rejects_missing_attachment_archive(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, include_attachments=False)

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "备份缺少必需资产"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_rejects_directory_masquerading_as_required_file(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, directory_asset="projects/project-map.json")

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "必须是文件"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_rejects_mixed_operating_commit(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root)
            manifest = json.loads((root / "manifest.json").read_text())
            manifest["operating_commit"]["commit_sha256"] = "f" * 64
            (root / "manifest.json").write_text(json.dumps(manifest))

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "不同提交版本"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_marks_legacy_schema_as_limited(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, backup_schema_version=2)

            result = lifeos_workbench.verify_backup(root)

            self.assertTrue(result["verified"])
            self.assertFalse(result["recovery_package_verified"])
            self.assertFalse(result["business_recovery_verified"])
            self.assertEqual(result["verification_level"], "limited_integrity")

    def test_verify_backup_rejects_unknown_backup_schema(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, backup_schema_version=99)

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "不支持的备份格式版本"
            ):
                lifeos_workbench.verify_backup(root)

    def test_verify_backup_rejects_sqlite_without_business_schema(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root)
            database = root / "lifeos-context.sqlite3"
            with sqlite3.connect(database) as connection:
                connection.execute("DROP TABLE ceo_review_queue")
            manifest = json.loads((root / "manifest.json").read_text())
            for item in manifest["files"]:
                if item["name"] == database.name:
                    item["sha256"] = lifeos_workbench._sha256_file(database)
            (root / "manifest.json").write_text(json.dumps(manifest))

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "缺少业务表"
            ):
                lifeos_workbench.verify_backup(root)

    def test_attachment_reference_validation_rejects_missing_object(self) -> None:
        with self.assertRaisesRegex(
            lifeos_workbench.WorkbenchError, "缺少数据库引用对象"
        ):
            lifeos_workbench._validate_attachment_references(
                ["/uploads/workspaces/a/missing.png"],
                [".", "workspaces/a/kept.png"],
            )

    def test_verify_backup_rejects_unknown_release_manifest_version(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root, release_schema_version=2)

            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError, "发布清单无效"
            ):
                lifeos_workbench.verify_backup(root)

    def test_restore_drill_drops_the_database_it_created(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            write_backup_fixture(root)
            lifeos_root = root / "lifeos"
            validator = lifeos_root / "scripts/commit_workplace_state.py"
            validator.parent.mkdir(parents=True)
            validator.write_text("# test-only validator\n", encoding="utf-8")
            commands = []

            def run(command, **_kwargs):
                commands.append(command)
                statement = next(
                    (argument for argument in command if isinstance(argument, str) and argument.startswith("SELECT")),
                    "",
                )
                if "SELECT count(*)" in statement:
                    stdout = "8\n"
                elif "SELECT tablename" in statement:
                    stdout = "\n".join(sorted(lifeos_workbench.POSTGRES_RECOVERY_TABLES)) + "\n"
                elif "SELECT url FROM attachment" in statement:
                    stdout = ""
                else:
                    stdout = ""
                return mock.Mock(returncode=0, stdout=stdout, stderr="")

            with mock.patch.object(
                lifeos_workbench, "runtime_environment", return_value={}
            ), mock.patch.object(
                lifeos_workbench, "docker_ready", return_value=True
            ), mock.patch.object(
                lifeos_workbench, "compose_command", return_value=["docker", "compose"]
            ), mock.patch.object(
                lifeos_workbench, "_run", side_effect=run
            ), mock.patch.object(
                lifeos_workbench.subprocess,
                "run",
                return_value=mock.Mock(returncode=0, stdout=b"", stderr=b""),
            ):
                result = lifeos_workbench.restore_drill(
                    root,
                    lifeos_root=lifeos_root,
                    controller_root=Path(tempdir) / "controller",
                )

            created = next(command for command in commands if "createdb" in command)
            dropped = next(command for command in commands if "dropdb" in command)
            validation = next(
                command
                for command in commands
                if str(validator) in command and "--verify-only" in command
            )
            self.assertEqual(created[-1], dropped[-1])
            self.assertRegex(created[-1], r"^lifeos_restore_[0-9]{14}_[0-9a-f]{6}$")
            restored_operating_root = Path(
                validation[validation.index("--root") + 1]
            )
            self.assertEqual(restored_operating_root.name, "lifeos-operating-assets")
            self.assertTrue(
                restored_operating_root.parent.name.startswith(
                    "lifeos-restore-drill-"
                )
            )
            self.assertTrue(result["restored_in_isolation"])
            self.assertTrue(result["business_recovery_verified"])
            self.assertTrue(result["operating_state_validated"])

    def test_ensure_env_creates_private_automation_credential(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            env_file = root / ".env.lifeos"
            token_file = root / "secrets/automation-token"
            with mock.patch.object(
                lifeos_workbench, "AUTOMATION_TOKEN_FILE", token_file
            ):
                values = lifeos_workbench.ensure_env(env_file)
                repeated = lifeos_workbench.ensure_env(env_file)

            self.assertEqual(
                token_file.read_text(encoding="utf-8").strip(),
                values["LIFEOS_AUTOMATION_TOKEN"],
            )
            self.assertEqual(
                values["LIFEOS_AUTOMATION_TOKEN"],
                repeated["LIFEOS_AUTOMATION_TOKEN"],
            )
            self.assertEqual(token_file.stat().st_mode & 0o777, 0o600)

    def test_public_origin_is_persisted_and_used_for_the_board_url(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            env_file = root / ".env.lifeos"
            token_file = root / "secrets/automation-token"
            with mock.patch.object(
                lifeos_workbench, "AUTOMATION_TOKEN_FILE", token_file
            ):
                values = lifeos_workbench.ensure_env(
                    env_file, public_origin="https://lifeos.example.com/"
                )
                repeated = lifeos_workbench.ensure_env(env_file)

            for key in (
                "FRONTEND_ORIGIN",
                "MULTICA_APP_URL",
                "CORS_ALLOWED_ORIGINS",
                "ALLOWED_ORIGINS",
            ):
                self.assertEqual(values[key], "https://lifeos.example.com")
                self.assertEqual(repeated[key], "https://lifeos.example.com")
            self.assertEqual(
                lifeos_workbench.configured_app_url(env_file),
                "https://lifeos.example.com/lifeos/issues",
            )

    def test_public_origin_rejects_paths_and_non_https_urls(self) -> None:
        for origin in ("http://lifeos.example.com", "https://lifeos.example.com/path"):
            with self.assertRaises(lifeos_workbench.WorkbenchError):
                lifeos_workbench._normalize_public_origin(origin)

    def test_strong_login_rollout_rotates_old_cookie_secret_once(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            env_file = root / ".env.lifeos"
            env_file.write_text(
                "JWT_SECRET=old-passwordless-secret\n"
                "LIFEOS_AUTOMATION_TOKEN=existing-machine-token\n",
                encoding="utf-8",
            )
            token_file = root / "secrets/automation-token"
            with mock.patch.object(
                lifeos_workbench, "AUTOMATION_TOKEN_FILE", token_file
            ):
                upgraded = lifeos_workbench.ensure_env(env_file)
                repeated = lifeos_workbench.ensure_env(env_file)

            self.assertEqual(upgraded["LIFEOS_AUTH_VERSION"], "2")
            self.assertNotEqual(upgraded["JWT_SECRET"], "old-passwordless-secret")
            self.assertEqual(repeated["JWT_SECRET"], upgraded["JWT_SECRET"])

    def test_migration_is_private_and_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as tempdir:
            root = Path(tempdir)
            lifeos_root = root / "Life OS AI"
            legacy = lifeos_root / "inbox/lifeos-workbench.sqlite3"
            legacy.parent.mkdir(parents=True)
            with sqlite3.connect(legacy) as connection:
                connection.execute("CREATE TABLE marker(value TEXT NOT NULL)")
                connection.execute("INSERT INTO marker(value) VALUES ('ready')")

            state_root = root / "Application Support/LifeOS"
            target = state_root / "data/lifeos-workbench.sqlite3"
            with mock.patch.object(lifeos_workbench, "STATE_ROOT", state_root), mock.patch.object(
                lifeos_workbench, "CONTEXT_DB", target
            ):
                first = lifeos_workbench.ensure_context_database(lifeos_root)
                second = lifeos_workbench.ensure_context_database(lifeos_root)

            self.assertEqual(first, target)
            self.assertEqual(second, target)
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            with sqlite3.connect("file:%s?mode=ro" % target, uri=True) as connection:
                value = connection.execute("SELECT value FROM marker").fetchone()[0]
            self.assertEqual(value, "ready")

    def test_board_only_start_is_explicit(self) -> None:
        args = lifeos_workbench.build_parser().parse_args(
            ["start", "--no-build", "--no-open", "--board-only"]
        )
        self.assertTrue(args.board_only)
        self.assertTrue(args.no_build)
        self.assertTrue(args.no_open)

    def test_ensure_is_a_first_class_recovery_command(self) -> None:
        args = lifeos_workbench.build_parser().parse_args(["ensure"])
        self.assertEqual(args.command, "ensure")

    def test_background_sync_is_a_first_class_silent_command(self) -> None:
        args = lifeos_workbench.build_parser().parse_args(
            [
                "background-sync",
                "--summary-limit",
                "5",
                "--triage-limit",
                "20",
                "--max-summaries",
                "25",
            ]
        )
        self.assertEqual(args.command, "background-sync")
        self.assertEqual(args.summary_limit, 5)
        self.assertEqual(args.triage_limit, 20)
        self.assertEqual(args.max_summaries, 25)

    def test_background_sync_drains_sanitized_queues_without_codex_task(self) -> None:
        lifeos_root = Path("/tmp/lifeos-root")
        controller_root = Path("/tmp/lifeos-controller")
        calls = []

        def controller_result(_lifeos_root, _controller_root, *arguments):
            calls.append(arguments)
            if arguments[0] == "process-summaries":
                return {
                    "processed": ["thread-1"],
                    "coverage": {
                        "summaries_pending": 0,
                        "ceo_reviews_pending": 1,
                    },
                }
            if arguments[0] == "triage-ceo":
                return {
                    "processed": [{"thread_id": "thread-1"}],
                    "coverage": {
                        "summaries_pending": 0,
                        "ceo_reviews_pending": 0,
                    },
                }
            raise AssertionError(arguments)

        provenance = {
            "status": "committed",
            "workbench_git_head": "a" * 40,
            "controller_git_head": "b" * 40,
        }
        with mock.patch.object(
            lifeos_workbench,
            "implementation_provenance",
            return_value=provenance,
        ) as provenance_check, mock.patch.object(
            lifeos_workbench,
            "persist_background_sync_receipt",
            return_value=Path("/tmp/background-sync-receipt.json"),
        ) as persist_receipt, mock.patch.object(
            lifeos_workbench, "ensure_running"
        ) as ensure, mock.patch.object(
            lifeos_workbench,
            "_controller_json",
            side_effect=controller_result,
        ), mock.patch("builtins.print"):
            result = lifeos_workbench.background_sync(
                lifeos_root,
                controller_root,
            )

        ensure.assert_called_once_with(lifeos_root, controller_root)
        self.assertEqual(provenance_check.call_count, 2)
        persist_receipt.assert_called_once()
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["summaries_processed"], 1)
        self.assertEqual(result["ceo_reviews_processed"], 1)
        self.assertFalse(result["raw_content_stored"])
        self.assertFalse(result["codex_task_created"])
        self.assertEqual(
            [call[0] for call in calls],
            ["process-summaries", "triage-ceo"],
        )
        self.assertEqual(result["action_projection"], "deferred_to_secretary_schedule")
        self.assertEqual(result["implementation_provenance"], provenance)
        self.assertEqual(
            result["receipt_ref"],
            "/tmp/background-sync-receipt.json",
        )

    def test_background_sync_receipt_is_private_and_commit_bound(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            state_root = Path(temporary)
            result = {
                "status": "completed",
                "implementation_provenance": {
                    "status": "committed",
                    "workbench_git_head": "a" * 40,
                    "controller_git_head": "b" * 40,
                },
                "raw_content_stored": False,
            }
            with mock.patch.object(
                lifeos_workbench,
                "STATE_ROOT",
                state_root,
            ):
                path = lifeos_workbench.persist_background_sync_receipt(result)

            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            payload = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(
                payload["result"]["implementation_provenance"],
                result["implementation_provenance"],
            )
            self.assertFalse(payload["result"]["raw_content_stored"])

    def test_background_sync_reports_exhausted_summaries_as_degraded(self) -> None:
        lifeos_root = Path("/tmp/lifeos-root")
        controller_root = Path("/tmp/lifeos-controller")

        def controller_result(_lifeos_root, _controller_root, *arguments):
            if arguments[0] == "process-summaries":
                return {
                    "processed": [],
                    "coverage": {
                        "summaries_pending": 0,
                        "ceo_reviews_pending": 0,
                        "summary_dead_letters": 2,
                    },
                }
            if arguments[0] == "triage-ceo":
                return {
                    "processed": [],
                    "coverage": {
                        "summaries_pending": 0,
                        "ceo_reviews_pending": 0,
                        "summary_dead_letters": 2,
                    },
                }
            raise AssertionError(arguments)

        provenance = {
            "status": "committed",
            "workbench_git_head": "a" * 40,
            "controller_git_head": "b" * 40,
        }
        with mock.patch.object(
            lifeos_workbench,
            "implementation_provenance",
            return_value=provenance,
        ), mock.patch.object(
            lifeos_workbench,
            "persist_background_sync_receipt",
            return_value=Path("/tmp/background-sync-receipt.json"),
        ), mock.patch.object(
            lifeos_workbench,
            "ensure_running",
        ), mock.patch.object(
            lifeos_workbench,
            "_controller_json",
            side_effect=controller_result,
        ), mock.patch("builtins.print"):
            result = lifeos_workbench.background_sync(
                lifeos_root,
                controller_root,
            )

        self.assertEqual(result["status"], "degraded")

    def test_background_sync_rejects_uncommitted_runtime_implementation(self) -> None:
        with mock.patch.object(
            lifeos_workbench,
            "_run",
            side_effect=[
                mock.Mock(stdout="a" * 40 + "\n"),
                mock.Mock(stdout=" M scripts/lifeos_workbench.py\n"),
            ],
        ):
            with self.assertRaisesRegex(
                lifeos_workbench.WorkbenchError,
                "未提交改动",
            ):
                lifeos_workbench._committed_implementation_revision(
                    Path("/tmp/repository"),
                    "scripts/lifeos_workbench.py",
                )

    def test_commit_binding_uses_deployed_hash_when_linked_git_is_protected(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)
            implementation = repository / "scripts/lifeos_controller.py"
            implementation.parent.mkdir()
            implementation.write_text("# committed controller\n", encoding="utf-8")
            deployed_sha256 = lifeos_workbench._sha256_file(implementation)
            with mock.patch.object(
                lifeos_workbench,
                "_run",
                side_effect=OSError("protected Git metadata"),
            ):
                revision = (
                    lifeos_workbench._committed_implementation_revision(
                        repository,
                        "scripts/lifeos_controller.py",
                        deployed_head="b" * 40,
                        deployed_sha256=deployed_sha256,
                    )
                )

        self.assertEqual(revision, "b" * 40)

    def test_commit_binding_rejects_script_drift_without_git_access(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)
            implementation = repository / "scripts/lifeos_controller.py"
            implementation.parent.mkdir()
            implementation.write_text("# changed controller\n", encoding="utf-8")
            with mock.patch.object(
                lifeos_workbench,
                "_run",
                side_effect=OSError("protected Git metadata"),
            ):
                with self.assertRaisesRegex(
                    lifeos_workbench.WorkbenchError,
                    "已部署提交绑定不一致",
                ):
                    lifeos_workbench._committed_implementation_revision(
                        repository,
                        "scripts/lifeos_controller.py",
                        deployed_head="b" * 40,
                        deployed_sha256="c" * 64,
                    )

    def test_autostart_deployment_environment_binds_both_scripts(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            controller_root = root / "controller"
            with mock.patch.object(
                lifeos_workbench,
                "REPO",
                root / "workbench",
            ), mock.patch.object(
                lifeos_workbench,
                "_committed_implementation_revision",
                side_effect=["a" * 40, "b" * 40, "b" * 40],
            ), mock.patch.object(
                lifeos_workbench,
                "_sha256_file",
                side_effect=["c" * 64, "d" * 64, "e" * 64],
            ):
                environment = (
                    lifeos_workbench.implementation_deployment_environment(
                        controller_root
                    )
                )

        self.assertEqual(
            environment,
            {
                lifeos_workbench.WORKBENCH_DEPLOYED_HEAD_ENV: "a" * 40,
                lifeos_workbench.WORKBENCH_DEPLOYED_SHA_ENV: "c" * 64,
                lifeos_workbench.CONTROLLER_DEPLOYED_HEAD_ENV: "b" * 40,
                lifeos_workbench.CONTROLLER_DEPLOYED_SHA_ENV: "d" * 64,
                "LIFEOS_SECRETARY_DEPLOYED_HEAD": "b" * 40,
                "LIFEOS_SECRETARY_DEPLOYED_SHA256": "e" * 64,
            },
        )

    def test_autostart_deployment_detects_stale_controller_binding(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            launch_dir = Path(temporary)
            plist_path = launch_dir / (lifeos_workbench.INDEX_LABEL + ".plist")
            installed = {
                lifeos_workbench.WORKBENCH_DEPLOYED_HEAD_ENV: "a" * 40,
                lifeos_workbench.WORKBENCH_DEPLOYED_SHA_ENV: "b" * 64,
                lifeos_workbench.CONTROLLER_DEPLOYED_HEAD_ENV: "c" * 40,
                lifeos_workbench.CONTROLLER_DEPLOYED_SHA_ENV: "d" * 64,
                "LIFEOS_SECRETARY_DEPLOYED_HEAD": "b" * 40,
                "LIFEOS_SECRETARY_DEPLOYED_SHA256": "e" * 64,
            }
            with plist_path.open("wb") as handle:
                plistlib.dump({"EnvironmentVariables": installed}, handle)
            expected = {
                **installed,
                lifeos_workbench.CONTROLLER_DEPLOYED_HEAD_ENV: "e" * 40,
            }
            with mock.patch.object(
                lifeos_workbench,
                "implementation_deployment_environment",
                return_value=expected,
            ):
                current = lifeos_workbench.autostart_deployment_is_current(
                    Path("/tmp/controller"),
                    launch_dir=launch_dir,
                )

        self.assertFalse(current)

    def test_ensure_autostart_reinstalls_only_when_binding_is_stale(self) -> None:
        lifeos_root = Path("/tmp/lifeos-root")
        controller_root = Path("/tmp/lifeos-controller")
        with mock.patch.object(
            lifeos_workbench,
            "autostart_deployment_is_current",
            side_effect=[True, False],
        ), mock.patch.object(
            lifeos_workbench,
            "install_autostart",
        ) as install:
            self.assertFalse(
                lifeos_workbench.ensure_autostart_deployment(
                    lifeos_root, controller_root
                )
            )
            self.assertTrue(
                lifeos_workbench.ensure_autostart_deployment(
                    lifeos_root, controller_root
                )
            )

        install.assert_called_once_with(lifeos_root, controller_root)

    def test_admin_commands_require_explicit_private_inputs(self) -> None:
        reset_args = lifeos_workbench.build_parser().parse_args(
            [
                "reset-login",
                "--username",
                "chairman",
                "--password-file",
                "/tmp/password",
            ]
        )
        self.assertEqual(reset_args.username, "chairman")
        self.assertEqual(reset_args.password_file, Path("/tmp/password"))

        tunnel_args = lifeos_workbench.build_parser().parse_args(
            [
                "install-tunnel",
                "--token-file",
                "/tmp/tunnel-token",
                "--public-origin",
                "https://lifeos.example.com",
            ]
        )
        self.assertEqual(tunnel_args.command, "install-tunnel")
        self.assertEqual(tunnel_args.public_origin, "https://lifeos.example.com")

    def test_ensure_never_rewrites_executor_configuration(self) -> None:
        lifeos_root = Path("/tmp/lifeos-root")
        controller_root = Path("/tmp/lifeos-controller")
        with mock.patch.object(lifeos_workbench, "start") as start:
            lifeos_workbench.ensure_running(lifeos_root, controller_root)

        start.assert_called_once_with(
            lifeos_root,
            controller_root,
            build=False,
            open_browser=False,
            start_executor=False,
        )


if __name__ == "__main__":
    unittest.main()
