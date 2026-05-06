#!/usr/bin/env bash
# Pulls latest main from GitHub, rebuilds if there are changes,
# and restarts launchd jobs. Triggered by webhook tool.
set -euo pipefail

REPO=/Users/sara/code/firtal-cerebro
LOG_DIR="$REPO/.deploy/logs"
mkdir -p "$LOG_DIR"

# Serialize concurrent invocations. Webhook can fire multiple times
# back-to-back (e.g. several merges in quick succession — see JEH-628
# where four merges in 13 minutes triggered four parallel `next build`
# processes that SIGTERM'd each other and left prod on a half-built
# .next/). macOS doesn't ship `flock`, so we emulate it with atomic
# `mkdir` and break stale locks left behind by SIGKILL.
#
# Coalescing: a queued waiter that wakes up after the holder finishes
# checks origin/main again below. If origin hasn't moved AND the
# previous deploy succeeded (LAST_OK_SHA matches), the waiter exits
# cleanly. Net effect: N back-to-back webhooks for the same final
# commit collapse to exactly one full build.
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

# LAST_OK_SHA records the SHA of the last fully-successful deploy.
# Written only once every step (install / backend build / frontend
# build / launchd restart) has completed without error. A queued deploy
# that finds HEAD == origin/main but LAST_OK_SHA != HEAD knows the
# previous run died mid-build and re-runs the full pipeline instead of
# bailing on the SHA-equality guard.
LAST_OK_FILE="$LOG_DIR/last-ok-sha"
LAST_OK_SHA=$(cat "$LAST_OK_FILE" 2>/dev/null || true)

# Make any non-zero exit loud in the log + mark the run as a failure
# so it doesn't get mistaken for "deployed cleanly". Webhook scrapes
# deploy-latest.log; an operator paging on "DEPLOY FAILED" gets an
# unambiguous signal. Keep this lightweight — Slack/Multica issue
# integration belongs in run-webhook.sh, not here.
trap 'rc=$?; if [ "$rc" -ne 0 ]; then echo "=== DEPLOY FAILED (exit $rc): $(date -Iseconds) ===" >&2; fi; rm -rf "$LOCK"' EXIT

OLD_SHA=$(git rev-parse HEAD)
echo "Current SHA:    $OLD_SHA"
echo "Last-OK SHA:    ${LAST_OK_SHA:-<none>}"

git fetch origin main
NEW_SHA=$(git rev-parse origin/main)
echo "Remote SHA:     $NEW_SHA"

if [ "$OLD_SHA" = "$NEW_SHA" ] && [ "$LAST_OK_SHA" = "$NEW_SHA" ]; then
  echo "No changes since last successful deploy — nothing to do."
  exit 0
fi
if [ "$OLD_SHA" = "$NEW_SHA" ] && [ "$LAST_OK_SHA" != "$NEW_SHA" ]; then
  echo "HEAD already at origin/main but last deploy did not finish — re-running."
fi

echo "Resetting to origin/main…"
git reset --hard origin/main

echo "Installing deps (pnpm)…"
if ! pnpm install --frozen-lockfile; then
  echo "ERROR: pnpm install failed — aborting before touching prod." >&2
  exit 1
fi

echo "Building Go backend…"
if ! make build; then
  echo "ERROR: Go backend build failed — aborting before touching prod." >&2
  exit 1
fi

echo "Running migrations…"
make migrate-up || echo "WARN: migrate-up failed (may be no-op)"

echo "Building Next.js frontend…"
# Next.js 16 sometimes leaves stale client-reference-manifest entries
# when an incremental build runs over a previous build with route-group
# changes — pages then 500 in production with InvariantError. Clean
# .next first so every deploy is a from-scratch build.
#
# CRITICAL: stop the running next-server BEFORE removing .next/ —
# otherwise the live process keeps reading from a directory the build
# is rewriting under it, and serves 500s with "client reference
# manifest does not exist" until the new build finishes (and even then
# only for users whose connection lands on a fresh next-server, not on
# an orphan hanging onto a stale .next/). Symptom: JEH-599 frontend
# spam after #80 mutex landed.
echo "Stopping frontend before .next reset…"
launchctl bootout gui/$(id -u)/com.multica.frontend 2>/dev/null || true
# Reap any next-server still around — webhook restarts can leave orphans
# from earlier deploys that bootout didn't catch.
pkill -f next-server 2>/dev/null || true
# Give the OS a moment to release port 4200 so the new launchd start
# below doesn't race a half-dead listener.
sleep 1

rm -rf apps/web/.next
if ! pnpm --filter @multica/web build; then
  echo "ERROR: Next.js build failed — frontend is currently DOWN (bootout above)." >&2
  echo "       Roll back manually: cd $REPO && git reset --hard $OLD_SHA && rerun deploy." >&2
  exit 1
fi

echo "Restarting launchd jobs…"
launchctl kickstart -k gui/$(id -u)/com.multica.backend
# Bring the frontend back up on the freshly built .next/. bootstrap is
# idempotent — bootout above may have already torn the job out, so we
# load it back before kickstart can act on it.
launchctl bootstrap gui/$(id -u) "$HOME/Library/LaunchAgents/com.multica.frontend.plist" 2>/dev/null || true
launchctl kickstart -k gui/$(id -u)/com.multica.frontend

echo "=== deploy finished: $(date -Iseconds) ==="
echo "Deployed: $OLD_SHA -> $NEW_SHA"

# Mark the deploy as fully successful so a queued waiter doesn't mistake
# the SHA-match for "incomplete previous run" (see LAST_OK_FILE comment
# above). Written last so every step had to succeed first.
echo "$NEW_SHA" > "$LAST_OK_FILE"
