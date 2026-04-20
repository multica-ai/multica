#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-${ENV_FILE:-.env}}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example first."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR/apps/web"

HOSTNAME="${HOSTNAME:-127.0.0.1}"
PORT="${FRONTEND_PORT:-13030}"
export HOSTNAME PORT

exec pnpm exec next start
