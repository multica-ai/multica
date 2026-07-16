#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export HERMES_HOME="$tmp/hermes"
export FIRTAL_REGISTRY_URL="https://registry.example.test/"
export FIRTAL_REGISTRY_KEY="synthetic-key-is-never-written"
export FIRTAL_REGISTRY_MODEL="gateway-test-model"

mkdir -p "$HERMES_HOME"
printf '%s\n' 'model:' '  provider: openai-codex' '  default: gpt-5.3-codex' > "$HERMES_HOME/config.yaml"
python3 "$repo_root/docker/configure-hermes-fallback.py"

python3 - "$HERMES_HOME/config.yaml" <<'PY'
import pathlib
import sys
import yaml

config_path = pathlib.Path(sys.argv[1])
config = yaml.safe_load(config_path.read_text())
assert config["model"] == {
    "provider": "openai-codex",
    "default": "gpt-5.3-codex",
}
entry = config["fallback_providers"][0]
assert entry == {
    "provider": "custom",
    "model": "gateway-test-model",
    "base_url": "https://registry.example.test/api/ai/proxy/v1",
    "key_env": "FIRTAL_REGISTRY_KEY",
}
assert "synthetic-key-is-never-written" not in config_path.read_text()
PY

python3 "$repo_root/docker/configure-hermes-fallback.py"
count="$(grep -c 'provider: custom' "$HERMES_HOME/config.yaml")"
[[ "$count" == "1" ]] || { printf 'fallback entry was duplicated\n' >&2; exit 1; }

unset FIRTAL_REGISTRY_MODEL
python3 "$repo_root/docker/configure-hermes-fallback.py"
python3 - "$HERMES_HOME/config.yaml" <<'PY'
import pathlib
import sys
import yaml

config = yaml.safe_load(pathlib.Path(sys.argv[1]).read_text())
assert config["fallback_providers"][0]["model"] == "claude-sonnet-5"
PY

printf 'Hermes uses the Firtal AI Gateway as a secret-safe fallback\n'
