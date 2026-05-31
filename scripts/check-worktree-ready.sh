#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${1:-$ROOT_DIR/.env.worktree}"
CHECK_MODE="${2:-runtime}"
MAIN_ENV_FILE="$ROOT_DIR/.env"

if [[ "$ENV_FILE" != /* ]]; then
  ENV_FILE="$ROOT_DIR/$ENV_FILE"
fi

if [ "$CHECK_MODE" != "runtime" ] && [ "$CHECK_MODE" != "setup" ]; then
  echo "Invalid check mode: $CHECK_MODE"
  echo "Expected 'runtime' or 'setup'."
  exit 1
fi

print_remediation() {
  if [ "$CHECK_MODE" = "setup" ]; then
    echo "The 'make setup-worktree' run did not finish cleanly."
    echo "Fix the issue above, then rerun 'make setup-worktree'."
    return
  fi

  echo "Run 'make setup-worktree' before starting or checking this worktree."
}

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing worktree env file: $ENV_FILE"
  print_remediation
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

POSTGRES_DB="${POSTGRES_DB:-}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"
PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"

if [ -z "$POSTGRES_DB" ]; then
  echo "POSTGRES_DB is missing in $ENV_FILE."
  print_remediation
  exit 1
fi

LOCAL_PSQL_BASE=(psql -v ON_ERROR_STOP=1 -p "$POSTGRES_PORT")
if [ -n "$POSTGRES_HOST" ] && [ "$POSTGRES_HOST" != "localhost" ]; then
  LOCAL_PSQL_BASE+=(-h "$POSTGRES_HOST")
fi

# Use a local admin connection to verify the database exists for this worktree.
run_local_admin_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${LOCAL_PSQL_BASE[@]}" "$@"
}

# Read the main checkout env to prevent a worktree from reusing its database or ports.
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

# Load the repository migration versions so readiness checks can verify full initialization.
load_expected_migrations() {
  local migrations_dir="$ROOT_DIR/server/migrations"
  local version_file

  if [ ! -d "$migrations_dir" ]; then
    echo "Missing migrations directory: $migrations_dir"
    exit 1
  fi

  EXPECTED_MIGRATIONS=()
  while IFS= read -r version_file; do
    EXPECTED_MIGRATIONS+=("$version_file")
  done <<EOF
$(find "$migrations_dir" -maxdepth 1 -type f -name "*.up.sql" -exec basename {} \; \
  | sed 's/\.up\.sql$//' \
  | sort)
EOF
}

# Convert migration versions into a SQL VALUES list for a single missing-migration query.
build_expected_values_sql() {
  local values=()
  local version escaped

  for version in "${EXPECTED_MIGRATIONS[@]}"; do
    escaped="${version//\'/\'\'}"
    values+=("('$escaped')")
  done

  local joined=""
  local value
  for value in "${values[@]}"; do
    if [ -n "$joined" ]; then
      joined+=", "
    fi
    joined+="$value"
  done

  printf '%s' "$joined"
}

if ! run_local_admin_psql -d postgres -Atqc "SELECT 1" >/dev/null 2>&1; then
  echo "PostgreSQL is not reachable using $ENV_FILE."
  print_remediation
  exit 1
fi

main_triplet="$(read_main_env_triplet || true)"
if [ -n "$main_triplet" ]; then
  main_db="$(printf '%s' "$main_triplet" | sed -n '1p')"
  main_port="$(printf '%s' "$main_triplet" | sed -n '2p')"
  main_frontend_port="$(printf '%s' "$main_triplet" | sed -n '3p')"

  if [ -n "$main_db" ] && [ "$POSTGRES_DB" = "$main_db" ]; then
    echo "Worktree env is pointing at the main checkout database '$POSTGRES_DB'."
    print_remediation
    exit 1
  fi

  if [ -n "$main_port" ] && [ "$PORT" = "$main_port" ]; then
    echo "Worktree backend port $PORT matches the main checkout port."
    print_remediation
    exit 1
  fi

  if [ -n "$main_frontend_port" ] && [ "$FRONTEND_PORT" = "$main_frontend_port" ]; then
    echo "Worktree frontend port $FRONTEND_PORT matches the main checkout port."
    print_remediation
    exit 1
  fi
fi

db_exists="$(run_local_admin_psql -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"
if [ "$db_exists" != "1" ]; then
  echo "Worktree database '$POSTGRES_DB' does not exist yet."
  print_remediation
  exit 1
fi

if ! PGPASSWORD="$POSTGRES_PASSWORD" psql "$DATABASE_URL" -Atqc "SELECT 1" >/dev/null 2>&1; then
  echo "Worktree database '$POSTGRES_DB' exists, but the configured app role cannot connect."
  print_remediation
  exit 1
fi

schema_migrations_exists="$(PGPASSWORD="$POSTGRES_PASSWORD" psql "$DATABASE_URL" -Atqc "SELECT to_regclass('public.schema_migrations') IS NOT NULL")"
if [ "$schema_migrations_exists" != "t" ]; then
  echo "Worktree database '$POSTGRES_DB' is missing schema_migrations."
  print_remediation
  exit 1
fi

load_expected_migrations
expected_values_sql="$(build_expected_values_sql)"

if [ -z "$expected_values_sql" ]; then
  echo "No migration files were found under server/migrations."
  exit 1
fi

missing_migrations="$(
  PGPASSWORD="$POSTGRES_PASSWORD" psql "$DATABASE_URL" -Atqc "
    WITH expected(version) AS (
      VALUES $expected_values_sql
    ),
    missing AS (
      SELECT version FROM expected
      EXCEPT
      SELECT version FROM schema_migrations
    )
    SELECT COALESCE(string_agg(version, ', ' ORDER BY version), '') FROM missing;
  "
)"

if [ -n "$missing_migrations" ]; then
  echo "Worktree database '$POSTGRES_DB' is missing applied migrations: $missing_migrations"
  print_remediation
  exit 1
fi

echo "✓ Worktree env ready: $ENV_FILE"
echo "  Database: $POSTGRES_DB"
echo "  Backend:  http://localhost:$PORT"
echo "  Frontend: http://localhost:$FRONTEND_PORT"
