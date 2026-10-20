#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

is_local() {
  [ -z "$DATABASE_URL" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

if is_local; then
  # ---------- Local: use Docker ----------
  # shellcheck source=scripts/dev-compose.sh
  . "$script_dir/dev-compose.sh"

  # Only ever act on the labelled dev project. Compose defaults the project
  # name to the checkout directory, which used to let this script adopt - and
  # stop - another deployment's container with the same name (SHAR-208).
  external_container=""
  own_container="$(dev_compose ps -q postgres 2> /dev/null | head -n 1 || true)"
  if [ -z "$own_container" ]; then
    port_owner="$(docker ps --filter "publish=$db_port" \
      --format '{{.ID}}|{{.Names}}|{{.Label "com.docker.compose.project"}}' 2> /dev/null \
      | head -n 1 || true)"
    if [ -n "$port_owner" ]; then
      IFS='|' read -r owner_id owner_name owner_project <<< "$port_owner"
      if [ "${MULTICA_PG_REUSE_EXTERNAL:-0}" = "1" ]; then
        echo "==> Reusing the PostgreSQL container already published on localhost:$db_port: $owner_name (compose project: ${owner_project:-none})"
        external_container="$owner_id"
      else
        {
          echo "Refusing to start the dev PostgreSQL: localhost:$db_port is already published by container '$owner_name' (compose project: ${owner_project:-none})."
          echo "This checkout does not manage that container, so it is left untouched - no stop, no recreate."
          echo ""
          echo "Options:"
          echo "  - reuse that instance as-is: MULTICA_PG_REUSE_EXTERNAL=1 <same command>"
          echo "  - or stop it / free port $db_port, then re-run."
          echo ""
          echo "Dev compose project for this checkout: $dev_compose_project"
        } >&2
        exit 1
      fi
    fi
  fi

  if [ -n "$external_container" ]; then
    pg_exec() { docker exec -i "$external_container" "$@"; }
  else
    echo "==> Ensuring shared dev PostgreSQL container is running on localhost:$db_port (compose project: $dev_compose_project)..."
    dev_compose up -d postgres
    pg_exec() { dev_compose exec -T postgres "$@"; }
  fi

  echo "==> Waiting for PostgreSQL to be ready..."
  ready=0
  for _ in $(seq 1 120); do
    if pg_exec pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  if [ "$ready" != "1" ]; then
    echo "PostgreSQL did not become ready within 120s (compose project: $dev_compose_project)." >&2
    echo "Inspect it with: docker compose -p $dev_compose_project logs postgres" >&2
    exit 1
  fi

  echo "==> Ensuring database '$POSTGRES_DB' exists..."
  db_exists="$(pg_exec psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

  if [ "$db_exists" != "1" ]; then
    pg_exec psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE \"$POSTGRES_DB\"" \
      > /dev/null
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
