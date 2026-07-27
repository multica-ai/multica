#!/usr/bin/env bash
#
# multica-start.sh — bring up the local self-hosted Multica stack.
#
# Order: colima (Docker engine) -> server containers (postgres/backend/frontend)
# -> local daemon. Every step checks whether its component is already running and
# skips it, so the script is idempotent and safe to re-run.
#
# Usage:
#   ./multica-start.sh            # start everything
#   ./multica-start.sh --code     # start, then print the latest login code from the backend log
#   ./multica-start.sh --logs     # start, then follow the backend log
#
set -euo pipefail

# ---- Configuration ---------------------------------------------------------
# The repo is the directory this script lives in, so a clone anywhere works
# without editing anything. Resolve it before any `cd` so a relative invocation
# (./multica-start.sh) and an absolute one behave the same.
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="docker-compose.selfhost.yml"
COLIMA_CPU=2
COLIMA_MEM=4
FRONTEND_URL="http://localhost:3000"
BACKEND_URL="http://localhost:8080"

# ---- Helpers ---------------------------------------------------------------
c_green() { printf '\033[32m%s\033[0m\n' "$*"; }
c_blue()  { printf '\033[34m%s\033[0m\n' "$*"; }
c_yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }
c_red()   { printf '\033[31m%s\033[0m\n' "$*"; }
step()    { c_blue "▶ $*"; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

# ---- Preflight -------------------------------------------------------------
for bin in colima docker multica; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    c_red "✗ Missing command: $bin — install it first"; exit 1
  fi
done
if ! docker compose version >/dev/null 2>&1; then
  c_red "✗ 'docker compose' is unavailable — install the Docker Compose plugin"; exit 1
fi
if [ ! -f "$REPO_DIR/$COMPOSE_FILE" ]; then
  c_red "✗ $REPO_DIR/$COMPOSE_FILE not found — is this script still at the repo root?"; exit 1
fi
cd "$REPO_DIR"

# ---- 1. colima (Docker engine) ---------------------------------------------
step "Checking the Docker engine (colima)..."
if docker info >/dev/null 2>&1; then
  c_green "  ✓ Docker daemon already running"
else
  c_yellow "  colima is not running, starting it (${COLIMA_CPU}C/${COLIMA_MEM}G)..."
  colima start --cpu "$COLIMA_CPU" --memory "$COLIMA_MEM"
  # Wait for the daemon to answer (up to 60s).
  for i in $(seq 1 30); do
    docker info >/dev/null 2>&1 && break
    sleep 2
  done
  docker info >/dev/null 2>&1 || { c_red "✗ Docker daemon did not come up in time"; exit 1; }
  c_green "  ✓ colima ready"
fi

# ---- 2. Server containers --------------------------------------------------
step "Checking server containers..."
running=$(compose ps --services --filter status=running 2>/dev/null | sort -u | tr '\n' ' ')
if echo "$running" | grep -q postgres && echo "$running" | grep -q backend && echo "$running" | grep -q frontend; then
  c_green "  ✓ postgres / backend / frontend all running"
else
  c_yellow "  Starting the server stack (docker compose up -d)..."
  compose up -d
fi

# Wait for the backend to answer (up to 60s).
step "Waiting for the backend..."
for i in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BACKEND_URL/api/config" 2>/dev/null || echo 000)
  [ "$code" = "200" ] && break
  sleep 2
done
if [ "${code:-000}" = "200" ]; then
  c_green "  ✓ Backend ready ($BACKEND_URL)"
else
  # Do not guess the cause — a migration in progress, a DB auth failure and a
  # port conflict all look identical from out here. Show the log instead.
  c_red "  ✗ Backend did not return 200 within 60s. Last log lines:"
  compose logs backend --tail 8 2>&1 | sed 's/^/    /' || true
fi

# ---- 3. Local daemon -------------------------------------------------------
step "Checking the local daemon..."
# `multica daemon status` exits 0 even when the daemon is stopped, so the
# output has to be matched — the exit code carries no signal here.
if multica daemon status 2>/dev/null | grep -q 'running'; then
  c_green "  ✓ Daemon already running"
else
  c_yellow "  Starting the daemon..."
  multica daemon start
fi

# ---- Summary ---------------------------------------------------------------
echo
c_green "✓ Local Multica stack is up"
echo "  Frontend: $FRONTEND_URL"
echo "  Backend:  $BACKEND_URL"
multica daemon status 2>/dev/null | sed 's/^/  /' || true

# ---- Optional: print the login code / follow logs --------------------------
case "${1:-}" in
  --code)
    echo
    step "Latest login code (from the backend log):"
    logs=$(compose logs backend 2>&1) || true
    if printf '%s' "$logs" | grep -q 'error from daemon in stream'; then
      # When the Docker data disk hits an I/O error the log file cannot be read
      # at all. That is a different failure from "no code has been sent yet" and
      # must be reported separately, otherwise it reads as the latter.
      c_red "  ✗ Could not read the backend log — Docker data disk error:"
      printf '%s\n' "$logs" | grep 'error from daemon in stream' | tail -1 | sed 's/^/    /'
      c_yellow "  Recover with: ./multica-stop.sh; colima stop; colima start; ./multica-start.sh"
    elif code_line=$(printf '%s\n' "$logs" | grep '\[DEV\] Verification' | tail -1) \
      && [ -n "$code_line" ]; then
      printf '%s\n' "$code_line" | sed 's/^/  /'
    else
      c_yellow "  No code yet — enter your email at $FRONTEND_URL to trigger one, then re-run --code"
    fi
    ;;
  --logs)
    echo; step "Following the backend log (Ctrl-C to exit)..."
    compose logs -f backend
    ;;
esac
