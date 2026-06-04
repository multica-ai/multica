#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# ---------- Check prerequisites ----------
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

# ---------- Environment file ----------
if [ -f .git ]; then
  # Inside a git worktree (.git is a file, not a directory)
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

# shellcheck disable=SC1091
. scripts/local-env.sh

# Ensure the Next.js rewrite proxy targets the local Go server, not a
# container-internal hostname that only resolves inside podman/docker.
# next.config.ts reads REMOTE_API_URL via dotenv; if .env sets it to
# host.containers.internal (for podman), override it here so `make dev`
# (native local dev) works out of the box.
export REMOTE_API_URL="http://localhost:${PORT:-8080}"

# ---------- Install dependencies ----------
if [ ! -d node_modules ]; then
  echo "==> Installing dependencies..."
  pnpm install
fi

# ---------- Database ----------
bash scripts/ensure-postgres.sh "$ENV_FILE"

# ---------- Pre-compile all Go binaries ----------
echo "==> Building Go binaries..."
(cd server && go build -o bin/server ./cmd/server)
(cd server && go build -o bin/multica ./cmd/multica)
(cd server && go build -o bin/migrate ./cmd/migrate)
echo "  ✓ server/bin/server"
echo "  ✓ server/bin/multica"
echo "  ✓ server/bin/migrate"

echo "==> Running migrations..."
server/bin/migrate up

# ---------- Build frontend (production mode for lower runtime overhead) ----------
if [ "${DEV_FRONTEND:-}" = "1" ]; then
  echo "==> Frontend: dev mode (Turbopack HMR)"
else
  echo "==> Building frontend (production)..."
  REMOTE_API_URL="http://localhost:${PORT:-8080}" pnpm --filter @multica/web build
  echo "  ✓ Frontend build complete"
fi

# ---------- Start services ----------
echo ""
echo "✓ Ready. Starting services..."
echo "  Backend:  http://localhost:${PORT:-8080}"
if [ "${DEV_FRONTEND:-}" = "1" ]; then
  echo "  Frontend: http://localhost:${FRONTEND_PORT:-3000} (dev mode)"
else
  echo "  Frontend: http://localhost:${FRONTEND_PORT:-3000} (production)"
fi
echo "  Daemon:   starting (agent runtime)"
echo ""

trap 'kill 0' EXIT
server/bin/server &
SERVER_PID=$!

if [ "${DEV_FRONTEND:-}" = "1" ]; then
  pnpm dev:web &
else
  (cd apps/web && npx next start -p "${FRONTEND_PORT:-3000}") &
fi
FRONTEND_PID=$!

# Wait for the backend to be healthy before starting the daemon, which
# needs to register runtimes via the API.
echo "==> Waiting for backend to be ready..."
for i in $(seq 1 30); do
  if curl -sf "http://localhost:${PORT:-8080}/health" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done

if curl -sf "http://localhost:${PORT:-8080}/health" > /dev/null 2>&1; then
  echo "==> Starting daemon..."
  server/bin/multica daemon start --foreground &
  DAEMON_PID=$!
  echo "  Daemon PID: $DAEMON_PID"
else
  echo "⚠ Backend did not become healthy in 30s — skipping daemon start."
  echo "  Start manually: ./server/bin/multica daemon start --foreground"
fi

wait
