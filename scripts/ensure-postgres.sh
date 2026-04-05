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
DATABASE_URL="${DATABASE_URL:-}"

export PGPASSWORD="$POSTGRES_PASSWORD"

# Extract host from DATABASE_URL to decide local vs remote.
# Supports formats: postgres://user:pass@host:port/db and similar.
db_host=""
if [ -n "$DATABASE_URL" ]; then
  # Strip scheme, userinfo, port, path — keep just the host
  db_host="$(echo "$DATABASE_URL" | sed -E 's|^[^:]+://([^@]+@)?([^/:]+).*|\2|')"
fi

is_local() {
  [ -z "$db_host" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

if is_local; then
  # ---------- Local: use Docker ----------
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

  echo "✓ PostgreSQL ready (local Docker). Database: $POSTGRES_DB"
else
  # ---------- Remote: skip Docker, verify connectivity ----------
  db_port="${POSTGRES_PORT:-5432}"
  echo "==> Remote database detected (host: $db_host). Skipping Docker."
  echo "==> Waiting for PostgreSQL at $db_host:$db_port to be ready..."
  until pg_isready -h "$db_host" -p "$db_port" -U "$POSTGRES_USER" -d "$POSTGRES_DB" > /dev/null 2>&1; do
    sleep 1
  done
  echo "✓ PostgreSQL ready (remote: $db_host:$db_port). Database: $POSTGRES_DB"
fi
