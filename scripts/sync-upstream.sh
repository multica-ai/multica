#!/usr/bin/env bash
# Report how far this fork is behind/ahead of upstream/main.
# Does not merge — operators decide when to sync (see FORKING.md).
set -euo pipefail

UPSTREAM_URL="${MULTICA_UPSTREAM_URL:-https://github.com/multica-ai/multica.git}"
UPSTREAM_REMOTE="${MULTICA_UPSTREAM_REMOTE:-upstream}"
UPSTREAM_BRANCH="${MULTICA_UPSTREAM_BRANCH:-main}"
LOCAL_REF="${1:-HEAD}"

if ! command -v git >/dev/null 2>&1; then
  echo "error: git is required" >&2
  exit 1
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: not inside a git repository" >&2
  exit 1
fi

if ! git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1; then
  echo "==> Adding remote '$UPSTREAM_REMOTE' -> $UPSTREAM_URL"
  git remote add "$UPSTREAM_REMOTE" "$UPSTREAM_URL"
fi

echo "==> Fetching $UPSTREAM_REMOTE..."
git fetch "$UPSTREAM_REMOTE" --prune

upstream_ref="${UPSTREAM_REMOTE}/${UPSTREAM_BRANCH}"
if ! git rev-parse --verify "$upstream_ref" >/dev/null 2>&1; then
  echo "error: missing $upstream_ref after fetch" >&2
  exit 1
fi

behind="$(git rev-list --count "${LOCAL_REF}..${upstream_ref}")"
ahead="$(git rev-list --count "${upstream_ref}..${LOCAL_REF}")"
local_sha="$(git rev-parse --short "$LOCAL_REF")"
upstream_sha="$(git rev-parse --short "$upstream_ref")"

echo ""
echo "Local:    $LOCAL_REF ($local_sha)"
echo "Upstream: $upstream_ref ($upstream_sha)"
echo "Behind:   $behind commit(s)"
echo "Ahead:    $ahead commit(s)"
echo ""

if [ "$behind" -eq 0 ] && [ "$ahead" -eq 0 ]; then
  echo "✓ In sync with $upstream_ref"
  exit 0
fi

if [ "$behind" -gt 0 ]; then
  echo "To merge upstream into the current branch:"
  echo "  git merge $upstream_ref"
  echo "Or rebase (rewrites history — only on unpublished branches):"
  echo "  git rebase $upstream_ref"
fi

if [ "$ahead" -gt 0 ]; then
  echo "This branch has $ahead commit(s) not in upstream (expected for a maintained fork)."
fi
