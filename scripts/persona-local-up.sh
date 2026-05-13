#!/usr/bin/env bash
# persona-local-up.sh — bring up everything needed to test the persona ↔
# Cerebro integration locally. Idempotent — safe to re-run.
#
# What this does:
#   1. Verifies persona's docker stack (Cerbos + Infisical) is running
#   2. Builds cerebro-persona-hook and the multica CLI from the Cerebro
#      worktree
#   3. Starts persona-API (if not already running) using the worktree's code
#   4. Starts the persona UI on http://localhost:3001
#   5. Prints a one-line summary of where to look
#
# Stop with: scripts/persona-local-down.sh

set -euo pipefail

PERSONA_REPO="${PERSONA_REPO:-/Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration}"
CEREBRO_REPO="${CEREBRO_REPO:-/Users/hvejsel/firtal-repos/firtal-cerebro-persona-integration}"

green() { printf '\033[32m%s\033[0m\n' "$*" >&2; }
yellow() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }

# 1. Cerbos.
if ! curl -sS -m 2 http://localhost:3593/_cerbos/health 2>/dev/null | grep -q SERVING; then
    yellow "Cerbos not healthy — bringing up persona docker stack…"
    (cd "$PERSONA_REPO" && docker-compose up -d cerbos infisical-backend) >/dev/null
    for i in 1 2 3 4 5 6 7 8 9 10; do
        sleep 1
        curl -sS -m 2 http://localhost:3593/_cerbos/health 2>/dev/null | grep -q SERVING && break
    done
    curl -sS -m 2 http://localhost:3593/_cerbos/health | grep -q SERVING \
        || fail "Cerbos did not become healthy after 10s — check 'docker logs persona-cerbos'"
fi
green "✓ Cerbos healthy on :3593"

# 2. Build cerebro binaries.
yellow "Building cerebro-persona-hook and multica…"
(cd "$CEREBRO_REPO/server" && go build -o bin/cerebro-persona-hook ./cmd/cerebro-persona-hook/ && go build -o bin/multica ./cmd/multica/)
green "✓ Built $CEREBRO_REPO/server/bin/{cerebro-persona-hook,multica}"

# 3. Persona API.
if ! curl -sS -m 2 http://localhost:4500/healthz 2>/dev/null | grep -q '"status":"ok"'; then
    yellow "Persona API not running — starting it…"
    (cd "$PERSONA_REPO" && nohup go run ./cmd/persona/ > /tmp/persona-api.log 2>&1 < /dev/null &) >/dev/null 2>&1
    for i in 1 2 3 4 5 6 7 8 9 10; do
        sleep 1
        curl -sS -m 2 http://localhost:4500/healthz 2>/dev/null | grep -q '"status":"ok"' && break
    done
    curl -sS -m 2 http://localhost:4500/healthz | grep -q '"status":"ok"' \
        || fail "Persona API did not start — check /tmp/persona-api.log"
fi
green "✓ Persona API healthy on :4500"

# 4. Persona UI.
if ! curl -sS -m 2 -o /dev/null -w "%{http_code}" http://localhost:3001/sandboxes 2>/dev/null | grep -q "^200$"; then
    yellow "Persona UI not running — starting it (~10s)…"
    BOOTSTRAP=$(cat "$PERSONA_REPO/data/.bootstrap-token")
    (cd "$PERSONA_REPO/ui" && PERSONA_BOOTSTRAP_TOKEN="$BOOTSTRAP" nohup pnpm dev > /tmp/persona-ui.log 2>&1 < /dev/null &) >/dev/null 2>&1
    for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
        sleep 1
        curl -sS -m 2 -o /dev/null -w "%{http_code}" http://localhost:3001/sandboxes 2>/dev/null | grep -q "^200$" && break
    done
    curl -sS -m 2 -o /dev/null -w "%{http_code}" http://localhost:3001/sandboxes | grep -q "^200$" \
        || fail "Persona UI did not start within 15s — check /tmp/persona-ui.log"
fi
green "✓ Persona UI on :3001"

# 5. Multica server with persona env wired in.
# Multica's PORT env defaults to 8484 (set in .env), not 8080.
MULTICA_PORT="${MULTICA_PORT:-8484}"
if ! curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$MULTICA_PORT/health" 2>/dev/null | grep -q "^200$"; then
    [ -f "$CEREBRO_REPO/.env" ] || fail "Multica needs $CEREBRO_REPO/.env (copy from main checkout: cp /Users/hvejsel/firtal-repos/firtal-cerebro/.env $CEREBRO_REPO/)"
    yellow "Multica server not running — starting it with persona wiring…"
    PERSONA_BOOTSTRAP=$(cat "$PERSONA_REPO/data/.cerebro-token")
    HOOK_BIN="$CEREBRO_REPO/server/bin/cerebro-persona-hook"
    (cd "$CEREBRO_REPO/server" && set -a && . "$CEREBRO_REPO/.env" && set +a && \
        MULTICA_PERSONA_URL="$PERSONA_URL" \
        MULTICA_PERSONA_TOKEN="$PERSONA_BOOTSTRAP" \
        MULTICA_PERSONA_HOOK_BIN="$HOOK_BIN" \
        nohup go run ./cmd/server > /tmp/multica-server.log 2>&1 < /dev/null &) >/dev/null 2>&1
    for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
        sleep 1
        curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$MULTICA_PORT/health" 2>/dev/null | grep -q "^200$" && break
    done
    curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$MULTICA_PORT/health" | grep -q "^200$" \
        || fail "Multica server did not start within 15s — check /tmp/multica-server.log"
fi
green "✓ Multica server on :$MULTICA_PORT (persona wiring ON)"

# 6. Multica web app — Next.js dev server on FRONTEND_PORT (default 3434).
# pnpm's hoisting layout doesn't symlink lightningcss's native module to where
# the JS expects, so we copy it once. Same fix any worktree needs after
# pnpm install with Node 20+.
LIGHTNINGCSS_NATIVE="$CEREBRO_REPO/node_modules/.pnpm/lightningcss-darwin-arm64@1.32.0/node_modules/lightningcss-darwin-arm64/lightningcss.darwin-arm64.node"
LIGHTNINGCSS_TARGET="$CEREBRO_REPO/node_modules/.pnpm/lightningcss@1.32.0/node_modules/lightningcss/lightningcss.darwin-arm64.node"
if [ -f "$LIGHTNINGCSS_NATIVE" ] && [ ! -f "$LIGHTNINGCSS_TARGET" ]; then
    cp "$LIGHTNINGCSS_NATIVE" "$LIGHTNINGCSS_TARGET" && yellow "✓ Patched lightningcss native module location"
fi

# FRONTEND_PORT comes from .env (defaults to 3434). FRONTEND_ORIGIN must
# match — the server's CORS check rejects mismatched origins.
WEB_PORT="${FRONTEND_PORT:-3434}"
if ! curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$WEB_PORT/" 2>/dev/null | grep -qE "^[1-5][0-9][0-9]$"; then
    yellow "Multica web not running — starting it on :$WEB_PORT (~30s first compile)…"
    (cd "$CEREBRO_REPO/apps/web" && set -a && . "$CEREBRO_REPO/.env" && set +a && \
        REMOTE_API_URL="http://localhost:$MULTICA_PORT" \
        nohup pnpm next dev --port "$WEB_PORT" > /tmp/multica-web.log 2>&1 < /dev/null &) >/dev/null 2>&1
    for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
        sleep 2
        curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$WEB_PORT/" 2>/dev/null | grep -qE "^[1-5][0-9][0-9]$" && break
    done
    curl -sS -m 2 -o /dev/null -w "%{http_code}" "http://localhost:$WEB_PORT/" | grep -qE "^[1-5][0-9][0-9]$" \
        || fail "Multica web did not start within 60s — check /tmp/multica-web.log"
fi
green "✓ Multica web on :$WEB_PORT (REMOTE_API_URL=http://localhost:$MULTICA_PORT)"

cat <<EOF >&2

Everything up. Try:

  Persona sandboxes UI:  open http://localhost:3001/sandboxes
  Multica web UI:        open http://localhost:$WEB_PORT/   (login then go to Agents → Settings)
  Run the e2e test:      PERSONA_REPO=$PERSONA_REPO CEREBRO_REPO=$CEREBRO_REPO \\
                         $CEREBRO_REPO/scripts/e2e-persona.sh
  Manual single spawn:   $CEREBRO_REPO/server/bin/multica agent e2e-spawn \\
                           --persona-actor <UUID-from-UI> \\
                           --prompt "Run the command: ls -la" \\
                           --persona-token \$(cat $PERSONA_REPO/data/.cerebro-token)

  Activity log API:      curl -H "Authorization: Bearer \$(cat $PERSONA_REPO/data/.bootstrap-token)" \\
                              http://localhost:4500/v1/log/search?limit=20 | jq

  Sandbox dropdown lives on each agent's settings tab in Multica web —
  open Multica → workspace → Agents → click an agent → Settings tab.

EOF
