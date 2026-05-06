#!/usr/bin/env bash
# Pulls latest main from GitHub, rebuilds if there are changes,
# and restarts launchd jobs. Triggered by webhook tool.
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
LOG_DIR="$REPO/.deploy/logs"
mkdir -p "$LOG_DIR"

# Serialize concurrent invocations. Webhook can fire multiple times
# back-to-back (e.g. several merges in quick succession). Without a
# mutex, parallel `next build` runs clobber each other's `.next/`
# output and the frontend ends up serving 500s with missing
# client-reference-manifest entries. macOS has no `flock`, so we
# use atomic `mkdir` and break stale locks left behind by SIGKILL.
LOCK="$LOG_DIR/deploy.lock"
WAIT=0
while ! mkdir "$LOCK" 2>/dev/null; do
  if [ -r "$LOCK/pid" ]; then
    PID=$(cat "$LOCK/pid" 2>/dev/null || true)
    if [ -n "$PID" ] && ! kill -0 "$PID" 2>/dev/null; then
      echo "Breaking stale deploy lock from dead PID $PID" >&2
      rm -rf "$LOCK"
      continue
    fi
  fi
  if [ "$WAIT" -ge 600 ]; then
    echo "Another deploy holds $LOCK after 10 min; giving up." >&2
    exit 1
  fi
  sleep 2
  WAIT=$((WAIT + 2))
done
echo $$ >"$LOCK/pid"
trap 'rm -rf "$LOCK"' EXIT

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
# Next.js 16 sometimes leaves stale client-reference-manifest entries
# when an incremental build runs over a previous build with route-group
# changes — pages then 500 in production with InvariantError. Clean
# .next first so every deploy is a from-scratch build.
rm -rf apps/web/.next
pnpm --filter @multica/web build

echo "Restarting launchd jobs…"
launchctl kickstart -k gui/$(id -u)/com.multica.backend
launchctl kickstart -k gui/$(id -u)/com.multica.frontend

echo "=== deploy finished: $(date -Iseconds) ==="
echo "Deployed: $OLD_SHA -> $NEW_SHA"
