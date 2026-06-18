#!/usr/bin/env bash
set -euo pipefail
ENV_FILE="${ENV_FILE:-.env}"
SELFHOST_STOP_DAEMON="${SELFHOST_STOP_DAEMON:-false}"

if [ "$SELFHOST_STOP_DAEMON" = "true" ] && [ -d server ]; then
  (
    cd server
    go run ./cmd/multica daemon stop || true
  )
fi

docker compose --env-file "$ENV_FILE" -f docker-compose.selfhost.yml down
