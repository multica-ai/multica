#!/usr/bin/env bash
# One command from a clean checkout to a logged-in, daemon-attached environment.
#
# Replaces the ~12-step manual sequence (generate env → create DB → start →
# wait → set verification code → restart → send-code → verify-code → PAT →
# workspace → CLI profile → daemon), where getting the order wrong means
# backtracking and some steps are not retryable. Everything here is mechanical,
# so it belongs in a script rather than in a human's or an agent's head.
#
#   make dev-bootstrap        # start
#   make dev-bootstrap-stop   # stop what it started
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

DEV_EMAIL="${MULTICA_DEV_EMAIL:-dev@localhost}"
WORKSPACE_NAME="${MULTICA_DEV_WORKSPACE_NAME:-Dev}"
WORKSPACE_SLUG="${MULTICA_DEV_WORKSPACE_SLUG:-dev}"

# The agent runtime exports these pointing at PRODUCTION, and MULTICA_SERVER_URL
# silently outranks server_url in a saved profile config. Every long-lived child
# below is launched without them so a local daemon cannot end up authenticating
# its local token against the production API (which fails as a bare 401 and
# looks like a product bug).
CLEAN_ENV=(env
  -u MULTICA_SERVER_URL -u MULTICA_TOKEN -u MULTICA_WORKSPACE_ID
  -u MULTICA_DAEMON_PORT -u MULTICA_AGENT_ID -u MULTICA_AGENT_NAME
  -u MULTICA_TASK_ID -u MULTICA_TASK_SLOT)

usage() {
  cat <<'EOF'
Usage: scripts/dev-bootstrap.sh [--stop]

  (no flags)  Bring up backend, frontend, database, login, workspace, and daemon.
  --stop      Stop the backend, frontend, and daemon this script started.

The database, the CLI profile, and the created workspace are left alone by
--stop; they are what make a re-run fast.
EOF
}

MODE=start
case "${1:-}" in
  "") ;;
  --stop) MODE=stop ;;
  -h | --help)
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
info() { printf '    %s\n' "$1"; }
die() {
  printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2
  exit 1
}

# ---------- Durable TMPDIR ----------
# An agent runs with TMPDIR=/tmp/multica-task-<id>, which is deleted when the run
# ends. Anything the Go toolchain builds there vanishes with it, so an
# environment handed over by an agent outlives its creator only if TMPDIR does.
# Humans inherit a fine TMPDIR already; pointing both at the same durable path
# means neither has to know this exists.
DEV_TMPDIR="${MULTICA_DEV_TMPDIR:-$HOME/.multica/dev-tmp}"
mkdir -p "$DEV_TMPDIR"
if [ "${TMPDIR:-}" != "$DEV_TMPDIR" ]; then
  PREVIOUS_TMPDIR="${TMPDIR:-<unset>}"
  export TMPDIR="$DEV_TMPDIR"
  export TMP="$DEV_TMPDIR" TEMP="$DEV_TMPDIR"
fi

# ---------- Identity: env file, profile, ports, logs ----------
if [ -f .git ] && [ ! -d .git ]; then
  ENV_FILE=".env.worktree"
  CHECKOUT_KIND="worktree"
  STOP_TARGET="make stop-worktree"
else
  ENV_FILE=".env"
  CHECKOUT_KIND="main checkout"
  STOP_TARGET="make stop"
fi

# Same derivation as scripts/init-worktree-env.sh, so the profile lines up with
# the ports and database that file allocates for this directory.
SLUG="$(basename "$PWD" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/_/g; s/__*/_/g; s/^_//; s/_$//')"
[ -n "$SLUG" ] || SLUG="multica"
OFFSET=$(($(printf '%s' "$PWD" | cksum | awk '{print $1}') % 1000))
PROFILE="dev-${SLUG}-${OFFSET}"

PROFILE_DIR="$HOME/.multica/profiles/$PROFILE"
DEV_LOG="$PROFILE_DIR/dev.log"
DEV_PID_FILE="$PROFILE_DIR/dev.pid"
DAEMON_LOG="$PROFILE_DIR/daemon.log"
MULTICA_BIN="server/bin/multica"

load_env() {
  [ -f "$ENV_FILE" ] || die "Missing $ENV_FILE — run scripts/dev-bootstrap.sh without --stop first."
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
  # shellcheck disable=SC1091
  . scripts/local-env.sh
  SERVER="http://localhost:${PORT}"
  FRONTEND="http://localhost:${FRONTEND_PORT}"
}

# ---------- Stop mode ----------
if [ "$MODE" = stop ]; then
  load_env
  step "Stopping the daemon [$PROFILE]"
  if [ -x "$MULTICA_BIN" ]; then
    "${CLEAN_ENV[@]}" "$MULTICA_BIN" daemon stop --profile "$PROFILE" 2>&1 | sed 's/^/    /' || true
  else
    info "$MULTICA_BIN not built; skipping (nothing this script started can be running)."
  fi

  step "Stopping backend and frontend"
  if [ -f "$DEV_PID_FILE" ]; then
    dev_pid="$(cat "$DEV_PID_FILE")"
    if kill -0 "$dev_pid" 2> /dev/null; then
      # Negative PID targets the whole process group the launcher created, so
      # `make` → dev.sh → go/pnpm all go down together.
      kill -TERM -"$dev_pid" 2> /dev/null || kill -TERM "$dev_pid" 2> /dev/null || true
      sleep 1
    fi
    rm -f "$DEV_PID_FILE"
  fi
  bash scripts/port-guard.sh stop "$PORT" backend
  bash scripts/port-guard.sh stop "$FRONTEND_PORT" frontend

  printf '\n\033[32m✓ Stopped.\033[0m PostgreSQL, the database, and the CLI profile were left in place.\n'
  info "Full reset: make db-reset && rm -rf $PROFILE_DIR"
  exit 0
fi

# ---------- 1. Prerequisites ----------
step "1/9  Prerequisites"
missing=()
for tool in node pnpm go docker curl; do
  command -v "$tool" > /dev/null 2>&1 || missing+=("$tool")
done
[ ${#missing[@]} -eq 0 ] || die "Missing prerequisites: ${missing[*]} (need Node.js v20+, pnpm v10.28+, Go v1.26+, Docker, curl)"
info "node, pnpm, go, docker, curl found."
if [ -n "${PREVIOUS_TMPDIR:-}" ]; then
  info "TMPDIR pinned to $TMPDIR (was $PREVIOUS_TMPDIR)."
fi

# JSON reads go through node rather than jq: node is already a hard requirement,
# jq is not.
json_field() {
  node -e '
    let payload;
    try { payload = JSON.parse(process.argv[1]); } catch { process.exit(1); }
    const value = process.argv[2].split(".").reduce((acc, key) => (acc == null ? acc : acc[key]), payload);
    if (value === undefined || value === null || value === "") process.exit(1);
    process.stdout.write(String(value));
  ' "$1" "$2" 2> /dev/null
}

# ---------- 2. Environment file ----------
step "2/9  Environment file"
if [ ! -f "$ENV_FILE" ]; then
  if [ "$CHECKOUT_KIND" = worktree ]; then
    info "Worktree detected — generating $ENV_FILE with unique ports and database."
    bash scripts/init-worktree-env.sh "$ENV_FILE" > /dev/null
  else
    info "Creating $ENV_FILE from .env.example."
    cp .env.example "$ENV_FILE"
  fi
fi

# The fixed verification code has to be in place BEFORE the backend starts:
# the handler reads the env var at request time, but the process only loads the
# file once. Setting it here is what removes the "start, edit, restart" detour
# from the manual flow.
if ! grep -qE '^MULTICA_DEV_VERIFICATION_CODE=[0-9]{6}$' "$ENV_FILE"; then
  if grep -q '^MULTICA_DEV_VERIFICATION_CODE=' "$ENV_FILE"; then
    tmp_env="$(mktemp)"
    sed 's/^MULTICA_DEV_VERIFICATION_CODE=.*/MULTICA_DEV_VERIFICATION_CODE=888888/' "$ENV_FILE" > "$tmp_env"
    mv "$tmp_env" "$ENV_FILE"
  else
    printf '\nMULTICA_DEV_VERIFICATION_CODE=888888\n' >> "$ENV_FILE"
  fi
  info "Set MULTICA_DEV_VERIFICATION_CODE=888888 (ignored when APP_ENV=production)."
fi

load_env
DEV_CODE="${MULTICA_DEV_VERIFICATION_CODE:-888888}"
info "$ENV_FILE  ·  backend :$PORT  ·  frontend :$FRONTEND_PORT  ·  db ${POSTGRES_DB}"

# ---------- 3. Ports ----------
step "3/9  Ports"
bash scripts/port-guard.sh require-free "$PORT" backend \
  || die "Backend port $PORT is busy. Run 'make dev-bootstrap-stop' first."
bash scripts/port-guard.sh require-free "$FRONTEND_PORT" frontend \
  || die "Frontend port $FRONTEND_PORT is busy. Run 'make dev-bootstrap-stop' first."
info "Ports $PORT and $FRONTEND_PORT are free."

# ---------- 4. Database ----------
step "4/9  Database"
ADMIN_URL=""
if command -v psql > /dev/null 2>&1 && [ -n "${DATABASE_URL:-}" ]; then
  ADMIN_URL="$(node -e '
    const url = new URL(process.argv[1]);
    url.pathname = "/postgres";
    process.stdout.write(url.toString());
  ' "$DATABASE_URL")"
fi

# Create the database against DATABASE_URL, not `docker exec`. When a native
# PostgreSQL already owns 5432 the container never binds the host port, so a
# docker-exec create lands in a server the backend never talks to and migrations
# then die with `database "..." does not exist`. Talking to whoever answers on
# the port is correct either way.
if [ -n "$ADMIN_URL" ] && psql "$ADMIN_URL" -tAc 'SELECT 1' > /dev/null 2>&1; then
  info "PostgreSQL already answering on ${POSTGRES_PORT:-5432}; using it directly."
else
  bash scripts/ensure-postgres.sh "$ENV_FILE" | sed 's/^/    /'
fi
if [ -n "$ADMIN_URL" ] && psql "$ADMIN_URL" -tAc 'SELECT 1' > /dev/null 2>&1; then
  if ! psql "$ADMIN_URL" -tAc "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" | grep -q 1; then
    psql "$ADMIN_URL" -c "CREATE DATABASE \"${POSTGRES_DB}\"" > /dev/null
    info "Created database ${POSTGRES_DB}."
  fi
fi
info "Database ${POSTGRES_DB} ready."

# ---------- 5. Backend + frontend ----------
step "5/9  Backend and frontend"
mkdir -p "$PROFILE_DIR"
LAUNCHED_AT="$(node -e 'process.stdout.write(String(Math.floor(Date.now() / 1000)))')"
: > "$DEV_LOG"

# `set -m` puts the launcher in its own process group, so --stop can take the
# whole tree down with one signal and dev.sh's own `trap 'kill 0'` can never
# reach back into the caller's shell.
(
  set -m
  nohup "${CLEAN_ENV[@]}" make dev > "$DEV_LOG" 2>&1 < /dev/null &
  printf '%s\n' "$!" > "$DEV_PID_FILE"
)
DEV_PID="$(cat "$DEV_PID_FILE")"
info "Launched (pid $DEV_PID) — installing deps and running migrations, this is the slow part."
info "Log: $DEV_LOG"

wait_for_backend() {
  local waited=0
  while [ "$waited" -lt 300 ]; do
    if curl -sf "$SERVER/health" > /dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$DEV_PID" 2> /dev/null; then
      return 1
    fi
    sleep 2
    waited=$((waited + 2))
  done
  return 1
}

if ! wait_for_backend; then
  tail -30 "$DEV_LOG" | sed 's/^/    /' >&2 || true
  die "Backend never became healthy. Full log: $DEV_LOG"
fi

# Prove the answering process is the one we just started. This is what /health's
# process identity is for: without it, a stale instance on the same port answers
# 200 and the whole bootstrap silently configures the wrong server.
HEALTH="$(curl -sf "$SERVER/health")"
HEALTH_STARTED_AT="$(json_field "$HEALTH" started_at || true)"
if [ -n "$HEALTH_STARTED_AT" ]; then
  health_epoch="$(node -e 'process.stdout.write(String(Math.floor(Date.parse(process.argv[1]) / 1000)))' "$HEALTH_STARTED_AT")"
  if [ "$health_epoch" -lt $((LAUNCHED_AT - 5)) ]; then
    die "Something else is serving :$PORT — it started at $HEALTH_STARTED_AT, before this run. Stop it and retry."
  fi
fi
info "Backend healthy at $SERVER (started $HEALTH_STARTED_AT)."

# ---------- 6. Login ----------
# send-code once, verify-code once: repeated verify attempts lock the code out
# and start returning 400 even when it is correct, so a retry loop here is
# self-defeating.
step "6/9  Login as $DEV_EMAIL"
curl -sf -X POST "$SERVER/auth/send-code" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${DEV_EMAIL}\"}" > /dev/null \
  || die "send-code failed. Is MULTICA_DEV_VERIFICATION_CODE set and APP_ENV non-production?"

VERIFY_RESPONSE="$(curl -sS -X POST "$SERVER/auth/verify-code" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${DEV_EMAIL}\",\"code\":\"${DEV_CODE}\"}")"
JWT="$(json_field "$VERIFY_RESPONSE" token || true)"
[ -n "$JWT" ] || die "verify-code failed: $VERIFY_RESPONSE
Do not retry immediately — repeated attempts lock the code. Wait ~40s and re-run."

PAT_RESPONSE="$(curl -sS -X POST "$SERVER/api/tokens" \
  -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' \
  -d '{"name":"dev-bootstrap","expires_in_days":365}')"
PAT="$(json_field "$PAT_RESPONSE" token || true)"
[ -n "$PAT" ] || die "Personal access token creation failed: $PAT_RESPONSE"
info "Logged in and minted a personal access token."

# ---------- 7. Workspace ----------
step "7/9  Workspace"
WS_RESPONSE="$(curl -sS -X POST "$SERVER/api/workspaces" \
  -H "Authorization: Bearer $PAT" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"${WORKSPACE_NAME}\",\"slug\":\"${WORKSPACE_SLUG}\"}")"
WS="$(json_field "$WS_RESPONSE" id || true)"
if [ -z "$WS" ]; then
  # Re-run against an existing environment: the slug is already taken by the
  # workspace this script created earlier.
  WS="$(node -e '
    let list;
    try { list = JSON.parse(process.argv[1]); } catch { process.exit(1); }
    const match = (Array.isArray(list) ? list : []).find(ws => ws.slug === process.argv[2]);
    if (!match) process.exit(1);
    process.stdout.write(match.id);
  ' "$(curl -sS "$SERVER/api/workspaces" -H "Authorization: Bearer $PAT")" "$WORKSPACE_SLUG" 2> /dev/null || true)"
fi
[ -n "$WS" ] || die "Workspace creation failed: $WS_RESPONSE"

# A fresh user has onboarded_at = NULL, so a browser login is bounced to
# /onboarding. Flipping it here means the printed URL actually lands in the app.
curl -sS -X POST "$SERVER/api/me/onboarding/complete" \
  -H "Authorization: Bearer $PAT" -H "X-Workspace-ID: $WS" \
  -H 'Content-Type: application/json' -d '{"exit":"existing"}' > /dev/null || true

info "Workspace '${WORKSPACE_NAME}' (${WS})."

# ---------- 8. CLI profile ----------
step "8/9  CLI profile [$PROFILE]"
mkdir -p "$PROFILE_DIR"
cat > "$PROFILE_DIR/config.json" << EOF
{
  "server_url": "$SERVER",
  "app_url": "$FRONTEND",
  "token": "$PAT",
  "workspace_id": "$WS"
}
EOF
chmod 600 "$PROFILE_DIR/config.json"
info "Wrote $PROFILE_DIR/config.json"

# ---------- 9. Daemon ----------
# Built, never `go run`: the daemon records its own executable path and re-execs
# it for every task, and `go run` deletes that binary when the launcher exits.
step "9/9  Daemon"
info "Building $MULTICA_BIN (a go run daemon would fail every task later)."
make multica-bin > /dev/null
# A non-zero exit here is not automatically fatal (an already-running daemon
# reports "already running"); the status probe below is the real gate.
"${CLEAN_ENV[@]}" "$MULTICA_BIN" daemon start --profile "$PROFILE" 2>&1 | sed 's/^/    /' || true

RUNTIME_STATUS="$("${CLEAN_ENV[@]}" "$MULTICA_BIN" daemon status --profile "$PROFILE" --output json 2>/dev/null || true)"
DAEMON_STATE="$(json_field "$RUNTIME_STATUS" status || echo unknown)"
if [ "$DAEMON_STATE" != running ]; then
  die "Daemon is '$DAEMON_STATE' after start. Log: $DAEMON_LOG"
fi

# ---------- Summary ----------
# There is no workspace index route, so point at a real surface: a bare
# /<slug> 404s.
APP_URL="$FRONTEND/$WORKSPACE_SLUG/issues"

cat << EOF

$(printf '\033[32m✓ Environment ready.\033[0m')

  Open        $APP_URL
  Sign in     ${DEV_EMAIL}  ·  code ${DEV_CODE}
  Backend     $SERVER   (GET /health reports pid + started_at + commit)

  Profile     $PROFILE
  CLI         $MULTICA_BIN --profile $PROFILE <command>
  Logs        $DEV_LOG
              $DAEMON_LOG

  Stop        make dev-bootstrap-stop
              ($STOP_TARGET stops only backend + frontend, leaving the daemon up)
EOF
