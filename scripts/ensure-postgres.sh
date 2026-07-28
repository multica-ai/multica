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

db_host=""
db_port="${POSTGRES_PORT:-5432}"
db_name="$POSTGRES_DB"

parse_database_url() {
  local rest authority hostport path port_part

  rest="${DATABASE_URL#*://}"
  rest="${rest%%\?*}"
  authority="${rest%%/*}"
  path="${rest#*/}"

  if [ "$authority" = "$rest" ]; then
    path=""
  fi

  hostport="${authority##*@}"

  if [[ "$hostport" == \[* ]]; then
    db_host="${hostport#\[}"
    db_host="${db_host%%]*}"
    port_part="${hostport#*\]}"
    if [[ "$port_part" == :* ]] && [ -n "${port_part#:}" ]; then
      db_port="${port_part#:}"
    fi
  else
    db_host="${hostport%%:*}"
    if [[ "$hostport" == *:* ]] && [ -n "${hostport##*:}" ]; then
      db_port="${hostport##*:}"
    fi
  fi

  if [ -n "$path" ]; then
    db_name="${path%%/*}"
  fi
}

if [ -n "$DATABASE_URL" ]; then
  parse_database_url
fi

# DATABASE_URL is authoritative for worktrees, whose database name differs
# from the shared POSTGRES_DB default.
POSTGRES_DB="$db_name"

is_local() {
  [ -z "$DATABASE_URL" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

if docker compose version > /dev/null 2>&1; then
  DC="docker compose"
else
  DC="docker-compose"
fi

if is_local; then
  # ---------- Local: use Docker ----------
  echo "==> Ensuring shared PostgreSQL container is running on localhost:5432..."
  $DC up -d postgres

  echo "==> Waiting for PostgreSQL to be ready..."
  if command -v pg_isready > /dev/null 2>&1 && command -v psql > /dev/null 2>&1; then
    connect_host="${db_host:-127.0.0.1}"
    until pg_isready -h "$connect_host" -p "$db_port" -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
      sleep 1
    done

    echo "==> Ensuring database '$POSTGRES_DB' exists..."
    db_exists="$(psql -h "$connect_host" -p "$db_port" -U "$POSTGRES_USER" -d postgres \
      -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

    if [ "$db_exists" != "1" ]; then
      psql -h "$connect_host" -p "$db_port" -U "$POSTGRES_USER" -d postgres \
        -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$POSTGRES_DB\"" > /dev/null
    fi
  else
    until $DC exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
      sleep 1
    done

    echo "==> Ensuring database '$POSTGRES_DB' exists..."
    db_exists="$($DC exec -T postgres \
      psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

    if [ "$db_exists" != "1" ]; then
      $DC exec -T postgres \
        psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
        -c "CREATE DATABASE \"$POSTGRES_DB\"" \
        > /dev/null
    fi
  fi

  echo "✓ PostgreSQL ready (local Docker). Database: $POSTGRES_DB"
else
  # ---------- Remote: skip Docker, verify connectivity ----------
  echo "==> Remote database detected (host: $db_host). Skipping Docker."
  if command -v pg_isready > /dev/null 2>&1; then
    echo "==> Waiting for PostgreSQL at $db_host:$db_port to be ready..."
    until pg_isready -d "$DATABASE_URL" > /dev/null 2>&1; do
      sleep 1
    done
    echo "✓ PostgreSQL ready (remote: $db_host:$db_port). Database: $db_name"
  else
    echo "==> pg_isready not found. Skipping remote connectivity preflight."
    echo "✓ PostgreSQL configured (remote: $db_host:$db_port). Database: $db_name"
  fi
fi
