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

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOGLE_REDIRECT_URI: http://localhost:3100/auth/callback'
require_config "$config" 'MULTICA_APP_URL: http://localhost:3100'

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
require_env "$local_env" 'FRONTEND_ORIGIN=http://localhost:3100'
require_env "$local_env" 'MULTICA_APP_URL=http://localhost:3100'
require_env "$local_env" 'GOOGLE_REDIRECT_URI=http://localhost:3100/auth/callback'
require_env "$local_env" 'MULTICA_SERVER_URL=ws://localhost:9100/ws'
require_env "$local_env" 'LOCAL_UPLOAD_BASE_URL=http://localhost:9100'
require_env "$local_env" 'PLAYWRIGHT_BASE_URL=http://localhost:3100'

# Build override threads the resolved official baseline into both images and
# never defaults provenance to `dev`. The no-dev constraint is verified against
# the file directly so it does not depend on a Docker daemon being present.
if grep -Fq '${VERSION:-dev}' docker-compose.selfhost.build.yml \
	|| grep -Fq 'NEXT_PUBLIC_APP_VERSION: dev' docker-compose.selfhost.build.yml; then
	echo "Build override must not default provenance to dev."
	exit 1
fi

# With a baseline supplied, the resolved config carries it for both images.
build_config_with_tag="$(
  VERSION=v9.9.9-test \
  NEXT_PUBLIC_APP_VERSION=v9.9.9-test \
  docker compose \
    -f docker-compose.selfhost.yml \
    -f docker-compose.selfhost.build.yml \
    config 2>/dev/null
)"
require_config "$build_config_with_tag" 'v9.9.9-test'

# A raw invocation without a baseline must fail rather than stamp `dev`.
if env -u VERSION -u NEXT_PUBLIC_APP_VERSION \
  docker compose \
    -f docker-compose.selfhost.yml \
    -f docker-compose.selfhost.build.yml \
    config >/dev/null 2>&1; then
	echo "Build override must require VERSION / NEXT_PUBLIC_APP_VERSION (no dev default)."
	exit 1
fi

echo "self-host env derivation ok"
