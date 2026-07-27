#!/usr/bin/env bash
#
# multica-stop.sh — stop the local self-hosted Multica stack.
#
# Order: local daemon -> server containers (postgres/backend/frontend). colima
# (the Docker engine) is left running by default because other projects may share
# it; pass --all to stop it too. Components that are already stopped are skipped,
# so the script is idempotent.
#
# Usage:
#   ./multica-stop.sh          # stop the daemon + server containers (keep colima)
#   ./multica-stop.sh --all    # also stop colima (the Docker engine)
#
set -euo pipefail

# ---- Configuration (mirrors multica-start.sh) ------------------------------
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="docker-compose.selfhost.yml"

# ---- Helpers ---------------------------------------------------------------
c_green() { printf '\033[32m%s\033[0m\n' "$*"; }
c_blue()  { printf '\033[34m%s\033[0m\n' "$*"; }
c_yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }
c_red()   { printf '\033[31m%s\033[0m\n' "$*"; }
step()    { c_blue "▶ $*"; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

STOP_ALL=false
[ "${1:-}" = "--all" ] && STOP_ALL=true

# ---- Preflight -------------------------------------------------------------
if [ ! -f "$REPO_DIR/$COMPOSE_FILE" ]; then
  c_red "✗ $REPO_DIR/$COMPOSE_FILE not found — is this script still at the repo root?"; exit 1
fi
cd "$REPO_DIR"

# ---- 1. Local daemon -------------------------------------------------------
step "Stopping the local daemon..."
if command -v multica >/dev/null 2>&1 && multica daemon status >/dev/null 2>&1; then
  multica daemon stop
  c_green "  ✓ Daemon stopped"
else
  c_yellow "  Daemon not running, skipping"
fi

# ---- 2. Server containers --------------------------------------------------
step "Stopping server containers..."
if docker info >/dev/null 2>&1; then
  running=$(compose ps --services --filter status=running 2>/dev/null | tr '\n' ' ')
  if [ -n "${running// /}" ]; then
    compose down
    c_green "  ✓ postgres / backend / frontend stopped (volumes kept)"
  else
    c_yellow "  Server containers not running, skipping"
  fi
else
  c_yellow "  Docker engine not running — treating server containers as stopped"
fi

# ---- 3. colima (optional) --------------------------------------------------
if $STOP_ALL; then
  step "Stopping colima (the Docker engine)..."
  if command -v colima >/dev/null 2>&1 && colima status >/dev/null 2>&1; then
    colima stop
    c_green "  ✓ colima stopped"
  else
    c_yellow "  colima not running, skipping"
  fi
else
  c_yellow "▶ Leaving colima (the Docker engine) running — re-run with --all to stop it too"
fi

echo
c_green "✓ Local Multica stack stopped"
