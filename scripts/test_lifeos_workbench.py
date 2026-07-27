#!/usr/bin/env python3
"""LifeOS 工作台本机运行入口的最小回归测试。"""

from __future__ import annotations

import json
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import lifeos_workbench


class ContextDatabaseTests(unittest.TestCase):
    def test_codex_index_sync_runs_every_two_hours(self) -> None:
        self.assertEqual(lifeos_workbench.INDEX_SYNC_INTERVAL_SECONDS, 7200)

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
        self.assertEqual(result["action_projection"], "deferred_to_visible_sync")
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
                side_effect=["a" * 40, "b" * 40],
            ), mock.patch.object(
                lifeos_workbench,
                "_sha256_file",
                side_effect=["c" * 64, "d" * 64],
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
            },
        )

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

    def test_ensure_restores_executor_without_rebuild_or_browser(self) -> None:
        lifeos_root = Path("/tmp/lifeos-root")
        controller_root = Path("/tmp/lifeos-controller")
        with mock.patch.object(lifeos_workbench, "start") as start:
            lifeos_workbench.ensure_running(lifeos_root, controller_root)

        start.assert_called_once_with(
            lifeos_root,
            controller_root,
            build=False,
            open_browser=False,
            start_executor=True,
        )


if __name__ == "__main__":
    unittest.main()
