#!/usr/bin/env bash
# Pulls latest main from GitHub, rebuilds if there are changes,
# and restarts launchd jobs. Triggered by webhook tool.
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
LOG_DIR="$REPO/.deploy/logs"
mkdir -p "$LOG_DIR"

LOG="$LOG_DIR/deploy-$(date +%Y%m%d-%H%M%S).log"
LATEST="$LOG_DIR/deploy-latest.log"

exec > >(tee -a "$LOG") 2>&1
ln -sf "$LOG" "$LATEST"

echo "=== deploy started: $(date -Iseconds) ==="

cd "$REPO"

export PATH="/Users/sara/.nvm/versions/node/v24.13.1/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

OLD_SHA=$(git rev-parse HEAD)
echo "Current SHA: $OLD_SHA"

git fetch origin main
NEW_SHA=$(git rev-parse origin/main)
echo "Remote SHA:  $NEW_SHA"

if [ "$OLD_SHA" = "$NEW_SHA" ]; then
  echo "No changes — nothing to deploy."
  exit 0
fi

echo "Resetting to origin/main…"
git reset --hard origin/main

echo "Installing deps (pnpm)…"
pnpm install --frozen-lockfile

echo "Building Go backend…"
make build

echo "Running migrations…"
make migrate-up || echo "WARN: migrate-up failed (may be no-op)"

echo "Building Next.js frontend…"
pnpm --filter @multica/web build

echo "Restarting launchd jobs…"
launchctl kickstart -k gui/$(id -u)/com.multica.backend
launchctl kickstart -k gui/$(id -u)/com.multica.frontend

echo "=== deploy finished: $(date -Iseconds) ==="
echo "Deployed: $OLD_SHA -> $NEW_SHA"
