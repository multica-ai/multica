#!/usr/bin/env python3
"""Configure Hermes' managed fallback without persisting the gateway key."""

from __future__ import annotations

import os
from pathlib import Path

import yaml


def main() -> None:
    registry_url = os.environ.get("FIRTAL_REGISTRY_URL", "").strip().rstrip("/")
    registry_key = os.environ.get("FIRTAL_REGISTRY_KEY", "").strip()
    if not registry_url or not registry_key:
        return

    hermes_home = Path(os.environ["HERMES_HOME"])
    config_path = hermes_home / "config.yaml"
    config = load_config(config_path)
    fallback_url = f"{registry_url}/api/ai/proxy/v1"
    fallback = {
        "provider": "custom",
        "model": os.environ.get("FIRTAL_REGISTRY_MODEL", "").strip()
        or "claude-sonnet-5",
        "base_url": fallback_url,
        "key_env": "FIRTAL_REGISTRY_KEY",
    }

    chain = config.get("fallback_providers")
    if not isinstance(chain, list):
        chain = []
    for index, entry in enumerate(chain):
        if (
            isinstance(entry, dict)
            and entry.get("provider") == "custom"
            and str(entry.get("base_url", "")).rstrip("/") == fallback_url
        ):
            chain[index] = fallback
            break
    else:
        chain.append(fallback)

    config["fallback_providers"] = chain
    hermes_home.mkdir(parents=True, exist_ok=True)
    config_path.write_text(yaml.safe_dump(config, sort_keys=False), encoding="utf-8")
    config_path.chmod(0o600)


def load_config(path: Path) -> dict:
    if not path.exists():
        return {}
    loaded = yaml.safe_load(path.read_text(encoding="utf-8"))
    return loaded if isinstance(loaded, dict) else {}


if __name__ == "__main__":
    main()
