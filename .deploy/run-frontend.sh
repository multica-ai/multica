#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LOG_NAME=frontend
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
export PORT="${FRONTEND_PORT:-4200}"

cd "$REPO/apps/web"
FRONTEND_CMD=(pnpm start --port "$PORT")

if [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_ID:-}" ] && [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET:-}" ]; then
  : "${INFISICAL_DOMAIN:?INFISICAL_DOMAIN required when Infisical auth is configured}"
  : "${INFISICAL_PROJECT_ID:?INFISICAL_PROJECT_ID required when Infisical auth is configured}"

  # `infisical run` does not auto-authenticate from universal-auth env vars —
  # launchd-spawned processes have no cached login session. Log in via
  # universal-auth, capture the access token, and pass it via INFISICAL_TOKEN
  # env var (not --token, which would put the JWT in `ps` output).
  INFISICAL_TOKEN=$(infisical login \
    --method=universal-auth \
    --client-id="$INFISICAL_UNIVERSAL_AUTH_CLIENT_ID" \
    --client-secret="$INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET" \
    --domain="$INFISICAL_DOMAIN" \
    --plain --silent)
  export INFISICAL_TOKEN

  exec infisical run \
    --domain="$INFISICAL_DOMAIN" \
    --projectId="$INFISICAL_PROJECT_ID" \
    --env=prod \
    -- \
    "${FRONTEND_CMD[@]}"
else
  exec "${FRONTEND_CMD[@]}"
fi
