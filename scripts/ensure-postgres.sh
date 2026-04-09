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

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"

export PGPASSWORD="$POSTGRES_PASSWORD"

if psql "$DATABASE_URL" -Atqc "SELECT 1" > /dev/null 2>&1; then
  echo "✓ PostgreSQL already reachable. Application database: $POSTGRES_DB"
  exit 0
fi

echo "==> Ensuring shared PostgreSQL container is running on localhost:5432..."
docker compose up -d postgres

echo "==> Waiting for PostgreSQL to be ready..."
until docker compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
  sleep 1
done

echo "==> Ensuring database '$POSTGRES_DB' exists..."
db_exists="$(docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

if [ "$db_exists" != "1" ]; then
  docker compose exec -T postgres \
    psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"$POSTGRES_DB\"" \
    > /dev/null
fi

echo "✓ PostgreSQL ready. Application database: $POSTGRES_DB"
