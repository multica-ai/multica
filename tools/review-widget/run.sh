#!/usr/bin/env bash
# Start the review ingest service with .env loaded.
#   ./run.sh          — foreground
#   ./run.sh --bg     — background, logging to /tmp/review-ingest.log
set -euo pipefail
cd "$(dirname "$0")"

[ -f .env ] || { echo "error: .env missing (cp .env.dev .env and fill it in)"; exit 1; }

# Load .env without letting the shell interpret values.
set -a
# shellcheck disable=SC1091
. ./.env
set +a

if [ "${1:-}" = "--bg" ]; then
  pkill -f "node src/server.js" 2>/dev/null || true
  nohup node src/server.js > /tmp/review-ingest.log 2>&1 &
  sleep 1.5
  echo "started (pid $!) — log: /tmp/review-ingest.log"
  curl -s --max-time 5 "http://127.0.0.1:${PORT:-8095}/health" && echo
else
  exec node src/server.js
fi
