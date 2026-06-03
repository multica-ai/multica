#!/usr/bin/env bash
# agentfarm-bootstrap.sh — agentfarm workspace provisioning.
#
# Called by entrypoint.sh in the background after the multica config is written
# and the daemon is started in the foreground. Waits for the daemon to register
# its claude runtime, then flips visibility and creates agents from templates.
#
# Reads from secret bag (env):   MULTICA_PAT, MULTICA_WORKSPACE_ID, LITELLM_API_KEY
# Reads from Downward API (env): WORKSPACE_SLUG (set from metadata.namespace)
# Optional from secret bag:      GIT_USER_EMAIL, JIRA_PAT
#                                (both together trigger acli auth; either missing skips)
# Defaulted constant:            ATLASSIAN_SITE (https://g2crowd.atlassian.net)
# Hardcoded constants:           MULTICA_SERVER_URL, LITELLM_BASE_URL (workspace-invariant)
# Reads from image:              /etc/multica/agent-templates/

set -euo pipefail

# ── Hardcoded workspace-invariant URLs ────────────────────────────────────────
readonly MULTICA_SERVER_URL="https://agentfarm.g2.com"
readonly LITELLM_BASE_URL="https://llmproxy.g2.com"

# ── 0. Sanity-check required env. Fail loud over silent partial provisioning. ─
: "${MULTICA_PAT:?MULTICA_PAT missing from secret bag}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID missing from secret bag}"
: "${LITELLM_API_KEY:?LITELLM_API_KEY missing from secret bag}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG missing — must be injected via Downward API (fieldRef: metadata.namespace)}"

# Trim agentrunner- prefix: namespace is "agentrunner-<slug>", device name must match.
SLUG="${WORKSPACE_SLUG#agentrunner-}"
DEVICE_NAME="agentrunner-${SLUG}"

# ── 1. Point CLI at the agentfarm server and log in as the bot. ───────────────
#    Write config directly (mirrors entrypoint.sh pattern) to avoid interactive
#    prompts from 'multica setup self-host' when pointing at a remote server.
config_dir="${HOME}/.multica"
mkdir -p "${config_dir}"
umask 077
cat > "${config_dir}/config.json" <<JSON
{
  "server_url": "${MULTICA_SERVER_URL}",
  "app_url": "${MULTICA_SERVER_URL}",
  "token": "${MULTICA_PAT}",
  "workspace_id": "${MULTICA_WORKSPACE_ID}"
}
JSON

# ── 1a. Optional: acli Atlassian auth (jira). ────────────────────────────────
#    --token reads from stdin — keeps JIRA_PAT off argv and /proc/<pid>/cmdline.
#    acli --site wants the bare hostname; strip the scheme from ATLASSIAN_SITE.
: "${ATLASSIAN_SITE:=https://g2crowd.atlassian.net}"
ATLASSIAN_SITE_HOST="${ATLASSIAN_SITE#*//}"

if [[ -n "${GIT_USER_EMAIL:-}" && -n "${JIRA_PAT:-}" ]]; then
  printf '%s' "${JIRA_PAT}" | acli jira auth login \
    --site  "${ATLASSIAN_SITE_HOST}" \
    --email "${GIT_USER_EMAIL}" \
    --token
  echo "agentfarm-bootstrap: acli authenticated against ${ATLASSIAN_SITE_HOST} as ${GIT_USER_EMAIL}"
else
  echo "agentfarm-bootstrap: GIT_USER_EMAIL or JIRA_PAT unset — skipping acli auth (acli still on PATH, just unauthenticated)"
fi

# ── 2. Wait for the claude runtime to register (≤60s). ──────────────────────
#    The daemon is started in the foreground by entrypoint.sh; we just poll here.
CLAUDE_RUNTIME_ID=""
for _ in $(seq 1 60); do
  CLAUDE_RUNTIME_ID="$(
    multica runtime list --output json \
      | jq -r --arg name "${DEVICE_NAME}" \
          '.[] | select(.device_info | startswith($name)) | select(.provider=="claude") | .id' \
      | head -n1
  )"
  [[ -n "${CLAUDE_RUNTIME_ID}" ]] && break
  sleep 1
done
[[ -n "${CLAUDE_RUNTIME_ID}" ]] || {
  echo "agentfarm-bootstrap: claude runtime did not register within 60s — daemon startup may have failed" >&2
  exit 1
}
echo "agentfarm-bootstrap: claude runtime registered: ${CLAUDE_RUNTIME_ID}"

# ── 4. Flip the claude runtime to public. ────────────────────────────────────
#    No multica CLI command for this today; curl is acceptable per PLA-339.
#    Idempotent: public→public is a no-op in runtime.go's UpdateAgentRuntime.
curl -fsS -X PATCH "${MULTICA_SERVER_URL}/api/runtimes/${CLAUDE_RUNTIME_ID}" \
  -H "Authorization: Bearer ${MULTICA_PAT}" \
  -H "X-Workspace-ID: ${MULTICA_WORKSPACE_ID}" \
  -H "Content-Type: application/json" \
  -d '{"visibility":"public"}'
echo "agentfarm-bootstrap: runtime ${CLAUDE_RUNTIME_ID} set to public"

# ── 5. Build the provider env matrix (Anthropic + OpenAI only). ─────────────
#    Both providers route through the same litellm virtual key.
#    - Anthropic at root, OpenAI at /v1 — matches SDK suffix conventions baked
#      into agent-runtime-base/Dockerfile:174-175.
#    - GEMINI_API_KEY deliberately omitted: bundled templates pin
#      claude-sonnet-4-6; gandalf does not write a Gemini key per workspace.
CUSTOM_ENV_FILE="$(mktemp)"
chmod 600 "${CUSTOM_ENV_FILE}"
jq -n \
  --arg anthropic_url "${LITELLM_BASE_URL}" \
  --arg anthropic_key "${LITELLM_API_KEY}" \
  --arg openai_url "${LITELLM_BASE_URL}/v1" \
  --arg openai_key "${LITELLM_API_KEY}" \
  '{
    ANTHROPIC_BASE_URL: $anthropic_url,
    ANTHROPIC_API_KEY:  $anthropic_key,
    OPENAI_BASE_URL:    $openai_url,
    OPENAI_API_KEY:     $openai_key
  }' > "${CUSTOM_ENV_FILE}"

# ── 6. Loop the bundled templates. ───────────────────────────────────────────
#    Default kit ships two: Engineer (lifecycle-triggered) + Reviewer (on-demand,
#    invoked by issue reassignment). Extras (PM, Architect, DevOps, etc.) are
#    opt-in installs from ai-enhancement-hub — not part of the default kit.
#    409 = agent_workspace_name_unique constraint = already exists = idempotent skip.
for tmpl in /etc/multica/agent-templates/*.yaml; do
  name="$(yq -r '.name' "${tmpl}")"
  description="$(yq -r '.description' "${tmpl}")"
  model="$(yq -r '.model' "${tmpl}")"
  max="$(yq -r '.max_concurrent_tasks' "${tmpl}")"
  visibility="$(yq -r '.visibility' "${tmpl}")"
  inst_file="$(yq -r '.instructions_file' "${tmpl}")"
  inst_path="$(dirname "${tmpl}")/${inst_file}"
  instructions="$(cat "${inst_path}")"

  set +e
  multica agent create \
    --name "${name}" \
    --description "${description}" \
    --model "${model}" \
    --max-concurrent-tasks "${max}" \
    --visibility "${visibility}" \
    --runtime-id "${CLAUDE_RUNTIME_ID}" \
    --instructions "${instructions}" \
    --custom-env-file "${CUSTOM_ENV_FILE}"
  rc=$?
  set -e

  if [[ ${rc} -ne 0 ]]; then
    if ! multica agent list --output json \
        | jq -e --arg n "${name}" '.[] | select(.name==$n)' > /dev/null; then
      echo "agentfarm-bootstrap: agent create failed for '${name}' (rc=${rc}) and agent does not exist — aborting" >&2
      rm -f "${CUSTOM_ENV_FILE}"
      exit 1
    fi
    echo "agentfarm-bootstrap: agent '${name}' already exists — skipping (idempotent)"
  else
    echo "agentfarm-bootstrap: agent '${name}' created"
  fi
done

rm -f "${CUSTOM_ENV_FILE}"
echo "agentfarm-bootstrap: agentfarm provisioning complete — returning to entrypoint"
