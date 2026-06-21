#!/usr/bin/env bash
# agentfarm-bootstrap.sh — agentfarm workspace provisioning.
#
# Called by entrypoint.sh in the background after the multica config is written
# and the daemon is started in the foreground. Waits for the daemon to register
# its claude runtime, then flips visibility and creates agents from templates.
#
# Reads from secret bag (env):   MULTICA_PAT, MULTICA_WORKSPACE_ID, WORKSPACE_SLUG, ANTHROPIC_API_KEY, OPENAI_API_KEY
# Optional from secret bag:      JIRA_EMAIL, JIRA_PAT
#                                (both together trigger acli auth; either missing skips)
#                                DEFAULT_GIT_REPO (comma-separated URLs; seeds workspace repos)
# Defaulted constant:            ATLASSIAN_SITE (https://g2crowd.atlassian.net)
# Env-overridable (default set): MULTICA_SERVER_URL (dev pipeline points it at the dev server)
# Hardcoded constants:           LITELLM_BASE_URL (workspace-invariant)
# Reads from image:              /etc/multica/agent-templates/

set -euo pipefail

# ── Server / LLM URLs ─────────────────────────────────────────────────────────
# MULTICA_SERVER_URL defaults to the tools/prod server; override via env to point
# at the development agentfarm server.
readonly MULTICA_SERVER_URL="${MULTICA_SERVER_URL:-https://agentfarm.g2.com}"
readonly LITELLM_BASE_URL="https://llmproxy.g2.com"

# ── 0. Sanity-check required env. Fail loud over silent partial provisioning. ─
: "${MULTICA_PAT:?MULTICA_PAT missing from secret bag}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID missing from secret bag}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY missing from secret bag}"
: "${OPENAI_API_KEY:?OPENAI_API_KEY missing from secret bag}"
: "${WORKSPACE_SLUG:?WORKSPACE_SLUG missing from secret bag}"

DEVICE_NAME="agentrunner-${WORKSPACE_SLUG}"

# ── 1. Optional: acli Atlassian auth (jira). ─────────────────────────────────
#    --token reads from stdin — keeps JIRA_PAT off argv and /proc/<pid>/cmdline.
#    acli --site wants the bare hostname; strip the scheme from ATLASSIAN_SITE.
: "${ATLASSIAN_SITE:=https://g2crowd.atlassian.net}"
ATLASSIAN_SITE_HOST="${ATLASSIAN_SITE#*//}"

if [[ -n "${JIRA_EMAIL:-}" && -n "${JIRA_PAT:-}" ]]; then
  if printf '%s' "${JIRA_PAT}" | acli jira auth login \
      --site  "${ATLASSIAN_SITE_HOST}" \
      --email "${JIRA_EMAIL}" \
      --token; then
    echo "agentfarm-bootstrap: acli authenticated against ${ATLASSIAN_SITE_HOST} as ${JIRA_EMAIL}"
  else
    echo "agentfarm-bootstrap: acli auth failed — continuing without Jira auth (acli still on PATH, just unauthenticated)" >&2
  fi
else
  echo "agentfarm-bootstrap: JIRA_EMAIL or JIRA_PAT unset — skipping acli auth (acli still on PATH, just unauthenticated)"
fi

# ── 2. Wait for the claude runtime to register. ─────────────────────────────
#    The daemon is started in the foreground by entrypoint.sh; we just poll here.
#    The daemon registers runtimes into every workspace the bot PAT belongs to,
#    serially (~20s each), and our target workspace can be processed last — so the
#    wait must outlast N-workspace registration, not a fixed 60s. Budget is a true
#    wall-clock bound (SECONDS) and stays well under gandalf's 15-min provisioning
#    poll. Override via BOOTSTRAP_RUNTIME_WAIT_SECS.
readonly RUNTIME_WAIT_SECS="${BOOTSTRAP_RUNTIME_WAIT_SECS:-600}"
CLAUDE_RUNTIME_ID=""
deadline=$(( SECONDS + RUNTIME_WAIT_SECS ))
while (( SECONDS < deadline )); do
  # || true: a transient runtime-list failure over the long window must not trip
  # set -e and abort provisioning — just retry on the next tick.
  CLAUDE_RUNTIME_ID="$(
    multica runtime list --output json 2>/dev/null \
      | jq -r --arg name "${DEVICE_NAME}" \
          '.[] | select(.device_info | startswith($name)) | select(.provider=="claude") | .id' \
      | head -n1 || true
  )"
  [[ -n "${CLAUDE_RUNTIME_ID}" ]] && break
  sleep 2
done
[[ -n "${CLAUDE_RUNTIME_ID}" ]] || {
  echo "agentfarm-bootstrap: claude runtime did not register within ${RUNTIME_WAIT_SECS}s — daemon startup may have failed" >&2
  exit 1
}
echo "agentfarm-bootstrap: claude runtime registered: ${CLAUDE_RUNTIME_ID}"

# ── 4. Flip all runtimes for this device to public. ──────────────────────────
#    No multica CLI command for this today; curl is acceptable per PLA-339.
#    Idempotent: public→public is a no-op in runtime.go's UpdateAgentRuntime.
while IFS= read -r _rid; do
  curl -fsS -X PATCH "${MULTICA_SERVER_URL}/api/runtimes/${_rid}" \
    -H "Authorization: Bearer ${MULTICA_PAT}" \
    -H "X-Workspace-ID: ${MULTICA_WORKSPACE_ID}" \
    -H "Content-Type: application/json" \
    -d '{"visibility":"public"}'
  echo "agentfarm-bootstrap: runtime ${_rid} set to public"
done < <(multica runtime list --output json \
  | jq -r --arg name "${DEVICE_NAME}" \
      '.[] | select(.device_info | startswith($name)) | .id')

# ── 5. Build the provider env matrix (Anthropic + OpenAI only). ─────────────
#    gandalf writes ANTHROPIC_API_KEY and OPENAI_API_KEY into the secret bag (both
#    are the same litellm virtual key today); we consume them as-is and only inject
#    the workspace-invariant base URLs here.
#    - Anthropic at root, OpenAI at /v1 — matches SDK suffix conventions baked
#      into agent-runtime-base/Dockerfile:174-175.
#    - GEMINI_API_KEY deliberately omitted: bundled templates pin
#      claude-sonnet-4-6; gandalf does not write a Gemini key per workspace.
CUSTOM_ENV_FILE="$(mktemp)"
chmod 600 "${CUSTOM_ENV_FILE}"
jq -n \
  --arg anthropic_url "${LITELLM_BASE_URL}" \
  --arg anthropic_key "${ANTHROPIC_API_KEY}" \
  --arg openai_url "${LITELLM_BASE_URL}/v1" \
  --arg openai_key "${OPENAI_API_KEY}" \
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

# ── 7. Seed workspace repos from DEFAULT_GIT_REPO. ───────────────────────────
#    Add-only merge: existing user-added repos are preserved; seeds are appended
#    only when absent. Idempotent: no PATCH if the desired state already matches.
#    Non-fatal: GET/PATCH failure logs a warning and lets boot continue.
#    TODO: if future migrations add per-repo fields (e.g. default_branch), the
#    bare {"url": "..."} shape here may overwrite those fields for seeded repos.
if [[ -z "${DEFAULT_GIT_REPO:-}" ]]; then
  echo "agentfarm-bootstrap: DEFAULT_GIT_REPO unset — skipping repo seeding"
else
  _seed_json="$(
    printf '%s' "${DEFAULT_GIT_REPO}" \
      | tr ',' '\n' \
      | sed 's/[[:space:]]//g' \
      | grep -v '^$' \
      | jq -R '{"url": .}' \
      | jq -s '.'
  )"

  set +e
  _get_resp="$(curl -fsS "${MULTICA_SERVER_URL}/api/workspaces/${MULTICA_WORKSPACE_ID}" \
    -H "Authorization: Bearer ${MULTICA_PAT}" \
    -H "X-Workspace-ID: ${MULTICA_WORKSPACE_ID}")"
  _rc=$?
  set -e

  if [[ ${_rc} -ne 0 ]]; then
    echo "agentfarm-bootstrap: workspace repo seeding failed (rc=${_rc}) — continuing" >&2
  else
    _current_json="$(printf '%s' "${_get_resp}" | jq '.repos // []')"
    _desired_json="$(
      jq -n \
        --argjson current "${_current_json}" \
        --argjson seed "${_seed_json}" \
        '$current + ($seed | map(select(.url as $u | $current | map(.url) | index($u) == null)))'
    )"

    if jq -e --argjson a "${_desired_json}" --argjson b "${_current_json}" '$a == $b' > /dev/null; then
      echo "agentfarm-bootstrap: repos already seeded"
    else
      set +e
      curl -fsS -X PATCH "${MULTICA_SERVER_URL}/api/workspaces/${MULTICA_WORKSPACE_ID}" \
        -H "Authorization: Bearer ${MULTICA_PAT}" \
        -H "X-Workspace-ID: ${MULTICA_WORKSPACE_ID}" \
        -H "Content-Type: application/json" \
        -d "{\"repos\": ${_desired_json}}"
      _rc=$?
      set -e

      if [[ ${_rc} -ne 0 ]]; then
        echo "agentfarm-bootstrap: workspace repo seeding failed (rc=${_rc}) — continuing" >&2
      else
        echo "agentfarm-bootstrap: workspace repos seeded"
      fi
    fi
  fi
fi

echo "agentfarm-bootstrap: agentfarm provisioning complete — returning to entrypoint"
