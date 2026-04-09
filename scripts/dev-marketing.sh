#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAIN_ENV_FILE="$ROOT_DIR/.env"
WORKTREE_ENV_FILE="$ROOT_DIR/.env.worktree"
SELECTED_ENV_FILE="${ENV_FILE:-}"

if [ -z "$SELECTED_ENV_FILE" ]; then
  if [ -f "$MAIN_ENV_FILE" ]; then
    SELECTED_ENV_FILE="$MAIN_ENV_FILE"
  elif [ -f "$WORKTREE_ENV_FILE" ]; then
    SELECTED_ENV_FILE="$WORKTREE_ENV_FILE"
  else
    SELECTED_ENV_FILE="$MAIN_ENV_FILE"
  fi
elif [[ "$SELECTED_ENV_FILE" != /* ]]; then
  SELECTED_ENV_FILE="$ROOT_DIR/$SELECTED_ENV_FILE"
fi

if [ ! -f "$SELECTED_ENV_FILE" ]; then
  echo "Missing env file: $SELECTED_ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$SELECTED_ENV_FILE"
set +a

MARKETING_PORT="${MARKETING_PORT:-3001}"
NEXT_DIST_DIR="${NEXT_DIST_DIR:-.next-marketing-${MARKETING_PORT}}"
MARKETING_TYPES_BACKUP_DIR="$(mktemp -d)"

cp "$ROOT_DIR/apps/web/next-env.d.ts" "$MARKETING_TYPES_BACKUP_DIR/next-env.d.ts"
cp "$ROOT_DIR/apps/web/tsconfig.json" "$MARKETING_TYPES_BACKUP_DIR/tsconfig.json"
find "$ROOT_DIR/apps/web" -maxdepth 1 -type d -name '.next-marketing*' -exec rm -rf {} +

cleanup() {
  trap - EXIT INT TERM

  if [ -f "$MARKETING_TYPES_BACKUP_DIR/next-env.d.ts" ]; then
    cp "$MARKETING_TYPES_BACKUP_DIR/next-env.d.ts" "$ROOT_DIR/apps/web/next-env.d.ts"
  fi

  if [ -f "$MARKETING_TYPES_BACKUP_DIR/tsconfig.json" ]; then
    cp "$MARKETING_TYPES_BACKUP_DIR/tsconfig.json" "$ROOT_DIR/apps/web/tsconfig.json"
  fi

  rm -rf "$MARKETING_TYPES_BACKUP_DIR"

  if [[ "$NEXT_DIST_DIR" == .next-marketing* ]]; then
    find "$ROOT_DIR/apps/web" -maxdepth 1 -type d -name '.next-marketing*' -exec rm -rf {} +
  fi
}

trap cleanup EXIT INT TERM

if lsof -tiTCP:"$MARKETING_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Marketing port $MARKETING_PORT is already in use."
  echo "Stop the existing process, change the port in your env file, or use a worktree env."
  exit 1
fi

cd "$ROOT_DIR/apps/web"
NEXT_DIST_DIR="$NEXT_DIST_DIR" ./node_modules/.bin/next dev --port "$MARKETING_PORT"
