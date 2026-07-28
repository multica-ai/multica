#!/usr/bin/env python3
"""Configure Hermes' managed gateway provider + fallback without persisting the key.

Two separate things are written, and they are not interchangeable:

* ``fallback_providers`` — Hermes' native retry chain. One model, used only when
  the primary model fails.
* ``providers.firtal-gateway`` — a *selectable* provider, so every model the
  gateway grants this key shows up in ``hermes model`` and can be pinned on an
  agent. This mirrors what docker/pi-firtal-gateway.ts already does for Pi.

The model list is discovered from GET /v1/models rather than hard-coded, so it
follows the gateway's grants instead of drifting from them. Discovery is
best-effort: a gateway that is down must never stop the container from starting,
so on any failure the provider is still registered with the fallback model alone.

The key itself is never written to disk — the provider entry references the
``FIRTAL_REGISTRY_KEY`` environment variable by name via ``key_env``.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from pathlib import Path

import yaml

MODELS_PATH = "/api/ai/proxy/v1/models"
DISCOVERY_TIMEOUT_SECONDS = 5
PROVIDER_NAME = "firtal-gateway"
DEFAULT_FALLBACK_MODEL = "claude-sonnet-5"


def main() -> None:
    registry_url = os.environ.get("FIRTAL_REGISTRY_URL", "").strip().rstrip("/")
    registry_key = os.environ.get("FIRTAL_REGISTRY_KEY", "").strip()
    if not registry_url or not registry_key:
        return

    hermes_home = Path(os.environ["HERMES_HOME"])
    config_path = hermes_home / "config.yaml"
    config = load_config(config_path)
    fallback_url = f"{registry_url}/api/ai/proxy/v1"
    fallback_model = (
        os.environ.get("FIRTAL_REGISTRY_MODEL", "").strip() or DEFAULT_FALLBACK_MODEL
    )
    fallback = {
        "provider": "custom",
        "model": fallback_model,
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

    providers = config.get("providers")
    if not isinstance(providers, dict):
        providers = {}
    providers[PROVIDER_NAME] = {
        "name": "Firtal AI Gateway",
        "base_url": fallback_url,
        "key_env": "FIRTAL_REGISTRY_KEY",
        "api_mode": "chat_completions",
        "models": discover_models(registry_url, registry_key, fallback_model),
    }
    config["providers"] = providers

    hermes_home.mkdir(parents=True, exist_ok=True)
    config_path.write_text(yaml.safe_dump(config, sort_keys=False), encoding="utf-8")
    config_path.chmod(0o600)


def discover_models(
    registry_url: str, registry_key: str, fallback_model: str
) -> list[str]:
    """Model ids this key may call, fallback-model first.

    Embeddings and other non-chat capabilities cannot serve a Hermes turn, so
    they are filtered out the same way the Pi extension filters them.
    """
    models: list[str] = [fallback_model]
    for entry in fetch_models(registry_url, registry_key):
        if not isinstance(entry, dict):
            continue
        capability = entry.get("firtal_capability")
        if capability is not None and capability != "chat":
            continue
        model_id = str(entry.get("id") or "").strip()
        if model_id and model_id not in models:
            models.append(model_id)
    return models


def fetch_models(registry_url: str, registry_key: str) -> list:
    request = urllib.request.Request(
        f"{registry_url}{MODELS_PATH}",
        headers={
            "authorization": f"Bearer {registry_key}",
            "accept": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(
            request, timeout=DISCOVERY_TIMEOUT_SECONDS
        ) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except (urllib.error.URLError, OSError, ValueError, json.JSONDecodeError):
        # A gateway that is down or slow must not stop the container from
        # starting — the fallback model alone is still a working provider.
        return []
    data = payload.get("data") if isinstance(payload, dict) else None
    return data if isinstance(data, list) else []


def load_config(path: Path) -> dict:
    if not path.exists():
        return {}
    loaded = yaml.safe_load(path.read_text(encoding="utf-8"))
    return loaded if isinstance(loaded, dict) else {}


if __name__ == "__main__":
    main()
