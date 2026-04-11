#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "Missing 'psql' on PATH. Install PostgreSQL client tools before running this command."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"

export PGPASSWORD="$POSTGRES_PASSWORD"

if psql "$DATABASE_URL" -Atqc "SELECT 1" > /dev/null 2>&1; then
  echo "✓ PostgreSQL reachable. Application database: $POSTGRES_DB"
  exit 0
fi

echo "PostgreSQL is not reachable using the current env configuration."
echo "Expected database: $POSTGRES_DB"
echo "Start your database service and ensure the database is ready before running this command."
exit 1
