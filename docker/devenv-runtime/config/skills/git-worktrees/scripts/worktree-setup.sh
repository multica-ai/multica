#!/usr/bin/env bash
# worktree-setup.sh — Create a git worktree and install dependencies
#
# Usage:
#   bash worktree-setup.sh <branch-name>
#   bash worktree-setup.sh <branch-name> [base-ref]
#
# Arguments:
#   branch-name   Name of the branch to check out in the new worktree.
#                 If the branch doesn't exist, it will be created.
#   base-ref      Optional. Commit, tag, or remote branch to base the new
#                 branch on (e.g. origin/main). Defaults to HEAD.
#
# Output:
#   Prints the absolute path to the new worktree on success.

set -euo pipefail

# ── Helpers ──────────────────────────────────────────────────────────────────

err() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  $*"; }

# ── Argument validation ───────────────────────────────────────────────────────

[[ $# -lt 1 ]] && err "Usage: $0 <branch-name> [base-ref]"

BRANCH="$1"
BASE_REF="${2:-}"

# Validate branch name (git rules: no spaces, no .., no leading -, etc.)
if ! git check-ref-format --branch "$BRANCH" &>/dev/null; then
  err "Invalid branch name: '$BRANCH'. Branch names cannot contain spaces, .., or start with -"
fi

# ── Ensure we're inside a git repo ───────────────────────────────────────────

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" \
  || err "Not inside a git repository."

cd "$REPO_ROOT"

# ── Worktree path ─────────────────────────────────────────────────────────────

WORKTREE_DIR=".worktrees/$BRANCH"
WORKTREE_ABS="$REPO_ROOT/$WORKTREE_DIR"

if [[ -d "$WORKTREE_ABS" ]]; then
  err "Worktree directory already exists: $WORKTREE_ABS"
fi

# ── .gitignore ────────────────────────────────────────────────────────────────

GITIGNORE="$REPO_ROOT/.gitignore"

if [[ -f "$GITIGNORE" ]]; then
  if ! grep -qxF ".worktrees/" "$GITIGNORE"; then
    echo ".worktrees/" >> "$GITIGNORE"
    info "Added .worktrees/ to .gitignore"
  fi
else
  echo ".worktrees/" > "$GITIGNORE"
  info "Created .gitignore with .worktrees/ entry"
fi

# ── Create worktree ───────────────────────────────────────────────────────────

echo "Creating worktree for branch '$BRANCH'..."

# Check if branch already exists locally
if git show-ref --verify --quiet "refs/heads/$BRANCH"; then
  info "Branch '$BRANCH' already exists — checking it out in new worktree"
  git worktree add "$WORKTREE_DIR" "$BRANCH"
else
  if [[ -n "$BASE_REF" ]]; then
    info "Creating new branch '$BRANCH' from '$BASE_REF'"
    git worktree add "$WORKTREE_DIR" -b "$BRANCH" "$BASE_REF"
  else
    info "Creating new branch '$BRANCH' from HEAD"
    git worktree add "$WORKTREE_DIR" -b "$BRANCH"
  fi
fi

# ── Detect package manager and install dependencies ───────────────────────────

echo "Detecting package manager..."

install_deps() {
  local dir="$1"

  if [[ -f "$dir/bun.lock" || -f "$dir/bun.lockb" ]]; then
    info "Detected bun — running bun install"
    bun install --cwd "$dir"

  elif [[ -f "$dir/pnpm-lock.yaml" ]]; then
    info "Detected pnpm — running pnpm install"
    pnpm install --dir "$dir"

  elif [[ -f "$dir/yarn.lock" ]]; then
    info "Detected yarn — running yarn install"
    yarn --cwd "$dir" install

  elif [[ -f "$dir/package-lock.json" ]]; then
    info "Detected npm — running npm install"
    npm install --prefix "$dir"

  elif [[ -f "$dir/package.json" ]]; then
    info "Found package.json without lock file — running npm install"
    npm install --prefix "$dir"

  elif [[ -f "$dir/poetry.lock" ]]; then
    info "Detected poetry — running poetry install"
    (cd "$dir" && poetry install)

  elif [[ -f "$dir/Pipfile" ]]; then
    info "Detected pipenv — running pipenv install"
    (cd "$dir" && pipenv install)

  elif [[ -f "$dir/requirements.txt" ]]; then
    info "Detected pip — running pip install -r requirements.txt"
    pip install -r "$dir/requirements.txt"

  elif [[ -f "$dir/go.mod" ]]; then
    info "Detected Go modules — running go mod download"
    (cd "$dir" && go mod download)

  elif [[ -f "$dir/Cargo.toml" ]]; then
    info "Detected Cargo — running cargo fetch"
    (cd "$dir" && cargo fetch)

  else
    info "No recognized package manager lock file found — skipping dependency install"
    info "If needed, install dependencies manually in: $dir"
  fi
}

install_deps "$WORKTREE_ABS"

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
echo "Worktree ready."
echo "$WORKTREE_ABS"
