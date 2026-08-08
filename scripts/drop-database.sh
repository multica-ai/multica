#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_input="${1:-.env}"

if [[ "$env_input" = /* ]]; then
  env_file="$env_input"
else
  env_file="$PWD/$env_input"
fi

if [ ! -f "$env_file" ]; then
  echo "Missing env file: $env_input" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

POSTGRES_DB="${POSTGRES_DB:-}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
DATABASE_URL="${DATABASE_URL:-}"

if [ -z "$POSTGRES_DB" ]; then
  echo "Refusing to drop a database: POSTGRES_DB is empty in $env_input." >&2
  exit 1
fi

case "$DATABASE_URL" in
  "" | *@localhost:* | *@localhost/* | *@127.0.0.1:* | *@127.0.0.1/* | *@\[::1\]:* | *@\[::1\]/*) ;;
  *)
    echo "Refusing to drop database '$POSTGRES_DB': DATABASE_URL points at a remote host." >&2
    exit 1
    ;;
esac

case "$POSTGRES_DB" in
  postgres | template0 | template1)
    echo "Refusing to drop protected PostgreSQL database '$POSTGRES_DB'." >&2
    exit 1
    ;;
  multica)
    if [ "${ALLOW_MAIN_DB_DROP:-0}" != "1" ]; then
      echo "Refusing to drop the default main database 'multica'." >&2
      echo "Re-run with ALLOW_MAIN_DB_DROP=1 only if deleting the main checkout database is intentional." >&2
      exit 1
    fi
    ;;
esac

echo "Database: $POSTGRES_DB"
echo "Source:   $env_input"
echo ""
printf "Drop this database permanently? [y/N] "
if ! IFS= read -r answer; then
  answer=""
fi

case "$answer" in
  y | Y) ;;
  *)
    echo "Cancelled."
    # Callers distinguish an intentional cancellation from an execution error.
    exit 2
    ;;
esac

cd "$root_dir"
docker compose exec -T postgres \
  dropdb \
  --username "$POSTGRES_USER" \
  --maintenance-db postgres \
  --if-exists \
  --force \
  -- \
  "$POSTGRES_DB"

echo "Dropped database '$POSTGRES_DB'."
