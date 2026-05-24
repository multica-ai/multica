#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAIN_ENV_FILE="$ROOT_DIR/.env"
WORKTREE_ENV_FILE="$ROOT_DIR/.env.worktree"
SELECTED_ENV_FILE="${ENV_FILE:-}"

if [ -z "$SELECTED_ENV_FILE" ]; then
  if [ -f "$MAIN_ENV_FILE" ]; then
    SELECTED_ENV_FILE="$MAIN_ENV_FILE"
  elif [ -f "$WORKTREE_ENV_FILE" ]; then
    SELECTED_ENV_FILE="$WORKTREE_ENV_FILE"
  else
    SELECTED_ENV_FILE="$MAIN_ENV_FILE"
  fi
elif [[ "$SELECTED_ENV_FILE" != /* ]]; then
  SELECTED_ENV_FILE="$ROOT_DIR/$SELECTED_ENV_FILE"
fi

if [ ! -f "$SELECTED_ENV_FILE" ]; then
  echo "Missing env file: $SELECTED_ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

if ! command -v air >/dev/null 2>&1; then
  echo "Missing 'air' on PATH."
  echo "Install it with: go install github.com/air-verse/air@latest"
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$SELECTED_ENV_FILE"
set +a

PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
FRONTEND_ORIGIN="${FRONTEND_ORIGIN:-http://localhost:${FRONTEND_PORT}}"
WORKSPACE_SITE_ORIGIN="${WORKSPACE_SITE_ORIGIN:-${FRONTEND_ORIGIN}}"
MULTICA_APP_URL="${MULTICA_APP_URL:-${FRONTEND_ORIGIN}}"
GOOGLE_REDIRECT_URI="${GOOGLE_REDIRECT_URI:-${FRONTEND_ORIGIN}/auth/callback}"

export ENV_FILE="$SELECTED_ENV_FILE"
export PORT
export FRONTEND_PORT
export FRONTEND_ORIGIN
export WORKSPACE_SITE_ORIGIN
export MULTICA_APP_URL
export GOOGLE_REDIRECT_URI

BACKEND_PID=""
WORKSPACE_PID=""

cleanup() {
  trap - EXIT INT TERM

  if [ -n "$BACKEND_PID" ]; then
    kill "$BACKEND_PID" 2>/dev/null && wait "$BACKEND_PID" 2>/dev/null || true
  fi

  if [ -n "$WORKSPACE_PID" ]; then
    kill "$WORKSPACE_PID" 2>/dev/null && wait "$WORKSPACE_PID" 2>/dev/null || true
  fi
}

trap cleanup EXIT INT TERM

echo "Using env file: $SELECTED_ENV_FILE"
echo "Backend/API: http://localhost:${PORT}"
echo "Workspace: http://localhost:${FRONTEND_PORT}"
echo "Starting backend with air and workspace SPA..."

bash "$ROOT_DIR/scripts/ensure-postgres.sh" "$SELECTED_ENV_FILE"
bash "$ROOT_DIR/scripts/check-dev-ports.sh" "$SELECTED_ENV_FILE" backend workspace

(
  cd "$ROOT_DIR/server"
  air -c .air.toml
) &
BACKEND_PID=$!

(
  cd "$ROOT_DIR"
  pnpm dev:workspace
) &
WORKSPACE_PID=$!

while true; do
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    wait "$BACKEND_PID"
    exit $?
  fi

  if ! kill -0 "$WORKSPACE_PID" 2>/dev/null; then
    wait "$WORKSPACE_PID"
    exit $?
  fi

  sleep 1
done
