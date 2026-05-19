#!/usr/bin/env bash
set -euo pipefail

env_file="${1:-.env}"
jwt_secret="${2:-}"
postgres_password="${3:-}"

random_hex() {
  local bytes="$1"
  openssl rand -hex "$bytes"
}

sed_in_place() {
  local expr="$1"
  local file="$2"
  if [ "$(uname -s)" = "Darwin" ]; then
    sed -i '' -E "$expr" "$file"
  else
    sed -i -E "$expr" "$file"
  fi
}

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\\&|]/\\&/g'
}

set_env_value() {
  local key="$1"
  local value="$2"
  local escaped
  escaped="$(escape_sed_replacement "$value")"
  sed_in_place "s|^${key}=.*|${key}=${escaped}|" "$env_file"
}

replace_database_url_password() {
  local password="$1"
  local escaped
  escaped="$(escape_sed_replacement "$password")"
  sed_in_place "s|^(DATABASE_URL=postgres(ql)?://[^:/@]+:)[^@]*(@.*)$|\\1${escaped}\\3|" "$env_file"
}

if [ ! -f "$env_file" ]; then
  echo "env file not found: $env_file" >&2
  exit 1
fi

if [ -z "$jwt_secret" ]; then
  jwt_secret="$(random_hex 32)"
fi
if [ -z "$postgres_password" ]; then
  postgres_password="$(random_hex 24)"
fi

set_env_value "JWT_SECRET" "$jwt_secret"
set_env_value "POSTGRES_PASSWORD" "$postgres_password"
replace_database_url_password "$postgres_password"
