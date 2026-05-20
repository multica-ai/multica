#!/usr/bin/env bash
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro

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

exec "$REPO/server/bin/server"
