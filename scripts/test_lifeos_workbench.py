#!/usr/bin/env python3
"""LifeOS 工作台本机运行入口的最小回归测试。"""

from __future__ import annotations

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
