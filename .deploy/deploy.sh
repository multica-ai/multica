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

echo "Building Next.js frontend (out-of-tree)…"
# Build into apps/web/.next.new while the live next-server keeps serving
# from apps/web/.next. After a successful build we stop the frontend,
# atomically rename .next -> .next.old and .next.new -> .next, restart,
# then delete .next.old. Frontend downtime ~1-3 sec per deploy regardless
# of build duration; failed builds leave the live frontend untouched.
#
# Replaces the old "stop-then-build-into-.next" pattern that put the
# whole build window (3-4 min) on the user-visible downtime budget.
# Safety from JEH-599 (client-reference-manifest tear) is preserved:
# the running server never sees a half-rewritten .next/.
NEXT_NEW=apps/web/.next.new
NEXT_LIVE=apps/web/.next
NEXT_OLD=apps/web/.next.old

rm -rf "$NEXT_NEW"
if ! NEXT_DIST_DIR=.next.new pnpm --filter @multica/web build; then
  echo "ERROR: Next.js build failed — live frontend untouched." >&2
  rm -rf "$NEXT_NEW"
  exit 1
fi

# Refuse to swap in an empty/half-built directory. BUILD_ID is the last
# file `next build` writes on success; if it's missing, abort.
if [ ! -f "$NEXT_NEW/BUILD_ID" ]; then
  echo "ERROR: $NEXT_NEW/BUILD_ID missing — build did not finish cleanly. Aborting swap." >&2
  rm -rf "$NEXT_NEW"
  exit 1
fi

echo "Atomic swap: killing next-server, renaming .next, restarting…"
# Kill the running next-server so it can't observe the .next/ rename
# midway. We do NOT bootout/bootstrap the launchd job — that race
# (bootout still settling when bootstrap runs) made earlier deploys
# exit 37 from a follow-up kickstart against a not-yet-loaded service.
# launchctl kickstart -k below is enough: it kills any current
# next-server and starts a fresh one on the just-swapped .next/.
pkill -f next-server 2>/dev/null || true
sleep 1

rm -rf "$NEXT_OLD"
if [ -e "$NEXT_LIVE" ]; then
  mv "$NEXT_LIVE" "$NEXT_OLD"
fi
mv "$NEXT_NEW" "$NEXT_LIVE"

echo "Restarting launchd jobs…"
# || true on every launchctl call: launchctl returns non-zero on
# benign races (job in transition, kickstart between KeepAlive
# respawns) and we don't want set -e to abort the deploy after
# the swap has already happened. Real failures surface as a 4200
# downtime — KeepAlive=true on the plist will keep retrying.
launchctl kickstart -k gui/$(id -u)/com.multica.backend || true
launchctl kickstart -k gui/$(id -u)/com.multica.frontend || true

# Async cleanup — frontend is already up, this is just disk hygiene.
rm -rf "$NEXT_OLD" &

echo "=== deploy finished: $(date -Iseconds) ==="
echo "Deployed: $OLD_SHA -> $NEW_SHA"

# Mark the deploy as fully successful so a queued waiter doesn't mistake
# the SHA-match for "incomplete previous run" (see LAST_OK_FILE comment
# above). Written last so every step had to succeed first.
echo "$NEW_SHA" > "$LAST_OK_FILE"
