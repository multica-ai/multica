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
    echo "Missing expected file content in $file:"
    echo "  $expected"
    exit 1
  fi
}

require_success() {
  local label=$1
  shift

  if ! "$@"; then
    echo "Expected success: $label"
    exit 1
  fi
}

require_failure() {
  local label=$1
  shift

  if "$@"; then
    echo "Expected failure: $label"
    exit 1
  fi
}

tmp_env="$(mktemp)"
tmp_dir="$(mktemp -d)"
trap 'rm -f "$tmp_env"; rm -rf "$tmp_dir"' EXIT
sed 's/^FRONTEND_PORT=.*/FRONTEND_PORT=3100/' .env.example >"$tmp_env"
printf '\nBACKEND_PORT=9100\n' >>"$tmp_env"

config="$(
  env -i PATH="$PATH" HOME="$HOME" DOCKER_HOST="${DOCKER_HOST:-}" docker compose \
    --env-file "$tmp_env" \
    -f docker-compose.selfhost.yml \
    config
)"

require_config "$config" 'published: "3100"'
require_config "$config" 'published: "9100"'
require_config "$config" 'host_ip: 127.0.0.1'
require_config "$config" 'FRONTEND_ORIGIN: http://localhost:3100'
require_config "$config" 'GOOGLE_REDIRECT_URI: http://localhost:3100/auth/callback'
require_config "$config" 'MULTICA_APP_URL: http://localhost:3100'
if grep -Fq 'MULTICA_SERVER_URL:' <<<"$config"; then
  echo "docker-compose.selfhost.yml must not pass daemon-only MULTICA_SERVER_URL into the backend container."
  exit 1
fi

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

default_env="$(
  env -i PATH="$PATH" bash -c '
    set -euo pipefail
    . scripts/selfhost-env.sh
    printf "%s\n" \
      "BACKEND_PORT=${BACKEND_PORT}" \
      "SERVER_PORT=${SERVER_PORT}" \
      "PORT=${PORT}" \
      "CORS_ALLOWED_ORIGINS=${CORS_ALLOWED_ORIGINS:-}" \
      "MULTICA_SERVER_URL=${MULTICA_SERVER_URL}" \
      "BACKEND_BASE_URL=${BACKEND_BASE_URL}" \
      "FRONTEND_BASE_URL=${FRONTEND_BASE_URL}"
  '
)"

require_env "$default_env" 'BACKEND_PORT=8080'
require_env "$default_env" 'SERVER_PORT=8080'
require_env "$default_env" 'PORT=8080'
require_env "$default_env" 'CORS_ALLOWED_ORIGINS='
require_env "$default_env" 'MULTICA_SERVER_URL=ws://localhost:8080/ws'
require_env "$default_env" 'BACKEND_BASE_URL=http://localhost:8080'
require_env "$default_env" 'FRONTEND_BASE_URL=http://localhost:3000'

for key in BACKEND_PORT API_PORT SERVER_PORT PORT; do
  alias_env="$(
    env -i PATH="$PATH" "$key=9187" bash -c '
      set -euo pipefail
      . scripts/selfhost-env.sh
      printf "%s\n" \
        "BACKEND_PORT=${BACKEND_PORT}" \
        "MULTICA_SERVER_URL=${MULTICA_SERVER_URL}" \
        "BACKEND_BASE_URL=${BACKEND_BASE_URL}"
    '
  )"
  require_env "$alias_env" 'BACKEND_PORT=9187'
  require_env "$alias_env" 'MULTICA_SERVER_URL=ws://localhost:9187/ws'
  require_env "$alias_env" 'BACKEND_BASE_URL=http://localhost:9187'
done

lan_env="$(
  env -i PATH="$PATH" \
    BIND_HOST=192.168.1.100 \
    FRONTEND_PORT=3100 \
    bash -c '
      set -euo pipefail
      . scripts/selfhost-env.sh
      printf "%s\n" \
        "FRONTEND_ORIGIN=${FRONTEND_ORIGIN}" \
        "CORS_ALLOWED_ORIGINS=${CORS_ALLOWED_ORIGINS}" \
        "MULTICA_APP_URL=${MULTICA_APP_URL}" \
        "MULTICA_SERVER_URL=${MULTICA_SERVER_URL}"
    '
)"

require_env "$lan_env" 'FRONTEND_ORIGIN=http://192.168.1.100:3100'
require_env "$lan_env" 'CORS_ALLOWED_ORIGINS=http://192.168.1.100:3100'
require_env "$lan_env" 'MULTICA_APP_URL=http://192.168.1.100:3100'
require_env "$lan_env" 'MULTICA_SERVER_URL=ws://192.168.1.100:8080/ws'

require_file_contains start.sh 'SELFHOST_START_DAEMON="${SELFHOST_START_DAEMON:-false}"'
require_file_contains start.sh 'wait_for_url "backend readiness" "${BACKEND_BASE_URL}/readyz"'
require_file_contains start.sh 'wait_for_url "frontend" "${FRONTEND_BASE_URL}/"'

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin" "$tmp_dir/server"

cat >"$fake_bin/docker" <<'SCRIPT'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"${SELFHOST_TEST_LOG:?}"
exit "${SELFHOST_FAKE_DOCKER_STATUS:-0}"
SCRIPT
chmod +x "$fake_bin/docker"

cat >"$fake_bin/go" <<'SCRIPT'
#!/usr/bin/env bash
printf 'go %s\n' "$*" >>"${SELFHOST_TEST_LOG:?}"
exit 0
SCRIPT
chmod +x "$fake_bin/go"

cat >"$fake_bin/curl" <<'SCRIPT'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >>"${SELFHOST_TEST_LOG:?}"
url="${*: -1}"
case "$url" in
  */readyz) exit "${SELFHOST_FAKE_READYZ_STATUS:-0}" ;;
  */health) exit "${SELFHOST_FAKE_HEALTH_STATUS:-0}" ;;
  *) exit "${SELFHOST_FAKE_FRONTEND_STATUS:-0}" ;;
esac
SCRIPT
chmod +x "$fake_bin/curl"

cp start.sh "$tmp_dir/start.sh"
chmod +x "$tmp_dir/start.sh"
cp stop.sh "$tmp_dir/stop.sh"
chmod +x "$tmp_dir/stop.sh"
cp -R scripts "$tmp_dir/scripts"
printf 'BACKEND_PORT=9188\nFRONTEND_PORT=3188\n' >"$tmp_dir/.env"

smoke_start() {
  (
    cd "$tmp_dir"
    PATH="$fake_bin:$PATH" SELFHOST_TEST_LOG="$tmp_dir/log" SELFHOST_MAX_ATTEMPTS=1 "$tmp_dir/start.sh"
  )
}

smoke_stop() {
  (
    cd "$tmp_dir"
    PATH="$fake_bin:$PATH" SELFHOST_TEST_LOG="$tmp_dir/log" "$tmp_dir/stop.sh"
  )
}

smoke_stop_with_daemon() {
  (
    cd "$tmp_dir"
    PATH="$fake_bin:$PATH" SELFHOST_TEST_LOG="$tmp_dir/log" SELFHOST_STOP_DAEMON=true "$tmp_dir/stop.sh"
  )
}

smoke_start_backend_unready() {
  SELFHOST_FAKE_READYZ_STATUS=22 smoke_start
}

smoke_start_pull_fails() {
  SELFHOST_FAKE_DOCKER_STATUS=1 smoke_start
}

: >"$tmp_dir/log"
require_success "start.sh succeeds when backend readiness and frontend checks pass" smoke_start
require_file_contains "$tmp_dir/log" 'curl -fsS --max-time 2 http://localhost:9188/readyz'
require_file_contains "$tmp_dir/log" 'curl -fsS --max-time 2 http://localhost:3188/'
if grep -Fq 'daemon start' "$tmp_dir/log"; then
  echo "start.sh must not start the daemon by default"
  exit 1
fi

: >"$tmp_dir/log"
require_failure "start.sh fails when backend readiness never succeeds" smoke_start_backend_unready
if grep -Fq 'daemon start' "$tmp_dir/log"; then
  echo "start.sh must not start the daemon after readiness failure"
  exit 1
fi

: >"$tmp_dir/log"
require_failure "start.sh fails fast when image pull fails" smoke_start_pull_fails
require_file_contains "$tmp_dir/log" 'docker compose --env-file .env -f docker-compose.selfhost.yml pull'

: >"$tmp_dir/log"
require_success "stop.sh stops only the Docker Compose stack by default" smoke_stop
require_file_contains "$tmp_dir/log" 'docker compose --env-file .env -f docker-compose.selfhost.yml down'
if grep -Fq 'daemon stop' "$tmp_dir/log"; then
  echo "stop.sh must not stop the daemon by default"
  exit 1
fi

: >"$tmp_dir/log"
require_success "stop.sh can stop the source daemon with explicit opt-in" smoke_stop_with_daemon
require_file_contains "$tmp_dir/log" 'docker compose --env-file .env -f docker-compose.selfhost.yml down'
require_file_contains "$tmp_dir/log" 'go run ./cmd/multica daemon stop'

echo "self-host env derivation ok"
