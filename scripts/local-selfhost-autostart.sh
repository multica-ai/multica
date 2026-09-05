#!/usr/bin/env bash
# Start local Multica self-host stack after login: Colima → Compose → daemon.
# Intended for macOS LaunchAgent (com.multica.selfhost).
set -euo pipefail

export PATH="/usr/bin:/bin:/usr/sbin:/sbin:/Users/zhenhuachen/.homebrew/bin:$HOME/.local/bin:$PATH"

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="$HOME/.multica"
LOG="$LOG_DIR/autostart.log"
API="${MULTICA_SERVER_URL:-http://localhost:8081}"
COMPOSE=(docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.override.yml)

mkdir -p "$LOG_DIR"
exec >>"$LOG" 2>&1

ts() { date '+%Y-%m-%d %H:%M:%S'; }
log() { echo "[$(ts)] $*"; }

log "==== Multica selfhost autostart begin ===="

# 1) Colima
if ! colima status 2>/dev/null | grep -qi 'Running'; then
  log "starting Colima..."
  colima start
else
  log "Colima already running"
fi

# Wait until docker responds
for i in $(seq 1 60); do
  if docker info >/dev/null 2>&1; then
    log "Docker ready (attempt $i)"
    break
  fi
  if [ "$i" -eq 60 ]; then
    log "ERROR: Docker not ready after Colima start"
    exit 1
  fi
  sleep 2
done

# 2) Compose stack
cd "$REPO"
log "bringing up compose..."
"${COMPOSE[@]}" up -d

# Wait for API
for i in $(seq 1 60); do
  if curl -fsS "$API/readyz" >/dev/null 2>&1; then
    log "API ready (attempt $i)"
    break
  fi
  if [ "$i" -eq 60 ]; then
    log "ERROR: API $API/readyz not ready"
    exit 1
  fi
  sleep 2
done

# 3) Daemon
if multica daemon status 2>/dev/null | grep -qi '^Daemon:[[:space:]]*running'; then
  log "daemon already running"
else
  log "starting multica daemon..."
  multica daemon start
fi

log "status:"
multica daemon status || true
curl -fsS "$API/readyz" || true
echo

# 4) CEO workbench — Feishu site-factory intake (127.0.0.1:9477)
WB="http://127.0.0.1:9477/api/health"
if curl -fsS --max-time 2 "$WB" >/dev/null 2>&1; then
  log "CEO workbench already running"
else
  log "starting CEO workbench..."
  CEO_WORKBENCH_OPEN_BROWSER=0 nohup bash "$REPO/scripts/ai-company/ceo-workbench.sh" >>"$LOG_DIR/ceo-workbench.log" 2>&1 &
  for i in $(seq 1 15); do
    if curl -fsS --max-time 2 "$WB" >/dev/null 2>&1; then
      log "CEO workbench ready (attempt $i)"
      break
    fi
    sleep 1
  done
fi

log "==== Multica selfhost autostart done ===="
