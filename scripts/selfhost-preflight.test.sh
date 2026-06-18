#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_contains() {
  local output=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$output"; then
    echo "Missing expected preflight output:"
    echo "  $expected"
    echo "Observed:"
    echo "$output"
    exit 1
  fi
}

require_not_contains() {
  local output=$1
  local forbidden=$2

  if grep -Fq "$forbidden" <<<"$output"; then
    echo "Preflight output included a sensitive value:"
    echo "  $forbidden"
    echo "Observed:"
    echo "$output"
    exit 1
  fi
}

run_preflight_expect_fail() {
  local env_file=$1
  local output

  if output="$(bash scripts/selfhost-preflight.sh "$env_file" 2>&1)"; then
    echo "Expected selfhost preflight to fail for $env_file" >&2
    echo "$output" >&2
    exit 1
  fi

  printf "%s" "$output"
}

run_preflight_expect_pass() {
  local env_file=$1
  local output

  if ! output="$(bash scripts/selfhost-preflight.sh "$env_file" 2>&1)"; then
    echo "Expected selfhost preflight to pass for $env_file" >&2
    echo "$output" >&2
    exit 1
  fi

  printf "%s" "$output"
}

make_env() {
  local env_file=$1
  shift

  cat >"$env_file" <<'ENV'
POSTGRES_PASSWORD=fixture-postgres-password
JWT_SECRET=fixture-jwt-secret
APP_ENV=production
BIND_HOST=127.0.0.1
FRONTEND_ORIGIN=http://localhost:3000
CORS_ALLOWED_ORIGINS=
RESEND_API_KEY=fixture-resend-key
SMTP_HOST=
SMTP_TLS_INSECURE=false
MULTICA_DEV_VERIFICATION_CODE=
MULTICA_TRUSTED_PROXIES=
RATE_LIMIT_TRUSTED_PROXIES=
ALLOW_SIGNUP=false
DISABLE_WORKSPACE_CREATION=true
ENV

  for line in "$@"; do
    local key="${line%%=*}"
    if grep -q "^${key}=" "$env_file"; then
      sed -i "s#^${key}=.*#${line}#" "$env_file"
    else
      printf '%s\n' "$line" >>"$env_file"
    fi
  done
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

safe_env="$tmp_dir/safe.env"
make_env "$safe_env"
safe_output="$(run_preflight_expect_pass "$safe_env")"
require_contains "$safe_output" "self-host preflight ok"

lan_safe_env="$tmp_dir/lan-safe.env"
make_env "$lan_safe_env" \
  "BIND_HOST=192.168.1.50" \
  "FRONTEND_ORIGIN=http://192.168.1.50:3000"
lan_safe_output="$(run_preflight_expect_pass "$lan_safe_env")"
require_contains "$lan_safe_output" "non-loopback bind requested"

lan_mixed_origin_env="$tmp_dir/lan-mixed-origin.env"
make_env "$lan_mixed_origin_env" \
  "BIND_HOST=192.168.1.50" \
  "FRONTEND_ORIGIN=http://localhost:3000" \
  "CORS_ALLOWED_ORIGINS=http://localhost:3000,http://192.168.1.50:3000"
lan_mixed_origin_output="$(run_preflight_expect_pass "$lan_mixed_origin_env")"
require_contains "$lan_mixed_origin_output" "non-loopback bind requested"

lan_missing_signup_env="$tmp_dir/lan-missing-signup.env"
make_env "$lan_missing_signup_env" \
  "BIND_HOST=192.168.1.50" \
  "FRONTEND_ORIGIN=http://192.168.1.50:3000"
sed -i '/^ALLOW_SIGNUP=/d' "$lan_missing_signup_env"
lan_missing_signup_output="$(run_preflight_expect_fail "$lan_missing_signup_env")"
require_contains "$lan_missing_signup_output" "ALLOW_SIGNUP defaults to true"

all_interface_env="$tmp_dir/all-interface.env"
make_env "$all_interface_env" "BIND_HOST=0.0.0.0"
all_interface_output="$(run_preflight_expect_fail "$all_interface_env")"
require_contains "$all_interface_output" "BIND_HOST=0.0.0.0 is refused"
require_contains "$all_interface_output" "MULTICA_SELFHOST_ALLOW_PUBLIC_BIND=1"

lan_unsafe_env="$tmp_dir/lan-unsafe.env"
make_env "$lan_unsafe_env" \
  "BIND_HOST=192.168.1.50" \
  "POSTGRES_PASSWORD=multica" \
  "JWT_SECRET=change-me-in-production" \
  "APP_ENV=development" \
  "MULTICA_DEV_VERIFICATION_CODE=888888" \
  "RESEND_API_KEY=" \
  "SMTP_HOST=" \
  "SMTP_TLS_INSECURE=true" \
  "MULTICA_TRUSTED_PROXIES=0.0.0.0/0" \
  "RATE_LIMIT_TRUSTED_PROXIES=0.0.0.0/0" \
  "FRONTEND_ORIGIN=" \
  "CORS_ALLOWED_ORIGINS=" \
  "ALLOW_SIGNUP=true" \
  "DISABLE_WORKSPACE_CREATION="
lan_unsafe_output="$(run_preflight_expect_fail "$lan_unsafe_env")"
require_contains "$lan_unsafe_output" "POSTGRES_PASSWORD still uses the example value"
require_contains "$lan_unsafe_output" "JWT_SECRET still uses the example value"
require_contains "$lan_unsafe_output" "APP_ENV should be production"
require_contains "$lan_unsafe_output" "MULTICA_DEV_VERIFICATION_CODE must be empty"
require_contains "$lan_unsafe_output" "no email provider is configured"
require_contains "$lan_unsafe_output" "SMTP_TLS_INSECURE=true"
require_contains "$lan_unsafe_output" "MULTICA_TRUSTED_PROXIES contains a broad CIDR"
require_contains "$lan_unsafe_output" "RATE_LIMIT_TRUSTED_PROXIES contains a broad CIDR"
require_contains "$lan_unsafe_output" "FRONTEND_ORIGIN or CORS_ALLOWED_ORIGINS must be set"
require_contains "$lan_unsafe_output" "ALLOW_SIGNUP defaults to true"
require_contains "$lan_unsafe_output" "DISABLE_WORKSPACE_CREATION is not true"
require_not_contains "$lan_unsafe_output" "fixture-postgres-password"
require_not_contains "$lan_unsafe_output" "fixture-jwt-secret"

override_env="$tmp_dir/override.env"
make_env "$override_env" \
  "BIND_HOST=0.0.0.0" \
  "MULTICA_SELFHOST_ALLOW_PUBLIC_BIND=1" \
  "FRONTEND_ORIGIN=https://multica.example.com"
override_output="$(run_preflight_expect_pass "$override_env")"
require_contains "$override_output" "explicit public bind override enabled"

echo "self-host preflight tests passed"
