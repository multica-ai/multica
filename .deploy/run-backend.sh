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

# launchd spawns this script with a minimal PATH (/usr/bin:/bin). Homebrew
# binaries (incl. `infisical`) live under /opt/homebrew/bin and must be on
# PATH for the Infisical wrap below to resolve. Mirrors run-daemon.sh.
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

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
  : "${INFISICAL_DOMAIN:?INFISICAL_DOMAIN required when Infisical auth is configured}"
  : "${INFISICAL_PROJECT_ID:?INFISICAL_PROJECT_ID required when Infisical auth is configured}"

  # `infisical run` does not auto-authenticate from universal-auth env vars —
  # it needs either a cached login session (which launchd-spawned processes
  # don't share) or an explicit token. We log in via universal-auth here and
  # pass the resulting access token via INFISICAL_TOKEN env var (not the
  # --token flag, which would put the JWT in `ps` output).
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
    "${BACKEND_CMD[@]}"
else
  exec "${BACKEND_CMD[@]}"
fi
