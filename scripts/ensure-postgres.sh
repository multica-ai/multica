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

export PGPASSWORD="$POSTGRES_PASSWORD"

echo "==> Ensuring shared PostgreSQL container is running on localhost:${POSTGRES_PORT}..."
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
