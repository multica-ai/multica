#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_config() {
  local config=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$config"; then
    echo "Missing expected docker compose config value:"
    echo "  $expected"
    exit 1
  fi
}

config="$(
  FRONTEND_PORT=3100 BACKEND_PORT=9100 docker compose \
    --env-file .env.example \
    -f docker-compose.selfhost.yml \
    config
)"

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOGLE_REDIRECT_URI: http://localhost:3100/auth/callback'
require_config "$config" 'MULTICA_APP_URL: http://localhost:3100'

if ! grep -Eq '^export PORT($|[[:space:]])' scripts/check.sh; then
  echo "scripts/check.sh must export the derived PORT for its backend subprocess."
  exit 1
fi

echo "self-host compose env derivation ok"
