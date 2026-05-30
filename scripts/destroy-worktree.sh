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
  echo "Nothing was destroyed."
  exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "Missing 'psql' on PATH. Install PostgreSQL client tools before destroying a worktree."
  exit 1
fi

if ! command -v dropdb >/dev/null 2>&1; then
  echo "Missing 'dropdb' on PATH. Install PostgreSQL client tools before destroying a worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [ "${MULTICA_ENV_KIND:-}" != "worktree" ]; then
  echo "Refusing to destroy '$ENV_FILE' because it is not marked as a worktree env."
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

if [ -z "$POSTGRES_DB" ]; then
  echo "POSTGRES_DB is missing in $ENV_FILE."
  exit 1
fi

if [ "$POSTGRES_DB" = "multica" ]; then
  echo "Refusing to destroy the default database '$POSTGRES_DB'."
  exit 1
fi

read_main_env_triplet() {
  if [ ! -f "$MAIN_ENV_FILE" ]; then
    return 0
  fi

  (
    set -a
    # shellcheck disable=SC1090
    . "$MAIN_ENV_FILE"
    set +a
    printf '%s\n%s\n%s\n' "${POSTGRES_DB:-}" "${PORT:-}" "${FRONTEND_PORT:-}"
  )
}

main_db=""
main_port=""
main_frontend_port=""
main_triplet="$(read_main_env_triplet || true)"
if [ -n "$main_triplet" ]; then
  main_db="$(printf '%s' "$main_triplet" | sed -n '1p')"
  main_port="$(printf '%s' "$main_triplet" | sed -n '2p')"
  main_frontend_port="$(printf '%s' "$main_triplet" | sed -n '3p')"
fi

if [ -n "$main_db" ] && [ "$POSTGRES_DB" = "$main_db" ]; then
  echo "Refusing to destroy the main checkout database '$POSTGRES_DB'."
  exit 1
fi

if [ -n "$main_port" ] && [ "$PORT" = "$main_port" ]; then
  echo "Refusing to destroy env '$ENV_FILE' because its backend port matches the main checkout."
  exit 1
fi

if [ -n "$main_frontend_port" ] && [ "$FRONTEND_PORT" = "$main_frontend_port" ]; then
  echo "Refusing to destroy env '$ENV_FILE' because its frontend port matches the main checkout."
  exit 1
fi

LOCAL_PSQL_BASE=(psql -v ON_ERROR_STOP=1 -p "$POSTGRES_PORT")
DROPDB_BASE=(dropdb -p "$POSTGRES_PORT")
if [ -n "$POSTGRES_HOST" ] && [ "$POSTGRES_HOST" != "localhost" ]; then
  LOCAL_PSQL_BASE+=(-h "$POSTGRES_HOST")
  DROPDB_BASE+=(-h "$POSTGRES_HOST")
fi

run_local_admin_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${LOCAL_PSQL_BASE[@]}" "$@"
}

drop_database() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${DROPDB_BASE[@]}" --force --if-exists "$POSTGRES_DB"
}

stop_port() {
  local port="$1"
  local name="$2"
  local pids
  local pid

  pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [ -z "$pids" ]; then
    return
  fi

  echo "Stopping $name processes on port $port..."
  for pid in $pids; do
    kill "$pid" 2>/dev/null || true
  done
}

if [ "${FORCE:-0}" != "1" ]; then
  echo "About to destroy worktree resources:"
  echo "  Env file: $ENV_FILE"
  echo "  Worktree: $WORKTREE_NAME"
  echo "  Root:     $WORKTREE_ROOT"
  echo "  Database: $POSTGRES_DB"
  echo "  Backend:  $PORT"
  echo "  Frontend: $FRONTEND_PORT"
  echo ""
  echo "Re-run with FORCE=1 to stop worktree processes, drop the database, and delete the env file."
  exit 1
fi

if ! run_local_admin_psql -d postgres -Atqc "SELECT 1" >/dev/null 2>&1; then
  echo "PostgreSQL is not reachable using $ENV_FILE."
  echo "Nothing was destroyed."
  exit 1
fi

stop_port "$PORT" "backend"
stop_port "$FRONTEND_PORT" "frontend"

db_exists="$(run_local_admin_psql -d postgres -Atqc "SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB') THEN '1' ELSE '0' END")"
if [ "$db_exists" = "1" ]; then
  active_connections="$(run_local_admin_psql -d postgres -Atqc "SELECT count(*) FROM pg_stat_activity WHERE datname = '$POSTGRES_DB' AND pid <> pg_backend_pid()")"
  if [ "$active_connections" != "0" ]; then
    run_local_admin_psql -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$POSTGRES_DB' AND pid <> pg_backend_pid()" >/dev/null
  fi
  drop_database
  echo "Dropped database '$POSTGRES_DB'."
else
  echo "Database '$POSTGRES_DB' does not exist. Skipping drop."
fi

rm -f "$ENV_FILE"
echo "Removed env file '$ENV_FILE'."
echo "Worktree resources destroyed."
