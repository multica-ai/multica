#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_PATH="${1:-}"

if [ -z "$TARGET_PATH" ]; then
  echo "Usage: bash scripts/remove-worktree.sh /path/to/worktree"
  exit 1
fi

if [[ "$TARGET_PATH" != /* ]]; then
  TARGET_PATH="$(cd "$ROOT_DIR" && cd "$TARGET_PATH" && pwd -P)"
fi

CURRENT_PATH="$(pwd -P)"
if [ "$TARGET_PATH" = "$CURRENT_PATH" ]; then
  echo "Refusing to remove the current working tree."
  echo "Run this command from another checkout, or destroy the current worktree with 'make destroy-worktree FORCE=1' first."
  exit 1
fi

if [ ! -d "$TARGET_PATH" ]; then
  echo "Missing worktree path: $TARGET_PATH"
  exit 1
fi

ENV_FILE="$TARGET_PATH/.env.worktree"
if [ ! -f "$ENV_FILE" ]; then
  echo "Missing worktree env file: $ENV_FILE"
  echo "Run 'FORCE=1 bash scripts/destroy-worktree.sh \"$ENV_FILE\"' yourself if the env lives elsewhere, then remove the worktree manually."
  exit 1
fi

FORCE="${FORCE:-0}" bash "$ROOT_DIR/scripts/destroy-worktree.sh" "$ENV_FILE"

git -C "$ROOT_DIR" worktree remove "$TARGET_PATH"
echo "Removed git worktree '$TARGET_PATH'."
