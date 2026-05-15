#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

missing=()
command -v node >/dev/null 2>&1 || missing+=("node")
command -v pnpm >/dev/null 2>&1 || missing+=("pnpm")
command -v go >/dev/null 2>&1 || missing+=("go")
command -v docker >/dev/null 2>&1 || missing+=("docker")

if [ ${#missing[@]} -gt 0 ]; then
  echo "✗ Missing prerequisites: ${missing[*]}"
  echo "  Please install: Node.js v20+, pnpm v10.28+, Go v1.26+, Docker"
  exit 1
fi

if [ -f .git ]; then
  ENV_FILE=".env.worktree"
  if [ ! -f "$ENV_FILE" ]; then
    echo "==> Worktree detected. Generating $ENV_FILE..."
    bash scripts/init-worktree-env.sh "$ENV_FILE"
  fi
else
  ENV_FILE=".env"
  if [ ! -f "$ENV_FILE" ]; then
    echo "==> Creating $ENV_FILE from .env.example..."
    cp .env.example "$ENV_FILE"
  fi
fi

echo "==> Using $ENV_FILE"

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [ ! -d node_modules ]; then
  echo "==> Installing dependencies..."
  pnpm install
fi

bash scripts/ensure-postgres.sh "$ENV_FILE"

echo "==> Running migrations..."
(cd server && go run ./cmd/migrate up)

echo "==> Building CLI..."
make build-cli

echo "✓ CLI ready at $REPO_ROOT/server/bin/multica"

echo ""
echo "✓ Ready. Starting services..."
echo "  CLI:      $REPO_ROOT/server/bin/multica"
echo "  Backend:  http://localhost:${PORT:-8080}"
echo "  Frontend: http://localhost:${FRONTEND_PORT:-3000}"
echo ""

BACKEND_PID=""
FRONTEND_PID=""
STARTED_BACKEND=false
STARTED_FRONTEND=false
EXIT_CODE=0

cleanup() {
  local exit_code=${1:-$EXIT_CODE}
  trap - EXIT INT TERM
  echo ""
  if [ "$STARTED_BACKEND" = true ] && [ -n "$BACKEND_PID" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" 2>/dev/null || true
    echo "Stopped backend (PID $BACKEND_PID)"
  fi
  if [ "$STARTED_FRONTEND" = true ] && [ -n "$FRONTEND_PID" ]; then
    kill "$FRONTEND_PID" 2>/dev/null || true
    wait "$FRONTEND_PID" 2>/dev/null || true
    echo "Stopped frontend (PID $FRONTEND_PID)"
  fi
  exit "$exit_code"
}

handle_signal() {
  EXIT_CODE=130
  cleanup "$EXIT_CODE"
}

trap 'cleanup "$EXIT_CODE"' EXIT
trap handle_signal INT TERM

(cd server && go run ./cmd/server) &
BACKEND_PID=$!
STARTED_BACKEND=true

pnpm dev:web &
FRONTEND_PID=$!
STARTED_FRONTEND=true

set +e
while true; do
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    wait "$BACKEND_PID"
    EXIT_CODE=$?
    break
  fi
  if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
    wait "$FRONTEND_PID"
    EXIT_CODE=$?
    break
  fi
  sleep 1
done
set -e

cleanup "$EXIT_CODE"
