#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
MARKETING_PORT="${MARKETING_PORT:-3001}"

check_port_available() {
  local port="$1"
  local name="$2"

  if lsof -tiTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "$name port $port is already in use."
    echo "Stop the existing process, change the port in your env file, or use a worktree env."
    exit 1
  fi
}

check_port_available "$PORT" "Backend"
check_port_available "$FRONTEND_PORT" "Workspace"
check_port_available "$MARKETING_PORT" "Marketing"
