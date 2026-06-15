#!/usr/bin/env bash
# smoke-test-dev.sh — End-to-end smoke test for the agentfarm development server.
#
# Validates the full user-facing path after an upstream sync + deploy to dev:
#   server health → workspace create → runner provision → agent task → teardown
#
# IMPORTANT — PAT requirement:
#   MULTICA_PAT must be a token belonging to the same account stored at
#   /agentfarm/development/agentrunner-dev/shared/MULTICA_PAT (the runner bot).
#   The runner pod authenticates with that shared PAT; the workspace was just
#   created by MULTICA_PAT, so the owner must match for the daemon to connect.
#   In practice: export MULTICA_PAT to the value you stored in shared/MULTICA_PAT.
#
# Required env vars:
#   MULTICA_PAT          Personal access token (see note above)
#   ANTHROPIC_API_KEY  Claude API key for the smoke workspace runner
#   AWS_PROFILE_TOOLS  AWS CLI profile for the tools account (SSM read/write)
#   AWS_PROFILE_DEV    AWS CLI profile for the development account (SSM write)
#   KUBE_CONTEXT_TOOLS kubectl context for the tools cluster (ArgoCD app check)
#   KUBE_CONTEXT_DEV   kubectl context for the development cluster (pod check)
#
# Optional env vars:
#   TASK_TIMEOUT  Seconds to wait for the agent to post its reply (default: 300)
#   SERVER        Override the target agentfarm server URL

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
SERVER="${SERVER:-https://agentfarm.development.g2.com}"
TASK_TIMEOUT="${TASK_TIMEOUT:-300}"
SSM_REGION="us-east-1"

: "${MULTICA_PAT:?MULTICA_PAT is required (see file header)}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"
: "${AWS_PROFILE_TOOLS:?AWS_PROFILE_TOOLS is required}"
: "${AWS_PROFILE_DEV:?AWS_PROFILE_DEV is required}"
: "${KUBE_CONTEXT_TOOLS:?KUBE_CONTEXT_TOOLS is required}"
: "${KUBE_CONTEXT_DEV:?KUBE_CONTEXT_DEV is required}"

TIMESTAMP="$(date -u +%Y%m%d-%H%M%S)"
SLUG="smoke-${TIMESTAMP}"
# AppSet generates namespace = agentrunner-dev-<slug>; device name matches namespace.
DEVICE_NAME="agentrunner-dev-${SLUG}"
ARGO_APP="agentrunner-dev-${SLUG}"
RUNNER_NS="agentrunner-dev-${SLUG}"

SSM_WORKSPACE_PREFIX="/agentfarm/development/agentrunner-dev/${SLUG}"
SSM_REGISTRY_KEY="/agentfarm/tools/agentrunner-dev/${SLUG}"

# Strip hyphens so the marker is a clean alphanumeric token Claude will reproduce faithfully.
MARKER="SMOKE_OK_${TIMESTAMP//-/}"

WORKSPACE_ID=""
ISSUE_ID=""
SSM_SEEDED=0

# ── Helpers ───────────────────────────────────────────────────────────────────
log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

# Unauthenticated or auth-only calls (no workspace context).
api() {
  local method="$1" path="$2"; shift 2
  curl -fsS --max-time 15 -X "$method" "${SERVER}${path}" \
    -H "Authorization: Bearer ${MULTICA_PAT}" \
    "$@"
}

# Workspace-scoped calls.
ws() {
  local method="$1" path="$2"; shift 2
  curl -fsS --max-time 15 -X "$method" "${SERVER}${path}" \
    -H "Authorization: Bearer ${MULTICA_PAT}" \
    -H "X-Workspace-ID: ${WORKSPACE_ID}" \
    "$@"
}

# poll <label> <timeout_s> <check_command_string>
# Runs eval "$3" every 5 s until it returns 0 or timeout expires.
poll() {
  local label="$1" timeout_s="$2" cmd="$3"
  local deadline=$(( $(date +%s) + timeout_s ))
  log "waiting: ${label} (${timeout_s}s)"
  while (( $(date +%s) < deadline )); do
    if eval "$cmd" 2>/dev/null; then return 0; fi
    sleep 5
  done
  fail "timeout waiting for: ${label}"
}

# ── Teardown (always runs via EXIT trap) ──────────────────────────────────────
teardown() {
  log "=== teardown ==="
  if [[ -n "${WORKSPACE_ID}" ]]; then
    log "deleting workspace ${WORKSPACE_ID}"
    # DELETE requires the workspace-owner role; the middleware checks X-Workspace-ID
    ws DELETE "/api/workspaces/${WORKSPACE_ID}" || log "workspace delete skipped (may not exist)"
  fi
  if (( SSM_SEEDED )); then
    log "deleting SSM params"
    aws ssm delete-parameters \
      --profile  "${AWS_PROFILE_DEV}" \
      --region   "${SSM_REGION}" \
      --names \
        "${SSM_WORKSPACE_PREFIX}/AGENTFARM_WORKSPACE_ID" \
        "${SSM_WORKSPACE_PREFIX}/ANTHROPIC_API_KEY" \
        "${SSM_WORKSPACE_PREFIX}/OPENAI_API_KEY" \
      > /dev/null || true
    aws ssm delete-parameter \
      --profile "${AWS_PROFILE_TOOLS}" \
      --region  "${SSM_REGION}" \
      --name    "${SSM_REGISTRY_KEY}" \
      > /dev/null || true
  fi
  # Wait up to 90s for the runner namespace to terminate before exiting.
  log "waiting for namespace ${RUNNER_NS} to terminate"
  local deadline=$(( $(date +%s) + 90 ))
  while (( $(date +%s) < deadline )); do
    if ! kubectl get namespace "${RUNNER_NS}" \
        --context "${KUBE_CONTEXT_DEV}" \
        --ignore-not-found -o name 2>/dev/null | grep -q .; then
      log "namespace terminated"
      break
    fi
    sleep 5
  done
  log "teardown complete"
}
trap teardown EXIT

# ── Phase 1 · Pre-flight ──────────────────────────────────────────────────────
log "=== phase 1: pre-flight ==="
for cmd in aws kubectl jq curl; do
  command -v "$cmd" &>/dev/null || fail "required command not found: ${cmd}"
done
# Verify AWS profiles resolve without error.
aws sts get-caller-identity --profile "${AWS_PROFILE_TOOLS}" > /dev/null \
  || fail "AWS_PROFILE_TOOLS credentials invalid or expired"
aws sts get-caller-identity --profile "${AWS_PROFILE_DEV}" > /dev/null \
  || fail "AWS_PROFILE_DEV credentials invalid or expired"
log "pre-flight ok — slug: ${SLUG}, marker: ${MARKER}"

# ── Phase 2 · Server health ───────────────────────────────────────────────────
log "=== phase 2: health ==="
# /health and /readyz are not exposed through the public ingress — only /api/*
# is routed to the backend. GET /api/workspaces validates TLS, auth, API
# routing, and DB in one shot.
api GET /api/workspaces | jq -e '. | type == "array"' > /dev/null \
  || fail "server not reachable: GET /api/workspaces failed (auth + DB check)"
log "server reachable (auth + DB ok)"

# ── Phase 3 · Workspace creation ──────────────────────────────────────────────
log "=== phase 3: workspace ==="
ws_create_resp="$(
  api POST /api/workspaces \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"Smoke Test ${TIMESTAMP}\",\"slug\":\"${SLUG}\"}"
)"
WORKSPACE_ID="$(printf '%s' "${ws_create_resp}" | jq -r '.id // empty')"
[[ -n "${WORKSPACE_ID}" ]] \
  || fail "workspace creation failed: ${ws_create_resp}"
log "workspace created: ${WORKSPACE_ID}"

# ── Phase 4 · Seed SSM ────────────────────────────────────────────────────────
log "=== phase 4: seed SSM ==="

# Development account — per-workspace secrets read by the runner pod via ESO.
aws ssm put-parameter \
  --profile  "${AWS_PROFILE_DEV}" \
  --region   "${SSM_REGION}" \
  --name     "${SSM_WORKSPACE_PREFIX}/AGENTFARM_WORKSPACE_ID" \
  --value    "${WORKSPACE_ID}" \
  --type     SecureString \
  --overwrite > /dev/null
log "dev SSM: AGENTFARM_WORKSPACE_ID"

aws ssm put-parameter \
  --profile  "${AWS_PROFILE_DEV}" \
  --region   "${SSM_REGION}" \
  --name     "${SSM_WORKSPACE_PREFIX}/ANTHROPIC_API_KEY" \
  --value    "${ANTHROPIC_API_KEY}" \
  --type     SecureString \
  --overwrite > /dev/null
log "dev SSM: ANTHROPIC_API_KEY"

aws ssm put-parameter \
  --profile  "${AWS_PROFILE_DEV}" \
  --region   "${SSM_REGION}" \
  --name     "${SSM_WORKSPACE_PREFIX}/OPENAI_API_KEY" \
  --value    "placeholder-smoke-test" \
  --type     SecureString \
  --overwrite > /dev/null
log "dev SSM: OPENAI_API_KEY (placeholder)"

# Tools account — registry key that triggers the AppSet to emit a new Application.
aws ssm put-parameter \
  --profile  "${AWS_PROFILE_TOOLS}" \
  --region   "${SSM_REGION}" \
  --name     "${SSM_REGISTRY_KEY}" \
  --value    "${SLUG}" \
  --type     String \
  --overwrite > /dev/null
log "tools SSM registry key: ${SSM_REGISTRY_KEY}"
SSM_SEEDED=1

# Force an immediate ESO refresh on the agentrunner-registry secret so we don't
# wait up to 5 minutes for the scheduled cycle to pick up the new registry key.
log "triggering ESO force-sync on agentrunner-registry"
kubectl annotate externalsecret agentrunner-registry \
  -n agentrunner-dev-generator \
  --context "${KUBE_CONTEXT_TOOLS}" \
  "force-sync=$(date +%s)" \
  --overwrite > /dev/null
sleep 5  # give ESO a moment to process the new annotation

# ── Phase 5 · Runner provisioning ─────────────────────────────────────────────
log "=== phase 5: runner provisioning ==="

# ESO refresh is triggered above; AppSet reconciles within ~2min, then ArgoCD
# syncs and the pod schedules. Allow up to 5 minutes.
poll "ArgoCD app ${ARGO_APP} Healthy" 300 \
  "kubectl get application ${ARGO_APP} -n argocd \
     --context ${KUBE_CONTEXT_TOOLS} \
     --ignore-not-found \
     -o jsonpath='{.status.health.status}' 2>/dev/null | grep -q '^Healthy\$'"

poll "pod Running in ${RUNNER_NS}" 120 \
  "kubectl get pod -n ${RUNNER_NS} \
     --context ${KUBE_CONTEXT_DEV} \
     --field-selector status.phase=Running \
     --ignore-not-found \
     -o name 2>/dev/null | grep -q ."

log "runner pod Running"

# ── Phase 6 · Daemon + agent provisioning ─────────────────────────────────────
log "=== phase 6: agent provisioning ==="

# The entrypoint starts the daemon; agentfarm-bootstrap.sh waits for the claude
# runtime to register, sets it public, then creates agents from bundled templates.
# Both steps are complete once at least one public agent appears in the workspace.
RUNTIME_ID=""
poll "claude runtime registered for ${DEVICE_NAME}" 90 "
  RUNTIME_ID=\"\$(ws GET /api/runtimes \
    | jq -r --arg d '${DEVICE_NAME}' \
        '.[] | select(.device_info | startswith(\$d))
              | select(.provider == \"claude\") | .id' \
    | head -1)\";
  [[ -n \"\${RUNTIME_ID}\" ]]"
log "runtime registered: ${RUNTIME_ID}"

AGENT_UUID=""
poll "agent created in workspace" 120 "
  AGENT_UUID=\"\$(ws GET /api/agents \
    | jq -r '.[0].id // empty')\";
  [[ -n \"\${AGENT_UUID}\" ]]"
log "agent ready: ${AGENT_UUID}"

# ── Phase 7 · Smoke task ──────────────────────────────────────────────────────
log "=== phase 7: smoke task ==="
DESCRIPTION="Automated smoke test $(date -u). Reply to this issue with a single comment containing exactly the following token (nothing else): ${MARKER}"

issue_resp="$(
  ws POST /api/issues \
    -H "Content-Type: application/json" \
    -d "$(jq -n \
      --arg title "Smoke test ${TIMESTAMP}" \
      --arg desc  "${DESCRIPTION}" \
      --arg at    "agent" \
      --arg aid   "${AGENT_UUID}" \
      '{title:$title, description:$desc, status:"todo", priority:"medium",
        assignee_type:$at, assignee_id:$aid, allow_duplicate:true}')"
)"
ISSUE_ID="$(printf '%s' "${issue_resp}" | jq -r '.id // empty')"
[[ -n "${ISSUE_ID}" ]] || fail "issue creation failed: ${issue_resp}"
log "issue created: ${ISSUE_ID} — marker: ${MARKER}"

# ── Phase 8 · Wait for agent reply ────────────────────────────────────────────
log "=== phase 8: waiting for agent reply (${TASK_TIMEOUT}s) ==="
poll "comment containing ${MARKER}" "${TASK_TIMEOUT}" "
  ws GET '/api/issues/${ISSUE_ID}/comments' \
    | jq -e --arg m '${MARKER}' \
        '[.[].content] | any(. != null and (. | contains(\$m)))' > /dev/null"

log ""
log "╔══════════════════════════════════════╗"
log "║       SMOKE TEST PASSED ✓            ║"
log "╚══════════════════════════════════════╝"
log "  server:  ${SERVER}"
log "  slug:    ${SLUG}"
log "  marker:  ${MARKER}"
log ""
# EXIT trap runs teardown automatically.
