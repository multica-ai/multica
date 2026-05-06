#!/bin/bash
# Entrypoint for the agentfarm agent-runner pod.
#
# Writes a minimal multica CLI config from environment variables, then starts
# the daemon in the foreground. The daemon picks up the token from the config
# file and auto-discovers and watches every workspace the token has access to
# (server/internal/daemon/daemon.go: resolveAuth + syncWorkspacesFromAPI).
#
# Required env (sourced from external secrets / configmap):
#   MULTICA_SERVER_URL  - websocket/HTTP URL of the Multica server
#   MULTICA_APP_URL     - HTTP URL of the Multica web app
#   MULTICA_TOKEN       - personal access token (mul_...) for the daemon user
#   ANTHROPIC_API_KEY   - API key consumed by `claude` (Claude Code) at runtime
set -euo pipefail

require() {
  local name="$1"
  if [ -z "${!name:-}" ]; then
    echo "agentrunner: required env var $name is not set" >&2
    exit 1
  fi
}

require MULTICA_SERVER_URL
require MULTICA_APP_URL
require MULTICA_TOKEN
require ANTHROPIC_API_KEY

config_dir="$HOME/.multica"
config_file="$config_dir/config.json"
mkdir -p "$config_dir"

umask 077
cat > "$config_file" <<JSON
{
  "server_url": "${MULTICA_SERVER_URL}",
  "app_url": "${MULTICA_APP_URL}",
  "token": "${MULTICA_TOKEN}"
}
JSON

multica auth status || true

exec multica daemon start --foreground
