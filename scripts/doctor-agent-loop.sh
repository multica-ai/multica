#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
MAIN_ENV_FILE="$ROOT_DIR/.env"

if [[ "$ENV_FILE" != /* ]]; then
  ENV_FILE="$ROOT_DIR/$ENV_FILE"
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "Missing 'psql' on PATH. Install PostgreSQL client tools before running this command."
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "Missing 'curl' on PATH."
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "Missing 'python3' on PATH."
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
BACKEND_URL="${BACKEND_URL:-http://localhost:${PORT}}"

RESULT="FAIL"
FAILED_AT=""
LATEST_REPO_MIGRATION=""
LATEST_APPLIED_MIGRATION=""
PENDING_MIGRATIONS=""
TASK_TOKEN_TABLE="unknown"
ONLINE_RUNTIME_COUNT="unchecked"
AGENT_LOOP="not_run"
ENV_KIND="main"

DOCTOR_PAT_ID=""
DOCTOR_PAT_TOKEN=""
DOCTOR_WORKSPACE_ID=""
DOCTOR_RUNTIME_ID=""
DOCTOR_RUNTIME_PROVIDER=""
DOCTOR_RUNTIME_NAME=""
DOCTOR_AGENT_ID=""
DOCTOR_USER_ID=""
DOCTOR_ISSUE_ID=""
DOCTOR_TASK_ID=""
DOCTOR_TASK_TOKEN=""
DOCTOR_CLAIM_RESPONSE=""

LOCAL_PSQL_BASE=(psql -v ON_ERROR_STOP=1 -p "$POSTGRES_PORT")
if [ -n "$POSTGRES_HOST" ] && [ "$POSTGRES_HOST" != "localhost" ]; then
  LOCAL_PSQL_BASE+=(-h "$POSTGRES_HOST")
fi

run_local_admin_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" "${LOCAL_PSQL_BASE[@]}" "$@"
}

run_app_psql() {
  PGPASSWORD="$POSTGRES_PASSWORD" psql "$DATABASE_URL" -v ON_ERROR_STOP=1 "$@"
}

print_step() {
  echo ""
  echo "==> $1"
}

set_failed_at() {
  if [ -z "$FAILED_AT" ]; then
    FAILED_AT="$1"
  fi
}

summarize_and_exit() {
  local exit_code="${1:-1}"
  echo ""
  echo "RESULT=$RESULT"
  echo "FAILED_AT=$FAILED_AT"
  echo "ENV_FILE=$ENV_FILE"
  echo "ENV_KIND=$ENV_KIND"
  echo "LATEST_REPO_MIGRATION=$LATEST_REPO_MIGRATION"
  echo "LATEST_APPLIED_MIGRATION=$LATEST_APPLIED_MIGRATION"
  echo "PENDING_MIGRATIONS=$PENDING_MIGRATIONS"
  echo "TASK_TOKEN_TABLE=$TASK_TOKEN_TABLE"
  echo "ONLINE_RUNTIME_COUNT=$ONLINE_RUNTIME_COUNT"
  echo "ONLINE_RUNTIME_ID=$DOCTOR_RUNTIME_ID"
  echo "ONLINE_RUNTIME_PROVIDER=$DOCTOR_RUNTIME_PROVIDER"
  echo "ONLINE_RUNTIME_NAME=$DOCTOR_RUNTIME_NAME"
  echo "AGENT_LOOP=$AGENT_LOOP"
  exit "$exit_code"
}

cleanup() {
  set +e

  # Delete the temporary PAT to avoid leaving extra auth material behind.
  if [ -n "$DOCTOR_PAT_ID" ]; then
    run_app_psql -Atqc "DELETE FROM personal_access_token WHERE id = '$DOCTOR_PAT_ID'" >/dev/null 2>&1 || true
  fi

  # Delete the temporary issue. Cascades clear the temporary task and task tokens.
  if [ -n "$DOCTOR_ISSUE_ID" ]; then
    run_app_psql -Atqc "DELETE FROM issue WHERE id = '$DOCTOR_ISSUE_ID'" >/dev/null 2>&1 || true
  elif [ -n "$DOCTOR_TASK_ID" ]; then
    run_app_psql -Atqc "DELETE FROM agent_task_queue WHERE id = '$DOCTOR_TASK_ID'" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT

load_env_kind() {
  if grep -q '^MULTICA_ENV_KIND=worktree$' "$ENV_FILE" 2>/dev/null; then
    ENV_KIND="worktree"
    return
  fi

  if [ "$ENV_FILE" != "$MAIN_ENV_FILE" ] && [[ "$ENV_FILE" == *.env.worktree ]]; then
    ENV_KIND="worktree"
  fi
}

load_expected_migrations() {
  local migrations_dir="$ROOT_DIR/server/migrations"
  local version_file

  EXPECTED_MIGRATIONS=()
  while IFS= read -r version_file; do
    EXPECTED_MIGRATIONS+=("$version_file")
  done <<EOF
$(find "$migrations_dir" -maxdepth 1 -type f -name "*.up.sql" -exec basename {} \; \
  | sed 's/\.up\.sql$//' \
  | sort)
EOF

  if [ "${#EXPECTED_MIGRATIONS[@]}" -eq 0 ]; then
    echo "No migration files were found under $migrations_dir."
    set_failed_at "schema"
    summarize_and_exit 1
  fi
}

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

print_context() {
  print_step "[1/5] Environment"
  echo "ENV_FILE: $ENV_FILE"
  echo "ENV_KIND: $ENV_KIND"
  echo "POSTGRES_DB: $POSTGRES_DB"
  echo "POSTGRES_HOST: $POSTGRES_HOST"
  echo "POSTGRES_PORT: $POSTGRES_PORT"
  echo "BACKEND_URL: $BACKEND_URL"
  echo "FRONTEND_PORT: $FRONTEND_PORT"
}

check_postgres() {
  if [ -z "$POSTGRES_DB" ]; then
    echo "POSTGRES_DB is missing in $ENV_FILE."
    set_failed_at "environment"
    summarize_and_exit 1
  fi

  if ! run_local_admin_psql -d postgres -Atqc "SELECT 1" >/dev/null 2>&1; then
    echo "PostgreSQL is not reachable using $ENV_FILE."
    set_failed_at "postgres"
    summarize_and_exit 1
  fi

  if ! run_app_psql -Atqc "SELECT 1" >/dev/null 2>&1; then
    echo "Application database is not reachable using DATABASE_URL."
    set_failed_at "postgres"
    summarize_and_exit 1
  fi
}

check_schema_state() {
  print_step "[2/5] Database schema"

  local schema_migrations_exists
  schema_migrations_exists="$(run_app_psql -Atqc "SELECT to_regclass('public.schema_migrations') IS NOT NULL")"
  if [ "$schema_migrations_exists" != "t" ]; then
    echo "schema_migrations table is missing."
    set_failed_at "schema"
    summarize_and_exit 1
  fi

  load_expected_migrations
  LATEST_REPO_MIGRATION="${EXPECTED_MIGRATIONS[${#EXPECTED_MIGRATIONS[@]}-1]}"

  LATEST_APPLIED_MIGRATION="$(run_app_psql -Atqc "SELECT COALESCE(MAX(version), '') FROM schema_migrations")"

  local expected_values_sql
  expected_values_sql="$(build_expected_values_sql)"
  PENDING_MIGRATIONS="$(
    run_app_psql -Atqc "
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

  TASK_TOKEN_TABLE="$(run_app_psql -Atqc "SELECT CASE WHEN to_regclass('public.task_token') IS NULL THEN 'missing' ELSE 'present' END")"

  echo "LATEST_REPO_MIGRATION: $LATEST_REPO_MIGRATION"
  echo "LATEST_APPLIED_MIGRATION: $LATEST_APPLIED_MIGRATION"
  echo "PENDING_MIGRATIONS: ${PENDING_MIGRATIONS:-<none>}"
  echo "TASK_TOKEN_TABLE: $TASK_TOKEN_TABLE"

  if [ -n "$PENDING_MIGRATIONS" ]; then
    echo "Database schema is behind the repository migrations."
    set_failed_at "schema"
    summarize_and_exit 1
  fi

  if [ "$TASK_TOKEN_TABLE" != "present" ]; then
    echo "task_token table is missing."
    set_failed_at "schema"
    summarize_and_exit 1
  fi
}

check_backend_health() {
  print_step "[3/5] Backend health"

  local health_response
  if ! health_response="$(curl -sf "$BACKEND_URL/health")"; then
    echo "Backend health check failed at $BACKEND_URL/health."
    set_failed_at "backend-health"
    summarize_and_exit 1
  fi

  echo "Backend health: $health_response"
}

find_online_runtime() {
  print_step "[4/5] Online runtimes"

  ONLINE_RUNTIME_COUNT="$(run_app_psql -Atqc "SELECT COUNT(*) FROM agent_runtime WHERE status = 'online'")"
  echo "ONLINE_RUNTIME_COUNT: $ONLINE_RUNTIME_COUNT"

  if [ "$ONLINE_RUNTIME_COUNT" = "0" ]; then
    echo "No online runtime found. Start a daemon and let it register first."
    set_failed_at "runtime"
    summarize_and_exit 1
  fi

  local runtime_row
  runtime_row="$(
    run_app_psql -F $'\t' -Atqc "
      SELECT
        ar.id,
        ar.workspace_id,
        ar.provider,
        ar.name,
        a.id,
        m.user_id
      FROM agent_runtime ar
      JOIN agent a ON a.runtime_id = ar.id
      JOIN member m ON m.workspace_id = ar.workspace_id
      WHERE ar.status = 'online'
        AND a.archived_at IS NULL
      ORDER BY ar.last_seen_at DESC NULLS LAST, ar.updated_at DESC, m.created_at ASC
      LIMIT 1;
    "
  )"

  if [ -z "$runtime_row" ]; then
    echo "No online runtime has an active agent plus workspace member."
    set_failed_at "runtime"
    summarize_and_exit 1
  fi

  IFS=$'\t' read -r DOCTOR_RUNTIME_ID DOCTOR_WORKSPACE_ID DOCTOR_RUNTIME_PROVIDER DOCTOR_RUNTIME_NAME DOCTOR_AGENT_ID DOCTOR_USER_ID <<<"$runtime_row"

  echo "Selected runtime: $DOCTOR_RUNTIME_NAME ($DOCTOR_RUNTIME_PROVIDER)"
  echo "Runtime ID: $DOCTOR_RUNTIME_ID"
  echo "Workspace ID: $DOCTOR_WORKSPACE_ID"
  echo "Agent ID: $DOCTOR_AGENT_ID"
  echo "User ID: $DOCTOR_USER_ID"
}

create_doctor_pat() {
  # A temporary PAT lets the doctor script call both daemon and user APIs
  # without relying on an interactive login flow.
  DOCTOR_PAT_TOKEN="mul_$(python3 - <<'PY'
import secrets
print(secrets.token_hex(20))
PY
)"

  local token_hash token_prefix
  token_hash="$(printf '%s' "$DOCTOR_PAT_TOKEN" | shasum -a 256 | awk '{print $1}')"
  token_prefix="${DOCTOR_PAT_TOKEN:0:12}"

  DOCTOR_PAT_ID="$(
    run_app_psql -Atqc "
      INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at)
      VALUES (
        '$DOCTOR_USER_ID',
        'doctor-agent-loop',
        '$token_hash',
        '$token_prefix',
        now() + interval '1 day'
      )
      RETURNING id;
    "
  )"
}

api_request() {
  local method="$1"
  local url="$2"
  local token="$3"
  local workspace_id="${4:-}"
  local body="${5:-}"
  local response_file status
  local -a curl_args

  response_file="$(mktemp)"
  curl_args=(
    -sS
    -X "$method"
    -H "Authorization: Bearer $token"
    -o "$response_file"
    -w "%{http_code}"
    "$url"
  )

  if [ -n "$workspace_id" ]; then
    curl_args+=(-H "X-Workspace-ID: $workspace_id")
  fi

  if [ -n "$body" ]; then
    curl_args+=(-H "Content-Type: application/json" -d "$body")
  fi

  status="$(curl "${curl_args[@]}")"
  API_STATUS="$status"
  API_RESPONSE="$(cat "$response_file")"
  rm -f "$response_file"
}

create_doctor_issue() {
  local payload
  payload="$(python3 - <<'PY'
import json, time
print(json.dumps({
    "title": f"doctor-agent-loop {int(time.time())}",
    "status": "todo",
    "priority": "none"
}))
PY
)"

  api_request "POST" "$BACKEND_URL/api/issues?workspace_id=$DOCTOR_WORKSPACE_ID" "$DOCTOR_PAT_TOKEN" "$DOCTOR_WORKSPACE_ID" "$payload"
  if [ "$API_STATUS" != "201" ]; then
    echo "Failed to create temporary issue: HTTP $API_STATUS"
    echo "$API_RESPONSE"
    set_failed_at "seed"
    summarize_and_exit 1
  fi

  DOCTOR_ISSUE_ID="$(printf '%s' "$API_RESPONSE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
}

create_doctor_task() {
  DOCTOR_TASK_ID="$(
    run_app_psql -Atqc "
      INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
      VALUES (
        '$DOCTOR_AGENT_ID',
        '$DOCTOR_RUNTIME_ID',
        '$DOCTOR_ISSUE_ID',
        'queued',
        0
      )
      RETURNING id;
    "
  )"

  if [ -z "$DOCTOR_TASK_ID" ]; then
    echo "Failed to create temporary task."
    set_failed_at "seed"
    summarize_and_exit 1
  fi
}

claim_doctor_task() {
  api_request "POST" "$BACKEND_URL/api/daemon/runtimes/$DOCTOR_RUNTIME_ID/tasks/claim" "$DOCTOR_PAT_TOKEN"
  if [ "$API_STATUS" != "200" ]; then
    echo "Task claim failed: HTTP $API_STATUS"
    echo "$API_RESPONSE"
    set_failed_at "claim"
    summarize_and_exit 1
  fi

  DOCTOR_CLAIM_RESPONSE="$API_RESPONSE"

  local claimed_task_id
  claimed_task_id="$(printf '%s' "$DOCTOR_CLAIM_RESPONSE" | python3 -c 'import json,sys; data=json.load(sys.stdin); print((data.get("task") or {}).get("id",""))')"
  DOCTOR_TASK_TOKEN="$(printf '%s' "$DOCTOR_CLAIM_RESPONSE" | python3 -c 'import json,sys; data=json.load(sys.stdin); print((data.get("task") or {}).get("auth_token",""))')"

  if [ -z "$claimed_task_id" ]; then
    echo "Claim returned no task payload."
    echo "$DOCTOR_CLAIM_RESPONSE"
    set_failed_at "claim"
    summarize_and_exit 1
  fi

  if [ "$claimed_task_id" != "$DOCTOR_TASK_ID" ]; then
    echo "Claim returned unexpected task. Expected $DOCTOR_TASK_ID, got $claimed_task_id."
    echo "$DOCTOR_CLAIM_RESPONSE"
    set_failed_at "claim"
    summarize_and_exit 1
  fi

  if [ -z "$DOCTOR_TASK_TOKEN" ]; then
    echo "Claim succeeded but auth_token is missing."
    echo "$DOCTOR_CLAIM_RESPONSE"
    set_failed_at "token-issued"
    summarize_and_exit 1
  fi
}

check_task_token_auth() {
  api_request "GET" "$BACKEND_URL/api/issues?limit=1" "$DOCTOR_TASK_TOKEN"
  if [ "$API_STATUS" != "200" ]; then
    echo "Task token failed to access GET /api/issues?limit=1: HTTP $API_STATUS"
    echo "$API_RESPONSE"
    set_failed_at "token-auth"
    summarize_and_exit 1
  fi
}

cancel_doctor_task() {
  api_request "POST" "$BACKEND_URL/api/issues/$DOCTOR_ISSUE_ID/tasks/$DOCTOR_TASK_ID/cancel" "$DOCTOR_PAT_TOKEN" "$DOCTOR_WORKSPACE_ID"
  if [ "$API_STATUS" != "200" ]; then
    echo "Task cancel failed: HTTP $API_STATUS"
    echo "$API_RESPONSE"
    set_failed_at "cancel"
    summarize_and_exit 1
  fi
}

check_task_token_revoked() {
  api_request "GET" "$BACKEND_URL/api/issues?limit=1" "$DOCTOR_TASK_TOKEN"
  if [ "$API_STATUS" = "200" ]; then
    echo "Task token still works after cancellation."
    echo "$API_RESPONSE"
    set_failed_at "token-revoked"
    summarize_and_exit 1
  fi
}

run_agent_loop_smoke() {
  print_step "[5/5] Agent loop smoke"
  AGENT_LOOP="running"

  echo "seed: creating temporary PAT"
  create_doctor_pat
  echo "seed: creating temporary issue"
  create_doctor_issue
  echo "seed: creating temporary task"
  create_doctor_task

  echo "claim: claiming task via runtime API"
  claim_doctor_task

  echo "token-issued: auth_token received"
  echo "token-auth: verifying task token against GET /api/issues?limit=1"
  check_task_token_auth

  echo "cancel: cancelling task via issue API"
  cancel_doctor_task

  echo "token-revoked: verifying cancelled task token no longer works"
  check_task_token_revoked

  AGENT_LOOP="pass"
}

load_env_kind
print_context
check_postgres
check_schema_state
check_backend_health
find_online_runtime
run_agent_loop_smoke

RESULT="PASS"
FAILED_AT=""
summarize_and_exit 0
