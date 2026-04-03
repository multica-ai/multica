#!/usr/bin/env bash
set -euo pipefail

# Sync local fork with upstream multica-ai/multica.
# Usage: ./scripts/sync-upstream.sh [branch]
#   branch: upstream branch to sync from (default: main)

UPSTREAM_BRANCH="${1:-main}"
UPSTREAM_REMOTE="upstream"

echo "=== Multica Local: Upstream Sync ==="
echo ""

# Ensure upstream remote exists
if ! git remote get-url "$UPSTREAM_REMOTE" &>/dev/null; then
    echo "Adding upstream remote..."
    git remote add "$UPSTREAM_REMOTE" https://github.com/multica-ai/multica.git
fi

# Fetch latest upstream
echo "Fetching upstream/${UPSTREAM_BRANCH}..."
git fetch "$UPSTREAM_REMOTE" "$UPSTREAM_BRANCH"

# Attempt merge
echo ""
echo "Merging upstream/${UPSTREAM_BRANCH} into current branch..."
if git merge "$UPSTREAM_REMOTE/$UPSTREAM_BRANCH" --no-edit; then
    echo ""
    echo "Merge successful."
else
    echo ""
    echo "CONFLICTS DETECTED. Files with conflicts:"
    echo ""
    git diff --name-only --diff-filter=U
    echo ""
    echo "=== Conflict Resolution Guide ==="
    echo ""
    echo "Files SAFE to take upstream version (git checkout --theirs <file>):"
    echo "  - apps/web/ (frontend, unless you modified it)"
    echo "  - server/pkg/agent/ (agent backends)"
    echo "  - server/internal/realtime/ (WebSocket hub)"
    echo "  - server/internal/events/ (event bus)"
    echo "  - docs/, README.md, LICENSE"
    echo ""
    echo "Files that MUST keep local version (git checkout --ours <file>):"
    echo "  - server/go.mod, server/go.sum (different dependencies)"
    echo "  - server/sqlc.yaml (SQLite config)"
    echo "  - server/migrations/ (SQLite migrations)"
    echo "  - server/pkg/db/queries/ (SQLite query dialect)"
    echo "  - server/pkg/db/generated/ (regenerate after resolving)"
    echo "  - server/cmd/server/main.go (SQLite + merged daemon)"
    echo "  - server/internal/storage/ (local filesystem)"
    echo "  - server/internal/auth/ (local auth)"
    echo "  - server/internal/service/email.go (removed)"
    echo ""
    echo "After resolving conflicts:"
    echo "  1. cd server && make sqlc  # regenerate DB code"
    echo "  2. go build ./...          # verify compilation"
    echo "  3. go test ./...           # run tests"
    echo "  4. git add . && git commit"
fi
