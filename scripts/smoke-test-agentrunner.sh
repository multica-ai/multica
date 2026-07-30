#!/usr/bin/env bash
# smoke-test-agentrunner.sh — Cloud-runnable agentfarm smoke test.
#
# Executes from inside the smoke agentrunner pod. Validates the essential
# agentrunner path (auth → workspace → agent exists → smoke task →
# agent reply) against the dev agentfarm server via the public ingress,
# with no AWS or kubectl dependency and no per-run namespace churn.
#
# There is deliberately no runtime-liveness or heartbeat probe. Whether a
# runtime can actually claim and execute work is proven by the smoke task
# completing (phases 8-9), not by a registry row or a last_seen_at
# timestamp. Do not reintroduce one.
#
# Triggered by a Multica autopilot (upstream-sync webhook + 30-min schedule)
# or manually by creating an issue assigned to the Engineer agent, which
# runs this script via the smoke skill imported from ai-enhancement-hub.
#
# Required env (all injected via agentrunner-secrets ESO envFrom — no new SSM keys):
#   MULTICA_PAT            Bot PAT (falls back to MULTICA_TOKEN when run as an agent task)
#   MULTICA_WORKSPACE_ID   Smoke workspace UUID (long-lived, provisioned once)
#   MULTICA_SERVER_URL     https://agentfarm.development.g2.com
#   ANTHROPIC_API_KEY      LiteLLM virtual key (confirms LLM path is wired)
#   SMOKE_TASK_TIMEOUT     Seconds to wait for agent reply (default: 300)
#
# Optional — sync-pipeline reporting (all unset ⇒ behaves exactly as before):
#   SMOKE_SYNC_ISSUE_ID           Sync ticket to report the verdict onto. One ticket
#                                 per sync hop, created by scripts/sync-tick.sh.
#   SMOKE_AUDIT_ROOT_COMMENT_ID   Root comment of that ticket's audit thread; the
#                                 verdict is posted as a reply under it.
#   SMOKE_PR_REPO                 owner/repo of the sync PR (e.g. g2crowd/agentfarm)
#   SMOKE_PR_NUMBER               Sync PR to comment the machine-readable verdict on
#   SMOKE_ARTIFACT_KIND           pr_sha | merge_sha — which artifact this run attests
#   SMOKE_ARTIFACT_SHA            Full SHA of that artifact
#   SMOKE_LABEL                   Human label for the banner (default: "Smoke")
#
# Optional — other:
#   ATLASSIAN_SITE         Atlassian Cloud site URL. NOT an SSM key — plain env in
#                          gitops/base/agent-runtime/deployment.yaml, default
#                          https://g2crowd.atlassian.net.
#   JIRA_EMAIL             Atlassian account email.
#   JIRA_PAT               Atlassian API token.
#
# GitHub credentials are needed ONLY when SMOKE_PR_NUMBER is set. ANK-38 correctly
# rejected listing them as required env when this script made no GitHub calls; the
# opt-in PR report below is the first one, so they are documented here as
# conditional rather than required. `gh` in the agentrunner authenticates as a
# GitHub App via GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY_FILE — GITHUB_TOKEN and
# GH_TOKEN are both unset, so any doc claiming a GitHub PAT env var is wrong.
#
# ── Why the reporting shape is what it is ─────────────────────────────────────
# This script used to leave an orphaned `Smoke <timestamp>` issue behind on every
# invocation, and pin `last_smoke_status` on one permanent issue via
# SMOKE_STATUS_ISSUE_ID. Both were rejected by ANK-34 constraint 1: pipeline state
# belongs to one ticket per sync, not to a single ticket forever, and a smoke result
# that is not attached to the sync it attests cannot be reasoned about. So:
#
#   * the verdict goes to the SYNC ticket, threaded under the audit root;
#   * a machine-readable marker goes on the PR, keyed to the artifact SHA. The PR
#     is the only bus between the dev and tools workspaces — they are separate
#     namespaces with separate tokens, and neither can call the other's Multica
#     API. Keying to the SHA is what lets sync-tick.sh tell which artifact a PASS
#     attests, and therefore which blocked gate a later PASS may auto-clear;
#   * the inner marker-reply issue stays (it is the only thing that proves an agent
#     can actually claim and execute work) but is cancelled on the way out and its
#     result rolled onto the sync ticket, so it stops accumulating as litter;
#   * SMOKE_STATUS_ISSUE_ID is gone.
#
# Those three back `acli`: agentfarm-bootstrap.sh runs `acli jira auth login` from
# them at pod startup. Phase 6 probes the resulting session and reports a WARNING
# when acli is absent, the session is missing or expired, or Atlassian is
# unreachable — never a failure, and never an early exit. They are optional because
# JIRA is not on the agentrunner path this script gates, so neither an Atlassian
# outage nor an unprovisioned JIRA credential may read as an agentfarm smoke
# failure. Do not make phase 6 fatal again.

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────
MULTICA_PAT="${MULTICA_PAT:-${MULTICA_TOKEN:-}}"
: "${MULTICA_PAT:?MULTICA_PAT is required (and MULTICA_TOKEN is also unset)}"
: "${MULTICA_WORKSPACE_ID:?MULTICA_WORKSPACE_ID is required}"
: "${MULTICA_SERVER_URL:?MULTICA_SERVER_URL is required}"
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY is required}"

SMOKE_TASK_TIMEOUT="${SMOKE_TASK_TIMEOUT:-300}"

# Sync-pipeline reporting. Every one of these is optional: with none set the
# script runs exactly as it did before and reports only to its own log.
SMOKE_SYNC_ISSUE_ID="${SMOKE_SYNC_ISSUE_ID:-}"
SMOKE_AUDIT_ROOT_COMMENT_ID="${SMOKE_AUDIT_ROOT_COMMENT_ID:-}"
SMOKE_PR_REPO="${SMOKE_PR_REPO:-}"
SMOKE_PR_NUMBER="${SMOKE_PR_NUMBER:-}"
SMOKE_ARTIFACT_KIND="${SMOKE_ARTIFACT_KIND:-}"
SMOKE_ARTIFACT_SHA="${SMOKE_ARTIFACT_SHA:-}"
SMOKE_LABEL="${SMOKE_LABEL:-Smoke}"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
NONCE="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 8 || true)"
# Alphanumeric + underscores only — Claude reproduces this token faithfully.
MARKER="SMOKE_OK_${TIMESTAMP//[^0-9A-Za-z]/_}_${NONCE}"

SMOKE_ISSUE_ID=""
SMOKE_PROJECT_ID=""
SMOKE_RESULT="fail:unknown"
# Newline-joined bullet list of non-blocking findings. A string rather than an
# array so the EXIT trap can read it under `set -u` without an empty-array guard.
SMOKE_WARNINGS=""

# ── Helpers ────────────────────────────────────────────────────────────────
log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
fail() { SMOKE_RESULT="fail:$*"; log "FAIL: $*"; exit 1; }
# Degraded but not disqualifying: recorded, surfaced in the banner and the result
# comment, and deliberately does not touch SMOKE_RESULT or the exit code.
warn() { SMOKE_WARNINGS+="${SMOKE_WARNINGS:+$'\n'}- $*"; log "WARN: $*"; }

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

# Scratch dir for --content-file bodies. multica rejects a --content-file outside
# the current working directory (MUL-4252), and inlining a multi-line body via
# --content lets the shell rewrite backticks and quotes in it (MUL-2904) — which
# matters here because the verdict contains both. So it must live under CWD; and
# when CWD is a checkout it must stay out of `git status`, because a sibling
# upstream-sync.sh run in the same checkout refuses to start on a dirty tree.
# .git/info/exclude covers that. `git rev-parse --git-path` rather than a literal
# .git/ because an agent checkout is a linked worktree: .git is a FILE there.
SMOKE_TMPD=".smoke-tmp"
if _exclude="$(git rev-parse --git-path info/exclude 2>/dev/null)"; then
  mkdir -p "$(dirname "${_exclude}")" 2>/dev/null || true
  grep -qxF '/.smoke-tmp/' "${_exclude}" 2>/dev/null \
    || printf '/.smoke-tmp/\n' >> "${_exclude}" 2>/dev/null || true
fi
mkdir -p "${SMOKE_TMPD}"

# ── Teardown (EXIT trap) ───────────────────────────────────────────────────
teardown() {
  log "=== phase 10: teardown ==="
  if [[ -n "${SMOKE_PROJECT_ID}" ]]; then
    multica project delete "${SMOKE_PROJECT_ID}" 2>/dev/null \
      || log "smoke project cleanup skipped"
  fi

  local _status _headline _body _f
  if [[ "${SMOKE_RESULT}" == "pass" ]]; then
    _status="PASS"
    _headline="**${SMOKE_LABEL} PASS** — ${MULTICA_SERVER_URL}"
  else
    _status="FAIL"
    _headline="**${SMOKE_LABEL} FAIL** — ${MULTICA_SERVER_URL}: \`${SMOKE_RESULT#fail:}\`"
  fi

  _body="${_headline}"
  [[ -n "${MARKER}" ]] && _body+=$'\n'"marker: \`${MARKER}\`"
  if [[ -n "${SMOKE_ARTIFACT_KIND}" && -n "${SMOKE_ARTIFACT_SHA}" ]]; then
    _body+=$'\n'"${SMOKE_ARTIFACT_KIND}: \`${SMOKE_ARTIFACT_SHA}\`"
  fi
  if [[ -n "${SMOKE_WARNINGS}" ]]; then
    _body+=$'\n\nWarnings (non-blocking):\n'"${SMOKE_WARNINGS}"
  fi

  # 1 · The inner marker-reply issue. It exists only to prove an agent can claim
  #     and execute work, so it is a throwaway: record the verdict on it, then
  #     cancel it so it stops reading as an open task nobody owns.
  if [[ -n "${SMOKE_ISSUE_ID}" ]]; then
    _f="${SMOKE_TMPD}/inner.md"
    printf '%s\n' "${_body}" > "${_f}"
    multica issue comment add "${SMOKE_ISSUE_ID}" --content-file "${_f}" >/dev/null 2>&1 \
      || log "inner result comment skipped"
    multica issue status "${SMOKE_ISSUE_ID}" cancelled >/dev/null 2>&1 \
      || log "inner issue status update skipped"
    rm -f "${_f}"
  fi

  # 2 · The sync ticket — one ticket per hop, threaded under its audit root.
  if [[ -n "${SMOKE_SYNC_ISSUE_ID}" ]]; then
    _f="${SMOKE_TMPD}/sync.md"
    {
      printf '%s\n' "${_body}"
      if [[ -n "${SMOKE_ISSUE_ID}" ]]; then
        printf '\nInner agent-claim task (throwaway, cancelled): `%s`\n' "${SMOKE_ISSUE_ID}"
      fi
    } > "${_f}"
    if [[ -n "${SMOKE_AUDIT_ROOT_COMMENT_ID}" ]]; then
      multica issue comment add "${SMOKE_SYNC_ISSUE_ID}" \
        --parent "${SMOKE_AUDIT_ROOT_COMMENT_ID}" --content-file "${_f}" >/dev/null 2>&1 \
        || log "sync ticket audit reply skipped"
    else
      multica issue comment add "${SMOKE_SYNC_ISSUE_ID}" --content-file "${_f}" >/dev/null 2>&1 \
        || log "sync ticket comment skipped"
    fi
    rm -f "${_f}"
  fi

  # 3 · The PR — the machine-readable verdict, and the only signal that crosses
  #     the workspace boundary. The marker must stay on the FIRST line and keep
  #     this exact shape: scripts/sync-tick.sh greps for
  #     `smoke-result <kind>=<sha>` and captures `status=(PASS|FAIL)`.
  if [[ -n "${SMOKE_PR_NUMBER}" && -n "${SMOKE_PR_REPO}" ]]; then
    if command -v gh &>/dev/null; then
      _f="${SMOKE_TMPD}/pr.md"
      {
        if [[ -n "${SMOKE_ARTIFACT_KIND}" && -n "${SMOKE_ARTIFACT_SHA}" ]]; then
          printf '<!-- smoke-result %s=%s; status=%s -->\n' \
            "${SMOKE_ARTIFACT_KIND}" "${SMOKE_ARTIFACT_SHA}" "${_status}"
        else
          printf '<!-- smoke-result status=%s -->\n' "${_status}"
        fi
        printf '%s\n' "${_body}"
      } > "${_f}"
      # REST, not `gh pr comment`: that is a GraphQL mutation and the agentrunner's
      # GitHub App cannot reach GraphQL mutations ("Resource not accessible by
      # integration").
      gh api -X POST "repos/${SMOKE_PR_REPO}/issues/${SMOKE_PR_NUMBER}/comments" \
        -f body="$(cat "${_f}")" >/dev/null 2>&1 \
        || log "PR result comment skipped"
      rm -f "${_f}"
    else
      log "gh not on PATH — PR result comment skipped"
    fi
  fi

  rmdir "${SMOKE_TMPD}" 2>/dev/null || true
  log "teardown complete"
}
trap teardown EXIT

# ── Phase 1 · Pre-flight ───────────────────────────────────────────────────
log "=== phase 1: pre-flight ==="
# `acli` is deliberately absent: it backs only the advisory phase 6, so a pod
# without it is degraded, not broken, and must still run phases 7-9.
for cmd in multica jq; do
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

# ── Phase 6 · JIRA connectivity (acli) — advisory, never a gate ────────────
# JIRA is not on the agentrunner path this script validates (auth → workspace →
# agent exists → smoke task → agent reply). Making this fatal meant an Atlassian
# outage — or a JIRA credential that was simply never provisioned in this pod's
# secret bag — reported as an agentfarm smoke failure, and aborted the run before
# phases 7-9 tested the thing actually under test. It reports instead.
log "=== phase 6: jira connectivity (advisory) ==="
if ! command -v acli &>/dev/null; then
  warn "jira-connectivity — acli not on PATH"
elif acli jira workitem search --jql "project = AIPLAT" > /dev/null 2>&1; then
  log "jira connectivity ok"
else
  warn "jira-connectivity — acli jira workitem search failed (check ATLASSIAN_SITE / JIRA_EMAIL / JIRA_PAT and whether the pod-startup acli session is visible here)"
fi

# ── Phase 7 · Agent exists ─────────────────────────────────────────────────
log "=== phase 7: agent exists ==="
AGENT_ID=""
AGENT_ID="$(
  multica agent list --output json 2>/dev/null \
    | jq -r '.[] | select(.name=="Engineer") | .id' \
    | head -n1
)"
[[ -n "${AGENT_ID}" ]] \
  || fail "agent-exists — Engineer agent not found in workspace"
log "agent ok: Engineer (${AGENT_ID})"

# ── Phase 8 · Smoke task create ────────────────────────────────────────────
log "=== phase 8: smoke task create ==="
DESCRIPTION="Automated smoke task (${TIMESTAMP}). Reply with a comment containing exactly this token and nothing else: ${MARKER}"

# Titled with the artifact it attests when there is one, so this throwaway is
# traceable to its sync at a glance instead of reading as an orphan. Teardown
# cancels it and rolls its verdict onto the sync ticket.
INNER_TITLE="Smoke ${TIMESTAMP}"
if [[ -n "${SMOKE_ARTIFACT_SHA}" ]]; then
  INNER_TITLE="${SMOKE_LABEL} agent-claim check ${SMOKE_ARTIFACT_SHA:0:8} (${TIMESTAMP})"
fi

ISSUE_RESP="$(
  multica issue create \
    --title "${INNER_TITLE}" \
    --description "${DESCRIPTION}" \
    --assignee-id "${AGENT_ID}" \
    --status todo \
    --output json
)"
SMOKE_ISSUE_ID="$(printf '%s' "${ISSUE_RESP}" | jq -r '.id // empty')"
[[ -n "${SMOKE_ISSUE_ID}" ]] || fail "smoke-task — issue creation failed"
log "smoke issue created: ${SMOKE_ISSUE_ID} — marker: ${MARKER}"

# ── Phase 9 · Wait for agent reply ─────────────────────────────────────────
log "=== phase 9: waiting for agent reply (${SMOKE_TASK_TIMEOUT}s) ==="
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
if [[ -n "${SMOKE_WARNINGS}" ]]; then
  log "  warnings (non-blocking):"
  while IFS= read -r _w; do log "    ${_w}"; done <<< "${SMOKE_WARNINGS}"
fi
log ""
# EXIT trap runs teardown automatically.
