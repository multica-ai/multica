#!/usr/bin/env python3
"""Tests for configure-hermes-fallback.py.

Run: /opt/hermes/bin/python docker/configure-hermes-fallback.test.py
(any python3 with PyYAML works — the script itself only needs yaml + stdlib).
"""

from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import yaml

_SPEC = importlib.util.spec_from_file_location(
    "configure_hermes_fallback",
    Path(__file__).with_name("configure-hermes-fallback.py"),
)
assert _SPEC and _SPEC.loader
cfg = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(cfg)

GATEWAY_PAYLOAD = {
    "object": "list",
    "data": [
        {"id": "claude-sonnet-5", "firtal_capability": "chat"},
        {"id": "moonshotai/kimi-k3", "firtal_capability": "chat"},
        {"id": "qwen/qwen3-embedding-8b", "firtal_capability": "embedding"},
        {"id": "legacy-no-capability"},
        {"id": "   "},
    ],
}


class ConfigureHermesFallbackTest(unittest.TestCase):
    def run_main(self, env: dict, payload=GATEWAY_PAYLOAD, existing: str | None = None):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            if existing is not None:
                (home / "config.yaml").write_text(existing, encoding="utf-8")
            full_env = {"HERMES_HOME": str(home), **env}
            with mock.patch.dict("os.environ", full_env, clear=True):
                with mock.patch.object(
                    cfg, "fetch_models", return_value=(payload or {}).get("data", [])
                ):
                    cfg.main()
            path = home / "config.yaml"
            return yaml.safe_load(path.read_text(encoding="utf-8")) if path.exists() else None

    def base_env(self):
        return {
            "FIRTAL_REGISTRY_URL": "https://registry.example/",
            "FIRTAL_REGISTRY_KEY": "rk_test",
        }

    def test_registers_selectable_provider_with_discovered_chat_models(self):
        config = self.run_main(self.base_env())
        provider = config["providers"]["firtal-gateway"]
        self.assertEqual(provider["base_url"], "https://registry.example/api/ai/proxy/v1")
        self.assertEqual(provider["api_mode"], "chat_completions")
        # Chat models kept; a model with no declared capability is kept too.
        self.assertIn("moonshotai/kimi-k3", provider["models"])
        self.assertIn("legacy-no-capability", provider["models"])
        # Embeddings cannot serve a turn.
        self.assertNotIn("qwen/qwen3-embedding-8b", provider["models"])
        # Blank ids are dropped.
        self.assertNotIn("   ", provider["models"])

    def test_key_is_referenced_by_env_name_never_written(self):
        config = self.run_main(self.base_env())
        serialized = yaml.safe_dump(config)
        self.assertNotIn("rk_test", serialized)
        self.assertEqual(
            config["providers"]["firtal-gateway"]["key_env"], "FIRTAL_REGISTRY_KEY"
        )
        self.assertEqual(config["fallback_providers"][0]["key_env"], "FIRTAL_REGISTRY_KEY")

    def test_fallback_model_is_listed_first_and_defaults_to_sonnet_5(self):
        config = self.run_main(self.base_env())
        self.assertEqual(config["fallback_providers"][0]["model"], "claude-sonnet-5")
        self.assertEqual(config["providers"]["firtal-gateway"]["models"][0], "claude-sonnet-5")

    def test_fallback_model_override_is_honoured_and_not_duplicated(self):
        env = {**self.base_env(), "FIRTAL_REGISTRY_MODEL": "moonshotai/kimi-k3"}
        config = self.run_main(env)
        models = config["providers"]["firtal-gateway"]["models"]
        self.assertEqual(config["fallback_providers"][0]["model"], "moonshotai/kimi-k3")
        self.assertEqual(models[0], "moonshotai/kimi-k3")
        self.assertEqual(models.count("moonshotai/kimi-k3"), 1)

    def test_gateway_down_still_registers_provider_with_fallback_model(self):
        config = self.run_main(self.base_env(), payload={"data": []})
        self.assertEqual(
            config["providers"]["firtal-gateway"]["models"], ["claude-sonnet-5"]
        )

    def test_existing_user_config_is_preserved(self):
        existing = yaml.safe_dump(
            {
                "model": {"default": "deepseek-v4-flash", "provider": "opencode-go"},
                "providers": {"my-own": {"base_url": "https://example.invalid"}},
            }
        )
        config = self.run_main(self.base_env(), existing=existing)
        self.assertEqual(config["model"]["provider"], "opencode-go")
        self.assertIn("my-own", config["providers"])
        self.assertIn("firtal-gateway", config["providers"])

    def test_rerun_is_idempotent(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            env = {"HERMES_HOME": str(home), **self.base_env()}
            for _ in range(3):
                with mock.patch.dict("os.environ", env, clear=True):
                    with mock.patch.object(
                        cfg, "fetch_models", return_value=GATEWAY_PAYLOAD["data"]
                    ):
                        cfg.main()
            config = yaml.safe_load((home / "config.yaml").read_text(encoding="utf-8"))
            self.assertEqual(len(config["fallback_providers"]), 1)
            self.assertEqual(len(config["providers"]), 1)

    def test_no_credentials_writes_nothing(self):
        config = self.run_main({"FIRTAL_REGISTRY_URL": "https://registry.example"})
        self.assertIsNone(config)


if __name__ == "__main__":
    sys.exit(0 if unittest.main(exit=False).result.wasSuccessful() else 1)
