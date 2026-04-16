#!/usr/bin/env bash
# Local dev orchestrator — backend+frontend in Docker, Postgres reused from the
# host's unitesync-postgres container, daemon runs natively on the host.
#
# Usage: ./scripts/local.sh <up|down|status|logs|rebuild|migrate|restart>

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="docker-compose.local.yml"
PG_CONTAINER="${PG_CONTAINER:-unitesync-postgres}"
ENV_FILE="${ENV_FILE:-.env}"
DAEMON_PROFILE="${DAEMON_PROFILE:-local}"

load_env() {
  if [ ! -f "$ENV_FILE" ]; then
    echo "✗ Missing env file: $ENV_FILE"
    exit 1
  fi
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
}

ensure_postgres() {
  if ! docker ps --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
    if docker ps -a --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
      echo "==> Starting existing $PG_CONTAINER..."
      docker start "$PG_CONTAINER" >/dev/null
    else
      echo "✗ Postgres container '$PG_CONTAINER' not found."
      echo "  Start your own Postgres first, then re-run this script."
      exit 1
    fi
  fi
  echo "✓ Postgres ($PG_CONTAINER) running on :5432"
}

wait_for_backend() {
  echo -n "==> Waiting for backend..."
  for _ in $(seq 1 30); do
    if curl -sf http://localhost:"${PORT:-8080}"/health >/dev/null 2>&1; then
      echo " ready."
      return 0
    fi
    echo -n "."
    sleep 1
  done
  echo
  echo "  (backend didn't respond in 30s — check: ./scripts/local.sh logs)"
}

start_daemon() {
  echo "==> Starting daemon (profile: $DAEMON_PROFILE)..."
  ( cd server && go run ./cmd/multica daemon restart --profile "$DAEMON_PROFILE" )
}

stop_daemon() {
  echo "==> Stopping daemon..."
  ( cd server && go run ./cmd/multica daemon stop --profile "$DAEMON_PROFILE" ) || true
}

cmd_up() {
  load_env
  ensure_postgres
  echo "==> Starting backend + frontend..."
  docker compose -f "$COMPOSE_FILE" up -d
  wait_for_backend
  start_daemon
  echo
  echo "✓ Multica is up."
  echo "  Frontend: http://localhost:${FRONTEND_PORT:-3000}"
  echo "  Backend:  http://localhost:${PORT:-8080}"
  echo "  Daemon log: ~/.multica/profiles/$DAEMON_PROFILE/daemon.log"
}

cmd_down() {
  stop_daemon
  echo "==> Stopping backend + frontend..."
  docker compose -f "$COMPOSE_FILE" down
  echo "✓ Stopped. ($PG_CONTAINER still running — stop it manually if you want.)"
}

cmd_restart() {
  cmd_down
  cmd_up
}

cmd_status() {
  load_env
  echo "=== Containers ==="
  docker compose -f "$COMPOSE_FILE" ps || true
  echo
  echo "=== Postgres ==="
  if docker ps --format '{{.Names}}' | grep -qx "$PG_CONTAINER"; then
    echo "✓ $PG_CONTAINER running"
  else
    echo "✗ $PG_CONTAINER NOT running"
  fi
  echo
  echo "=== Health ==="
  if curl -sf http://localhost:"${PORT:-8080}"/health >/dev/null 2>&1; then
    echo "✓ backend :${PORT:-8080} healthy"
  else
    echo "✗ backend :${PORT:-8080} not responding"
  fi
  if curl -sf -o /dev/null -w "%{http_code}" http://localhost:"${FRONTEND_PORT:-3000}" | grep -q "^2\|^3"; then
    echo "✓ frontend :${FRONTEND_PORT:-3000} responding"
  else
    echo "✗ frontend :${FRONTEND_PORT:-3000} not responding"
  fi
  echo
  echo "=== Daemon ==="
  ( cd server && go run ./cmd/multica daemon status --profile "$DAEMON_PROFILE" ) || true
}

cmd_logs() {
  docker compose -f "$COMPOSE_FILE" logs -f --tail=100 "${1:-backend}"
}

cmd_rebuild() {
  load_env
  ensure_postgres
  echo "==> Rebuilding backend + frontend images..."
  docker compose -f "$COMPOSE_FILE" up -d --build --force-recreate
  wait_for_backend
  start_daemon
  echo "✓ Rebuilt and running."
}

cmd_migrate() {
  load_env
  ensure_postgres
  echo "==> Running migrations..."
  ( cd server && go run ./cmd/migrate up )
  if docker compose -f "$COMPOSE_FILE" ps --services --filter "status=running" | grep -qx backend; then
    echo "==> Restarting backend to pick up schema changes..."
    docker compose -f "$COMPOSE_FILE" restart backend
  fi
  echo "✓ Migrations applied."
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  up        Start Postgres (if stopped) + backend/frontend + daemon
  down      Stop daemon + backend/frontend (Postgres left running)
  restart   down then up
  status    Show container, health, and daemon status
  logs [s]  Tail logs for a compose service (default: backend)
  rebuild   Rebuild backend+frontend images, then up
  migrate   Apply DB migrations + restart backend

Env overrides:
  PG_CONTAINER=$PG_CONTAINER   (host Postgres container name)
  ENV_FILE=$ENV_FILE           (env file to source)
  DAEMON_PROFILE=$DAEMON_PROFILE   (multica daemon profile)
EOF
}

case "${1:-}" in
  up)      cmd_up ;;
  down)    cmd_down ;;
  restart) cmd_restart ;;
  status)  cmd_status ;;
  logs)    shift; cmd_logs "${1:-backend}" ;;
  rebuild) cmd_rebuild ;;
  migrate) cmd_migrate ;;
  ""|help|-h|--help) usage ;;
  *) echo "Unknown command: $1"; echo; usage; exit 1 ;;
esac
