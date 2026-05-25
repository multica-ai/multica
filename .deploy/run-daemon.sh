#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro

LOG_NAME=daemon
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

export PATH="/Users/sara/.nvm/versions/node/v24.13.1/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

# Claude-agenter spawned af denne daemon bruger Max-kontoen jesperhvejsel@gmail.com.
export CLAUDE_CONFIG_DIR=/Users/sara/.claude-accounts/jesperhvejsel@gmail.com

DAEMON_CMD=("$REPO/server/bin/multica" daemon start --profile local --foreground)

if [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_ID:-}" ] && [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET:-}" ]; then
  exec infisical run \
    --domain="${INFISICAL_DOMAIN:?INFISICAL_DOMAIN required when Infisical auth is configured}" \
    --projectId="${INFISICAL_PROJECT_ID:?INFISICAL_PROJECT_ID required when Infisical auth is configured}" \
    --env=prod \
    -- \
    "${DAEMON_CMD[@]}"
else
  exec "${DAEMON_CMD[@]}"
fi
