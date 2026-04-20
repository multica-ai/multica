#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-${ENV_FILE:-.env}}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example first."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

BACKEND_URL="http://127.0.0.1:${PORT:-13080}"
FRONTEND_URL="http://127.0.0.1:${FRONTEND_PORT:-13030}"

printf '==> Backend health: %s/health\n' "$BACKEND_URL"
curl -fsS "$BACKEND_URL/health"
printf '\n==> Frontend page: %s\n' "$FRONTEND_URL"
curl -I -fsS "$FRONTEND_URL" | sed -n '1,10p'
