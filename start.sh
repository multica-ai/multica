#!/usr/bin/env bash
set -euo pipefail
ENV_FILE="${ENV_FILE:-.env}"
[ -f "$ENV_FILE" ] || cp .env.example "$ENV_FILE"
docker compose --env-file "$ENV_FILE" -f docker-compose.selfhost.yml pull
docker compose --env-file "$ENV_FILE" -f docker-compose.selfhost.yml up -d

PORT="$(grep '^PORT=' "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2)"
PORT="${PORT:-8080}"

for _ in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

cd server
go run ./cmd/multica daemon start
