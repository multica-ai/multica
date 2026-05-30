#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${1:-$ROOT_DIR/.env.worktree}"
MAIN_ENV_FILE="$ROOT_DIR/.env"

if [[ "$ENV_FILE" != /* ]]; then
  ENV_FILE="$ROOT_DIR/$ENV_FILE"
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing worktree env file: $ENV_FILE"
  echo "Run 'make setup-worktree' before listing worktree resources."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [ "${MULTICA_ENV_KIND:-}" != "worktree" ]; then
  echo "Refusing to inspect '$ENV_FILE' because it is not marked as a worktree env."
  exit 1
fi

POSTGRES_DB="${POSTGRES_DB:-}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
WORKTREE_ROOT="${WORKTREE_ROOT:-$(dirname "$ENV_FILE")}"
WORKTREE_NAME="${WORKTREE_NAME:-$(basename "$WORKTREE_ROOT")}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"

if [ -z "$POSTGRES_DB" ]; then
  echo "POSTGRES_DB is missing in $ENV_FILE."
  exit 1
fi

LOCAL_PSQL_BASE=(psql -v ON_ERROR_STOP=1 -p "$POSTGRES_PORT")
if [ -n "$POSTGRES_HOST" ] && [ "$POSTGRES_HOST" != "localhost" ]; then
  LOCAL_PSQL_BASE+=(-h "$POSTGRES_HOST")
fi

run_local_admin_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${LOCAL_PSQL_BASE[@]}" "$@"
}

db_exists="unknown"
if command -v psql >/dev/null 2>&1 && run_local_admin_psql -d postgres -Atqc "SELECT 1" >/dev/null 2>&1; then
  db_exists="$(run_local_admin_psql -d postgres -Atqc "SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB') THEN 'yes' ELSE 'no' END")"
fi

backend_pids="$(lsof -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | paste -sd, - || true)"
frontend_pids="$(lsof -tiTCP:"$FRONTEND_PORT" -sTCP:LISTEN 2>/dev/null | paste -sd, - || true)"
if [ -z "$backend_pids" ]; then
  backend_pids="none"
fi
if [ -z "$frontend_pids" ]; then
  frontend_pids="none"
fi

branch_name="unknown"
if [ -d "$WORKTREE_ROOT/.git" ] || [ -f "$WORKTREE_ROOT/.git" ]; then
  branch_name="$(git -C "$WORKTREE_ROOT" branch --show-current 2>/dev/null || printf 'unknown')"
fi

printf 'Worktree env: %s\n' "$ENV_FILE"
printf 'Worktree root: %s\n' "$WORKTREE_ROOT"
printf 'Worktree name: %s\n' "$WORKTREE_NAME"
printf 'Branch: %s\n' "$branch_name"
printf 'Database: %s\n' "$POSTGRES_DB"
printf 'Database exists: %s\n' "$db_exists"
printf 'Database URL: %s\n' "$DATABASE_URL"
printf 'Backend port: %s\n' "$PORT"
printf 'Backend PIDs: %s\n' "$backend_pids"
printf 'Frontend port: %s\n' "$FRONTEND_PORT"
printf 'Frontend PIDs: %s\n' "$frontend_pids"
