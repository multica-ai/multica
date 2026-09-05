#!/usr/bin/env bash
# After agent delivery: land changes on main and return the primary checkout to main.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
TARGET_BRANCH="${TARGET_BRANCH:-main}"
SOURCE_BRANCH=""
ISSUE_NUMBER=""
VIA_PR="${VIA_PR:-auto}"  # auto | pr | local | skip
DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: finalize-to-main.sh [--branch <name>] [--issue <n>] [--via-pr auto|pr|local|skip] [--dry-run]

End state:
  1. Changes are on origin/main (PR merge or local merge + push)
  2. Primary repo at REPO_ROOT is checked out on main and synced

Environment:
  REPO_ROOT       Product repo (default: repo root)
  TARGET_BRANCH   Integration branch (default: main)
  GITHUB_REPOSITORY  owner/name for gh (optional, inferred)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --branch) SOURCE_BRANCH="${2:?}"; shift 2 ;;
    --issue) ISSUE_NUMBER="${2:?}"; shift 2 ;;
    --via-pr) VIA_PR="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [ -z "$SOURCE_BRANCH" ] && [ -n "$ISSUE_NUMBER" ]; then
  SOURCE_BRANCH="cursor-issue-${ISSUE_NUMBER}"
fi

if [ -z "$SOURCE_BRANCH" ]; then
  SOURCE_BRANCH="$(git -C "$REPO_ROOT" branch --show-current 2>/dev/null || true)"
fi

if [ -z "$SOURCE_BRANCH" ] || [ "$SOURCE_BRANCH" = "$TARGET_BRANCH" ]; then
  echo "error: specify --branch or --issue (current branch is already $TARGET_BRANCH or detached)" >&2
  exit 1
fi

if ! git -C "$REPO_ROOT" rev-parse --is-inside-work-tree &>/dev/null; then
  echo "error: $REPO_ROOT is not a git repository" >&2
  exit 1
fi

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

echo "finalize: source=$SOURCE_BRANCH target=$TARGET_BRANCH root=$REPO_ROOT via=$VIA_PR"

# Ensure we know about remote branches.
run git -C "$REPO_ROOT" fetch origin "$TARGET_BRANCH" "$SOURCE_BRANCH" --quiet 2>/dev/null || \
  run git -C "$REPO_ROOT" fetch origin --quiet

resolve_pr_number() {
  if ! command -v gh &>/dev/null; then
    return 1
  fi
  gh pr list --head "$SOURCE_BRANCH" --state open --json number -q '.[0].number' 2>/dev/null || true
}

PR_NUM="$(resolve_pr_number || true)"
mode="$VIA_PR"

if [ "$mode" = "auto" ]; then
  if [ -n "$PR_NUM" ]; then
    mode="pr"
  else
    mode="local"
  fi
fi

case "$mode" in
  skip)
    echo "Skipping merge; checking out $TARGET_BRANCH only."
    run git -C "$REPO_ROOT" checkout "$TARGET_BRANCH"
    run git -C "$REPO_ROOT" pull --ff-only origin "$TARGET_BRANCH" 2>/dev/null || true
    ;;
  pr)
    if [ -z "$PR_NUM" ]; then
      echo "error: --via-pr pr but no open PR for head $SOURCE_BRANCH" >&2
      exit 1
    fi
    echo "Merging PR #$PR_NUM into $TARGET_BRANCH (squash, delete branch)."
    run gh pr merge "$PR_NUM" --squash --delete-branch
    run git -C "$REPO_ROOT" checkout "$TARGET_BRANCH"
    run git -C "$REPO_ROOT" pull --ff-only origin "$TARGET_BRANCH"
    ;;
  local)
    echo "Local merge $SOURCE_BRANCH -> $TARGET_BRANCH"
    run git -C "$REPO_ROOT" checkout "$TARGET_BRANCH"
    run git -C "$REPO_ROOT" pull --ff-only origin "$TARGET_BRANCH" 2>/dev/null || true
    if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/$SOURCE_BRANCH"; then
      run git -C "$REPO_ROOT" merge --no-ff "$SOURCE_BRANCH" -m "chore(agent): merge $SOURCE_BRANCH into $TARGET_BRANCH"
    elif git -C "$REPO_ROOT" show-ref --verify --quiet "refs/remotes/origin/$SOURCE_BRANCH"; then
      run git -C "$REPO_ROOT" merge --no-ff "origin/$SOURCE_BRANCH" -m "chore(agent): merge $SOURCE_BRANCH into $TARGET_BRANCH"
    else
      echo "error: cannot find branch $SOURCE_BRANCH" >&2
      exit 1
    fi
    run git -C "$REPO_ROOT" push origin "$TARGET_BRANCH"
    run git -C "$REPO_ROOT" branch -d "$SOURCE_BRANCH" 2>/dev/null || true
    run git -C "$REPO_ROOT" push origin --delete "$SOURCE_BRANCH" 2>/dev/null || true
    ;;
  *)
    echo "error: invalid --via-pr mode: $mode" >&2
    exit 1
    ;;
esac

# Drop linked worktrees for this branch if any (best-effort).
if [ "$DRY_RUN" -eq 0 ]; then
  while IFS= read -r wt_path; do
    [ -z "$wt_path" ] && continue
    if [ "$wt_path" != "$REPO_ROOT" ]; then
      git -C "$REPO_ROOT" worktree remove "$wt_path" --force 2>/dev/null || true
    fi
  done < <(git -C "$REPO_ROOT" worktree list --porcelain | awk '/^worktree / {print $2}')
fi

CURRENT="$(git -C "$REPO_ROOT" branch --show-current)"
if [ "$CURRENT" != "$TARGET_BRANCH" ]; then
  run git -C "$REPO_ROOT" checkout "$TARGET_BRANCH"
fi

echo "done: on $TARGET_BRANCH at $(git -C "$REPO_ROOT" rev-parse --short HEAD)"
