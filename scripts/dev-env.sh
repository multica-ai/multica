#!/usr/bin/env bash
# Local development environments as named, listable, deletable objects.
#
#   make up                      # start this checkout's environment (api + web)
#   make up C=api,web,daemon     # pick the components
#   make status                  # what is running, and is it mine
#   make list                    # every environment on this machine
#   make down                    # stop the processes, keep the data
#   make destroy                 # stop, then delete database + profile + slot
#
# Three rules the old flow got wrong, and why they are here:
#
#  1. Ports, database names and CLI profiles are ALLOCATED under a lock and
#     recorded in a registry, not recomputed from cksum($PWD) by every caller.
#     Deriving an identity in a 1000-slot namespace with no coordination
#     collides, and a collision here is silent: the loser's process fails to
#     bind while the winner keeps answering.
#  2. Nothing prints a checkmark for a resource it has not reached the way the
#     application reaches it. The database is verified through DATABASE_URL,
#     never through `docker exec` — when a native PostgreSQL owns 5432 the
#     container never binds the host port, so a docker-exec create lands in a
#     server the backend never talks to.
#  3. Creating an environment writes down how to destroy it. Without that
#     record there is no destroy, only archaeology; the databases, ports and
#     daemons leak, and nothing on the machine can even list them.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DEV_HOME="${MULTICA_DEV_HOME:-$HOME/.multica/dev}"
ENVS_DIR="$DEV_HOME/envs"
LOCK_DIR="$DEV_HOME/lock.d"
DEV_WORKSPACES_PARENT="${MULTICA_DEV_WORKSPACES_PARENT:-$HOME}"
DEV_DESKTOP_APP_DATA="${MULTICA_DEV_DESKTOP_APP_DATA:-}"
DEV_PROFILES_HOME="${MULTICA_DEV_PROFILES_HOME:-$HOME/.multica/profiles}"

DEV_EMAIL="${MULTICA_DEV_EMAIL:-dev@localhost}"
DEV_CODE_DEFAULT=888888
WORKSPACE_NAME="${MULTICA_DEV_WORKSPACE_NAME:-Dev}"
WORKSPACE_SLUG="${MULTICA_DEV_WORKSPACE_SLUG:-dev}"

ALL_COMPONENTS="api web daemon desktop"
DEFAULT_COMPONENTS="api web"

# An agent runs with TMPDIR=/tmp/multica-task-<id>, deleted when the run ends.
# Anything the Go toolchain builds there goes with it, so a binary started from
# such a build stops being re-executable the moment its creator finishes.
DEV_TMPDIR="${MULTICA_DEV_TMPDIR:-$HOME/.multica/dev-tmp}"

# The agent runtime exports these pointing at PRODUCTION, and MULTICA_SERVER_URL
# silently outranks server_url in a saved profile config. Every long-lived child
# is launched without them, so a local daemon cannot authenticate its local
# token against the production API — which fails as a bare 401 and reads like a
# product bug. PATH is never stripped: the daemon resolves agent CLI paths by
# forking the login shell.
CLEAN_ENV=(env
  -u MULTICA_SERVER_URL -u MULTICA_TOKEN -u MULTICA_WORKSPACE_ID
  -u MULTICA_DAEMON_PORT -u MULTICA_AGENT_ID -u MULTICA_AGENT_NAME
  -u MULTICA_TASK_ID -u MULTICA_TASK_SLOT
  -u MULTICA_TASK_CONFIG_ROOT -u MULTICA_TASK_WORKSPACES_ROOT
  -u MULTICA_WORKSPACES_ROOT)

# ---------------------------------------------------------------- output ----

if [ -t 1 ]; then
  C_BOLD=$'\033[1m'; C_GREEN=$'\033[32m'; C_RED=$'\033[31m'; C_DIM=$'\033[2m'; C_OFF=$'\033[0m'
else
  C_BOLD=""; C_GREEN=""; C_RED=""; C_DIM=""; C_OFF=""
fi

step() { printf '\n%s==> %s%s\n' "$C_BOLD" "$1" "$C_OFF"; }
info() { printf '    %s\n' "$1"; }
ok()   { printf '    %s✓%s %s\n' "$C_GREEN" "$C_OFF" "$1"; }
warn() { printf '    %s!%s %s\n' "$C_RED" "$C_OFF" "$1"; }
die()  { printf '\n%s✗ %s%s\n' "$C_RED" "$1" "$C_OFF" >&2; exit 1; }

json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g'
}

now_iso() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
now_epoch() { date -u '+%s'; }

expires_at_after_hours() {
  node -e '
    const hours = Number(process.argv[1]);
    process.stdout.write(new Date(Date.now() + hours * 3600_000).toISOString().replace(/\.\d{3}Z$/, "Z"));
  ' "$1"
}

# ----------------------------------------------------------------- locking ---

# flock is not on a stock macOS, so the lock is an atomic mkdir. The holder's
# pid is recorded so a lock left behind by a killed process is recoverable
# instead of wedging every later command.
acquire_lock() {
  local waited=0
  mkdir -p "$DEV_HOME"
  while ! mkdir "$LOCK_DIR" 2>/dev/null; do
    local holder=""
    holder="$(cat "$LOCK_DIR/pid" 2>/dev/null || true)"
    if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
      rm -rf "$LOCK_DIR"
      continue
    fi
    waited=$((waited + 1))
    [ "$waited" -lt 100 ] || die "Timed out waiting for the allocation lock at $LOCK_DIR. If nothing else is running, remove it."
    sleep 0.1
  done
  printf '%s\n' "$$" > "$LOCK_DIR/pid"
}

release_lock() { rm -rf "$LOCK_DIR"; }

# ---------------------------------------------------------------- registry ---

env_dir()      { printf '%s/%s' "$ENVS_DIR" "$1"; }
manifest_of()  { printf '%s/%s/manifest.env' "$ENVS_DIR" "$1"; }

valid_env_name() {
  case "$1" in
    ""|*[!a-z0-9_-]*|-*|_*) return 1 ;;
    *) [ "${#1}" -le 128 ] ;;
  esac
}

require_env_name() {
  valid_env_name "$1" || die "Invalid environment name '$1'. Use 1-128 lowercase letters, numbers, '-' or '_', starting with a letter or number."
}

require_ttl() {
  case "$1" in
    ""|0|*[!0-9]*) die "TTL must be a positive integer number of hours." ;;
  esac
}

list_env_names() {
  [ -d "$ENVS_DIR" ] || return 0
  local path
  for path in "$ENVS_DIR"/*/manifest.env; do
    [ -f "$path" ] || continue
    basename "$(dirname "$path")"
  done
  return 0
}

# Loads a manifest into NAME/DIR/BACKEND_PORT/... in the caller's scope.
load_manifest() {
  local file
  require_env_name "$1"
  file="$(manifest_of "$1")"
  [ -f "$file" ] || return 1
  # shellcheck disable=SC1090
  . "$file"
  [ "$NAME" = "$1" ] || die "Manifest $file declares NAME=$NAME; expected $1."
}

# Prints nothing (and succeeds) for a missing manifest or key: callers compare
# the value, and a non-zero return from a command substitution is fatal under
# `set -e` on bash 3.2.
manifest_field() {
  local file key=$2
  require_env_name "$1"
  file="$(manifest_of "$1")"
  [ -f "$file" ] || return 0
  (
    # shellcheck disable=SC1090
    . "$file"
    case "$key" in
      DIR) printf '%s' "$DIR" ;;
      OFFSET) printf '%s' "$OFFSET" ;;
      *) return 1 ;;
    esac
  )
}

write_manifest_value() {
  printf '%s=' "$1"
  printf '%q' "$2"
  printf '\n'
}

env_name_for_dir() {
  local name
  while read -r name; do
    [ -n "$name" ] || continue
    if [ "$(manifest_field "$name" DIR)" = "$1" ]; then
      printf '%s' "$name"
      return 0
    fi
  done <<EOF
$(list_env_names)
EOF
  return 1
}

# ------------------------------------------------------------------- ports ---

# -sTCP:LISTEN matters: an unfiltered lsof also returns CLIENTS of the port, and
# the daemon holds a long-lived connection to the backend. Killing what the
# unfiltered lookup returns takes the daemon down with the server (#6573).
#
# The trailing `|| true` is load-bearing on macOS's bash 3.2: `x="$(fn)"` inside
# a function aborts the script under `set -e` when fn's last command fails, and
# "no process is listening" is the normal answer here, not an error.
port_listener_pid() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null | head -1 || true
}

port_free() { [ -z "$(port_listener_pid "$1")" ]; }

describe_port_owner() {
  local pid
  pid="$(port_listener_pid "$1")"
  [ -n "$pid" ] || { printf 'free'; return; }
  printf 'pid %s (%s), up %s' "$pid" \
    "$(ps -p "$pid" -o comm= 2>/dev/null | sed 's/^ *//' || echo unknown)" \
    "$(ps -p "$pid" -o etime= 2>/dev/null | sed 's/^ *//' || echo unknown)"
}

# -------------------------------------------------------------- allocation ---

slugify() {
  local slug
  slug="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
  printf '%s' "${slug:-env}"
}

path_offset() {
  local sum
  sum="$(printf '%s' "$1" | cksum | awk '{print $1}')"
  printf '%s' $((sum % 1000))
}

renderer_port_for_offset() {
  local port=$((5174 + $1))
  [ "$port" -ne 6000 ] || port=6174
  printf '%s' "$port"
}

desktop_app_data_root() {
  if [ -n "$DEV_DESKTOP_APP_DATA" ]; then
    printf '%s' "$DEV_DESKTOP_APP_DATA"
    return 0
  fi
  case "$(uname -s)" in
    Darwin) printf '%s/Library/Application Support' "$HOME" ;;
    MINGW*|MSYS*|CYGWIN*) printf '%s' "${APPDATA:-$HOME/AppData/Roaming}" ;;
    *) printf '%s' "${XDG_CONFIG_HOME:-$HOME/.config}" ;;
  esac
}

desktop_user_data_dir() {
  printf '%s/Multica Canary %s' "$(desktop_app_data_root)" "$1"
}

offset_registered() {
  local name
  while read -r name; do
    [ -n "$name" ] || continue
    [ "$name" = "${2:-}" ] && continue
    if [ "$(manifest_field "$name" OFFSET)" = "$1" ]; then
      return 0
    fi
  done <<EOF
$(list_env_names)
EOF
  return 1
}

# Probes for a usable slot starting at the path hash, so the common case keeps
# the deterministic number a checkout has always had, and only a real conflict
# moves it. A slot is usable when the registry does not hold it AND both ports
# are actually free — the registry alone cannot see a process started before
# this tooling existed.
allocate_offset() {
  local dir=$1 self=${2:-} start i offset backend frontend renderer
  start="$(path_offset "$dir")"
  for i in $(seq 0 999); do
    offset=$(((start + i) % 1000))
    backend=$((18080 + offset))
    frontend=$((13000 + offset))
    renderer="$(renderer_port_for_offset "$offset")"
    offset_registered "$offset" "$self" && continue
    port_free "$backend" || continue
    port_free "$frontend" || continue
    port_free "$renderer" || continue
    printf '%s' "$offset"
    return 0
  done
  return 1
}

# ---------------------------------------------------------------- env file ---

detect_env_file() {
  if [ -f "$REPO_ROOT/.env" ]; then
    printf '.env'
  elif [ -f "$REPO_ROOT/.env.worktree" ]; then
    printf '.env.worktree'
  elif [ -f "$REPO_ROOT/.git" ] && [ ! -d "$REPO_ROOT/.git" ]; then
    printf '.env.worktree'
  else
    printf '.env'
  fi
}

load_env_file() {
  local root="${2:-$REPO_ROOT}"
  set -a
  # shellcheck disable=SC1090
  . "$root/$1"
  set +a
  # shellcheck disable=SC1091
  . "$root/scripts/local-env.sh"
}

# The verification code has to be in the file BEFORE the backend starts: the
# handler reads the variable at request time, but the process loads the file
# once. Setting it here removes the start → edit → restart detour, and is what
# makes `up` able to log itself in without a human reading a log for a code.
ensure_dev_code() {
  local file="$REPO_ROOT/$1" tmp
  if grep -qE '^MULTICA_DEV_VERIFICATION_CODE=[0-9]{6}$' "$file"; then
    return 0
  fi
  if grep -q '^MULTICA_DEV_VERIFICATION_CODE=' "$file"; then
    tmp="$(mktemp)"
    sed "s/^MULTICA_DEV_VERIFICATION_CODE=.*/MULTICA_DEV_VERIFICATION_CODE=$DEV_CODE_DEFAULT/" "$file" > "$tmp"
    mv "$tmp" "$file"
  else
    printf '\nMULTICA_DEV_VERIFICATION_CODE=%s\n' "$DEV_CODE_DEFAULT" >> "$file"
  fi
  info "Set MULTICA_DEV_VERIFICATION_CODE=$DEV_CODE_DEFAULT in $1 (ignored when APP_ENV=production)."
}

rewrite_env_ports() {
  local file="$REPO_ROOT/$1" offset=$2 backend=$3 frontend=$4 db=$5 tmp database_url escaped_database_url
  database_url="$(database_url_with_name "${DATABASE_URL:-}" "$db")" \
    || die "DATABASE_URL is not a valid PostgreSQL URL: ${DATABASE_URL:-<unset>}"
  escaped_database_url="$(printf '%s' "$database_url" | sed 's/[\\&|]/\\&/g')"
  tmp="$(mktemp)"
  sed \
    -e "s|^PORT=.*|PORT=${backend}|" \
    -e "s|^FRONTEND_PORT=.*|FRONTEND_PORT=${frontend}|" \
    -e "s|^FRONTEND_ORIGIN=.*|FRONTEND_ORIGIN=http://localhost:${frontend}|" \
    -e "s|^POSTGRES_DB=.*|POSTGRES_DB=${db}|" \
    -e "s|^DATABASE_URL=.*|DATABASE_URL=${escaped_database_url}|" \
    -e "s|^MULTICA_SERVER_URL=.*|MULTICA_SERVER_URL=ws://localhost:${backend}/ws|" \
    -e "s|^MULTICA_PUBLIC_URL=.*|MULTICA_PUBLIC_URL=http://localhost:${backend}|" \
    -e "s|^MULTICA_APP_URL=.*|MULTICA_APP_URL=http://localhost:${frontend}|" \
    -e "s|^NEXT_PUBLIC_API_URL=.*|NEXT_PUBLIC_API_URL=http://localhost:${backend}|" \
    -e "s|^NEXT_PUBLIC_WS_URL=.*|NEXT_PUBLIC_WS_URL=ws://localhost:${backend}/ws|" \
    "$file" > "$tmp"
  mv "$tmp" "$file"
}

# ---------------------------------------------------------------- database ---

admin_database_url() {
  node -e '
    const url = new URL(process.argv[1]);
    url.pathname = "/postgres";
    process.stdout.write(url.toString());
  ' "$1" 2>/dev/null || true
}

database_url_with_name() {
  node -e '
    const url = new URL(process.argv[1]);
    if (url.protocol !== "postgres:" && url.protocol !== "postgresql:") process.exit(1);
    url.pathname = "/" + process.argv[2];
    process.stdout.write(url.toString());
  ' "$1" "$2" 2>/dev/null
}

# Diagnoses the failure mode this whole script exists to make impossible:
# something other than the container owns 5432, so the container never bound
# the host port and a docker-exec create landed in the wrong server.
diagnose_database() {
  local owner
  owner="$(lsof -nP -iTCP:"${POSTGRES_PORT:-5432}" -sTCP:LISTEN 2>/dev/null | awk 'NR==2 {print $1" (pid "$2", user "$3")"}')"
  printf '\n'
  warn "The database the tooling created is not the one the application reaches."
  info "Port ${POSTGRES_PORT:-5432} is served by: ${owner:-nothing}"
  info "DATABASE_URL: ${DATABASE_URL}"
  info "If that is a native PostgreSQL, the Docker container never bound the host port."
  info "Either stop it (brew services stop postgresql@17) or point DATABASE_URL at it."
}

ensure_database() {
  local admin_url=""
  if command -v psql >/dev/null 2>&1 && [ -n "${DATABASE_URL:-}" ]; then
    admin_url="$(admin_database_url "$DATABASE_URL")"
  fi

  # Preferred path: create through the same connection string the application
  # uses, so "created" and "reachable" cannot describe two different servers.
  if [ -n "$admin_url" ] && PGCONNECT_TIMEOUT=3 psql "$admin_url" -tAc 'SELECT 1' >/dev/null 2>&1; then
    if ! PGCONNECT_TIMEOUT=3 psql "$admin_url" -tAc "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" | grep -q 1; then
      psql "$admin_url" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"${POSTGRES_DB}\"" >/dev/null
      info "Created database ${POSTGRES_DB} through DATABASE_URL."
    fi
  else
    info "Nothing is answering on ${POSTGRES_PORT:-5432} yet; starting the shared container."
    bash "$REPO_ROOT/scripts/ensure-postgres.sh" "$ENV_FILE" | sed 's/^/    /'
  fi
}

migrate_database() {
  # `migrate up` connects with DATABASE_URL and pings before doing anything, so
  # a successful run is the proof that the application can reach the database
  # this script just prepared. That is why nothing above prints a checkmark.
  if ! (cd "$REPO_ROOT/server" && go run ./cmd/migrate up) > "$LOG_DIR/migrate.log" 2>&1; then
    tail -5 "$LOG_DIR/migrate.log" | sed 's/^/    /' >&2 || true
    if grep -q '3D000\|does not exist' "$LOG_DIR/migrate.log"; then
      diagnose_database
    fi
    die "Migrations failed. Full log: $LOG_DIR/migrate.log"
  fi
}

# -------------------------------------------------------------- components ---

component_selected() {
  case " $COMPONENTS " in *" $1 "*) return 0 ;; *) return 1 ;; esac
}

pid_file()  { printf '%s/%s.pid' "$STATE_DIR" "$1"; }
listener_pid_file() { printf '%s/%s.listener.pid' "$STATE_DIR" "$1"; }
log_file()  { printf '%s/%s.log' "$LOG_DIR" "$1"; }

component_pid() {
  local file
  file="$(pid_file "$1")"
  [ -f "$file" ] || return 1
  local pid
  pid="$(cat "$file")"
  kill -0 "$pid" 2>/dev/null || return 1
  printf '%s' "$pid"
}

# set -m puts the launcher in its own process group, so stopping can signal the
# whole tree (make → go run → server) with one kill, and the child's own
# `trap 'kill 0'` can never reach back into this shell.
launch_detached() {
  local name=$1
  shift
  (
    set -m
    nohup "${CLEAN_ENV[@]}" "$@" > "$(log_file "$name")" 2>&1 < /dev/null &
    printf '%s\n' "$!" > "$(pid_file "$name")"
  )
}

health_json() { curl -sf --max-time 3 "http://localhost:${BACKEND_PORT}/health" 2>/dev/null; }

json_field() {
  node -e '
    let payload;
    try { payload = JSON.parse(process.argv[1]); } catch { process.exit(1); }
    const value = process.argv[2].split(".").reduce((acc, key) => (acc == null ? acc : acc[key]), payload);
    if (value === undefined || value === null || value === "") process.exit(1);
    process.stdout.write(String(value));
  ' "$1" "$2" 2>/dev/null
}

api_started_after() {
  local started_at epoch
  started_at="$(json_field "$1" started_at || true)"
  [ -n "$started_at" ] || return 1
  epoch="$(node -e 'process.stdout.write(String(Math.floor(Date.parse(process.argv[1]) / 1000)))' "$started_at" 2>/dev/null || echo 0)"
  [ "$epoch" -ge $(($2 - 5)) ]
}

checkout_commit() {
  git -C "${DIR:-$REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || printf 'unknown'
}

process_group_id() {
  ps -p "$1" -o pgid= 2>/dev/null | tr -d ' ' || true
}

listener_belongs_to_component() {
  local component=$1 port=$2 launcher listener recorded
  launcher="$(component_pid "$component" || true)"
  listener="$(port_listener_pid "$port")"
  [ -n "$launcher" ] && [ -n "$listener" ] || return 1
  recorded="$(cat "$(listener_pid_file "$component")" 2>/dev/null || true)"
  [ -n "$recorded" ] && [ "$listener" = "$recorded" ] && return 0
  [ "$(process_group_id "$listener")" = "$launcher" ]
}

health_belongs_to_api() {
  local health=$1 health_pid listener
  health_pid="$(json_field "$health" pid || true)"
  listener="$(port_listener_pid "$BACKEND_PORT")"
  [ -n "$health_pid" ] && [ "$health_pid" = "$listener" ] \
    && listener_belongs_to_component api "$BACKEND_PORT"
}

api_identity_matches() {
  local health=$1 expected_commit=$2 launched_at=${3:-0} reported_commit started_at
  health_belongs_to_api "$health" || return 1
  reported_commit="$(json_field "$health" commit || true)"
  started_at="$(json_field "$health" started_at || true)"
  [ -n "$started_at" ] && [ "$reported_commit" = "$expected_commit" ] || return 1
  [ "$launched_at" = 0 ] || api_started_after "$health" "$launched_at"
}

start_api() {
  local launched_at health waited=0 expected_commit
  expected_commit="$(checkout_commit)"
  if health="$(health_json)" && [ -n "$health" ] && component_pid api >/dev/null; then
    if api_identity_matches "$health" "$expected_commit"; then
      ok "api already running on :$BACKEND_PORT (pid $(json_field "$health" pid), commit $expected_commit)"
      return 0
    fi
    if health_belongs_to_api "$health"; then
      warn "api on :$BACKEND_PORT is ours but not commit $expected_commit; restarting it."
      stop_component api
    else
      die "Port $BACKEND_PORT answers /health, but its pid/commit does not match this environment. Refusing to reuse or kill it."
    fi
  fi
  if ! port_free "$BACKEND_PORT"; then
    die "Port $BACKEND_PORT is busy: $(describe_port_owner "$BACKEND_PORT").
Run 'make down' here first — a leftover instance answers /health with 200 and you would test it instead of your build."
  fi

  launched_at="$(now_epoch)"
  launch_detached api make -C "$REPO_ROOT" -s api-dev ENV_FILE="$ENV_FILE"
  info "api launching (pid $(cat "$(pid_file api)")), log: $(log_file api)"

  while [ "$waited" -lt 300 ]; do
    health="$(health_json || true)"
    if [ -n "$health" ]; then
      # A 200 is not enough: pid, process group, commit and launch time all have
      # to identify the process this environment just started.
      if ! api_identity_matches "$health" "$expected_commit" "$launched_at"; then
        stop_component api
        die "Something else is serving :$BACKEND_PORT, or the launched api did not report pid/commit/started_at for commit $expected_commit."
      fi
      ok "api healthy at http://localhost:$BACKEND_PORT (pid $(json_field "$health" pid), commit $expected_commit)"
      return 0
    fi
    component_pid api >/dev/null || { tail -20 "$(log_file api)" | sed 's/^/    /' >&2; die "api exited during startup. Log: $(log_file api)"; }
    sleep 2
    waited=$((waited + 2))
  done
  die "api never became healthy. Log: $(log_file api)"
}

start_web() {
  local waited=0 listener
  if curl -sf --max-time 15 "http://localhost:${FRONTEND_PORT}" >/dev/null 2>&1 \
    && listener_belongs_to_component web "$FRONTEND_PORT"; then
    ok "web already running on :$FRONTEND_PORT"
    return 0
  fi
  if ! port_free "$FRONTEND_PORT"; then
    die "Port $FRONTEND_PORT is busy: $(describe_port_owner "$FRONTEND_PORT"). Run 'make down' here first."
  fi

  launch_detached web make -C "$REPO_ROOT" -s web-dev ENV_FILE="$ENV_FILE"
  info "web launching (pid $(cat "$(pid_file web)")), log: $(log_file web)"

  while [ "$waited" -lt 300 ]; do
    if curl -sf --max-time 15 "http://localhost:${FRONTEND_PORT}" >/dev/null 2>&1; then
      listener="$(port_listener_pid "$FRONTEND_PORT")"
      if ! listener_belongs_to_component web "$FRONTEND_PORT"; then
        stop_component web
        die "Web on :$FRONTEND_PORT is not owned by the process group this environment launched."
      fi
      ok "web serving http://localhost:$FRONTEND_PORT (pid ${listener:-?})"
      return 0
    fi
    component_pid web >/dev/null || { tail -20 "$(log_file web)" | sed 's/^/    /' >&2; die "web exited during startup. Log: $(log_file web)"; }
    sleep 2
    waited=$((waited + 2))
  done
  die "web never came up. Log: $(log_file web)"
}

# send-code once, verify-code once. Repeated verify attempts lock the code out
# and start returning 400 even when it is correct, so retrying is self-defeating.
write_profile_config() {
  local config=$1 pat=$2 ws=$3
  mkdir -p "$PROFILE_DIR"
  cat > "$config" <<EOF
{
  "server_url": "http://localhost:${BACKEND_PORT}",
  "app_url": "http://localhost:${FRONTEND_PORT}",
  "token": "$(json_escape "$pat")",
  "workspace_id": "$(json_escape "$ws")",
  "workspaces_root": "$(json_escape "$WORKSPACES_ROOT")"
}
EOF
  chmod 600 "$config"
}

ensure_credentials() {
  local server="http://localhost:${BACKEND_PORT}" config="$PROFILE_DIR/config.json"
  local code="${MULTICA_DEV_VERIFICATION_CODE:-$DEV_CODE_DEFAULT}"
  local verify jwt pat ws

  if [ -f "$config" ]; then
    pat="$(json_field "$(cat "$config")" token || true)"
    ws="$(json_field "$(cat "$config")" workspace_id || true)"
    if [ -n "$pat" ] && curl -sf --max-time 5 "$server/api/me" -H "Authorization: Bearer $pat" >/dev/null 2>&1; then
      WORKSPACE_ID="$ws"
      write_profile_config "$config" "$pat" "$ws"
      ok "CLI profile $PROFILE already authenticated"
      return 0
    fi
  fi

  curl -sf -X POST "$server/auth/send-code" -H 'Content-Type: application/json' \
    -d "{\"email\":\"${DEV_EMAIL}\"}" >/dev/null \
    || die "send-code failed. Is MULTICA_DEV_VERIFICATION_CODE set and APP_ENV non-production?"

  verify="$(curl -sS -X POST "$server/auth/verify-code" -H 'Content-Type: application/json' \
    -d "{\"email\":\"${DEV_EMAIL}\",\"code\":\"${code}\"}")"
  jwt="$(json_field "$verify" token || true)"
  [ -n "$jwt" ] || die "verify-code failed: $verify
Do not retry immediately — repeated attempts lock the code. Wait ~40s and re-run."

  local pat_response
  pat_response="$(curl -sS -X POST "$server/api/tokens" -H "Authorization: Bearer $jwt" \
    -H 'Content-Type: application/json' -d '{"name":"dev-env","expires_in_days":365}')"
  pat="$(json_field "$pat_response" token || true)"
  [ -n "$pat" ] || die "Personal access token creation failed: $pat_response"

  local ws_response
  ws_response="$(curl -sS -X POST "$server/api/workspaces" -H "Authorization: Bearer $pat" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${WORKSPACE_NAME}\",\"slug\":\"${WORKSPACE_SLUG}\"}")"
  ws="$(json_field "$ws_response" id || true)"
  if [ -z "$ws" ]; then
    ws="$(node -e '
      let list;
      try { list = JSON.parse(process.argv[1]); } catch { process.exit(1); }
      const match = (Array.isArray(list) ? list : []).find(w => w.slug === process.argv[2]);
      if (!match) process.exit(1);
      process.stdout.write(match.id);
    ' "$(curl -sS "$server/api/workspaces" -H "Authorization: Bearer $pat")" "$WORKSPACE_SLUG" 2>/dev/null || true)"
  fi
  [ -n "$ws" ] || die "Workspace creation failed: $ws_response"

  # A fresh user has onboarded_at = NULL and a browser login is bounced to
  # /onboarding, so the URL this script prints would not land in the app.
  curl -sS -X POST "$server/api/me/onboarding/complete" \
    -H "Authorization: Bearer $pat" -H "X-Workspace-ID: $ws" \
    -H 'Content-Type: application/json' -d '{"exit":"existing"}' >/dev/null 2>&1 || true

  write_profile_config "$config" "$pat" "$ws"
  WORKSPACE_ID="$ws"
  ok "Logged in as $DEV_EMAIL and wrote profile $PROFILE"
}

# The CLI refuses `daemon start` anywhere under a daemon-task marker, so a task
# cannot spawn a second daemon that competes for its own work. Checking the
# marker here turns that refusal into one actionable line, before this component
# has spent a login and a CLI build on an outcome it cannot reach.
daemon_task_marker() {
  local dir="$REPO_ROOT" marker
  while :; do
    marker="$dir/.multica/daemon_task_context.json"
    if [ -f "$marker" ] && grep -q 'multica-daemon-task' "$marker" 2>/dev/null; then
      printf '%s' "$marker"
      return 0
    fi
    [ "$dir" != "/" ] || break
    dir="$(dirname "$dir")"
  done
  return 0
}

start_daemon() {
  local status state
  ensure_credentials

  # Built, never `go run`: the daemon records its own executable path at startup
  # and re-execs it as the execution-environment helper for every task. Under
  # `go run` the toolchain deletes that binary when the launcher exits, so the
  # daemon registers, heartbeats, and then fails every task with
  # "fork/exec .../go-build.../exe/multica: no such file or directory".
  info "Building $MULTICA_BIN (a go run daemon would fail every task later)."
  (cd "$REPO_ROOT/server" && go build -o bin/multica ./cmd/multica) || die "Failed to build the multica CLI."

  "${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" \
    "$MULTICA_BIN" daemon start --profile "$PROFILE" 2>&1 | sed 's/^/    /' || true

  status="$("${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" \
    "$MULTICA_BIN" daemon status --profile "$PROFILE" --output json 2>/dev/null || true)"
  state="$(json_field "$status" status || echo unknown)"
  # `daemon status` reports "stopped" plus port_conflict when the daemon
  # answering this profile's health port belongs to another profile, so a
  # collision can never be read here as a healthy daemon.
  if [ "$state" != running ]; then
    if [ -n "$(json_field "$status" port_conflict.profile || true)" ]; then
      die "The health port for $PROFILE is served by profile $(json_field "$status" port_conflict.profile). Rename one of them."
    fi
    die "Daemon is '$state' after start. Log: $PROFILE_DIR/daemon.log"
  fi
  ok "daemon running for profile $PROFILE (pid $(json_field "$status" pid || echo '?'))"
}

start_desktop() {
  local waited=0 listener stable_listener
  if component_pid desktop >/dev/null \
    && curl -sf --max-time 10 "http://localhost:${DESKTOP_RENDERER_PORT}" >/dev/null 2>&1 \
    && listener_belongs_to_component desktop "$DESKTOP_RENDERER_PORT" \
    && desktop_env_matches; then
      ok "desktop already running (pid $(component_pid desktop), renderer :$DESKTOP_RENDERER_PORT)"
      return 0
  fi
  if component_pid desktop >/dev/null; then
    warn "desktop launcher exists but its renderer/backend identity is stale; restarting it."
    stop_component desktop
  fi
  if ! port_free "$DESKTOP_RENDERER_PORT"; then
    die "Desktop renderer port $DESKTOP_RENDERER_PORT is busy: $(describe_port_owner "$DESKTOP_RENDERER_PORT")."
  fi

  # The marker makes destroy remove only a file this tool owns. Explicit
  # renderer/app values bind Desktop to the registry allocation rather than
  # independently hashing the checkout path again.
  cat > "$DESKTOP_ENV_FILE" <<EOF
# Managed by scripts/dev-env.sh for environment ${NAME}.
VITE_API_URL=http://localhost:${BACKEND_PORT}
VITE_WS_URL=ws://localhost:${BACKEND_PORT}/ws
EOF
  launch_detached desktop env \
    DESKTOP_RENDERER_PORT="$DESKTOP_RENDERER_PORT" DESKTOP_APP_SUFFIX="$DESKTOP_APP_SUFFIX" \
    make -C "$REPO_ROOT" -s desktop-dev ENV_FILE="$ENV_FILE"

  while [ "$waited" -lt 300 ]; do
    if curl -sf --max-time 10 "http://localhost:${DESKTOP_RENDERER_PORT}" >/dev/null 2>&1; then
      listener="$(port_listener_pid "$DESKTOP_RENDERER_PORT")"
      if ! component_pid desktop >/dev/null || [ -z "$listener" ] || ! desktop_env_matches; then
        stop_component desktop
        die "Desktop renderer on :$DESKTOP_RENDERER_PORT does not belong to this environment."
      fi
      # Electron can bring Vite up and then crash during renderer bootstrap.
      # Require a short stable window before the environment claims readiness.
      sleep 5
      stable_listener="$(port_listener_pid "$DESKTOP_RENDERER_PORT")"
      if ! component_pid desktop >/dev/null || [ "$stable_listener" != "$listener" ] \
        || ! curl -sf --max-time 10 "http://localhost:${DESKTOP_RENDERER_PORT}" >/dev/null 2>&1; then
        tail -20 "$(log_file desktop)" | sed 's/^/    /' >&2 || true
        stop_component desktop
        die "desktop exited during renderer bootstrap. Log: $(log_file desktop)"
      fi
      printf '%s\n' "$listener" > "$(listener_pid_file desktop)"
      ok "desktop ready (launcher $(component_pid desktop), renderer pid ${listener:-?}, backend :$BACKEND_PORT)"
      return 0
    fi
    component_pid desktop >/dev/null \
      || { tail -20 "$(log_file desktop)" | sed 's/^/    /' >&2; die "desktop exited during startup. Log: $(log_file desktop)"; }
    sleep 2
    waited=$((waited + 2))
  done
  stop_component desktop
  die "desktop renderer never became ready on :$DESKTOP_RENDERER_PORT. Log: $(log_file desktop)"
}

desktop_env_matches() {
  [ -f "$DESKTOP_ENV_FILE" ] \
    && grep -Fqx "# Managed by scripts/dev-env.sh for environment ${NAME}." "$DESKTOP_ENV_FILE" \
    && grep -Fqx "VITE_API_URL=http://localhost:${BACKEND_PORT}" "$DESKTOP_ENV_FILE" \
    && grep -Fqx "VITE_WS_URL=ws://localhost:${BACKEND_PORT}/ws" "$DESKTOP_ENV_FILE"
}

stop_component() {
  local name=$1 pid launcher="" status state recorded_listener=""
  case "$name" in
    daemon)
      if [ -x "$MULTICA_BIN" ]; then
        if "${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" \
          "$MULTICA_BIN" daemon stop --profile "$PROFILE" >/dev/null 2>&1; then
          ok "daemon stopped"
        else
          status="$("${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" \
            "$MULTICA_BIN" daemon status --profile "$PROFILE" --output json 2>/dev/null || true)"
          state="$(json_field "$status" status || echo stopped)"
          if [ "$state" = running ]; then
            warn "daemon for profile $PROFILE is still running"
            return 1
          fi
          info "daemon was not running"
        fi
      else
        pid="$(cat "$PROFILE_DIR/daemon.pid" 2>/dev/null || true)"
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
          warn "cannot stop daemon pid $pid because no usable multica binary was found"
          return 1
        fi
        info "daemon skipped (no usable binary and no live profile pid)"
      fi
      return 0
      ;;
  esac

  recorded_listener="$(cat "$(listener_pid_file "$name")" 2>/dev/null || true)"
  pid="$(component_pid "$name" || true)"
  if [ -n "$pid" ]; then
    launcher="$pid"
    # Negative pid targets the process group, so make → go run → server all go
    # down together instead of leaving the real listener orphaned.
    kill -TERM -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL -"$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
      sleep 1
    fi
    if kill -0 "$pid" 2>/dev/null; then
      warn "$name launcher pid $pid is still running"
      return 1
    fi
    rm -f "$(pid_file "$name")"
    ok "$name stopped (pid $pid)"
  else
    rm -f "$(pid_file "$name")"
    info "$name was not running"
  fi

  # A process group kill can miss a listener that has reparented away from its
  # launcher. Only kill that listener when its process group still proves it
  # belongs to the recorded launcher; a stale manifest must never kill an
  # unrelated process that later reused the port.
  local port=""
  case "$name" in
    api) port="$BACKEND_PORT" ;;
    web) port="$FRONTEND_PORT" ;;
    desktop) port="$DESKTOP_RENDERER_PORT" ;;
  esac
  if [ -n "$port" ]; then
    local listener
    listener="$(port_listener_pid "$port")"
    if [ -n "$listener" ]; then
      if { [ -n "$recorded_listener" ] && [ "$listener" = "$recorded_listener" ]; } \
        || { [ -n "$launcher" ] && [ "$(process_group_id "$listener")" = "$launcher" ]; }; then
        kill -TERM "$listener" 2>/dev/null || true
        sleep 1
        if kill -0 "$listener" 2>/dev/null; then
          kill -KILL "$listener" 2>/dev/null || true
          sleep 1
        fi
        if kill -0 "$listener" 2>/dev/null; then
          warn "$name listener pid $listener is still running"
          return 1
        fi
        info "released :$port (pid $listener)"
      else
        warn "left :$port alone: listener pid $listener is not owned by this environment"
      fi
    fi
  fi
  rm -f "$(listener_pid_file "$name")"
}

# ------------------------------------------------------------------ status ---

component_state() {
  case "$1" in
    api)
      local health
      health="$(health_json || true)"
      if [ -n "$health" ] && api_identity_matches "$health" "$(checkout_commit)"; then
        printf 'running|http://localhost:%s|pid %s commit %s started %s' "$BACKEND_PORT" \
          "$(json_field "$health" pid || echo '?')" \
          "$(json_field "$health" commit || echo '?')" \
          "$(json_field "$health" started_at || echo '?')"
      elif [ -n "$health" ]; then
        printf 'mismatch|http://localhost:%s|health responder is not this checkout/process' "$BACKEND_PORT"
      else
        printf 'stopped|http://localhost:%s|' "$BACKEND_PORT"
      fi
      ;;
    web)
      if curl -sf --max-time 10 "http://localhost:${FRONTEND_PORT}" >/dev/null 2>&1 \
        && listener_belongs_to_component web "$FRONTEND_PORT"; then
        printf 'running|http://localhost:%s|pid %s' "$FRONTEND_PORT" "$(port_listener_pid "$FRONTEND_PORT")"
      elif [ -n "$(port_listener_pid "$FRONTEND_PORT")" ]; then
        printf 'mismatch|http://localhost:%s|listener is not owned by this environment' "$FRONTEND_PORT"
      else
        printf 'stopped|http://localhost:%s|' "$FRONTEND_PORT"
      fi
      ;;
    daemon)
      local status state
      if [ -x "$MULTICA_BIN" ]; then
        status="$("${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" \
          "$MULTICA_BIN" daemon status --profile "$PROFILE" --output json 2>/dev/null || true)"
        state="$(json_field "$status" status || echo stopped)"
        printf '%s|%s|pid %s' "$state" "$PROFILE" "$(json_field "$status" pid || echo '-')"
      else
        printf 'stopped|%s|not built' "$PROFILE"
      fi
      ;;
    desktop)
      local pid
      pid="$(component_pid desktop || true)"
      if [ -n "$pid" ] \
        && curl -sf --max-time 10 "http://localhost:${DESKTOP_RENDERER_PORT}" >/dev/null 2>&1 \
        && listener_belongs_to_component desktop "$DESKTOP_RENDERER_PORT" \
        && desktop_env_matches; then
        printf 'running|http://localhost:%s|launcher %s renderer %s' \
          "$DESKTOP_RENDERER_PORT" "$pid" "$(port_listener_pid "$DESKTOP_RENDERER_PORT")"
      elif [ -n "$pid" ] || [ -n "$(port_listener_pid "$DESKTOP_RENDERER_PORT")" ]; then
        printf 'mismatch|http://localhost:%s|renderer/backend identity does not match' "$DESKTOP_RENDERER_PORT"
      else
        printf 'stopped|http://localhost:%s|' "$DESKTOP_RENDERER_PORT"
      fi
      ;;
  esac
}

print_status_human() {
  local comp state url detail row
  printf '\n%s%s%s  %s%s%s\n' "$C_BOLD" "$NAME" "$C_OFF" "$C_DIM" "$DIR" "$C_OFF"
  printf '  %-9s %-9s %-32s %s\n' COMPONENT STATE ADDRESS DETAIL
  for comp in $ALL_COMPONENTS; do
    row="$(component_state "$comp")"
    state="${row%%|*}"; row="${row#*|}"
    url="${row%%|*}"; detail="${row#*|}"
    printf '  %-9s %-9s %-32s %s\n' "$comp" "$state" "${url:--}" "${detail:--}"
  done
  printf '  %-9s %-9s %-32s %s\n' database "$(database_state)" "$DB_NAME" "${DATABASE_URL%%\?*}"
  printf '\n  owner %s · created %s%s\n' "$OWNER" "$CREATED_AT" "$( [ "${TTL_HOURS:-0}" != 0 ] && printf ' · expires %s' "$EXPIRES_AT" )"
  printf '  logs  %s\n' "$LOG_DIR"
}

# Reported from the connection string the application uses, so "present" can
# never mean "present in a server nothing talks to".
database_state() {
  local admin_url
  command -v psql >/dev/null 2>&1 || { printf 'unknown'; return; }
  admin_url="$(admin_database_url "$DATABASE_URL")"
  PGCONNECT_TIMEOUT=3 psql "$admin_url" -tAc 'SELECT 1' >/dev/null 2>&1 || { printf 'no-server'; return; }
  if PGCONNECT_TIMEOUT=3 psql "$admin_url" -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" 2>/dev/null | grep -q 1; then
    printf 'present'
  else
    printf 'missing'
  fi
}

print_status_json() {
  local comp row state url detail first=1
  printf '{"name":"%s","dir":"%s","owner":"%s","created_at":"%s","ttl_hours":%s,"expires_at":"%s",' \
    "$(json_escape "$NAME")" "$(json_escape "$DIR")" "$(json_escape "$OWNER")" \
    "$(json_escape "$CREATED_AT")" "${TTL_HOURS:-0}" "$(json_escape "$EXPIRES_AT")"
  printf '"backend_port":%s,"frontend_port":%s,"desktop_renderer_port":%s,"database":"%s","profile":"%s","env_file":"%s","logs":"%s","components":{' \
    "$BACKEND_PORT" "$FRONTEND_PORT" "$DESKTOP_RENDERER_PORT" "$(json_escape "$DB_NAME")" "$(json_escape "$PROFILE")" \
    "$(json_escape "$ENV_FILE")" "$(json_escape "$LOG_DIR")"
  for comp in $ALL_COMPONENTS; do
    row="$(component_state "$comp")"
    state="${row%%|*}"; row="${row#*|}"
    url="${row%%|*}"; detail="${row#*|}"
    [ "$first" = 1 ] || printf ','
    first=0
    printf '"%s":{"state":"%s","address":"%s","detail":"%s"}' \
      "$comp" "$(json_escape "$state")" "$(json_escape "$url")" "$(json_escape "$detail")"
  done
  printf '}}\n'
}

print_handoff() {
  local entrypoint
  if component_selected web; then
    entrypoint="Open        http://localhost:${FRONTEND_PORT}/${WORKSPACE_SLUG}/issues"
  elif component_selected desktop; then
    entrypoint="Desktop     renderer http://localhost:${DESKTOP_RENDERER_PORT} → backend :${BACKEND_PORT}"
  else
    entrypoint="API only    http://localhost:${BACKEND_PORT}"
  fi
  cat <<EOF

${C_GREEN}✓ Environment ready.${C_OFF}

  ${entrypoint}
  Sign in     ${DEV_EMAIL}  ·  code ${MULTICA_DEV_VERIFICATION_CODE:-$DEV_CODE_DEFAULT}
  Backend     http://localhost:${BACKEND_PORT}   (GET /health reports pid + commit + started_at)
  Commit      $(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)
  Environment ${NAME}$( [ "${TTL_HOURS:-0}" != 0 ] && printf ' (expires %s)' "$EXPIRES_AT" )

  Inspect     make status
  Stop        make down            (keeps the database, restarts in seconds)
  Delete      make destroy         (drops the database and frees the slot)
EOF
}

# ------------------------------------------------------------------- verbs ---

resolve_env_for_read() {
  local name="${1:-}"
  if [ -z "$name" ]; then
    name="$(env_name_for_dir "$REPO_ROOT" || true)"
    [ -n "$name" ] || die "No environment registered for $REPO_ROOT. Run 'make up' first."
  fi
  require_env_name "$name"
  load_manifest "$name" || die "Unknown environment '$name'. Run 'make list' to see what exists."
  bind_paths
}

bind_paths() {
  STATE_DIR="$(env_dir "$NAME")"
  LOG_DIR="$STATE_DIR/logs"
  PROFILE_DIR="$DEV_PROFILES_HOME/$PROFILE"
  WORKSPACES_ROOT="${WORKSPACES_ROOT:-$DEV_WORKSPACES_PARENT/multica_workspaces_$PROFILE}"
  DESKTOP_RENDERER_PORT="${DESKTOP_RENDERER_PORT:-$(renderer_port_for_offset "$OFFSET")}"
  DESKTOP_APP_SUFFIX="${DESKTOP_APP_SUFFIX:-$NAME}"
  DESKTOP_USER_DATA_DIR="${DESKTOP_USER_DATA_DIR:-$(desktop_user_data_dir "$DESKTOP_APP_SUFFIX")}"
  DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$DIR/apps/desktop/.env.development.local}"
  EXPIRES_AT="${EXPIRES_AT:-}"
  MULTICA_BIN="$DIR/server/bin/multica"
  if [ ! -x "$MULTICA_BIN" ] && [ -x "$REPO_ROOT/server/bin/multica" ]; then
    MULTICA_BIN="$REPO_ROOT/server/bin/multica"
  fi
  mkdir -p "$LOG_DIR"
}

save_manifest() {
  {
    write_manifest_value NAME "$NAME"
    write_manifest_value DIR "$DIR"
    write_manifest_value CREATED_AT "$CREATED_AT"
    write_manifest_value OWNER "$OWNER"
    write_manifest_value TTL_HOURS "$TTL_HOURS"
    write_manifest_value EXPIRES_AT "$EXPIRES_AT"
    write_manifest_value ENV_FILE "$ENV_FILE"
    write_manifest_value OFFSET "$OFFSET"
    write_manifest_value BACKEND_PORT "$BACKEND_PORT"
    write_manifest_value FRONTEND_PORT "$FRONTEND_PORT"
    write_manifest_value DB_NAME "$DB_NAME"
    write_manifest_value DATABASE_URL "$DATABASE_URL"
    write_manifest_value PROFILE "$PROFILE"
    write_manifest_value WORKSPACES_ROOT "$WORKSPACES_ROOT"
    write_manifest_value DESKTOP_RENDERER_PORT "$DESKTOP_RENDERER_PORT"
    write_manifest_value DESKTOP_APP_SUFFIX "$DESKTOP_APP_SUFFIX"
    write_manifest_value DESKTOP_USER_DATA_DIR "$DESKTOP_USER_DATA_DIR"
    write_manifest_value DESKTOP_ENV_FILE "$DESKTOP_ENV_FILE"
  } > "$(manifest_of "$NAME")"
}

cmd_up() {
  local requested="$DEFAULT_COMPONENTS" name="" owner=human ttl=0 lifecycle_requested=0 comp

  while [ $# -gt 0 ]; do
    case "$1" in
      --components|-c) requested="$(printf '%s' "$2" | tr ',' ' ')"; shift 2 ;;
      --all) requested="$ALL_COMPONENTS"; shift ;;
      --name) name="$2"; shift 2 ;;
      --ephemeral) owner=agent; lifecycle_requested=1; [ "$ttl" != 0 ] || ttl=24; shift ;;
      --ttl) ttl="$2"; owner=agent; lifecycle_requested=1; shift 2 ;;
      *) die "Unknown flag for up: $1" ;;
    esac
  done

  [ -z "$name" ] || require_env_name "$name"
  [ "$ttl" = 0 ] || require_ttl "$ttl"

  for comp in $requested; do
    case " $ALL_COMPONENTS " in *" $comp "*) ;; *) die "Unknown component '$comp'. Valid: $ALL_COMPONENTS" ;; esac
  done
  # web, daemon and desktop are all clients of the backend; selecting one
  # without api would produce an environment that cannot serve a single request.
  case " $requested " in *" api "*) ;; *) requested="api $requested" ;; esac
  COMPONENTS="$requested"

  # No resident cleanup service is required: every future environment start is
  # a safe opportunity to collect expired or directory-less environments.
  cmd_gc --auto

  step "Prerequisites"
  local missing=() tool needed="node go curl"
  # pnpm is only required by the components that actually build JavaScript, so
  # `up C=api` works on a checkout that has never run an install.
  if component_selected web || component_selected desktop; then needed="$needed pnpm"; fi
  for tool in $needed; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  [ ${#missing[@]} -eq 0 ] || die "Missing prerequisites: ${missing[*]}"
  mkdir -p "$DEV_TMPDIR"
  if [ "${TMPDIR:-}" != "$DEV_TMPDIR" ]; then
    info "TMPDIR pinned to $DEV_TMPDIR (was ${TMPDIR:-<unset>}) so builds outlive the run that made them."
    export TMPDIR="$DEV_TMPDIR" TMP="$DEV_TMPDIR" TEMP="$DEV_TMPDIR"
  fi
  ok "node, go, curl found"

  if component_selected daemon; then
    local marker
    marker="$(daemon_task_marker)"
    [ -z "$marker" ] || die "The daemon component cannot start under a daemon-managed task.
This checkout sits below $marker, and the CLI refuses 'daemon start' there so a
task cannot spawn a second daemon competing for its own work.
Start the rest with 'make up C=api,web', or run 'make up C=daemon' from your own shell."
  fi

  step "Environment"
  ENV_FILE="$(detect_env_file)"
  if [ ! -f "$REPO_ROOT/$ENV_FILE" ]; then
    if [ "$ENV_FILE" = .env.worktree ]; then
      bash "$REPO_ROOT/scripts/init-worktree-env.sh" "$ENV_FILE" >/dev/null
    else
      cp "$REPO_ROOT/.env.example" "$REPO_ROOT/$ENV_FILE"
    fi
    info "Created $ENV_FILE"
  fi
  ensure_dev_code "$ENV_FILE"
  load_env_file "$ENV_FILE"

  acquire_lock
  trap release_lock EXIT
  local existing offset
  existing="$(env_name_for_dir "$REPO_ROOT" || true)"
  if [ -n "$existing" ] && [ -z "$name" ]; then
    name="$existing"
  fi

  if [ -n "$existing" ]; then
    load_manifest "$existing"
    NAME="$existing"
    bind_paths
    if [ "$lifecycle_requested" = 1 ]; then
      OWNER="$owner"
      TTL_HOURS="$ttl"
      EXPIRES_AT="$(expires_at_after_hours "$ttl")"
      save_manifest
    fi
    info "Reusing environment $NAME (ports $BACKEND_PORT/$FRONTEND_PORT, database $DB_NAME)"
  else
    offset=$((PORT - 18080))
    # A slot is adopted only when the registry does not hold it and both ports
    # are genuinely free; otherwise a fresh one is allocated and the env file is
    # rewritten, which is the whole point of allocating instead of computing.
    local candidate_renderer
    candidate_renderer="$(renderer_port_for_offset "$offset")"
    if [ "$PORT" -lt 18080 ] || [ "$FRONTEND_PORT" -ne $((13000 + offset)) ] \
      || offset_registered "$offset" || ! port_free "$PORT" \
      || ! port_free "$FRONTEND_PORT" || ! port_free "$candidate_renderer"; then
      if [ "$PORT" -ge 18080 ] && { offset_registered "$offset" || ! port_free "$PORT"; }; then
        warn "Slot $offset (port $PORT) is taken: $(describe_port_owner "$PORT"). Allocating another."
      fi
      offset="$(allocate_offset "$REPO_ROOT")" || die "No free slot left; run 'make gc' or 'make list'."
      local new_backend=$((18080 + offset)) new_frontend=$((13000 + offset))
      local new_db="multica_$(slugify "$(basename "$REPO_ROOT")")_${offset}"
      rewrite_env_ports "$ENV_FILE" "$offset" "$new_backend" "$new_frontend" "$new_db"
      load_env_file "$ENV_FILE"
      info "Allocated slot $offset — backend $new_backend, frontend $new_frontend, database $new_db"
    fi

    NAME="${name:-$(slugify "$(basename "$REPO_ROOT")")-${offset}}"
    require_env_name "$NAME"
    PROFILE="dev-$(slugify "$(basename "$REPO_ROOT")")-${offset}"
    WORKSPACES_ROOT="$DEV_WORKSPACES_PARENT/multica_workspaces_$PROFILE"
    DESKTOP_RENDERER_PORT="$(renderer_port_for_offset "$offset")"
    DESKTOP_APP_SUFFIX="$NAME"
    DESKTOP_USER_DATA_DIR="$(desktop_user_data_dir "$DESKTOP_APP_SUFFIX")"
    DESKTOP_ENV_FILE="$REPO_ROOT/apps/desktop/.env.development.local"
    [ ! -f "$(manifest_of "$NAME")" ] || die "Environment '$NAME' already exists for a different directory."
    mkdir -p "$(env_dir "$NAME")/logs"
    DIR="$REPO_ROOT"
    CREATED_AT="$(now_iso)"
    OWNER="$owner"
    TTL_HOURS="$ttl"
    EXPIRES_AT=""
    [ "$ttl" = 0 ] || EXPIRES_AT="$(expires_at_after_hours "$ttl")"
    OFFSET="$offset"
    BACKEND_PORT="$PORT"
    DB_NAME="$POSTGRES_DB"
    save_manifest
    load_manifest "$NAME"
    ok "Registered environment $NAME"
  fi
  release_lock
  trap - EXIT

  bind_paths
  # The manifest is the source of truth from here on; re-export so every child
  # sees the same values the registry recorded.
  export PORT="$BACKEND_PORT" FRONTEND_PORT DATABASE_URL POSTGRES_DB="$DB_NAME"

  if [ ! -d "$REPO_ROOT/node_modules" ] && { component_selected web || component_selected desktop; }; then
    step "Dependencies"
    (cd "$REPO_ROOT" && pnpm install) || die "pnpm install failed."
  fi

  step "Database"
  ensure_database
  migrate_database
  ok "$DB_NAME reachable through DATABASE_URL and migrated"

  step "Components: $COMPONENTS"
  component_selected api && start_api
  component_selected web && start_web
  component_selected daemon && start_daemon
  component_selected desktop && start_desktop

  print_handoff
}

cmd_down() {
  local name="" requested="$ALL_COMPONENTS"
  while [ $# -gt 0 ]; do
    case "$1" in
      --components|-c) requested="$(printf '%s' "$2" | tr ',' ' ')"; shift 2 ;;
      -*) die "Unknown flag for down: $1" ;;
      *) name="$1"; shift ;;
    esac
  done
  resolve_env_for_read "$name"
  export PORT="$BACKEND_PORT" FRONTEND_PORT DATABASE_URL POSTGRES_DB="$DB_NAME"

  step "Stopping $NAME: $requested"
  local comp
  for comp in $requested; do
    case " $ALL_COMPONENTS " in *" $comp "*) ;; *) die "Unknown component '$comp'. Valid: $ALL_COMPONENTS" ;; esac
    stop_component "$comp"
  done
  printf '\n%s✓ %s stopped.%s Database, profile and slot kept — `make up` restarts in seconds.\n' "$C_GREEN" "$NAME" "$C_OFF"
}

cmd_destroy() {
  local name="" assume_yes=0 reply admin_url failures=0
  local expected_workspaces expected_desktop_data
  while [ $# -gt 0 ]; do
    case "$1" in
      --yes|-y) assume_yes=1; shift ;;
      -*) die "Unknown flag for destroy: $1" ;;
      *) name="$1"; shift ;;
    esac
  done
  resolve_env_for_read "$name"

  if [ "$assume_yes" != 1 ]; then
    printf 'Destroy %s? This drops database %s and profile %s. [y/N] ' "$NAME" "$DB_NAME" "$PROFILE"
    read -r reply || reply=n
    case "$reply" in y|Y|yes|YES) ;; *) printf 'Cancelled.\n'; return 0 ;; esac
  fi

  export PORT="$BACKEND_PORT" FRONTEND_PORT DATABASE_URL POSTGRES_DB="$DB_NAME"
  step "Destroying $NAME"
  local comp
  for comp in $ALL_COMPONENTS; do
    if ! stop_component "$comp"; then failures=$((failures + 1)); fi
  done

  if command -v psql >/dev/null 2>&1; then
    admin_url="$(admin_database_url "$DATABASE_URL")"
    if PGCONNECT_TIMEOUT=3 psql "$admin_url" -tAc 'SELECT 1' >/dev/null 2>&1; then
      if psql "$admin_url" -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$DB_NAME\" WITH (FORCE)" >/dev/null; then
        ok "dropped database $DB_NAME"
      else
        warn "failed to drop database $DB_NAME; keeping its manifest"
        failures=$((failures + 1))
      fi
    else
      warn "Nothing answered on the database host; $DB_NAME was left in place."
      failures=$((failures + 1))
    fi
  else
    warn "psql not found; $DB_NAME was left in place."
    failures=$((failures + 1))
  fi

  if rm -rf "$PROFILE_DIR"; then
    ok "removed CLI profile $PROFILE"
  else
    warn "failed to remove CLI profile $PROFILE"
    failures=$((failures + 1))
  fi

  expected_workspaces="$DEV_WORKSPACES_PARENT/multica_workspaces_$PROFILE"
  if [ "$WORKSPACES_ROOT" != "$expected_workspaces" ]; then
    warn "refusing to remove unexpected workspaces root $WORKSPACES_ROOT (expected $expected_workspaces)"
    failures=$((failures + 1))
  elif rm -rf "$WORKSPACES_ROOT"; then
    ok "removed daemon workspaces $WORKSPACES_ROOT"
  else
    warn "failed to remove daemon workspaces $WORKSPACES_ROOT"
    failures=$((failures + 1))
  fi

  expected_desktop_data="$(desktop_user_data_dir "$DESKTOP_APP_SUFFIX")"
  if [ "$DESKTOP_USER_DATA_DIR" != "$expected_desktop_data" ]; then
    warn "refusing to remove unexpected Desktop userData $DESKTOP_USER_DATA_DIR"
    failures=$((failures + 1))
  elif rm -rf "$DESKTOP_USER_DATA_DIR"; then
    ok "removed Desktop userData $DESKTOP_USER_DATA_DIR"
  else
    warn "failed to remove Desktop userData $DESKTOP_USER_DATA_DIR"
    failures=$((failures + 1))
  fi

  if [ -f "$DESKTOP_ENV_FILE" ] \
    && grep -Fqx "# Managed by scripts/dev-env.sh for environment ${NAME}." "$DESKTOP_ENV_FILE"; then
    if rm -f "$DESKTOP_ENV_FILE"; then
      ok "removed managed Desktop env file"
    else
      warn "failed to remove $DESKTOP_ENV_FILE"
      failures=$((failures + 1))
    fi
  fi

  if [ "$failures" -ne 0 ]; then
    die "$NAME was only partially destroyed ($failures cleanup failure(s)). Its manifest and slot were kept so 'make destroy' can retry."
  fi

  rm -rf "$(env_dir "$NAME")"
  ok "released slot $OFFSET"
  printf '\n%s✓ %s destroyed.%s\n' "$C_GREEN" "$NAME" "$C_OFF"
}

cmd_status() {
  local name="" as_json=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --json) as_json=1; shift ;;
      -*) die "Unknown flag for status: $1" ;;
      *) name="$1"; shift ;;
    esac
  done
  resolve_env_for_read "$name"
  if [ "$as_json" = 1 ]; then print_status_json; else print_status_human; fi
}

cmd_list() {
  local as_json=0 name first=1 alive
  [ "${1:-}" = "--json" ] && as_json=1

  if [ "$as_json" = 1 ]; then printf '['; fi
  local printed=0
  while read -r name; do
    [ -n "$name" ] || continue
    (
      load_manifest "$name"
      bind_paths
      alive="$(component_state api)"
      alive="${alive%%|*}"
      if [ "$as_json" = 1 ]; then
        printf '{"name":"%s","dir":"%s","owner":"%s","api":"%s","backend_port":%s,"frontend_port":%s,"desktop_renderer_port":%s,"database":"%s","created_at":"%s","ttl_hours":%s,"expires_at":"%s"}' \
          "$(json_escape "$NAME")" "$(json_escape "$DIR")" "$(json_escape "$OWNER")" "$alive" \
          "$BACKEND_PORT" "$FRONTEND_PORT" "$DESKTOP_RENDERER_PORT" "$(json_escape "$DB_NAME")" "$(json_escape "$CREATED_AT")" "${TTL_HOURS:-0}" "$(json_escape "$EXPIRES_AT")"
      else
        printf '%-24s %-8s %-8s %-6s %-28s %s\n' "$NAME" "$alive" "$OWNER" \
          "$BACKEND_PORT" "$DB_NAME" "$( [ -d "$DIR" ] && printf '%s' "$DIR" || printf '%s(directory gone)%s' "$C_RED" "$C_OFF" )"
      fi
    ) | { if [ "$as_json" = 1 ] && [ "$printed" != 0 ]; then printf ','; fi; cat; }
    printed=1
    first=0
  done <<EOF
$(list_env_names)
EOF
  if [ "$as_json" = 1 ]; then
    printf ']\n'
  elif [ "$printed" = 0 ]; then
    printf 'No environments registered. Run `make up`.\n'
  fi
}

# Leaks become visible here rather than being discovered by counting databases
# in psql: an environment whose directory is gone, or whose TTL has passed, has
# no owner left to stop it.
cmd_gc() {
  local dry_run=0 automatic=0 name age_hours created_epoch expiry_epoch reason
  while [ $# -gt 0 ]; do
    case "$1" in
      --dry-run) dry_run=1 ;;
      --auto) automatic=1 ;;
      *) die "Unknown flag for gc: $1" ;;
    esac
    shift
  done

  while read -r name; do
    [ -n "$name" ] || continue
    (
      load_manifest "$name"
      reason=""
      [ -d "$DIR" ] || reason="its checkout directory is gone"
      if [ -z "$reason" ] && [ "${TTL_HOURS:-0}" != 0 ]; then
        if [ -n "${EXPIRES_AT:-}" ]; then
          expiry_epoch="$(node -e 'process.stdout.write(String(Math.floor(Date.parse(process.argv[1]) / 1000)))' "$EXPIRES_AT" 2>/dev/null || echo 0)"
          [ "$(now_epoch)" -lt "$expiry_epoch" ] || reason="it expired at $EXPIRES_AT"
        else
          created_epoch="$(node -e 'process.stdout.write(String(Math.floor(Date.parse(process.argv[1]) / 1000)))' "$CREATED_AT" 2>/dev/null || echo 0)"
          age_hours=$(( ($(now_epoch) - created_epoch) / 3600 ))
          [ "$age_hours" -lt "$TTL_HOURS" ] || reason="it expired ${age_hours}h after a ${TTL_HOURS}h ttl"
        fi
      fi
      [ -n "$reason" ] || exit 0
      if [ "$dry_run" = 1 ]; then
        printf '%s would be collected: %s\n' "$NAME" "$reason"
      else
        printf '%s: %s\n' "$NAME" "$reason"
        if ! bash "$REPO_ROOT/scripts/dev-env.sh" destroy "$NAME" --yes; then
          if [ "$automatic" = 1 ]; then
            warn "automatic cleanup of $NAME failed; its manifest was kept for retry"
          else
            exit 1
          fi
        fi
      fi
    )
  done <<EOF
$(list_env_names)
EOF
}

# Runs a command with this environment's variables, without the agent runtime's
# production MULTICA_* values and with a durable TMPDIR. Replaces the prefix
# people used to have to copy out of a document by hand.
cmd_exec() {
  local name=""
  if [ "${1:-}" != "--" ] && [ $# -gt 0 ]; then name="$1"; shift; fi
  [ "${1:-}" = "--" ] && shift
  [ $# -gt 0 ] || die "Usage: dev-env.sh exec [name] -- <command> [args...]"

  resolve_env_for_read "$name"
  mkdir -p "$DEV_TMPDIR"
  cd "$DIR"
  load_env_file "$ENV_FILE" "$DIR"
  export PORT="$BACKEND_PORT" FRONTEND_PORT DATABASE_URL POSTGRES_DB="$DB_NAME"
  export TMPDIR="$DEV_TMPDIR" TMP="$DEV_TMPDIR" TEMP="$DEV_TMPDIR"
  export MULTICA_DEV_PROFILE="$PROFILE"
  exec "${CLEAN_ENV[@]}" MULTICA_WORKSPACES_ROOT="$WORKSPACES_ROOT" "$@"
}

usage() {
  cat <<'EOF'
Local development environments: named, listable, deletable.

  dev-env.sh up      [--components api,web,daemon,desktop] [--all]
                     [--name N] [--ephemeral] [--ttl HOURS]
  dev-env.sh status  [name] [--json]
  dev-env.sh list    [--json]
  dev-env.sh down    [name] [--components ...]
  dev-env.sh destroy [name] [--yes]
  dev-env.sh gc      [--dry-run]
  dev-env.sh exec    [name] -- <command> [args...]

Components: api (Go backend), web (Next.js), daemon (agent daemon),
desktop (Electron). Anything selected implies api.

down keeps the database, the CLI profile and the allocated slot.
destroy consumes them.
EOF
}

main() {
  local verb="${1:-}"
  [ $# -gt 0 ] && shift || true
  case "$verb" in
    up) cmd_up "$@" ;;
    down) cmd_down "$@" ;;
    status) cmd_status "$@" ;;
    list|ls) cmd_list "$@" ;;
    destroy) cmd_destroy "$@" ;;
    gc) cmd_gc "$@" ;;
    exec) cmd_exec "$@" ;;
    ""|-h|--help|help) usage ;;
    *) usage >&2; exit 2 ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
