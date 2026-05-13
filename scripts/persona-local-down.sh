#!/usr/bin/env bash
# persona-local-down.sh — stop the local persona ↔ Cerebro test stack.
# Cerbos and Infisical are LEFT running (other things may depend on them).
# Only the persona-API and UI started by persona-local-up.sh are stopped.

set -uo pipefail

# Persona API (Go process listening on :4500).
PERSONA_PID=$(lsof -ti :4500 2>/dev/null || true)
if [ -n "$PERSONA_PID" ]; then
    kill -TERM "$PERSONA_PID" 2>/dev/null && echo "Stopped persona API (PID $PERSONA_PID)"
fi

# Persona UI (Next dev server on :3001).
UI_PID=$(lsof -ti :3001 2>/dev/null || true)
if [ -n "$UI_PID" ]; then
    kill -TERM "$UI_PID" 2>/dev/null && echo "Stopped persona UI (PID $UI_PID)"
fi

# Multica web (Next dev server on :3434).
WEB_PID=$(lsof -ti :3434 2>/dev/null || true)
if [ -n "$WEB_PID" ]; then
    kill -TERM "$WEB_PID" 2>/dev/null && echo "Stopped Multica web (PID $WEB_PID)"
fi

# Multica server (Go process on :8080) — only stop if it was started by
# persona-local-up.sh. We can't tell exactly, so we leave it running by
# default. Set MULTICA_DOWN=1 to stop it too.
if [ "${MULTICA_DOWN:-0}" = "1" ]; then
    MULTICA_PID=$(lsof -ti :8484 2>/dev/null || true)
    if [ -n "$MULTICA_PID" ]; then
        kill -TERM "$MULTICA_PID" 2>/dev/null && echo "Stopped Multica server (PID $MULTICA_PID)"
    fi
fi

echo "Cerbos / Infisical left running — stop with: cd \$PERSONA_REPO && docker-compose down"
