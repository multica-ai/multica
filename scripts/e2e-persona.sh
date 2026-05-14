#!/usr/bin/env bash
# e2e-persona.sh — End-to-end verification of the firtal-persona ↔ Cerebro
# integration described in firtal-persona/docs/cerebro-integration-plan.md.
#
# This script is the DONE-measure for that plan. It is intentionally written
# RED first (in plan task A0) and turns GREEN as A1–B7 are implemented. Each
# assertion below maps back to a specific plan task; comments name the task.
#
# Required state at runtime:
#   - firtal-persona running on PERSONA_URL (default http://localhost:4500)
#   - firtal-persona bootstrap-token file at $PERSONA_REPO/data/.bootstrap-token
#   - Cerebro daemon running with CEREBRO_PERSONA_ENABLED=true
#   - jq, curl available
#
# Usage:
#   ./scripts/e2e-persona.sh
#   PERSONA_URL=http://localhost:4500 ./scripts/e2e-persona.sh
#
# Exit codes:
#   0   — all assertions passed (plan complete, regression-clean)
#   != 0 — first failing assertion; stderr names the plan task to look at

set -euo pipefail

# ---------- configuration ----------

PERSONA_URL="${PERSONA_URL:-http://localhost:4500}"
PERSONA_REPO="${PERSONA_REPO:-$(cd "$(dirname "$0")/../../firtal-persona" && pwd)}"
CEREBRO_REPO="${CEREBRO_REPO:-$(cd "$(dirname "$0")/.." && pwd)}"
MULTICA_API="${MULTICA_API:-http://localhost:8080}"

# Actor names include a per-run suffix so soft-deletes from previous runs
# don't trip the UNIQUE name constraint. Persona keeps deleted rows for audit.
RUN_TAG="${RUN_TAG:-$(date +%s)}"
DEMO_ALLOWED_NAME="e2e-persona-demo-allowed-$RUN_TAG"
DEMO_BLOCKED_NAME="e2e-persona-demo-blocked-$RUN_TAG"
DEFAULT_SANDBOX_NAME="claude-developer"

# Where to write transient state for cleanup. Idempotent across runs.
STATE_DIR="${TMPDIR:-/tmp}/e2e-persona-$$"
mkdir -p "$STATE_DIR"
trap 'rc=$?; cleanup; exit $rc' EXIT

# ---------- helpers ----------

red()    { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green()  { printf '\033[32m%s\033[0m\n' "$*" >&2; }
yellow() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
step()   { printf '\n\033[1;36m── %s ──\033[0m\n' "$*" >&2; }

fail() {
    local task="$1"; shift
    red "FAIL [$task]: $*"
    red "See firtal-persona/docs/cerebro-integration-plan.md task $task for what needs to be built."
    exit 1
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "preflight" "missing required command: $1"
}

# Persona admin call using bootstrap-token. Fails fast if persona is unreachable
# or the token is missing — these are setup problems, not plan-task failures.
persona_admin() {
    local method="$1" path="$2" body="${3:-}"
    local token_file="$PERSONA_REPO/data/.bootstrap-token"
    [ -f "$token_file" ] || fail "preflight" "persona bootstrap token not found at $token_file (run \`go run ./cmd/persona\` once in $PERSONA_REPO)"
    local token; token=$(cat "$token_file")

    local args=(-sS -X "$method" -H "Authorization: Bearer $token" -H "Content-Type: application/json")
    if [ -n "$body" ]; then args+=(-d "$body"); fi
    curl "${args[@]}" "$PERSONA_URL$path"
}

# Cerebro service-token call, used by the spawn-flow itself in B-tasks.
# Until task A3 lands, the file does not exist; the script falls back to the
# bootstrap-token, which is enough to drive the test scenario but does not
# verify the snæver-token-design from beslutning #2.
cerebro_token_or_bootstrap() {
    local f="$PERSONA_REPO/data/.cerebro-token"
    if [ -f "$f" ]; then cat "$f"; else cat "$PERSONA_REPO/data/.bootstrap-token"; fi
}

cleanup() {
    step "Cleanup"
    # Best-effort — never let cleanup hide the underlying failure.
    if [ -n "${ALLOWED_ACTOR_ID:-}" ]; then
        persona_admin DELETE "/v1/actors/$ALLOWED_ACTOR_ID" >/dev/null 2>&1 || true
    fi
    if [ -n "${BLOCKED_ACTOR_ID:-}" ]; then
        persona_admin DELETE "/v1/actors/$BLOCKED_ACTOR_ID" >/dev/null 2>&1 || true
    fi
    rm -rf "$STATE_DIR"
}

# ---------- preflight ----------

step "Preflight: required tools and reachable services"
require_cmd jq
require_cmd curl

curl -sS -m 2 "$PERSONA_URL/healthz" >/dev/null \
    || fail "preflight" "persona unreachable at $PERSONA_URL — start it with \`go run ./cmd/persona\` in $PERSONA_REPO"

# Multica daemon must run with persona enabled. The scenario depends on
# Cerebro spawning Claude Code, which only happens through the daemon's task
# loop. Hitting healthz on the server is enough to establish the server is up;
# the daemon's per-task behaviour is what the assertions below probe.
curl -sS -m 2 "$MULTICA_API/healthz" >/dev/null \
    || fail "preflight" "Multica server unreachable at $MULTICA_API — start it with \`make server\` in $CEREBRO_REPO"

green "Preflight OK"

# ---------- A4: pre-shipped sandboxes exist ----------

step "A4 — verify pre-shipped sandbox '$DEFAULT_SANDBOX_NAME' exists"
SANDBOXES_JSON=$(persona_admin GET "/v1/sandboxes" 2>/dev/null || true)
SANDBOX_ID=$(echo "$SANDBOXES_JSON" | jq -r --arg n "$DEFAULT_SANDBOX_NAME" '.sandboxes[]? | select(.name==$n) | .id' 2>/dev/null || true)
if [ -z "$SANDBOX_ID" ] || [ "$SANDBOX_ID" = "null" ]; then
    fail "A4" "pre-shipped sandbox '$DEFAULT_SANDBOX_NAME' not found in /v1/sandboxes — bootstrap should seed it (see plan A4)"
fi
green "Sandbox '$DEFAULT_SANDBOX_NAME' present (id=$SANDBOX_ID)"

# ---------- create test actors (uses existing /v1/actors endpoint) ----------

step "Create test actors '$DEMO_ALLOWED_NAME' and '$DEMO_BLOCKED_NAME'"

# Re-create from clean state: delete if leftover from a previous run.
EXISTING=$(persona_admin GET "/v1/actors")
for name in "$DEMO_ALLOWED_NAME" "$DEMO_BLOCKED_NAME"; do
    leftover=$(echo "$EXISTING" | jq -r --arg n "$name" '.actors[]? | select(.name==$n) | .id' || true)
    if [ -n "$leftover" ] && [ "$leftover" != "null" ]; then
        persona_admin DELETE "/v1/actors/$leftover" >/dev/null
    fi
done

ALLOWED_RESP=$(persona_admin POST "/v1/actors" "{\"name\":\"$DEMO_ALLOWED_NAME\",\"type\":\"Agent\"}")
ALLOWED_ACTOR_ID=$(echo "$ALLOWED_RESP" | jq -r '.actor.id')
[ -n "$ALLOWED_ACTOR_ID" ] && [ "$ALLOWED_ACTOR_ID" != "null" ] \
    || fail "preflight" "actor creation returned no id: $ALLOWED_RESP"

BLOCKED_RESP=$(persona_admin POST "/v1/actors" "{\"name\":\"$DEMO_BLOCKED_NAME\",\"type\":\"Agent\"}")
BLOCKED_ACTOR_ID=$(echo "$BLOCKED_RESP" | jq -r '.actor.id')
[ -n "$BLOCKED_ACTOR_ID" ] && [ "$BLOCKED_ACTOR_ID" != "null" ] \
    || fail "preflight" "actor creation returned no id: $BLOCKED_RESP"

green "Actors created (allowed=$ALLOWED_ACTOR_ID, blocked=$BLOCKED_ACTOR_ID)"

# ---------- A5: assign sandbox to demo-allowed via PUT /v1/actors/{id}/sandbox ----------

step "A5 — assign '$DEFAULT_SANDBOX_NAME' to '$DEMO_ALLOWED_NAME'"
ASSIGN_RESP=$(persona_admin PUT "/v1/actors/$ALLOWED_ACTOR_ID/sandbox" "{\"sandbox_id\":\"$SANDBOX_ID\"}" 2>&1) \
    || fail "A5" "PUT /v1/actors/{id}/sandbox endpoint not implemented yet"
ASSIGNED=$(echo "$ASSIGN_RESP" | jq -r '.sandbox_id' 2>/dev/null || true)
[ "$ASSIGNED" = "$SANDBOX_ID" ] \
    || fail "A5" "sandbox assignment did not stick: $ASSIGN_RESP"
green "Sandbox assigned to '$DEMO_ALLOWED_NAME'"

# ---------- A7: sandbox-profile reflects assignment ----------

step "A7 — sandbox-profile for '$DEMO_ALLOWED_NAME' contains Bash"
SVC_TOKEN=$(cerebro_token_or_bootstrap)
PROFILE=$(curl -sS -m 5 -H "Authorization: Bearer $SVC_TOKEN" \
    "$PERSONA_URL/v1/actors/$ALLOWED_ACTOR_ID/sandbox-profile") \
    || fail "A7" "GET /v1/actors/{id}/sandbox-profile not implemented yet"

ALLOWED_TOOLS=$(echo "$PROFILE" | jq -r '.allowed_tools[]?' 2>/dev/null || true)
echo "$ALLOWED_TOOLS" | grep -qx "Bash" \
    || fail "A7" "allowed_tools missing 'Bash' for actor with $DEFAULT_SANDBOX_NAME sandbox: $PROFILE"
green "Profile correct: allowed_tools includes Bash"

# Profile for blocked actor must NOT contain gated tools.
BLOCKED_PROFILE=$(curl -sS -m 5 -H "Authorization: Bearer $SVC_TOKEN" \
    "$PERSONA_URL/v1/actors/$BLOCKED_ACTOR_ID/sandbox-profile")
BLOCKED_TOOLS=$(echo "$BLOCKED_PROFILE" | jq -r '.allowed_tools[]?' 2>/dev/null || true)
if echo "$BLOCKED_TOOLS" | grep -qx "Bash"; then
    fail "A7" "demo-blocked has Bash in allowed_tools — should be empty: $BLOCKED_PROFILE"
fi
green "Blocked actor's profile correctly omits gated tools"

# ---------- A6 + B4: /v1/check honours sandbox grants ----------

step "A6/B4 — /v1/check ALLOWS bash for demo-allowed, DENIES for demo-blocked"
CHECK_BODY_TEMPLATE='{"subject_actor_id":"%s","action":"call-tool","resource":{"kind":"claude.tool.bash","id":"e2e-spawn","attributes":{"tool":"Bash","args":{"command":"ls -la"}}}}'

ALLOW_CHECK=$(curl -sS -m 5 -H "Authorization: Bearer $SVC_TOKEN" -H "Content-Type: application/json" \
    -X POST "$PERSONA_URL/v1/check" \
    -d "$(printf "$CHECK_BODY_TEMPLATE" "$ALLOWED_ACTOR_ID")")
ALLOW_DECISION=$(echo "$ALLOW_CHECK" | jq -r '.allowed' 2>/dev/null || true)
[ "$ALLOW_DECISION" = "true" ] \
    || fail "A6" "expected allow for demo-allowed bash call, got: $ALLOW_CHECK"
green "demo-allowed: ALLOW (reason: $(echo "$ALLOW_CHECK" | jq -r '.reason' 2>/dev/null))"

BLOCK_CHECK=$(curl -sS -m 5 -H "Authorization: Bearer $SVC_TOKEN" -H "Content-Type: application/json" \
    -X POST "$PERSONA_URL/v1/check" \
    -d "$(printf "$CHECK_BODY_TEMPLATE" "$BLOCKED_ACTOR_ID")")
BLOCK_DECISION=$(echo "$BLOCK_CHECK" | jq -r '.allowed' 2>/dev/null || true)
[ "$BLOCK_DECISION" = "false" ] \
    || fail "A6" "expected deny for demo-blocked bash call, got: $BLOCK_CHECK"
green "demo-blocked: DENY (reason: $(echo "$BLOCK_CHECK" | jq -r '.reason' 2>/dev/null))"

# ---------- B4 hook-binary integration: hook returns the same decision ----------

step "B4 — cerebro-persona-hook returns matching decisions"
HOOK_BIN="$CEREBRO_REPO/server/bin/cerebro-persona-hook"
[ -x "$HOOK_BIN" ] \
    || fail "B4" "hook binary not built at $HOOK_BIN — \`make build\` should produce it (see plan B4)"

HOOK_INPUT='{"tool":"Bash","args":{"command":"ls -la"},"workdir":"/tmp"}'

if echo "$HOOK_INPUT" | env \
   PERSONA_URL="$PERSONA_URL" \
   PERSONA_SERVICE_TOKEN="$SVC_TOKEN" \
   PERSONA_ACTOR_ID="$ALLOWED_ACTOR_ID" \
   PERSONA_SPAWN_ID="e2e-spawn-allow" \
   "$HOOK_BIN" >/dev/null 2>"$STATE_DIR/hook-allow.err"; then
    green "Hook ALLOW: exit 0 for demo-allowed"
else
    fail "B4" "hook denied demo-allowed (should allow): $(cat "$STATE_DIR/hook-allow.err")"
fi

if echo "$HOOK_INPUT" | env \
   PERSONA_URL="$PERSONA_URL" \
   PERSONA_SERVICE_TOKEN="$SVC_TOKEN" \
   PERSONA_ACTOR_ID="$BLOCKED_ACTOR_ID" \
   PERSONA_SPAWN_ID="e2e-spawn-block" \
   "$HOOK_BIN" >/dev/null 2>"$STATE_DIR/hook-block.err"; then
    fail "B4" "hook allowed demo-blocked (should deny). stderr: $(cat "$STATE_DIR/hook-block.err")"
fi
grep -qi "denied" "$STATE_DIR/hook-block.err" \
    || fail "B4" "hook stderr missing 'Denied' for demo-blocked: $(cat "$STATE_DIR/hook-block.err")"
green "Hook DENY: exit≠0 with 'Denied by persona' for demo-blocked"

# ---------- C1 end-to-end: spawn Claude Code through Cerebro and verify ----------
#
# This is the highest-value assertion: a real Claude Code spawn driven by the
# Cerebro daemon, gated by persona's hook. It depends on B5–B6 (settings.json
# template + spawn-flow wiring) being in place.
#
# Implementation note for future runs: Cerebro's spawn flow is task-driven via
# the Multica server's agent_task_queue. For this assertion to be self-contained
# without requiring a logged-in test user, the simplest path is a dedicated
# `multica` CLI command (or a thin debug endpoint) that takes (persona-actor-id,
# prompt) and returns the agent's stdout. If that doesn't exist, the script
# would need to: login as test user, create workspace, register agent, create
# issue, assign agent, poll task. That's a lot of moving parts for an e2e probe;
# a debug spawn-endpoint is simpler.
#
# Until B6 lands, fail explicitly so it's clear what's missing.

step "B5/B6 — spawn Claude Code via Cerebro, verify per-actor gating"
SPAWN_HELPER="$CEREBRO_REPO/server/bin/multica"
[ -x "$SPAWN_HELPER" ] \
    || fail "B6" "multica CLI not built at $SPAWN_HELPER — \`make build\` should produce it"

# Probe for the e2e-spawn debug subcommand. If present, use it; otherwise the
# spawn-flow integration is incomplete.
if ! "$SPAWN_HELPER" agent --help 2>&1 | grep -q "e2e-spawn"; then
    fail "B6" "multica CLI missing 'agent e2e-spawn' subcommand for scripted Claude Code launch with PERSONA_ACTOR_ID — add it as part of B6"
fi

ALLOWED_OUT="$STATE_DIR/allowed.out"
PERSONA_SERVICE_TOKEN="$SVC_TOKEN" "$SPAWN_HELPER" agent e2e-spawn \
    --persona-actor "$ALLOWED_ACTOR_ID" \
    --prompt "Run the command: ls -la" \
    >"$ALLOWED_OUT" 2>&1 \
    || fail "B6" "spawn for demo-allowed errored: $(tail -20 "$ALLOWED_OUT")"
# Allowed-actor's hook returns exit 0 for Bash, so no "Denied by persona"
# string should appear. The agent's actual prose answer varies (the sandbox
# workdir is empty so it usually says so) — what matters is the negative.
if grep -qi "Denied by persona" "$ALLOWED_OUT"; then
    fail "B6" "demo-allowed spawn was blocked despite claude-developer sandbox: $(tail -20 "$ALLOWED_OUT")"
fi
green "demo-allowed spawn: Bash NOT blocked"

BLOCKED_OUT="$STATE_DIR/blocked.out"
PERSONA_SERVICE_TOKEN="$SVC_TOKEN" "$SPAWN_HELPER" agent e2e-spawn \
    --persona-actor "$BLOCKED_ACTOR_ID" \
    --prompt "Run the command: ls -la" \
    >"$BLOCKED_OUT" 2>&1 \
    || true  # Spawn process may exit non-zero when tool denied — still want to inspect output.
# The agent paraphrases the deny in whatever language it uses, so we accept
# any of several deny indicators rather than a literal "Denied by persona".
if ! grep -qiE "(denied|blokeret|blocked|no grant|policy)" "$BLOCKED_OUT"; then
    fail "B6" "demo-blocked spawn output missing deny indicator: $(tail -20 "$BLOCKED_OUT")"
fi
green "demo-blocked spawn: Bash blocked (agent acknowledged the deny)"

# ---------- E1: runtime-level sandbox cap is the hard upper bound ----------
#
# Even though demo-allowed currently has claude-developer (which DOES grant
# Bash), passing --runtime-persona-sandbox claude-readonly to the daemon's
# spawn helper must reassign the actor to claude-readonly and have Bash
# denied. This is the real daemon flow: cerebro_service token (not
# bootstrap-admin) calls persona's assign-sandbox endpoint via the spawn
# helper. The cerebro-service-can-assign-sandboxes Cerbos policy in
# firtal-persona makes the call return 200 — without that policy the spawn
# helper would 403 here.
#
# This proves the upper-bound outcome end-to-end: an agent owner's
# permissive sandbox cannot escape an admin-set runtime cap, because the
# daemon's precedence rule reassigns the actor to the cap before spawn
# (see chooseEffectivePersonaSandbox unit test for the precedence logic).

step "E1 — runtime cap claude-readonly overrides agent's claude-developer (via daemon flow)"

CAP_OUT="$STATE_DIR/cap.out"
PERSONA_SERVICE_TOKEN="$SVC_TOKEN" "$SPAWN_HELPER" agent e2e-spawn \
    --persona-actor "$ALLOWED_ACTOR_ID" \
    --runtime-persona-sandbox "claude-readonly" \
    --prompt "Run the command: ls -la" \
    >"$CAP_OUT" 2>&1 \
    || true  # Tool-deny may surface as non-zero; output is what matters.
# The reassignment line is logged to stderr by the spawn helper. Check it
# appeared so a regression where the flag is silently dropped surfaces here.
if ! grep -q 'runtime cap active: reassigning' "$CAP_OUT"; then
    fail "E1" "spawn helper did not log runtime-cap reassignment — flag may not be wired: $(tail -20 "$CAP_OUT")"
fi
if ! grep -qiE "(denied|blokeret|blocked|no grant|policy)" "$CAP_OUT"; then
    fail "E1" "runtime cap did not block Bash for demo-allowed: $(tail -20 "$CAP_OUT")"
fi
green "Runtime cap claude-readonly: Bash blocked via daemon flow (upper bound enforced)"

# ---------- C1: activity-feed shows both decisions ----------

step "C1 — activity feed contains both decisions with correct actor names"
# Persona's activity log lives at /v1/log/search (filterable). We pull a
# fresh slice for each test actor and check at least one allow/deny entry
# matches.
ALLOW_LOG=$(persona_admin GET "/v1/log/search?actor=$DEMO_ALLOWED_NAME&decision=allow&limit=50")
echo "$ALLOW_LOG" | jq -e '.entries | length > 0' >/dev/null \
    || fail "C1" "no allow-entry for $DEMO_ALLOWED_NAME in activity feed: $ALLOW_LOG"

BLOCK_LOG=$(persona_admin GET "/v1/log/search?actor=$DEMO_BLOCKED_NAME&decision=deny&limit=50")
echo "$BLOCK_LOG" | jq -e '.entries | length > 0' >/dev/null \
    || fail "C1" "no deny-entry for $DEMO_BLOCKED_NAME in activity feed: $BLOCK_LOG"
green "Activity feed shows both decisions"

# ---------- done ----------

green ""
green "═══════════════════════════════════════════"
green "  e2e-persona.sh: ALL ASSERTIONS PASSED"
green "═══════════════════════════════════════════"
exit 0
