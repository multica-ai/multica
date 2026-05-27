#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Build a self-contained sandbox with stub `curl` and a tarball that the
# release-binary fallback path will download. Each test supplies its own
# `brew` stub to model a specific Homebrew failure mode.
_setup_sandbox() {
  local tmp="$1"
  local stub_bin="$tmp/stub-bin"
  local install_bin="$tmp/install-bin"
  local payload_dir="$tmp/payload"
  mkdir -p "$stub_bin" "$install_bin" "$payload_dir"

  cat >"$payload_dir/multica" <<'STUB'
#!/usr/bin/env bash
echo "multica v0.3.2 (commit: test)"
STUB
  chmod +x "$payload_dir/multica"
  tar -czf "$tmp/multica.tar.gz" -C "$payload_dir" multica

  cat >"$stub_bin/curl" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == *"-sI"* ]]; then
  printf 'HTTP/2 302\r\nlocation: https://github.com/multica-ai/multica/releases/tag/v0.3.2\r\n'
  exit 0
fi

out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [[ -z "$out" ]]; then
  echo "stub curl expected -o" >&2
  exit 2
fi
cp "$MULTICA_TEST_ARCHIVE" "$out"
STUB
  chmod +x "$stub_bin/curl"
}

_run_installer() {
  local tmp="$1"
  local out="$tmp/install.out"
  local err="$tmp/install.err"
  if ! PATH="$tmp/stub-bin:$tmp/install-bin:/usr/bin:/bin" \
    MULTICA_BIN_DIR="$tmp/install-bin" \
    MULTICA_TEST_ARCHIVE="$tmp/multica.tar.gz" \
    bash "$ROOT_DIR/scripts/install.sh" >"$out" 2>"$err"; then
    echo "install.sh exited non-zero" >&2
    cat "$out" >&2 || true
    cat "$err" >&2 || true
    return 1
  fi

  if [[ ! -x "$tmp/install-bin/multica" ]]; then
    echo "expected fallback binary at $tmp/install-bin/multica" >&2
    cat "$out" >&2 || true
    cat "$err" >&2 || true
    return 1
  fi

  if ! grep -q "Homebrew output (last 80 lines):" "$err"; then
    echo "expected diagnostic tail in stderr" >&2
    cat "$err" >&2 || true
    return 1
  fi
}

test_brew_install_failure_falls_back_to_release_binary() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    exit 0
    ;;
  install)
    echo "simulated brew install failure" >&2
    exit 42
    ;;
  list)
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  _run_installer "$tmp"
}

test_brew_tap_failure_falls_back_to_release_binary() {
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  _setup_sandbox "$tmp"
  cat >"$tmp/stub-bin/brew" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  tap)
    echo "simulated brew tap failure" >&2
    exit 17
    ;;
  *)
    echo "brew $* should not be reached after tap failure" >&2
    exit 99
    ;;
esac
STUB
  chmod +x "$tmp/stub-bin/brew"

  _run_installer "$tmp"
}

test_generated_selfhost_env_randomizes_postgres_password() {
  local tmp
  MULTICA_INSTALL_SH_SOURCE_ONLY=1 source "$ROOT_DIR/scripts/install.sh"
  trap - RETURN

  tmp="$(mktemp -d)"

  cp "$ROOT_DIR/.env.example" "$tmp/.env"

  local jwt postgres_password database_url
  jwt="$(random_hex 32)"
  postgres_password="$(random_hex 24)"
  database_url="$(env_file_value "$tmp/.env" DATABASE_URL "")"
  set_env_file_value "$tmp/.env" JWT_SECRET "$jwt"
  set_env_file_value "$tmp/.env" POSTGRES_PASSWORD "$postgres_password"
  set_env_file_value "$tmp/.env" DATABASE_URL "$(postgres_url_with_password "$database_url" "$postgres_password")"

  if grep -Fxq "POSTGRES_PASSWORD=multica" "$tmp/.env"; then
    echo "expected generated .env to replace default POSTGRES_PASSWORD" >&2
    cat "$tmp/.env" >&2
    return 1
  fi

  if ! grep -Eq '^POSTGRES_PASSWORD=[0-9a-f]{48}$' "$tmp/.env"; then
    echo "expected generated .env to contain a 48-character hex POSTGRES_PASSWORD" >&2
    cat "$tmp/.env" >&2
    return 1
  fi

  if ! grep -Fxq "DATABASE_URL=postgres://multica:${postgres_password}@localhost:5432/multica?sslmode=disable" "$tmp/.env"; then
    echo "expected DATABASE_URL to use the generated Postgres password" >&2
    cat "$tmp/.env" >&2
    return 1
  fi

  if ! grep -Fxq "JWT_SECRET=$jwt" "$tmp/.env"; then
    echo "expected generated .env to contain random JWT_SECRET" >&2
    cat "$tmp/.env" >&2
    rm -rf "$tmp"
    return 1
  fi

  rm -rf "$tmp"
}

test_postgres_url_password_replacement_preserves_connection_parts() {
  MULTICA_INSTALL_SH_SOURCE_ONLY=1 source "$ROOT_DIR/scripts/install.sh"
  trap - RETURN

  local url updated
  url='postgres://db_user:old-password@db.internal.example:6543/custom_db?sslmode=require&pool_max_conns=10'
  updated="$(postgres_url_with_password "$url" "newpass123")"

  if [[ "$updated" != 'postgres://db_user:newpass123@db.internal.example:6543/custom_db?sslmode=require&pool_max_conns=10' ]]; then
    echo "expected DATABASE_URL password replacement to preserve user, host, port, db, and query string" >&2
    echo "Observed: $updated" >&2
    return 1
  fi
}

test_make_selfhost_uses_generated_postgres_password() {
  local tmp repo log generated_password
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  repo="$tmp/repo"
  log="$tmp/docker-env.log"
  mkdir -p "$repo/scripts" "$tmp/stub-bin"
  cp "$ROOT_DIR/Makefile" "$repo/Makefile"
  cp "$ROOT_DIR/.env.example" "$repo/.env.example"
  cp "$ROOT_DIR/scripts/install.sh" "$repo/scripts/install.sh"

  cat >"$tmp/stub-bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s POSTGRES_PASSWORD=%s DATABASE_URL=%s\n' "$*" "$POSTGRES_PASSWORD" "$DATABASE_URL" >>"$MULTICA_TEST_DOCKER_ENV_LOG"
exit 0
STUB
  chmod +x "$tmp/stub-bin/docker"

  cat >"$tmp/stub-bin/curl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  chmod +x "$tmp/stub-bin/curl"

  PATH="$tmp/stub-bin:/usr/bin:/bin" \
    MULTICA_TEST_DOCKER_ENV_LOG="$log" \
    make -C "$repo" selfhost >/dev/null

  generated_password="$(grep '^POSTGRES_PASSWORD=' "$repo/.env" | cut -d= -f2-)"
  if [[ -z "$generated_password" || "$generated_password" == "multica" ]]; then
    echo "expected make selfhost to generate a non-default POSTGRES_PASSWORD" >&2
    cat "$repo/.env" >&2
    return 1
  fi

  if grep -q 'POSTGRES_PASSWORD=multica' "$log"; then
    echo "expected docker compose commands to receive the generated POSTGRES_PASSWORD, not the Makefile default" >&2
    cat "$log" >&2
    return 1
  fi

  if ! grep -q "POSTGRES_PASSWORD=$generated_password" "$log"; then
    echo "expected docker compose commands to receive the generated POSTGRES_PASSWORD" >&2
    cat "$log" >&2
    return 1
  fi

  if ! grep -q "DATABASE_URL=postgres://multica:$generated_password@localhost:5432/multica?sslmode=disable" "$log"; then
    echo "expected docker compose commands to receive DATABASE_URL with the generated POSTGRES_PASSWORD" >&2
    cat "$log" >&2
    return 1
  fi
}

test_brew_install_failure_falls_back_to_release_binary
test_brew_tap_failure_falls_back_to_release_binary
test_generated_selfhost_env_randomizes_postgres_password
test_postgres_url_password_replacement_preserves_connection_parts
test_make_selfhost_uses_generated_postgres_password
echo "install.sh tests passed"
