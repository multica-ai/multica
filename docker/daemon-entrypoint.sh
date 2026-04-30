#!/bin/sh
# Multica daemon container entrypoint.
#
# Translates the limited set of environment variables a container operator
# wants to set into the on-disk CLI config the daemon expects, then runs
# `multica daemon start --foreground` so the daemon owns PID 1's child slot
# and signals propagate cleanly via tini.
#
# Required env:
#   MULTICA_SERVER_URL — backend URL (http(s) or ws(s) — daemon normalizes)
#   MULTICA_TOKEN      — personal access token (mul_...) created in the web UI
#
# Optional env:
#   MULTICA_APP_URL              — frontend URL, only used by other CLI commands
#   MULTICA_DAEMON_DEVICE_NAME   — display name for the runtime (default: hostname)
#   MULTICA_AGENT_RUNTIME_NAME   — runtime name shown in the UI
#   MULTICA_DAEMON_POLL_INTERVAL — task poll interval (default: 3s)
#   ANTHROPIC_API_KEY            — passed through to `claude`
#   OPENAI_API_KEY               — passed through to `codex`

set -eu

CONFIG_DIR="${HOME}/.multica"
CONFIG_FILE="${CONFIG_DIR}/config.json"

if [ -z "${MULTICA_SERVER_URL:-}" ]; then
    echo "FATAL: MULTICA_SERVER_URL is required" >&2
    exit 1
fi
if [ -z "${MULTICA_TOKEN:-}" ]; then
    echo "FATAL: MULTICA_TOKEN is required (create a personal access token in the web UI)" >&2
    exit 1
fi

mkdir -p "${CONFIG_DIR}"

# Always (re)write config.json from env so rotating MULTICA_TOKEN takes effect
# on next container restart without manually clearing the volume.
umask 077
cat > "${CONFIG_FILE}" <<EOF
{
  "server_url": "${MULTICA_SERVER_URL}",
  "app_url": "${MULTICA_APP_URL:-}",
  "token": "${MULTICA_TOKEN}"
}
EOF

# Drop MULTICA_TOKEN from the daemon's env — the daemon reads it from
# config.json anyway, and leaking it via /proc/<pid>/environ to spawned
# agent processes is unnecessary.
unset MULTICA_TOKEN

exec multica daemon start --foreground
