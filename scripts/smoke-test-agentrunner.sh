#!/usr/bin/env bash
# smoke-test-agentrunner.sh — Cloud-runnable agentfarm smoke test.
#
# Executes from inside the smoke agentrunner pod. Validates the essential
# agentrunner path (auth → workspace → runtime liveness → agent exists →
# smoke task → agent reply) against the dev agentfarm server via the public
# ingress, with no AWS or kubectl dependency and no per-run namespace churn.
#
# Triggered by a Multica autopilot (upstream-sync webhook + 30-min schedule)
# or manually by creating an issue assigned to the Engineer agent, which
# runs this script via the smoke skill imported from ai-enhancement-hub.
#
# Required env (all injected via agentrunner-secrets ESO envFrom — no new SSM keys):
#   MULTICA_PAT            Bot PAT
#   MULTICA_WORKSPACE_ID   Smoke workspace UUID (long-lived, provisioned once)
#   MULTICA_SERVER_URL     https://agentfarm.development.g2.com
#   ANTHROPIC_API_KEY      LiteLLM virtual key (confirms LLM path is wired)
#   SMOKE_TASK_TIMEOUT     Seconds to wait for agent reply (default: 300)
#   WORKSPACE_SLUG         Injected by AppSet; used to identify this pod's runtime
#
# Optional:
#   SMOKE_STATUS_ISSUE_ID  If set, pin last_smoke_status metadata on this issue

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────
: "${MULTICA_PAT:?MULTICA_PAT is required}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID is required}"
: "${MULTICA_SERVER_URL:?MULTICA_SERVER_URL is required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG is required}"

SMOKE_TASK_TIMEOUT="${SMOKE_TASK_TIMEOUT:-300}"
SMOKE_STATUS_ISSUE_ID="${SMOKE_STATUS_ISSUE_ID:-}"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
NONCE="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 8 || true)"
# Alphanumeric + underscores only — Claude reproduces this token faithfully.
MARKER="SMOKE_OK_${TIMESTAMP//[^0-9A-Za-z]/_}_${NONCE}"

DEVICE_PREFIX="agentrunner-${WORKSPACE_SLUG}"
SMOKE_ISSUE_ID=""
SMOKE_PROJECT_ID=""
SMOKE_RESULT="fail:unknown"

# ── Helpers ────────────────────────────────────────────────────────────────
log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
fail() { SMOKE_RESULT="fail:$*"; log "FAIL: $*"; exit 1; }

# poll <label> <timeout_s> <check_cmd>
poll() {
  local label="$1" timeout_s="$2" cmd="$3"
  local deadline=$(( $(date +%s) + timeout_s ))
  log "waiting: ${label} (${timeout_s}s)"
  while (( $(date +%s) < deadline )); do
    if eval "$cmd" 2>/dev/null; then return 0; fi
    sleep 5
  done
  fail "${label} — timeout"
}

# ── Teardown (EXIT trap) ───────────────────────────────────────────────────
teardown() {
  log "=== phase 11: teardown ==="
  if [[ -n "${SMOKE_PROJECT_ID}" ]]; then
    multica project delete "${SMOKE_PROJECT_ID}" 2>/dev/null \
      || log "smoke project cleanup skipped"
  fi
  if [[ -n "${SMOKE_ISSUE_ID}" ]]; then
    if [[ "${SMOKE_RESULT}" == "pass" ]]; then
      _comment="PASS — smoke test completed successfully."
    else
      _comment="FAIL — ${SMOKE_RESULT#fail:}"
    fi
    multica issue comment add "${SMOKE_ISSUE_ID}" \
      --content "${_comment}" 2>/dev/null \
      || log "result comment skipped"
    multica issue status "${SMOKE_ISSUE_ID}" in_review 2>/dev/null \
      || log "smoke issue status update skipped"
  fi
  if [[ -n "${SMOKE_STATUS_ISSUE_ID}" ]]; then
    multica issue metadata set "${SMOKE_STATUS_ISSUE_ID}" \
      --key last_smoke_status --value "${SMOKE_RESULT}" 2>/dev/null \
      || log "metadata pin skipped"
  fi
  log "teardown complete"
}
trap teardown EXIT

# ── Phase 1 · Pre-flight ───────────────────────────────────────────────────
log "=== phase 1: pre-flight ==="
for cmd in multica jq acli; do
  command -v "$cmd" &>/dev/null \
    || fail "pre-flight — required command not found: ${cmd}"
done
log "pre-flight ok — marker: ${MARKER}"

# ── Phase 2 · API + auth ───────────────────────────────────────────────────
log "=== phase 2: api + auth ==="
# GET /api/workspaces validates TLS, auth, and DB in one shot.
multica workspace list --output json \
  | jq -e '. | type == "array"' > /dev/null \
  || fail "api-auth — workspace list failed (auth or DB check)"
log "api + auth ok"

# ── Phase 3 · Workspace verify ─────────────────────────────────────────────
log "=== phase 3: workspace verify ==="
multica workspace list --output json \
  | jq -e --arg id "${MULTICA_WORKSPACE_ID}" \
      '.[] | select(.id==$id) | .id' > /dev/null \
  || fail "workspace-verify — smoke workspace ${MULTICA_WORKSPACE_ID} not reachable"
log "workspace ok: ${MULTICA_WORKSPACE_ID}"

# ── Phase 4 · Project create ───────────────────────────────────────────────
log "=== phase 4: project create ==="
SMOKE_PROJECT_RESP="$(
  multica project create \
    --title "smoke-${TIMESTAMP}" \
    --output json 2>/dev/null
)"
SMOKE_PROJECT_ID="$(printf '%s' "${SMOKE_PROJECT_RESP}" | jq -r '.id // empty')"
[[ -n "${SMOKE_PROJECT_ID}" ]] \
  || fail "project-create — project creation failed"
log "smoke project created: ${SMOKE_PROJECT_ID}"

# ── Phase 5 · Repo attach (multica-repo-workspace) ─────────────────────────
log "=== phase 5: repo attach ==="
multica project resource add "${SMOKE_PROJECT_ID}" \
  --type github_repo \
  --url "https://github.com/g2crowd/actions_test" \
  --output json 2>/dev/null \
  | jq -e '.id // empty' > /dev/null \
  || fail "repo-attach — failed to attach github repo to smoke project"
log "repo attach ok"

# ── Phase 6 · JIRA connectivity (acli) ────────────────────────────────────
log "=== phase 6: jira connectivity ==="
acli jira workitem search --jql "project = AIPLAT" > /dev/null 2>&1 \
  || fail "jira-connectivity — acli jira workitem search failed (check acli credentials)"
log "jira connectivity ok"

# ── Phase 7 · Runtime liveness ─────────────────────────────────────────────
log "=== phase 7: runtime liveness ==="
RUNTIME_ID=""
RUNTIME_ID="$(
  multica runtime list --output json 2>/dev/null \
    | jq -r --arg p "${DEVICE_PREFIX}" \
        '.[] | select(.device_info | startswith($p)) | select(.provider=="claude") | .id' \
    | head -n1
)"
[[ -n "${RUNTIME_ID}" ]] \
  || fail "runtime-liveness — claude runtime not found for ${DEVICE_PREFIX}"

# Verify heartbeat is fresh (last_seen_at within 60s). Skip if field absent.
RUNTIME_LAST_SEEN="$(
  multica runtime list --output json 2>/dev/null \
    | jq -r --arg id "${RUNTIME_ID}" \
        '.[] | select(.id==$id) | .last_seen_at // empty'
)"
if [[ -n "${RUNTIME_LAST_SEEN}" ]]; then
  NOW_TS="$(date +%s)"
  # GNU date first, then BSD date (macOS).
  RUNTIME_TS="$(date -d "${RUNTIME_LAST_SEEN}" +%s 2>/dev/null \
    || date -j -f '%Y-%m-%dT%H:%M:%SZ' "${RUNTIME_LAST_SEEN}" +%s 2>/dev/null \
    || echo "${NOW_TS}")"
  AGE=$(( NOW_TS - RUNTIME_TS ))
  (( AGE <= 60 )) || fail "runtime-liveness — heartbeat stale (${AGE}s)"
  log "runtime heartbeat ok (${AGE}s old)"
fi
log "runtime ok: ${RUNTIME_ID}"

# ── Phase 8 · Agent exists ─────────────────────────────────────────────────
log "=== phase 8: agent exists ==="
AGENT_ID=""
AGENT_ID="$(
  multica agent list --output json 2>/dev/null \
    | jq -r '.[] | select(.name=="Engineer") | .id' \
    | head -n1
)"
[[ -n "${AGENT_ID}" ]] \
  || fail "agent-exists — Engineer agent not found in workspace"
log "agent ok: Engineer (${AGENT_ID})"

# ── Phase 9 · Smoke task create ────────────────────────────────────────────
log "=== phase 9: smoke task create ==="
DESCRIPTION="Automated smoke task (${TIMESTAMP}). Reply with a comment containing exactly this token and nothing else: ${MARKER}"

ISSUE_RESP="$(
  multica issue create \
    --title "Smoke ${TIMESTAMP}" \
    --description "${DESCRIPTION}" \
    --assignee-id "${AGENT_ID}" \
    --status todo \
    --output json
)"
SMOKE_ISSUE_ID="$(printf '%s' "${ISSUE_RESP}" | jq -r '.id // empty')"
[[ -n "${SMOKE_ISSUE_ID}" ]] || fail "smoke-task — issue creation failed"
log "smoke issue created: ${SMOKE_ISSUE_ID} — marker: ${MARKER}"

# ── Phase 10 · Wait for agent reply ────────────────────────────────────────
log "=== phase 10: waiting for agent reply (${SMOKE_TASK_TIMEOUT}s) ==="
poll "comment containing ${MARKER}" "${SMOKE_TASK_TIMEOUT}" "
  multica issue comment list '${SMOKE_ISSUE_ID}' --output json 2>/dev/null \
    | jq -e --arg m '${MARKER}' \
        '[.[].content] | any(. != null and (. | contains(\$m)))' > /dev/null"

SMOKE_RESULT="pass"
log ""
log "╔══════════════════════════════════════╗"
log "║       SMOKE TEST PASSED              ║"
log "╚══════════════════════════════════════╝"
log "  server:  ${MULTICA_SERVER_URL}"
log "  marker:  ${MARKER}"
log ""
# EXIT trap runs teardown automatically.
