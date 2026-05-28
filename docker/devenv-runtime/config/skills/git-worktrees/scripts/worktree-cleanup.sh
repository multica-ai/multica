#!/usr/bin/env bash
# worktree-cleanup.sh — Remove git worktrees and prune stale entries
#
# Usage:
#   bash worktree-cleanup.sh <branch-name>     Remove a single worktree
#   bash worktree-cleanup.sh --all             Remove all worktrees under .worktrees/
#   bash worktree-cleanup.sh --list            List all active worktrees
#
# Options:
#   --force    Force removal even if worktree has uncommitted changes

set -euo pipefail

err() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  $*"; }

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" \
  || err "Not inside a git repository."

cd "$REPO_ROOT"

WORKTREES_DIR="$REPO_ROOT/.worktrees"

FORCE=""
ACTION=""
BRANCH=""

for arg in "$@"; do
  case "$arg" in
    --force) FORCE="--force" ;;
    --all)   ACTION="all" ;;
    --list)  ACTION="list" ;;
    -*)      err "Unknown option: $arg" ;;
    *)       BRANCH="$arg" ;;
  esac
done

if [[ "$ACTION" == "list" ]]; then
  echo "Active worktrees:"
  git worktree list
  exit 0
fi

remove_worktree() {
  local branch="$1"
  local wt_path="$WORKTREES_DIR/$branch"

  if [[ ! -d "$wt_path" ]]; then
    err "Worktree not found: $wt_path"
  fi

  echo "Removing worktree '$branch' at $wt_path..."

  if [[ -n "$FORCE" ]]; then
    git worktree remove --force "$wt_path"
  else
    git worktree remove "$wt_path" 2>/dev/null || {
      echo ""
      echo "ERROR: Worktree has uncommitted changes." >&2
      echo "  Commit or discard changes first, or use --force:" >&2
      echo "  bash $0 $branch --force" >&2
      exit 1
    }
  fi

  info "Removed worktree: $branch"
}

remove_all() {
  if [[ ! -d "$WORKTREES_DIR" ]]; then
    echo "No .worktrees/ directory found. Nothing to clean."
    exit 0
  fi

  local count=0

  for wt in "$WORKTREES_DIR"/*/; do
    [[ -d "$wt" ]] || continue
    local name
    name="$(basename "$wt")"
    echo "Removing worktree '$name'..."

    if [[ -n "$FORCE" ]]; then
      git worktree remove --force "$wt" 2>/dev/null && info "Removed: $name" || info "Skipped (already removed): $name"
    else
      git worktree remove "$wt" 2>/dev/null && info "Removed: $name" || info "SKIPPED (uncommitted changes): $name — use --force"
    fi

    count=$((count + 1))
  done

  if [[ "$count" -eq 0 ]]; then
    echo "No worktrees found under .worktrees/"
  fi
}

if [[ "$ACTION" == "all" ]]; then
  remove_all
elif [[ -n "$BRANCH" ]]; then
  remove_worktree "$BRANCH"
else
  err "Usage: $0 <branch-name> | --all | --list [--force]"
fi

echo ""
info "Pruning stale worktree entries..."
git worktree prune

echo ""
echo "Remaining worktrees:"
git worktree list
