#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"
KIND="${2:-frontend}"

env_value() {
  local key=$1
  local default=${2:-}
  local line value

  line="$(grep -E "^[[:space:]]*${key}=" "$ENV_FILE" 2>/dev/null | tail -n 1 || true)"
  if [ -z "$line" ]; then
    printf '%s' "$default"
    return
  fi

  value="${line#*=}"
  value="${value%%#*}"
  value="${value%$'\r'}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value="${value%\"}"
  value="${value#\"}"
  value="${value%\'}"
  value="${value#\'}"

  if [ -z "$value" ]; then
    printf '%s' "$default"
  else
    printf '%s' "$value"
  fi
}

url_host() {
  local host=$1

  case "$host" in
    ""|"127.0.0.1"|"0.0.0.0"|"::"|"[::]"|"*")
      printf 'localhost'
      ;;
    "localhost"|"[::1]")
      printf '%s' "$host"
      ;;
    "::1")
      printf '[::1]'
      ;;
    *:*)
      printf '[%s]' "$host"
      ;;
    *)
      printf '%s' "$host"
      ;;
  esac
}

bind_host="$(env_value BIND_HOST "127.0.0.1")"
frontend_port="$(env_value FRONTEND_PORT "3000")"
backend_port="$(env_value BACKEND_PORT "$(env_value API_PORT "$(env_value SERVER_PORT "$(env_value PORT "8080")")")")"
host="$(url_host "$bind_host")"

case "$KIND" in
  frontend)
    printf 'http://%s:%s\n' "$host" "$frontend_port"
    ;;
  backend)
    printf 'http://%s:%s\n' "$host" "$backend_port"
    ;;
  health)
    printf 'http://%s:%s/health\n' "$host" "$backend_port"
    ;;
  *)
    echo "usage: $0 [env-file] frontend|backend|health" >&2
    exit 2
    ;;
esac
