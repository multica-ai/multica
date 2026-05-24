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

LOCAL_PSQL_BASE=(psql -v ON_ERROR_STOP=1 -p "$POSTGRES_PORT")
if [ -n "${POSTGRES_HOST:-}" ] && [ "$POSTGRES_HOST" != "localhost" ]; then
  LOCAL_PSQL_BASE+=(-h "$POSTGRES_HOST")
fi

# 使用本机管理员连接执行角色、数据库和 owner 修复。
run_local_admin_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${LOCAL_PSQL_BASE[@]}" "$@"
}

if ! run_local_admin_psql -d postgres -Atqc "SELECT 1" > /dev/null 2>&1; then
  echo "PostgreSQL is not reachable using the current env configuration."
  echo "Expected database: $POSTGRES_DB"
  echo "Start PostgreSQL 18 locally, for example:"
  echo "  brew services start postgresql@18"
  exit 1
fi

echo "==> Ensuring application role '$POSTGRES_USER' exists locally..."
run_local_admin_psql -d postgres <<SQL
DO \$\$
DECLARE
  target_role text := '$POSTGRES_USER';
  target_password text := '$POSTGRES_PASSWORD';
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = target_role) THEN
    EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', target_role, target_password);
  ELSE
    EXECUTE format('ALTER ROLE %I LOGIN PASSWORD %L', target_role, target_password);
  END IF;
END
\$\$;
SQL

echo "==> Ensuring database '$POSTGRES_DB' exists..."
db_exists="$(run_local_admin_psql -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

if [ "$db_exists" != "1" ]; then
  run_local_admin_psql -d postgres -c "CREATE DATABASE \"$POSTGRES_DB\" OWNER \"$POSTGRES_USER\"" > /dev/null
fi

echo "==> Aligning database ownership and grants for '$POSTGRES_USER'..."
run_local_admin_psql -d postgres -c "ALTER DATABASE \"$POSTGRES_DB\" OWNER TO \"$POSTGRES_USER\"" > /dev/null
run_local_admin_psql -d "$POSTGRES_DB" <<SQL
DO \$\$
DECLARE
  target_role text := '$POSTGRES_USER';
  object_row record;
BEGIN
  EXECUTE format('ALTER SCHEMA public OWNER TO %I', target_role);

  FOR object_row IN
    SELECT c.relkind, c.relname
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relkind IN ('r', 'p', 'S', 'v', 'm')
  LOOP
    IF object_row.relkind IN ('r', 'p') THEN
      EXECUTE format('ALTER TABLE public.%I OWNER TO %I', object_row.relname, target_role);
    ELSIF object_row.relkind = 'S' THEN
      EXECUTE format('ALTER SEQUENCE public.%I OWNER TO %I', object_row.relname, target_role);
    ELSIF object_row.relkind = 'v' THEN
      EXECUTE format('ALTER VIEW public.%I OWNER TO %I', object_row.relname, target_role);
    ELSIF object_row.relkind = 'm' THEN
      EXECUTE format('ALTER MATERIALIZED VIEW public.%I OWNER TO %I', object_row.relname, target_role);
    END IF;
  END LOOP;
END
\$\$;
SQL

run_local_admin_psql -d "$POSTGRES_DB" -c "GRANT ALL PRIVILEGES ON SCHEMA public TO \"$POSTGRES_USER\"" > /dev/null
run_local_admin_psql -d "$POSTGRES_DB" -c "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO \"$POSTGRES_USER\"" > /dev/null
run_local_admin_psql -d "$POSTGRES_DB" -c "GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO \"$POSTGRES_USER\"" > /dev/null
run_local_admin_psql -d "$POSTGRES_DB" -c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL PRIVILEGES ON TABLES TO \"$POSTGRES_USER\"" > /dev/null
run_local_admin_psql -d "$POSTGRES_DB" -c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO \"$POSTGRES_USER\"" > /dev/null

if ! PGPASSWORD="$POSTGRES_PASSWORD" psql "$DATABASE_URL" -Atqc "SELECT 1" > /dev/null 2>&1; then
  echo "Application database is reachable locally, but the configured app role still cannot connect."
  echo "DATABASE_URL=$DATABASE_URL"
  exit 1
fi

echo "✓ PostgreSQL ready. Application database: $POSTGRES_DB"
