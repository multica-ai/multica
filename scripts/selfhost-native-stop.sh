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

BACKEND_PORT="${PORT:-13080}"
FRONTEND_PORT_VALUE="${FRONTEND_PORT:-13030}"

pids_by_port() {
  local port="$1"

  if command -v lsof >/dev/null 2>&1; then
    lsof -ti tcp:"$port" 2>/dev/null || true
    return
  fi

  if command -v ss >/dev/null 2>&1; then
    ss -lntp 2>/dev/null | awk -v port=":${port}" '
      index($4, port) {
        while (match($0, /pid=[0-9]+/)) {
          print substr($0, RSTART + 4, RLENGTH - 4)
          $0 = substr($0, RSTART + RLENGTH)
        }
      }
    ' | sort -u
    return
  fi

  true
}

kill_by_port() {
  local port="$1"
  local pids
  pids="$(pids_by_port "$port")"
  if [ -n "$pids" ]; then
    echo "Stopping processes on port $port: $pids"
    kill $pids
  else
    echo "No process is listening on port $port"
  fi
}

kill_by_port "$BACKEND_PORT"
kill_by_port "$FRONTEND_PORT_VALUE"
