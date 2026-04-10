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

# ---------- Install dependencies ----------
if [ ! -d node_modules ]; then
  echo "==> Installing dependencies..."
  pnpm install
fi

# ---------- Database ----------
bash scripts/ensure-postgres.sh "$ENV_FILE"

echo "==> Running migrations..."
(cd server && go run ./cmd/migrate up)

# ---------- Start services ----------
echo ""
echo "✓ Ready. Starting services..."
echo "  Backend:  http://localhost:${PORT:-8080}"
echo "  Frontend: http://localhost:${FRONTEND_PORT:-3000}"
echo ""

trap 'kill 0' EXIT
(cd server && go run ./cmd/server) &
pnpm dev:web &
wait
