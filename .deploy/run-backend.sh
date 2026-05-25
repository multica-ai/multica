#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

LOG_NAME=backend
# shellcheck source=_log-rotation.sh
source "$REPO/.deploy/_log-rotation.sh"

cd "$REPO"

set -a
# shellcheck source=/dev/null
source .env
set +a

# If the previous backend instance is still holding the port (typical
# right after `launchctl kickstart -k` — the OS hasn't released the
# listening socket yet), wait up to 3s for it to release. Avoids the
# noisy "address already in use" first-bind retry that KeepAlive=true
# eventually recovers from but still drops in backend.err.log. JEH-1881.
PORT_TO_WAIT="${PORT:-8180}"
for _ in 1 2 3 4 5 6; do
  if ! nc -z 127.0.0.1 "$PORT_TO_WAIT" 2>/dev/null; then
    break
  fi
  sleep 0.5
done

BACKEND_CMD=("$REPO/server/bin/server")

if [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_ID:-}" ] && [ -n "${INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET:-}" ]; then
  exec infisical run \
    --domain="${INFISICAL_DOMAIN:?INFISICAL_DOMAIN required when Infisical auth is configured}" \
    --projectId="${INFISICAL_PROJECT_ID:?INFISICAL_PROJECT_ID required when Infisical auth is configured}" \
    --env=prod \
    -- \
    "${BACKEND_CMD[@]}"
else
  exec "${BACKEND_CMD[@]}"
fi
