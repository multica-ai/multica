#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/../../.." && pwd)"

ENV_FILE="${MULTICA_ENV_FILE:-}"
if [ -z "$ENV_FILE" ]; then
  if [ -f "$ROOT_DIR/.env.worktree" ]; then
    ENV_FILE="$ROOT_DIR/.env.worktree"
  elif [ -f "$ROOT_DIR/.env" ]; then
    ENV_FILE="$ROOT_DIR/.env"
  fi
fi

if [ -n "$ENV_FILE" ] && [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

if [ -z "${EXPO_PUBLIC_API_URL:-}" ]; then
  if [ -n "${NEXT_PUBLIC_API_URL:-}" ]; then
    export EXPO_PUBLIC_API_URL="$NEXT_PUBLIC_API_URL"
  else
    export EXPO_PUBLIC_API_URL="http://localhost:${PORT:-8080}"
  fi
fi

if [ -z "${EXPO_PUBLIC_WS_URL:-}" ]; then
  if [ -n "${NEXT_PUBLIC_WS_URL:-}" ]; then
    export EXPO_PUBLIC_WS_URL="$NEXT_PUBLIC_WS_URL"
  else
    export EXPO_PUBLIC_WS_URL="ws://localhost:${PORT:-8080}/ws"
  fi
fi

if [ -z "${EXPO_PUBLIC_APP_URL:-}" ] && [ -n "${MULTICA_APP_URL:-}" ]; then
  export EXPO_PUBLIC_APP_URL="$MULTICA_APP_URL"
fi

exec "$@"
