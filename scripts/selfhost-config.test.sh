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

require_env() {
  local output=$1
  local expected=$2

  if ! grep -Fxq "$expected" <<<"$output"; then
    echo "Missing expected derived env value:"
    echo "  $expected"
    echo "Observed:"
    echo "$output"
    exit 1
  fi
}

require_file_contains() {
  local file=$1
  local expected=$2

  if ! grep -Fq "$expected" "$file"; then
    echo "$file missing expected value:"
    echo "  $expected"
    exit 1
  fi
}

tmp_env="$(mktemp)"
trap 'rm -f "$tmp_env"' EXIT
sed 's/^FRONTEND_PORT=.*/FRONTEND_PORT=3100/' .env.example >"$tmp_env"
printf '\nBACKEND_PORT=9100\n' >>"$tmp_env"

config="$(
  docker compose \
    --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml \
    config
)"

external_env="$(mktemp)"
trap 'rm -f "$tmp_env" "$external_env"' EXIT
cp "$tmp_env" "$external_env"
printf 'DATABASE_URL=postgres://external-user:external-pass@external-postgres.example.com:5432/externaldb?sslmode=require\n' >>"$external_env"

external_config="$(
  docker compose \
    --env-file "$external_env" \
    -f docker-compose.selfhost.yml \
    config
)"

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOGLE_REDIRECT_URI: http://localhost:3100/auth/callback'
require_config "$config" 'MULTICA_APP_URL: http://localhost:3100'
require_config "$config" 'DATABASE_URL: postgres://multica:multica@postgres:5432/multica?sslmode=disable'
require_config "$external_config" 'DATABASE_URL: postgres://external-user:external-pass@external-postgres.example.com:5432/externaldb?sslmode=require'
require_config "$config" 'test:'
require_config "$config" 'CMD-SHELL'
require_config "$config" 'wget -qO- http://127.0.0.1:8080/readyz >/dev/null'
require_config "$config" 'start_period: 30s'

require_file_contains Dockerfile 'addgroup -S -g 10001 multica'
require_file_contains Dockerfile 'install -d -o multica -g multica /app/data/uploads'
require_file_contains Dockerfile 'USER multica:multica'
require_file_contains Makefile 'http://localhost:$${PORT:-8080}/readyz'
require_file_contains scripts/install.sh 'http://localhost:${backend_port}/readyz'
require_file_contains scripts/install.ps1 'http://localhost:$backendPort/readyz'
require_file_contains deploy/helm/multica/values.yaml 'runAsUser: 10001'
require_file_contains deploy/helm/multica/values.yaml 'fsGroup: 10001'
require_file_contains deploy/helm/multica/values.yaml 'runAsNonRoot: true'
require_file_contains deploy/helm/multica/templates/backend.yaml '.Values.backend.podSecurityContext'
require_file_contains deploy/helm/multica/templates/backend.yaml '.Values.backend.securityContext'

for script in scripts/dev.sh scripts/check.sh; do
  if ! grep -Fq '. scripts/local-env.sh' "$script"; then
    echo "$script must source scripts/local-env.sh for shared local env derivation."
    exit 1
  fi
done

local_env="$(
  env -i PATH="$PATH" bash -c '
    set -euo pipefail
    env_file=$1
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
    # shellcheck disable=SC1091
    . scripts/local-env.sh
    printf "%s\n" \
      "PORT=${PORT}" \
      "FRONTEND_PORT=${FRONTEND_PORT}" \
      "DATABASE_URL=${DATABASE_URL}" \
      "FRONTEND_ORIGIN=${FRONTEND_ORIGIN}" \
      "MULTICA_APP_URL=${MULTICA_APP_URL}" \
      "GOOGLE_REDIRECT_URI=${GOOGLE_REDIRECT_URI}" \
      "MULTICA_SERVER_URL=${MULTICA_SERVER_URL}" \
      "LOCAL_UPLOAD_BASE_URL=${LOCAL_UPLOAD_BASE_URL}" \
      "PLAYWRIGHT_BASE_URL=${PLAYWRIGHT_BASE_URL}"
  ' _ "$tmp_env"
)"

require_env "$local_env" 'PORT=9100'
require_env "$local_env" 'FRONTEND_PORT=3100'
require_env "$local_env" 'DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable'
require_env "$local_env" 'FRONTEND_ORIGIN=http://localhost:3100'
require_env "$local_env" 'MULTICA_APP_URL=http://localhost:3100'
require_env "$local_env" 'GOOGLE_REDIRECT_URI=http://localhost:3100/auth/callback'
require_env "$local_env" 'MULTICA_SERVER_URL=ws://localhost:9100/ws'
require_env "$local_env" 'LOCAL_UPLOAD_BASE_URL=http://localhost:9100'
require_env "$local_env" 'PLAYWRIGHT_BASE_URL=http://localhost:3100'

echo "self-host env derivation ok"
