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

# Sanity-check the build before swapping: BUILD_ID alone is not enough.
# JEH symptom (May 2026) was a "successful" build where every chunk
# under .next/static/ returned 500 from next-server — the manifest
# listed chunks that weren't on disk. Verify the static dir is populated
# and that a sample of manifest-referenced chunks actually exist.
echo "Verifying build output…"
if [ ! -d "$NEXT_NEW/static/chunks" ] || [ -z "$(ls -A "$NEXT_NEW/static/chunks" 2>/dev/null)" ]; then
  echo "ERROR: $NEXT_NEW/static/chunks missing or empty. Aborting swap." >&2
  rm -rf "$NEXT_NEW"
  exit 1
fi
BUILD_MANIFEST="$NEXT_NEW/build-manifest.json"
if [ ! -f "$BUILD_MANIFEST" ]; then
  echo "ERROR: $BUILD_MANIFEST missing — build incomplete. Aborting swap." >&2
  rm -rf "$NEXT_NEW"
  exit 1
fi
# Sample 5 chunk paths from the manifest and confirm each exists on disk.
# If the manifest references files that aren't present, next-server will
# happily serve HTML pointing at them and return 500 on every asset.
MISSING_CHUNKS=$(grep -oE '"static/chunks/[^"]+"' "$BUILD_MANIFEST" 2>/dev/null \
  | tr -d '"' | sort -u | head -50 \
  | while read -r p; do [ -f "$NEXT_NEW/$p" ] || echo "$p"; done)
if [ -n "$MISSING_CHUNKS" ]; then
  echo "ERROR: build-manifest.json references chunks missing from disk:" >&2
  echo "$MISSING_CHUNKS" | head -10 >&2
  echo "Aborting swap." >&2
  rm -rf "$NEXT_NEW"
  exit 1
fi

echo "Atomic swap: renaming .next, restarting launchd jobs…"
# We do NOT pkill next-server before the rename. Unix file handles
# survive rename — the running process keeps serving from the same
# inode (now reachable via .next.old/) until kickstart -k replaces it.
# Pkill-then-mv used to race with launchd's KeepAlive=true respawn:
# launchd would restart next-server in the gap between pkill and
# kickstart, the respawn would mmap .next/ mid-swap, and end up with
# a manifest pointing at chunk paths that no longer existed on disk.
# JEH symptom: all /_next/static/* return 500 while HTML still 200s.
rm -rf "$NEXT_OLD"
if [ -e "$NEXT_LIVE" ]; then
  mv "$NEXT_LIVE" "$NEXT_OLD"
fi
mv "$NEXT_NEW" "$NEXT_LIVE"

# || true on launchctl: it returns non-zero on benign races (job in
# transition, kickstart between KeepAlive respawns). Hard failures
# surface as the smoke test below failing.
#
# JEH-438: restart com.multica.daemon too — `make build` rewrote the
# binary but the running launchd job keeps executing the old one until
# kickstart. Forgetting daemon was the root cause of JEH-418 looking
# broken after deploy.
launchctl kickstart -k gui/$(id -u)/com.multica.backend || true
launchctl kickstart -k gui/$(id -u)/com.multica.frontend || true
launchctl kickstart -k gui/$(id -u)/com.multica.daemon || true

# ---------------------------------------------------------------------
# Post-deploy smoke test + rollback.
# ---------------------------------------------------------------------
# Without this, a build that produces 500-on-every-asset (like the May
# 2026 incident) sails through unnoticed because next-server is "up"
# (the launchd job is running) and the HTML route returns 200. The
# user only finds out by hitting the app — and by then .next.old has
# been async-deleted and there is no fast path back.
#
# Smoke test polls / for up to 60s after kickstart, then verifies one
# JS chunk from the rendered HTML returns 200. If either fails, swap
# .next.old back into place, restart, and exit non-zero so the
# DEPLOY FAILED trap fires.
# Read FRONTEND_PORT from .env if set there (deploy.sh doesn't source .env
# itself, but run-frontend.sh does — keep them in sync). Falls back to
# 4200 to match run-frontend.sh's default.
if [ -f .env ]; then
  PORT_FROM_ENV=$(grep -E '^FRONTEND_PORT=' .env | tail -1 | cut -d= -f2- | tr -d '"' || true)
else
  PORT_FROM_ENV=""
fi
PORT_FOR_SMOKE="${FRONTEND_PORT:-${PORT_FROM_ENV:-4200}}"
SMOKE_URL="http://127.0.0.1:$PORT_FOR_SMOKE/"

smoke_test() {
  local html chunk code deadline
  deadline=$(( $(date +%s) + 60 ))
  echo "Smoke test: polling $SMOKE_URL until 200 (60s budget)…"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    html=$(curl -fsS --max-time 5 "$SMOKE_URL" 2>/dev/null || true)
    if [ -n "$html" ]; then break; fi
    sleep 2
  done
  if [ -z "$html" ]; then
    echo "Smoke FAIL: $SMOKE_URL did not return 200 within 60s." >&2
    return 1
  fi
  echo "Smoke test: / served. Verifying a static chunk loads…"
  chunk=$(printf '%s' "$html" \
    | grep -oE '/_next/static/chunks/[A-Za-z0-9._-]+\.js' \
    | head -1 || true)
  if [ -z "$chunk" ]; then
    echo "Smoke WARN: could not find a chunk URL in HTML — accepting / only." >&2
    return 0
  fi
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 \
    "http://127.0.0.1:$PORT_FOR_SMOKE$chunk")
  if [ "$code" != "200" ]; then
    echo "Smoke FAIL: chunk $chunk returned $code (expected 200)." >&2
    return 1
  fi
  echo "Smoke OK: / and $chunk both 200."
  return 0
}

rollback_to_old() {
  echo "ROLLBACK: swapping .next.old back into .next and restarting." >&2
  if [ -d "$NEXT_OLD" ]; then
    local broken_name="apps/web/.next.broken-$(date +%Y%m%d-%H%M%S)"
    if [ -e "$NEXT_LIVE" ]; then
      mv "$NEXT_LIVE" "$broken_name"
      echo "Broken build preserved at $broken_name for inspection." >&2
    fi
    mv "$NEXT_OLD" "$NEXT_LIVE"
    launchctl kickstart -k gui/$(id -u)/com.multica.frontend || true
  else
    echo "ROLLBACK FAILED: no .next.old to restore — frontend may be down." >&2
  fi
}

if ! smoke_test; then
  rollback_to_old
  echo "=== DEPLOY FAILED (smoke test): rolled back to previous .next ===" >&2
  exit 1
fi

# Smoke test passed. Now safe to remove the previous build asynchronously.
rm -rf "$NEXT_OLD" &

echo "=== deploy finished: $(date -Iseconds) ==="
echo "Deployed: $OLD_SHA -> $NEW_SHA"

# Mark the deploy as fully successful so a queued waiter doesn't mistake
# the SHA-match for "incomplete previous run" (see LAST_OK_FILE comment
# above). Written last so every step had to succeed first.
echo "$NEW_SHA" > "$LAST_OK_FILE"
