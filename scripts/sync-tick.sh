#!/usr/bin/env bash
# sync-tick.sh — advance the upstream-sync pipeline by exactly one step, then exit.
#
# Run every 15 minutes by a `run_only` Multica autopilot in the TOOLS workspace.
# The full pipeline spans hours (dev bake + deploy, dev smoke, human merge, tools
# bake + deploy, pod roll, tools smoke), so it cannot be one long agent turn: a
# GitHub Actions run is an external system, not agent-owned work, and no agent may
# sit polling one across a turn. This script is therefore strictly non-blocking —
# it takes ONE snapshot, advances at most one stage, and returns. The recurrence
# is the polling.
#
# State lives in Multica (issue metadata + labels) and on the PR (a state table in
# the body), never in the agent's head — any tick can pick up where the last left
# off, including after a crash.
#
#   idle → syncing → dev_deploying → dev_smoke_pending → awaiting_merge
#        → tools_deploying → tools_smoke_pending → done
#
# `blocked` is reachable from any stage. `syncing` is not in the original design
# (ANK-34 §6.4); it exists so the single-flight mutex covers the upstream-sync.sh
# run itself, and so a run that dies mid-sync has somewhere to record that.
#
# ── Contract with the caller (this is what keeps the tick quiet) ───────────────
# STDOUT is the report. If this script writes nothing to stdout, the tick had
# nothing to do and the caller MUST post no comment, create no issue and notify
# nobody (ANK-34 Q5). Progress chatter goes to stderr, which the caller ignores.
# Exit status is 0 for "tick completed" — including a quiet no-op and including a
# transition into `blocked`. A non-zero exit means the tick itself malfunctioned.
#
# ── Required env ──────────────────────────────────────────────────────────────
#   MULTICA_TOKEN / MULTICA_PAT   Tools-workspace credential (agent task supplies)
#   MULTICA_SERVER_URL            https://agentfarm.g2.com
#   GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY_FILE
#                                 `gh` authenticates as the GitHub App from these.
#                                 There is no GitHub PAT in the agent runtime.
#
# ── Optional env ──────────────────────────────────────────────────────────────
#   FORK_SLUG                     default g2crowd/agentfarm
#   SYNC_REQUESTER_ID             member UUID notified once on awaiting_merge and
#                                 on entry to blocked (ANK-34 Q6)
#   SYNC_ROLLOUT_DEADLINE_MIN     default 45 — tools_deploying staleness guard
#   SYNC_ENGINEER_AGENT           default Engineer — runs the dispatched tools smoke
#   SYNC_TICK_DRY_RUN=1           read every source, mutate nothing, report the
#                                 action that would have been taken
#   SYNC_SWEEP_AGE_MIN            default 60 — age gate for the opportunistic
#                                 throwaway sweep on the quiet no-op path
#   SYNC_JIRA_PROJECT             default AIPLAT — project the per-hop item lands in
#   SYNC_JIRA_TYPE                default Task
#   SYNC_JIRA_UMBRELLA            default AIPLAT-166 — referenced in the item body.
#                                 It is a Story with no parent, so per-hop Tasks
#                                 cannot be --parent'ed under it.
#   SYNC_JIRA_TIMEOUT             default 30 — seconds per acli call
#   SYNC_JIRA_DISABLE=1           skip the JIRA mirror entirely
#   SYNC_AUTOFIX_MAX_ATTEMPTS     default 2 — cap on retry/autofix attempts per
#                                 hop per block reason, so a failure this
#                                 script can't actually fix still falls through
#                                 to a human instead of looping forever
#                                 (ANK-96 postmortem, see below)
#   SYNC_CI_RETRY_CONCLUSIONS     default "cancelled" — workflow-run conclusions
#                                 treated as a free flake retry (no autofix
#                                 budget spent) rather than a real failure
#
# ── CI auto-remediation on block (ANK-96 postmortem) ──────────────────────────
# Blocked used to be a dead end this script never looked at again: every block
# reason parked until a human read the ticket, diagnosed it and either fixed it
# by hand or removed the block. In practice this meant genuinely transient or
# mechanically-fixable CI failures (an intermittent runner flake, a stray
# version-pin drift between two files in the same sync) sat blocked for a full
# day even though nothing about them needed human judgement (ANK-96: the dev
# deploy failed on a `GO_VERSION` pin drift that a `git-diff`-visible one-line
# bump would have fixed, and instead sat `blocked` for ~24h).
#
# stage_blocked() now re-examines dev_deploy / tools_deploy failures on every
# tick, not just dev_smoke / tools_smoke:
#   * intermittent  — re-running the SAME run's failed jobs costs one
#     `gh run rerun --failed` and no autofix-attempt budget; a flaky runner
#     clears itself without ever bumping stage_entered_at away from the
#     original failure.
#   * simple/known  — a small, explicitly-recognised class of fixes (today:
#     the agent-runtime-base GO_VERSION lagging server/go.mod's `go` directive,
#     the one root cause behind ANK-96) is applied as a follow-up commit pushed
#     to the SAME sync branch, which re-triggers dev.yml/publish.yml on
#     `synchronize`. Bounded by SYNC_AUTOFIX_MAX_ATTEMPTS so a class of failure
#     this script cannot actually fix still surfaces to a human instead of
#     spinning.
#   * unrecognised — falls through to the pre-existing behaviour: parked,
#     human notified once, no further action.
# `rollout_stale` and every other reason are unaffected by this section — they
# have no CI-run failure to re-classify — but do get the human-comment handling
# below.
# Every attempt is on the audit thread either way, so "the tick tried X and it
# didn't help" is exactly as visible as "the tick is stuck and needs you".
#
# ── Human comments while blocked (ANK-96 postmortem) ──────────────────────────
# The second half of the same postmortem: a human resolved the ANK-96 block by
# commenting "The deployment to development is now successful. Remove block"
# directly on the sync ticket, without touching `blocked_reason` or the
# `sync-blocked` label — and the tick never looked at the comment thread at
# all, so the hop sat blocked for a full day after it was already fixable.
# stage_blocked() now reads for a new human comment on every tick it is
# invoked. What happens to it depends on the reason already parked:
#   * the reasons this script actively re-polls itself (dev_smoke, tools_smoke
#     via their existing smoke-verdict check; dev_deploy, tools_deploy,
#     rollout_stale via the auto-remediation above) get an acknowledgement
#     reply — the human's note was read, but the re-check already in flight is
#     the thing that actually clears the block, so nothing is force-retried
#     from the comment alone.
#   * every other reason (sync_conflict, sync_invariant, unknown_stage, a
#     stale/interrupted branch, ...) has no automatic re-check today, so a
#     comment that reads as a resolution (see RESOLUTION_KEYWORDS) triggers
#     exactly one resume: clear blocked_reason and hand the hop back to the
#     stage it was in before the block, so THAT stage's own next-tick logic
#     re-validates state rather than this code guessing the human's fix
#     worked. Bounded by SYNC_AUTOFIX_MAX_ATTEMPTS per reason, same budget as
#     the CI autofixes above, so a comment that doesn't actually fix anything
#     doesn't bounce forever either.
# Comments are matched by id against `human_comment_seen_<reason>`, so a tick
# never reacts to the same comment twice, and only comments posted at or after
# the CURRENT blocked_reason's stage_entered_at count — an old comment from a
# prior block on this same hop cannot resurrect a new one.
# ── Throwaway smoke tickets (ANK-43 scope 2) ──────────────────────────────────
# A hop leaves two disposable Multica issues behind: the tools-smoke dispatch
# ticket this script creates, and the inner agent-claim check the smoke script
# creates to prove an agent can claim work. Neither may be cancelled inline by
# the script that made it — the claiming agent's runtime posts its Ownership-mode
# `status in_review` AFTER its final comment, so an inline cancel always races the
# agent it just waited on and always loses (measured on the v0.4.14 hop: cancel at
# ~07:44:34Z, agent's in_review write at 07:44:42Z). Both are therefore swept from
# a LATER tick, at every terminal transition, which is strictly after that write.
#   * inner agent-claim checks → `cancelled` (they prove nothing once read)
#   * the tools-smoke dispatch ticket → `done` (it did real work; its result is on
#     the sync ticket)
# The work list is explicit: `tools_smoke_issue_id` plus the CSV
# `smoke_throwaway_issue_ids` the smoke script appends to. Title matching is only
# the belt-and-braces backstop on the quiet path.
#
# ── JIRA mirror (ANK-43 scope 3) ──────────────────────────────────────────────
# One AIPLAT work item per hop, mirroring the one Multica sync ticket, with a
# comment per transition. Multica's `sync-*` labels are deliberately NOT mirrored.
# JIRA is a mirror, never a source of truth: every acli call is wrapped in
# `timeout` and its failure is swallowed, `jira_key` is never a precondition for
# anything, and a JIRA outage costs the hop nothing but one degradation note on
# the Multica audit thread.
#
# ── GitHub capability notes, verified from the tools agentrunner pod ───────────
# The GitHub App CANNOT reach GraphQL mutations: `gh pr create`, `gh pr comment`
# and `gh pr edit` all return "Resource not accessible by integration". Every
# write below therefore goes through REST. GraphQL *reads* work, but always pass
# `--repo` explicitly — without it `gh` infers the repo from the checkout and can
# answer about an entirely different PR.
set -euo pipefail

# ── Git ownership trust ───────────────────────────────────────────────────────
# The runtime creates the checkout under a different uid than the one this script
# runs as, so git refuses the repository with `detected dubious ownership` and
# exits 128 on the FIRST git command — before the tick can reach a stage or report
# anything. That is ANK-49, and it is why a failing tick printed nothing at all.
#
# Concretely: everything under multica_workspaces is owned by uid 50012 while the
# agent process is uid 1000. The runtime's own fix-efs-permissions init container
# tries to chown it, but root is squashed on the EFS access point, so that chown is
# a silent no-op and the mismatch survives every pod start.
#
# Declared through GIT_CONFIG_* rather than `git config --global --add
# safe.directory <path>`, for two reasons. The env is scoped to this process tree,
# so nothing is written to the pod's shared ~/.gitconfig — which is long-lived and
# would otherwise accrete one entry per task forever. And a path-specific entry
# cannot work anyway: every task gets a freshly named workdir, so an entry added by
# one run never covers the next. Hence `*`; the tick reads several checkouts plus
# the bare object cache, and none of those paths are knowable from here.
#
# Existing GIT_CONFIG_* pairs are appended to, never overwritten, so an env-scoped
# auth header or URL rewrite placed by the runtime survives.
trust_git_checkouts() {
  local n="${GIT_CONFIG_COUNT:-0}"
  case "${n}" in ''|*[!0-9]*) n=0 ;; esac
  export "GIT_CONFIG_KEY_${n}=safe.directory"
  export "GIT_CONFIG_VALUE_${n}=*"
  export GIT_CONFIG_COUNT=$(( n + 1 ))
}
trust_git_checkouts

FORK_SLUG="${FORK_SLUG:-g2crowd/agentfarm}"
SYNC_REQUESTER_ID="${SYNC_REQUESTER_ID:-b97bf628-51c0-417a-8d15-b5bdd8789ceb}"
SYNC_ROLLOUT_DEADLINE_MIN="${SYNC_ROLLOUT_DEADLINE_MIN:-45}"
SYNC_ENGINEER_AGENT="${SYNC_ENGINEER_AGENT:-Engineer}"
DRY_RUN="${SYNC_TICK_DRY_RUN:-}"
SWEEP_AGE_MIN="${SYNC_SWEEP_AGE_MIN:-60}"
SWEEP_MAX="${SYNC_SWEEP_MAX:-20}"

JIRA_PROJECT="${SYNC_JIRA_PROJECT:-AIPLAT}"
JIRA_TYPE="${SYNC_JIRA_TYPE:-Task}"
JIRA_UMBRELLA="${SYNC_JIRA_UMBRELLA:-AIPLAT-166}"
JIRA_TIMEOUT="${SYNC_JIRA_TIMEOUT:-30}"
JIRA_DISABLE="${SYNC_JIRA_DISABLE:-}"

AUTOFIX_MAX_ATTEMPTS="${SYNC_AUTOFIX_MAX_ATTEMPTS:-2}"
CI_RETRY_CONCLUSIONS="${SYNC_CI_RETRY_CONCLUSIONS:-cancelled}"

DEV_HOST="https://agentfarm.development.g2.com"
TOOLS_HOST="https://agentfarm.g2.com"
SYNC_SCRIPT="scripts/upstream-sync.sh"
CURSOR_FILE=".upstream-sync-cursor"

# Body markers. The smoke-request / smoke-result shapes are a LIVE CONTRACT with
# the dev-workspace autopilot, which already answers them (proven on PR #243:
# request `<!-- smoke-request pr_sha=9a17b785… -->`, reply `<!-- smoke-result
# pr_sha=9a17b785…; status=PASS -->`). Do not reformat these without changing the
# dev autopilot in the same breath — it is in a different workspace, behind a
# different token, and cannot be updated from here.
STATE_START='<!-- sync-state:start -->'
STATE_END='<!-- sync-state:end -->'

log()  { printf '[sync-tick] %s\n' "$*" >&2; }
say()  { printf '%s\n' "$*"; }
die()  { log "ERROR: $*"; exit 1; }

# Checked in main() rather than here so scripts/sync-pipeline.test.sh can source
# this file and exercise the pure helpers on a host that has no agent runtime.
require_deps() {
  command -v jq >/dev/null 2>&1 || die "jq is required"
  command -v gh >/dev/null 2>&1 || die "gh is required"
  command -v multica >/dev/null 2>&1 || die "multica is required"
}

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# Scratch dir for --content-file bodies. Two constraints pull against each other:
# multica rejects a --content-file outside the current working directory
# (MUL-4252), so it must live under the repo; and upstream-sync.sh refuses to run
# on a dirty tree, so it must not show up in `git status`. Registering it in
# .git/info/exclude satisfies both. `git rev-parse --git-path` is used rather than
# a literal .git/ because an agent checkout is a linked worktree, where .git is a
# FILE and the real gitdir is elsewhere.
TMPD="${REPO_ROOT}/.sync-tick-tmp"
EXCLUDE_FILE="$(git rev-parse --git-path info/exclude)"
mkdir -p "$(dirname "${EXCLUDE_FILE}")"
if ! grep -qxF '/.sync-tick-tmp/' "${EXCLUDE_FILE}" 2>/dev/null; then
  printf '/.sync-tick-tmp/\n' >> "${EXCLUDE_FILE}"
fi
mkdir -p "${TMPD}"
trap 'rm -rf "${TMPD}"' EXIT
JIRA_FAIL_FILE="${TMPD}/jira-failures"

now_epoch() { date -u +%s; }

# ── Multica helpers ───────────────────────────────────────────────────────────
TICKET=""
META_JSON='{}'

load_meta() {
  META_JSON="$(multica issue metadata list "${TICKET}" --output json 2>/dev/null || echo '{}')"
  # A bare `null` or a non-object reply must not poison every later jq read.
  printf '%s' "${META_JSON}" | jq -e 'type == "object"' >/dev/null 2>&1 || META_JSON='{}'
}

mget() { printf '%s' "${META_JSON}" | jq -r --arg k "$1" '.[$k] // empty | tostring'; }

mset() {
  local k="$1" v="$2" t="${3:-string}"
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: metadata ${k}=${v}"; return 0; fi
  multica issue metadata set "${TICKET}" --key "${k}" --value "${v}" --type "${t}" >/dev/null \
    || { log "metadata set ${k} failed"; return 1; }
  META_JSON="$(printf '%s' "${META_JSON}" | jq --arg k "${k}" --arg v "${v}" '.[$k]=$v')"
}

mdel() {
  local k="$1"
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: metadata delete ${k}"; return 0; fi
  multica issue metadata delete "${TICKET}" --key "${k}" >/dev/null 2>&1 || true
  META_JSON="$(printf '%s' "${META_JSON}" | jq --arg k "${k}" 'del(.[$k])')"
}

label_id() {
  multica label list --output json 2>/dev/null \
    | jq -r --arg n "$1" '.[] | select(.name==$n) | .id' | head -n1
}

# Labels are the lifecycle marker a human sees on the board. Exactly one of the
# four is ever attached, so the transition itself is the visible state.
set_sync_label() {
  local want="$1" n id
  [[ -n "${DRY_RUN}" ]] && { log "DRY: label → ${want}"; return 0; }
  for n in sync-active sync-passed sync-failed sync-blocked; do
    id="$(label_id "${n}")"
    [[ -z "${id}" ]] && continue
    if [[ "${n}" == "${want}" ]]; then
      multica issue label add "${TICKET}" "${id}" >/dev/null 2>&1 || true
    else
      multica issue label remove "${TICKET}" "${id}" >/dev/null 2>&1 || true
    fi
  done
}

# Append-only audit trail: one root comment per sync, every transition a threaded
# reply under it. The CLI has no `comment edit`, so "one comment updated in place"
# (ANK-34 constraint 5) is not achievable; a thread is the closest shape that
# keeps comment IDs and notification history intact (Q3).
audit() {
  local body="$1" root f
  root="$(mget audit_root_comment_id)"
  [[ -z "${root}" ]] && { log "no audit root yet — skipping threaded reply"; return 0; }
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: audit reply under ${root}"; return 0; fi
  f="${TMPD}/audit.md"
  printf '%s\n' "${body}" > "${f}"
  multica issue comment add "${TICKET}" --parent "${root}" --content-file "${f}" --output json >/dev/null \
    || log "audit reply failed"
  rm -f "${f}"
}

# Notify a human at most once for a given key (Q6: once on entry, never per tick).
notify_once() {
  local key="$1" body="$2" root f
  [[ -z "${SYNC_REQUESTER_ID}" ]] && return 0
  [[ -n "$(mget "notified_${key}")" ]] && return 0
  root="$(mget audit_root_comment_id)"
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: notify once (${key})"; return 0; fi
  f="${TMPD}/notify.md"
  {
    printf '%s\n\n' "${body}"
    printf '[@requester](mention://member/%s)\n' "${SYNC_REQUESTER_ID}"
  } > "${f}"
  if [[ -n "${root}" ]]; then
    multica issue comment add "${TICKET}" --parent "${root}" --content-file "${f}" --output json >/dev/null \
      || log "notify failed"
  fi
  rm -f "${f}"
  mset "notified_${key}" true bool || true
}

# ── Throwaway smoke-ticket sweep ──────────────────────────────────────────────
issue_status() {
  multica issue get "$1" --output json 2>/dev/null | jq -r '.status // empty'
}

# Drive one throwaway to a terminal status. Idempotent, and never downgrades a
# status a human already set: an issue that is already done/cancelled is left be.
retire_throwaway() {
  local id="$1" want="$2" cur
  [[ -z "${id}" ]] && return 0
  cur="$(issue_status "${id}")"
  [[ -z "${cur}" ]] && { log "sweep: ${id} not readable — skipping"; return 0; }
  case "${cur}" in done|cancelled) return 0 ;; esac
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: sweep ${id} (${cur}) → ${want}"; return 0; fi
  if multica issue status "${id}" "${want}" >/dev/null 2>&1; then
    log "sweep: ${id} ${cur} → ${want}"
  else
    log "sweep: could not set ${id} → ${want}"
  fi
  return 0
}

# Explicit work list for the current hop. Called at every terminal transition, so
# it always runs at least one tick after the smoke agent's own exit write.
sweep_throwaways() {
  local ids id dispatch
  ids="$(mget smoke_throwaway_issue_ids)"
  if [[ -n "${ids}" ]]; then
    while IFS= read -r id; do
      id="${id//[[:space:]]/}"
      [[ -n "${id}" ]] && retire_throwaway "${id}" cancelled
    done < <(printf '%s' "${ids}" | tr ',' '\n')
  fi
  dispatch="$(mget tools_smoke_issue_id)"
  [[ -n "${dispatch}" ]] && retire_throwaway "${dispatch}" done
  return 0
}

# Terminal status a throwaway title should end at, or empty when the title is not
# a throwaway shape. The two live shapes are
#   `Tools smoke for <tag> (<sha>)`                 — dispatch_tools_smoke()
#   `<label> agent-claim check <sha> (<ts>)`        — smoke-test-agentrunner.sh
# plus the pre-rework `Smoke <timestamp>` inner issues, kept so the backstop can
# still tidy legacy litter.
sweep_want_status() {
  local title="$1"
  case "${title}" in
    *"agent-claim check "*)   printf 'cancelled' ;;
    Smoke\ 20[0-9][0-9]*)     printf 'cancelled' ;;
    "Tools smoke for "*)      printf 'done' ;;
    *)                        : ;;
  esac
}

# Belt and braces for the quiet no-active-sync path: tidy anything the explicit
# list missed (a hop whose ticket was deleted, a manual smoke run, the dev-side
# equivalent when this script happens to run in that workspace). Age-gated so it
# can never touch a smoke that is still in flight, bounded so a workspace full of
# litter cannot turn a tick into a long-running job, and silent — nothing is
# printed to stdout, so the caller still posts no comment.
sweep_stale_throwaways() {
  local cutoff id title want n=0
  cutoff=$(( $(now_epoch) - SWEEP_AGE_MIN * 60 ))
  while IFS=$'\t' read -r id title; do
    [[ -z "${id}" ]] && continue
    want="$(sweep_want_status "${title}")"
    [[ -z "${want}" ]] && continue
    retire_throwaway "${id}" "${want}"
    n=$(( n + 1 ))
    (( n >= SWEEP_MAX )) && { log "sweep: hit SYNC_SWEEP_MAX=${SWEEP_MAX}, stopping"; break; }
  done < <(multica issue list --limit 100 --output json 2>/dev/null \
    | jq -r --argjson cut "${cutoff}" '
        (.issues // [])[]
        | select(.status != "done" and .status != "cancelled")
        | select((.updated_at // "") != "")
        | select((.updated_at | fromdateiso8601) < $cut)
        | "\(.id)\t\(.title)"' 2>/dev/null)
  return 0
}

# ── JIRA mirror (acli) ────────────────────────────────────────────────────────
# Every call goes through jira_run, which is the whole failure-isolation story:
# a bounded timeout, stderr captured to the log, non-zero swallowed. Nothing
# below may abort a tick, change a stage or block a hop.
jira_available() {
  [[ -z "${JIRA_DISABLE}" ]] || return 1
  command -v acli >/dev/null 2>&1 || return 1
  command -v timeout >/dev/null 2>&1 || return 1
}

# Atlassian hanging is the failure mode that would actually hurt: a stalled tick
# holds its task slot and the next tick fires 15 minutes later regardless. Hence
# `timeout` on every call rather than trusting acli to give up.
#
# Failures are recorded in a FILE, not a variable: jira_run is called inside
# command substitution (`out="$(jira_run …)"`), and a subshell cannot set a global
# — noting the degradation from in here would lose the "already noted" flag and
# re-post the note on every tick. jira_degraded_flush drains the file at the end
# of the tick, in the parent shell. (JIRA_FAIL_FILE is set next to TMPD above.)
jira_run() {
  local out rc=0
  out="$(timeout "${JIRA_TIMEOUT}" acli "$@" 2>&1)" || rc=$?
  if (( rc != 0 )); then
    log "acli ${1:-} ${2:-} ${3:-} failed (exit ${rc}): $(printf '%s' "${out}" | tr '\n' ' ' | tail -c 300)"
    [[ -n "${JIRA_FAIL_FILE}" ]] \
      && printf '`acli %s %s %s` exited %s\n' "${1:-}" "${2:-}" "${3:-}" "${rc}" >> "${JIRA_FAIL_FILE}"
    return 1
  fi
  printf '%s' "${out}"
  return 0
}

# Surface degradation once per hop, the same way notify_once works — a mirror that
# is behind is worth saying, but not every 15 minutes.
jira_degraded_flush() {
  local detail
  [[ -z "${TICKET}" || -z "${JIRA_FAIL_FILE}" ]] && return 0
  [[ -s "${JIRA_FAIL_FILE}" ]] || return 0
  [[ -n "$(mget jira_degraded_noted)" ]] && return 0
  detail="$(head -n1 "${JIRA_FAIL_FILE}")"
  mset jira_degraded_noted true bool || true
  audit "_The JIRA mirror is behind._ ${detail}

JIRA is a mirror, not a gate: this hop continues unaffected, and this thread plus the PR body remain the complete record. Later transitions may be missing from the AIPLAT item."
  return 0
}

# acli takes plain text or ADF, so the Markdown used in the Multica audit thread
# would render literally. Flatten links to `text (url)` and drop the emphasis and
# table pipes rather than shipping the raw markup.
jira_flatten() {
  printf '%s\n' "$1" \
    | sed -E 's#\[([^]]*)\]\(([^)]+)\)#\1 (\2)#g' \
    | sed -E 's/`//g; s/\*\*//g' \
    | sed -E '/^[[:space:]]*\|.*\|[[:space:]]*$/ {
                s/^[[:space:]]*\|[[:space:]]*//
                s/[[:space:]]*\|[[:space:]]*$//
                s/[[:space:]]*\|[[:space:]]*/ — /g
              }' \
    | sed -E 's/^-{3,}( — -{3,})*$//'
}

jira_key_from() {
  local out="$1" key
  key="$(printf '%s' "${out}" \
    | jq -r 'if type=="object" then (.key // .issue.key // empty)
             elif type=="array" then (.[0].key // empty)
             else empty end' 2>/dev/null)"
  # acli's create output is not guaranteed to be JSON on every version, so fall
  # back to the key shape itself rather than losing a work item we just created.
  [[ -z "${key}" ]] && key="$(printf '%s' "${out}" \
    | grep -oE '[A-Z][A-Z0-9_]+-[0-9]+' | head -n1)"
  printf '%s' "${key}"
}

# Create the per-hop item, or recover the one an earlier tick created. Prints the
# key on success and returns non-zero on every failure path — callers treat that
# as "no mirror this tick", never as an error.
jira_ensure_item() {
  local from="$1" to="$2" key summary attempts out descf
  key="$(mget jira_key)"
  [[ -n "${key}" ]] && { printf '%s' "${key}"; return 0; }
  jira_available || return 1
  summary="Upstream sync ${from} → ${to}"

  # Idempotency, second line of defence: a tick that created the item but died
  # before writing metadata must not create a second one. The JQL matches loosely
  # (tags and the arrow tokenize unpredictably) and the exact summary is compared
  # here, where it is cheap and exact.
  out="$(jira_run jira workitem search \
    --jql "project = ${JIRA_PROJECT} AND summary ~ \"Upstream sync\" ORDER BY created DESC" \
    --limit 50 --fields "key,summary" --json)" || out=""
  key="$(printf '%s' "${out}" | jq -r --arg s "${summary}" \
    'if type=="array" then (.[] | select(.fields.summary == $s) | .key) else empty end' 2>/dev/null \
    | head -n1)"

  if [[ -z "${key}" ]]; then
    attempts="$(mget jira_create_attempts)"; attempts="${attempts:-0}"
    if (( attempts >= 2 )); then
      log "JIRA item creation already failed ${attempts}×; not retrying for this hop"
      return 1
    fi
    if [[ -n "${DRY_RUN}" ]]; then log "DRY: would create a ${JIRA_PROJECT} ${JIRA_TYPE} '${summary}'"; return 1; fi
    mset jira_create_attempts "$(( attempts + 1 ))" number || true
    descf="${TMPD}/jira-desc.txt"
    {
      printf 'Autonomous upstream sync hop %s -> %s, driven by scripts/sync-tick.sh in g2crowd/agentfarm.\n\n' "${from}" "${to}"
      printf 'Multica sync ticket (source of truth): %s\n' "${TICKET}"
      printf 'Umbrella item for this effort: %s\n\n' "${JIRA_UMBRELLA}"
      printf 'Each pipeline state change is mirrored here as a comment. The pipeline is autonomous up to a green dev smoke, then parks for a human to merge to main.\n'
    } > "${descf}"
    out="$(jira_run jira workitem create --project "${JIRA_PROJECT}" --type "${JIRA_TYPE}" \
      --summary "${summary}" --description-file "${descf}" --json)" || out=""
    rm -f "${descf}"
    key="$(jira_key_from "${out}")"
  fi

  [[ -z "${key}" ]] && return 1
  mset jira_key "${key}" string || true
  log "JIRA mirror: ${key}"
  printf '%s' "${key}"
}

jira_comment() {
  local body="$1" key f
  key="$(mget jira_key)"
  [[ -z "${key}" ]] && return 0
  jira_available || return 0
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: JIRA comment on ${key}"; return 0; fi
  f="${TMPD}/jira-comment.txt"
  {
    jira_flatten "${body}"
    printf '\n--\nMirrored from Multica sync ticket %s by scripts/sync-tick.sh.\n' "${TICKET}"
  } > "${f}"
  jira_run jira workitem comment create --key "${key}" --body-file "${f}" --json >/dev/null || true
  rm -f "${f}"
  return 0
}

# An unrecognised status name must degrade to a warning, never a failure: the
# AIPLAT workflow is not owned here and its transition names can change.
jira_transition() {
  local status="$1" key
  key="$(mget jira_key)"
  [[ -z "${key}" ]] && return 0
  jira_available || return 0
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: JIRA transition ${key} → ${status}"; return 0; fi
  jira_run jira workitem transition --key "${key}" --status "${status}" --yes >/dev/null \
    || log "JIRA transition of ${key} to '${status}' was not accepted — leaving its status alone"
  return 0
}

# ── GitHub helpers (REST for every write) ─────────────────────────────────────
gh_pr_json() { gh api "repos/${FORK_SLUG}/pulls/$1" 2>/dev/null; }

gh_add_label() {
  local pr="$1" label="$2"
  [[ -n "${DRY_RUN}" ]] && { log "DRY: add PR label ${label} → #${pr}"; return 0; }
  gh api -X POST "repos/${FORK_SLUG}/issues/${pr}/labels" -f "labels[]=${label}" >/dev/null 2>&1 \
    || log "could not add ${label} to #${pr}"
}

gh_remove_label() {
  local pr="$1" label="$2"
  [[ -n "${DRY_RUN}" ]] && { log "DRY: remove PR label ${label} ← #${pr}"; return 0; }
  gh api -X DELETE "repos/${FORK_SLUG}/issues/${pr}/labels/${label}" >/dev/null 2>&1 || true
}

# `jira-ref-check-and-description` reads the PR TITLE and fails when it carries no
# bracketed ref. upstream-sync.sh now stamps one in at creation time, so this only
# repairs the two paths where the title predates the ref: an orphan PR opened
# before that change and then adopted, and a hop whose JIRA item only appeared on
# a later tick. A title that already carries a ref — including one a human typed —
# is never rewritten.
ensure_pr_jira_ref() {
  local pr="$1" title key
  [[ -z "${pr}" ]] && return 0
  title="$(gh_pr_json "${pr}" | jq -r '.title // empty')"
  [[ -z "${title}" ]] && return 0
  if printf '%s' "${title}" | grep -qE '\[([A-Z][A-Z0-9_]+-[0-9]+|NO JIRA)\]'; then
    return 0
  fi
  key="$(mget jira_key)"; key="${key:-NO JIRA}"
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: retitle #${pr} → ... [${key}]"; return 0; fi
  gh api -X PATCH "repos/${FORK_SLUG}/pulls/${pr}" -f title="${title} [${key}]" >/dev/null 2>&1 \
    || { log "could not add the JIRA ref to the title of #${pr}"; return 0; }
  log "added [${key}] to the title of #${pr}"
}

gh_pr_comment() {
  local pr="$1" body="$2"
  [[ -n "${DRY_RUN}" ]] && { log "DRY: PR comment → #${pr}: ${body%%$'\n'*}"; return 0; }
  gh api -X POST "repos/${FORK_SLUG}/issues/${pr}/comments" -f body="${body}" >/dev/null \
    || log "PR comment on #${pr} failed"
}

# Most recent run of <workflow-file> for <sha> that actually attempted work.
#
# A PR accumulates several runs per SHA (`opened`, `labeled` and `synchronize` all
# fire dev.yml), so only the newest counts — but `skipped` runs must be dropped
# first, and that is not a nicety. Every dev.yml job is gated on
# `if: contains(github.event.pull_request.labels.*.name, 'Development')`, so
# OPENING the PR produces a completed run whose conclusion is `skipped`: it ran no
# jobs and attests nothing. Applying the label fires the real run seconds later.
# Reading the skipped one as the verdict blocked the sync on its very first tick —
# the most common path there is. Observed on PR #248: run 30514751613 completed
# `skipped` at 04:45:54, the real run 30514782142 appeared at 04:46:35.
gh_latest_run() {
  local wf="$1" sha="$2"
  gh api "repos/${FORK_SLUG}/actions/workflows/${wf}/runs?head_sha=${sha}&per_page=30" \
    --jq '[.workflow_runs[] | select(.conclusion != "skipped")]
          | sort_by(.created_at) | last // empty' 2>/dev/null
}

# ── CI auto-remediation helpers (ANK-96 postmortem) ───────────────────────────
# Count of autofix/retry attempts already spent on THIS block reason for THIS
# hop. Keyed by reason so dev_deploy and tools_deploy track separately, and
# reset implicitly every time a hop starts (fresh ticket, fresh metadata).
autofix_attempts() { mget "autofix_attempts_$1"; }
bump_autofix_attempts() {
  local reason="$1" n; n="$(autofix_attempts "${reason}")"; n="${n:-0}"
  mset "autofix_attempts_${reason}" "$(( n + 1 ))" number || true
}

# Re-run only the failed jobs of a completed workflow run, in place, no new
# commit. Free — does not consume the autofix budget — because it changes
# nothing about the tree; it is the correct response to a flake and nothing
# else. `gh run rerun` is a REST-backed CLI command (not a GraphQL mutation),
# confirmed reachable from the same GitHub App identity that already does
# every other write in this script.
gh_rerun_failed() {
  local runid="$1"
  [[ -n "${DRY_RUN}" ]] && { log "DRY: rerun failed jobs of run ${runid}"; return 0; }
  gh run rerun "${runid}" --repo "${FORK_SLUG}" --failed >/dev/null 2>&1 \
    || { log "could not rerun failed jobs of run ${runid}"; return 1; }
  return 0
}

# Grep the failed jobs' logs of a completed run for a pattern, bounded to what
# we need to classify the failure — never used to decide pass/fail, only to
# pick which autofix (if any) applies. `--allow-escape-sequences` is required
# by this `gh` version even for a grep-only consumer; ANSI codes are stripped
# before matching so they cannot hide or fake a match.
gh_run_failed_log_grep() {
  local runid="$1" pattern="$2"
  gh api "repos/${FORK_SLUG}/actions/runs/${runid}/jobs" --jq '.jobs[] | select(.conclusion=="failure") | .id' 2>/dev/null \
    | while IFS= read -r jobid; do
        [[ -z "${jobid}" ]] && continue
        gh api "repos/${FORK_SLUG}/actions/jobs/${jobid}/logs" --allow-escape-sequences 2>/dev/null \
          | sed 's/\x1b\[[0-9;]*m//g'
      done \
    | grep -qE -- "${pattern}"
}

# Known-cause autofix (root cause of ANK-96): agent-runtime-base's pinned
# GO_VERSION lagging server/go.mod's `go` directive fails `go mod download`
# deterministically with `go: go.mod requires go >= X.Y.Z` on EVERY retry —
# a rerun can never clear it, only a version bump can. Bumps both places that
# pin it (the bake file's default and the Dockerfile ARG default, which the
# bake file's own header comment already says must agree) and pushes straight
# to the sync branch, so the PR's existing `synchronize` trigger redeploys —
# same re-trigger path `dev_deploying`'s `cancelled` case already relies on,
# just via a new commit instead of a label cycle.
#
# Deliberately dev_deploy ONLY. tools_deploy runs `publish.yml` against
# `merge_sha` on `main` — pushing any commit there, autofix or not, is exactly
# the irreversible step ANK-34 Q4 reserves for a human, and stage_tools_deploying
# already says "the pipeline deliberately does not roll back" for that reason.
# A tools_deploy failure gets the free flake-retry below and nothing more.
#
# Runs in a throwaway `git worktree`, never the tick's own checkout — the sync
# branch may be at a different commit than whatever HEAD this script started
# at, and this must never touch main or the merge upstream-sync.sh already
# sealed. Reads GO_VERSION / the go.mod directive off the BRANCH TIP on
# origin, not this checkout, so it is correct even if this checkout is stale.
apply_go_version_autofix() {
  local pr branch required current new_sha worktree rc
  pr="$(mget sync_pr)"
  branch="$(gh_pr_json "${pr}" | jq -r '.head.ref // empty')"
  [[ -z "${branch}" ]] && { log "could not resolve the PR branch for the GO_VERSION autofix"; return 1; }

  git fetch --quiet origin "${branch}" || { log "could not fetch ${branch} for the GO_VERSION autofix"; return 1; }
  required="$(git show "origin/${branch}:server/go.mod" 2>/dev/null \
    | sed -nE 's/^go[[:space:]]+([0-9]+\.[0-9]+\.[0-9]+).*/\1/p' | head -1)"
  current="$(git show "origin/${branch}:docker/agent-runtime-base/docker-bake.hcl" 2>/dev/null \
    | sed -nE 's/^variable "GO_VERSION"[[:space:]]*\{[[:space:]]*default[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/p' | head -1)"
  if [[ -z "${required}" || -z "${current}" ]]; then
    log "could not read GO_VERSION/go.mod's go directive off ${branch} — skipping the autofix"
    return 1
  fi
  if [[ "${current}" == "${required}" ]]; then
    log "GO_VERSION (${current}) already matches server/go.mod — not the cause here"
    return 1
  fi

  if [[ -n "${DRY_RUN}" ]]; then
    log "DRY: would bump GO_VERSION ${current} → ${required} on ${branch}"
    return 0
  fi

  worktree="${TMPD}/autofix-gover"
  rm -rf "${worktree}"
  # A prior tick killed mid-autofix (pod eviction, task timeout) can leave a
  # worktree registration pointing at a directory this rm just removed, or a
  # `_autofix/<branch>` ref still checked out there — `prune` clears the first,
  # `--force` on both the branch and the add handles the second.
  git worktree prune >/dev/null 2>&1 || true
  git branch -D "_autofix/${branch}" >/dev/null 2>&1 || true
  if ! git worktree add --quiet --force -B "_autofix/${branch}" "${worktree}" "origin/${branch}" >/dev/null 2>&1; then
    log "could not create the autofix worktree for ${branch}"
    return 1
  fi
  # `set -e` does not abort on a compound command's nonzero status when that
  # status is captured by an explicit `|| rc=$?` — but WOULD abort the whole
  # tick on a bare `( ... )` followed by `rc=$?` on the next line, since that
  # is two separate simple commands. Keep the assignment on the same command.
  rc=0
  (
    cd "${worktree}"
    sed -i -E "s#(variable \"GO_VERSION\"[[:space:]]*\{[[:space:]]*default[[:space:]]*=[[:space:]]*\")[^\"]*(\")#\1${required}\2#" \
      docker/agent-runtime-base/docker-bake.hcl
    sed -i -E "s#^(ARG GO_VERSION=).*#\1${required}#" docker/agent-runtime-base/Dockerfile
    git add docker/agent-runtime-base/docker-bake.hcl docker/agent-runtime-base/Dockerfile
    git commit --quiet -m "fix(agent-runtime-base): bump GO_VERSION to ${required} to match server/go.mod"
    git push --quiet origin "HEAD:${branch}"
  ) || rc=$?
  git worktree remove --force "${worktree}" >/dev/null 2>&1 || true
  git branch -D "_autofix/${branch}" >/dev/null 2>&1 || true
  if (( rc != 0 )); then
    log "GO_VERSION autofix commit/push failed for ${branch}"
    return 1
  fi

  new_sha="$(gh_pr_json "${pr}" | jq -r '.head.sha // empty')"
  bump_autofix_attempts dev_deploy
  mset pr_sha "${new_sha}" string || true
  mdel blocked_reason
  mdel notified_blocked_dev_deploy
  set_sync_label sync-active
  [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
  advance dev_deploying "\`dev.yml\`'s build failed because \`docker/agent-runtime-base\`'s pinned \`GO_VERSION\` (\`${current}\`) is older than \`server/go.mod\`'s \`go ${required}\` directive — every retry would fail identically. Pushed a follow-up commit bumping \`GO_VERSION\` to \`${required}\` (autofix attempt $(autofix_attempts dev_deploy)/${AUTOFIX_MAX_ATTEMPTS}), now at \`${new_sha:0:8}\`. Clearing the \`dev_deploy\` block — \`synchronize\` will re-trigger the dev deploy."
  say "Applied the GO_VERSION autofix to #${pr}; sync ${TICKET} resumed at dev_deploying."
  return 0
}

# A flake retry costs nothing but is only ever attempted once per run id —
# `autofix_last_retry_run_<reason>` remembers which run this hop already
# re-ran, so a run stuck retrying forever cannot be re-kicked every 15 minutes.
# Returns 0 (handled — caller should stop here) whenever the conclusion looks
# retryable, whether or not the rerun call itself succeeded.
try_flake_retry() {
  local reason="$1" runid="$2" conclusion="$3" wf="$4" last
  [[ " ${CI_RETRY_CONCLUSIONS} " == *" ${conclusion} "* ]] || return 1
  last="$(mget "autofix_last_retry_run_${reason}")"
  if [[ "${last}" == "${runid}" ]]; then
    log "already retried ${reason} run ${runid}; waiting on it"
    return 0
  fi
  if gh_rerun_failed "${runid}"; then
    mset "autofix_last_retry_run_${reason}" "${runid}" string || true
    audit "\`${wf}\` [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) concluded \`${conclusion}\`, which looks like a runner-level flake rather than a real regression. Re-ran its failed jobs (no autofix budget spent) — staying \`${reason}\` until the retry reports."
  fi
  return 0
}

# Decide what to do about a dev_deploy failure that stage_blocked() is
# re-examining: a free retry for a runner-level flake, the one known-cause
# autofix if the logs match it, or leave it parked (unrecognised failures are
# exactly what still needs a human — this never widens to guessing).
try_dev_deploy_autofix() {
  local runid="$1" conclusion="$2" attempts
  try_flake_retry dev_deploy "${runid}" "${conclusion}" dev.yml && return 0

  attempts="$(autofix_attempts dev_deploy)"; attempts="${attempts:-0}"
  if (( attempts >= AUTOFIX_MAX_ATTEMPTS )); then
    log "autofix budget (${AUTOFIX_MAX_ATTEMPTS}) exhausted for dev_deploy — leaving parked for a human"
    return 0
  fi

  if gh_run_failed_log_grep "${runid}" 'go\.mod requires go [>=]+ [0-9]+\.[0-9]+\.[0-9]+'; then
    apply_go_version_autofix
    return 0
  fi

  log "dev_deploy failure on run ${runid} (${conclusion}) does not match a known autofix — leaving parked for a human"
}

# tools_deploy runs publish.yml against merge_sha on main — see the note on
# apply_go_version_autofix for why this stops at the free flake retry and
# never pushes a fix commit.
try_tools_deploy_autofix() {
  local runid="$1" conclusion="$2"
  try_flake_retry tools_deploy "${runid}" "${conclusion}" publish.yml && return 0
  log "tools_deploy failure on run ${runid} (${conclusion}) is post-merge — no auto-remediation, leaving parked for a human"
}

# ── Human intervention on a blocked hop ───────────────────────────────────────
# stage_blocked() used to be one-way for every reason except the two smoke
# gates: it waited silently for a human to edit metadata or the label by hand.
# In practice a human resolves a block by commenting on the ticket — "fixed,
# please continue", "remove block" — without ever touching blocked_reason or
# the sync-blocked label (ANK-96: a human posted "The deployment to
# development is now successful. Remove block" and it sat unactioned because
# nothing ever read it). This surfaces that comment every tick and, for the
# reasons this script can safely re-attempt on its own, acts on it — once per
# comment, so a tick never re-reacts to the same note twice.
RESOLUTION_KEYWORDS='resolved|fixed|retry|remove.?block|unblock|clear(ed)?|resume|go ahead|proceed|deploy(ment)?.*(now )?(successful|passed|green|works|ok)'

latest_unseen_human_comment() {
  local reason="$1" since seen comments
  since="$(mget stage_entered_at)"; since="${since:-0}"
  seen="$(mget "human_comment_seen_${reason}")"
  comments="$(multica issue comment list "${TICKET}" --output json 2>/dev/null || echo '[]')"
  printf '%s' "${comments}" | jq -c --argjson since "${since}" --arg seen "${seen}" '
    [ .[] | select(.author_type == "member")
          | select((.created_at | fromdateiso8601) >= $since)
          | select(.id != $seen) ]
    | sort_by(.created_at) | last // empty' 2>/dev/null
}

# Reasons this script already re-polls on its own every tick: the two smoke
# gates, dev_deploy/tools_deploy (try_dev_deploy_autofix / try_tools_deploy_autofix)
# and rollout_stale (re-reads deployed_version() below). A comment on one of
# these gets acknowledged, not force-retried — the re-check already running is
# what actually clears it, and force-retrying on top of that could race it
# (e.g. resetting a rerun already in flight).
is_self_repolled_reason() {
  case "$1" in
    dev_smoke|tools_smoke|dev_deploy|tools_deploy|rollout_stale) return 0 ;;
    *) return 1 ;;
  esac
}

# Reads for a new human comment on the blocked ticket and, for reasons with no
# automatic re-check, resumes the hop once if the comment reads as a
# resolution. Sets HUMAN_COMMENT_ACTED=1 when it actually resumed the hop this
# tick, so the stage_blocked() caller does not also fall through to the
# (now stale) reason-based wait case in the same tick.
handle_human_comment() {
  local reason="$1" c id body quoted attempts resume_stage
  HUMAN_COMMENT_ACTED=0
  c="$(latest_unseen_human_comment "${reason}")"
  [[ -z "${c}" || "${c}" == "null" ]] && return 1
  id="$(printf '%s' "${c}" | jq -r '.id')"
  body="$(printf '%s' "${c}" | jq -r '.content // empty')"
  mset "human_comment_seen_${reason}" "${id}" string || true
  quoted="$(printf '%s' "${body}" | head -5 | sed 's/^/> /')"

  if is_self_repolled_reason "${reason}"; then
    # Already re-checked against live GH/PR state every tick regardless of any
    # comment — acknowledge so the human sees their note was read, not just
    # eventually superseded by a silent auto-clear.
    audit "Noticed a comment on this blocked hop — re-checking \`${reason}\` against current CI/PR state now.

${quoted}"
    return 0
  fi

  if ! printf '%s' "${body}" | grep -qiE "${RESOLUTION_KEYWORDS}"; then
    audit "Noticed a comment on this blocked hop. \`${reason}\` has no automatic re-check, so this still needs your explicit next step (see the block note above) unless your comment already resolved it.

${quoted}"
    return 0
  fi

  attempts="$(autofix_attempts "retry_${reason}")"; attempts="${attempts:-0}"
  if (( attempts >= AUTOFIX_MAX_ATTEMPTS )); then
    audit "A comment looks like it's marking \`${reason}\` resolved, but this hop already used its resume budget (${AUTOFIX_MAX_ATTEMPTS}) on that reason. Leaving it blocked for you to clear by hand (edit \`blocked_reason\` or close the ticket).

${quoted}"
    return 0
  fi

  # Hand the hop back to the stage it was actually blocked FROM, so that
  # stage's own next-tick logic re-validates live state rather than this code
  # assuming the human's fix worked. Falls back to `syncing` only for a block
  # raised before any stage was recorded (defensive — block() always records
  # one today).
  resume_stage="$(mget "blocked_from_stage_${reason}")"
  resume_stage="${resume_stage:-syncing}"
  bump_autofix_attempts "retry_${reason}"
  mdel blocked_reason
  mdel "notified_blocked_${reason}"
  mdel "blocked_from_stage_${reason}"
  set_sync_label sync-active
  [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
  advance "${resume_stage}" "A comment looks like it's marking \`${reason}\` resolved — resuming at \`${resume_stage}\` (attempt $(( attempts + 1 ))/${AUTOFIX_MAX_ATTEMPTS}). That stage will re-validate live state on its own rather than trusting this blindly.

${quoted}"
  HUMAN_COMMENT_ACTED=1
  return 0
}

# Minutes since the current stage was entered, or empty if unknown.
stage_age_min() {
  local entered; entered="$(mget stage_entered_at)"
  [[ -z "${entered}" ]] && return 0
  printf '%s' $(( ( $(now_epoch) - entered ) / 60 ))
}

# Rewrite the state table in the PR body (Q3: tabular state on the PR side).
# `gh pr edit` is a GraphQL mutation and is unavailable, so this is a REST PATCH.
pr_body_update() {
  local pr="$1" blockfile="$2" cur out
  cur="${TMPD}/body.cur"; out="${TMPD}/body.new"
  gh api "repos/${FORK_SLUG}/pulls/${pr}" --jq '.body // ""' > "${cur}" 2>/dev/null || : > "${cur}"
  # Drop any previous block, then re-append — one code path for insert and update.
  awk -v s="${STATE_START}" -v e="${STATE_END}" '
    index($0, s) == 1 { skip = 1; next }
    index($0, e) == 1 { skip = 0; next }
    !skip { print }
  ' "${cur}" > "${out}"
  { echo; echo "${STATE_START}"; cat "${blockfile}"; echo "${STATE_END}"; } >> "${out}"
  if [[ -n "${DRY_RUN}" ]]; then log "DRY: PATCH body of #${pr}"; return 0; fi
  gh api -X PATCH "repos/${FORK_SLUG}/pulls/${pr}" -f body="$(cat "${out}")" >/dev/null 2>&1 \
    || log "could not rewrite body of #${pr} — state table not updated this tick"
}

render_state_table() {
  local f="${TMPD}/state.md" msha br jkey
  msha="$(mget merge_sha)"
  br="$(mget blocked_reason)"
  jkey="$(mget jira_key)"
  {
    echo '### Upstream sync pipeline'
    echo
    echo '| field | value |'
    echo '|---|---|'
    printf '| stage | `%s` |\n' "$(mget pipeline_stage)"
    printf '| from → to | `%s` → `%s` |\n' "$(mget sync_from)" "$(mget sync_to)"
    printf '| pr_sha | `%s` |\n' "$(mget pr_sha)"
    printf '| merge_sha | %s |\n' "${msha:+\`${msha}\`}"
    printf '| blocked_reason | %s |\n' "${br:+\`${br}\`}"
    printf '| jira | %s |\n' "${jkey:+[${jkey}](https://g2crowd.atlassian.net/browse/${jkey})}"
    echo
    printf '_Maintained by `scripts/sync-tick.sh`. Ticket: %s_\n' "${TICKET}"
  } > "${f}"
  printf '%s' "${f}"
}

# ── Stage transitions ─────────────────────────────────────────────────────────
advance() {
  local stage="$1" note="$2"
  mset pipeline_stage "${stage}" string || true
  mset stage_entered_at "$(now_epoch)" number || true
  log "stage → ${stage}"
  if [[ -n "${note}" ]]; then
    audit "${note}"
    # The JIRA comment mirrors the text already appended to the Multica thread,
    # so the mirror can never become a second source of truth: there is exactly
    # one place the transition text is written.
    jira_comment "Pipeline stage: ${stage}

${note}"
  fi
  # Status mirroring, per ANK-43 scope 3. `blocked` deliberately gets a comment
  # and no transition — the AIPLAT workflow has no Blocked status.
  case "${stage}" in
    awaiting_merge) jira_transition "In Review" ;;
    done)           jira_transition "Done" ;;
  esac
  local pr; pr="$(mget sync_pr)"
  [[ -n "${pr}" ]] && pr_body_update "${pr}" "$(render_state_table)"
  return 0
}

block() {
  local reason="$1" detail="$2" prev_reason prev_stage
  prev_reason="$(mget blocked_reason)"
  # Recorded before advance() overwrites pipeline_stage to `blocked`, so an
  # unrecognised reason that a human resolves by comment (see
  # handle_human_comment) can hand the hop back to the stage it was actually
  # in, rather than guessing. Only set on a genuine reason change — re-blocking
  # on the same reason must not clobber the original stage with `blocked`.
  if [[ "${prev_reason}" != "${reason}" ]]; then
    prev_stage="$(mget pipeline_stage)"
    [[ -n "${prev_stage}" && "${prev_stage}" != "blocked" ]] \
      && mset "blocked_from_stage_${reason}" "${prev_stage}" string || true
  fi
  mset blocked_reason "${reason}" string || true
  # Re-blocking on the SAME reason is now routine: stage_blocked() actively
  # re-polls dev_deploy/tools_deploy/rollout_stale every tick (ANK-96 /
  # AIPLAT-218 — a dev-deploy failure that later went green sat blocked
  # indefinitely because nothing ever looked again). Without this guard every
  # one of those polls would re-post the same audit/JIRA comment and reset
  # stage_entered_at, turning a quiet retry into 15-minute spam. A genuine
  # reason CHANGE (or the first block) still reports as before.
  if [[ "${prev_reason}" != "${reason}" ]]; then
    advance blocked "**Blocked — \`${reason}\`**

${detail}"
  else
    log "still blocked on ${reason} — not re-posting an unchanged block note"
  fi
  set_sync_label sync-blocked
  [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" blocked >/dev/null 2>&1 || true
  notify_once "blocked_${reason}" "This sync is **blocked** on \`${reason}\` and needs a human.

${detail}"
  say "Sync ${TICKET} blocked: ${reason}"
  say ""
  say "${detail}"
}

# Terminal: release the single-flight mutex so the next tick may start a new hop.
clear_guard() { mdel sync_active; }

# ── Version + release resolution ──────────────────────────────────────────────
# `multica version` is a purely local read of compile-time ldflags fed from
# MULTICA_VERSION in docker/agent-runtime-base/docker-bake.hcl. It reports the
# build inside THIS pod — which is exactly the signal we want (the agent confirms
# rollout by observing its own new runtime) but attests only this namespace, and
# only after the agentrunner has rolled. It is the sole semver marker in the
# deployment: no server version endpoint, and backend/web images are SHA-tagged.
deployed_version() {
  local v
  v="$(multica version --output json 2>/dev/null | jq -r '.version // empty')"
  [[ -z "${v}" ]] && return 1
  printf 'v%s' "${v#v}"
}

# Latest upstream release. Resolved from remote tag refs, NOT the releases API:
# the GitHub App installation is scoped to the g2crowd org, so
# `gh api repos/multica-ai/multica/releases/latest` is not readable from here.
# Consequently "skip drafts and prereleases" is enforced by tag-name shape —
# drafts have no tag ref at all, prereleases are dropped by the exact-semver
# filter. Same resolution rule as upstream-sync.sh, deliberately.
latest_upstream_tag() {
  git remote get-url upstream >/dev/null 2>&1 \
    || git remote add upstream https://github.com/multica-ai/multica.git
  git ls-remote --tags --refs upstream 'v*' 2>/dev/null \
    | sed 's#.*refs/tags/##' \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V | tail -1
}

cursor_tag() { sed -n 's/^tag=//p' "${CURSOR_FILE}" 2>/dev/null | tr -d '[:space:]'; }

open_sync_pr() {
  gh api --paginate "repos/${FORK_SLUG}/pulls?state=open&per_page=100" --jq '.[]' 2>/dev/null \
    | jq -s -c 'map(select(.head.ref | startswith("upstream-sync/")))
                | sort_by(.created_at) | last // empty' 2>/dev/null
}

# Print the name of a branch already on origin for the hop that targets ${1}, or
# nothing. Callers use it to tell "this hop has never been attempted" apart from
# "a previous attempt pushed a branch and then died".
#
# Scoped by `-to-<target>`, never a bare `upstream-sync/*`: upstream-sync.sh names
# branches `upstream-sync/<from>-to-<target>`, where <from> is the cursor tag or the
# fork's short SHA, so only the target side is predictable from here. Branches from
# hops that already merged are never deleted, so an unscoped glob matches all of
# them and reports every hop as stale.
#
# Always exits 0 — `set -e` is on, and "no such branch" is an answer, not a failure.
stale_sync_branch() {
  local target="$1" ref=""
  ref="$(git ls-remote --heads origin "upstream-sync/*-to-${target}" 2>/dev/null \
          | head -1 | awk '{print $2}')" || true
  printf '%s\n' "${ref#refs/heads/}"
}

# Read a smoke verdict off the PR. The dev workspace cannot reach this workspace's
# Multica API and vice versa (two namespaces, two tokens, ANK-34 constraint 6), so
# the PR is the only cross-boundary bus (Q2). Keyed to the artifact SHA so a stale
# verdict from an earlier push can never be mistaken for this one — that keying is
# what lets the blocked auto-unblock know which artifact a PASS attests.
#     `gh api --jq` takes no --arg, so the pattern is applied by a piped jq. The
#     comment list is paginated: a busy PR outruns one page, and a verdict that
#     scrolled onto page 2 would otherwise read as "still pending" forever.
pr_comments() {
  gh api --paginate "repos/${FORK_SLUG}/issues/$1/comments?per_page=100" --jq '.[]' 2>/dev/null
}

smoke_verdict() {
  local pr="$1" key="$2" sha="$3"
  [[ -z "${pr}" || -z "${sha}" ]] && return 0
  pr_comments "${pr}" \
    | jq -s -r --arg pat "smoke-result ${key}=${sha}" '
        [ .[] | select(.body != null and (.body | contains($pat))) ]
        | sort_by(.created_at) | last
        | if . == null then empty
          else (.body | capture("status=(?<s>PASS|FAIL)") | .s)
          end' 2>/dev/null
}

smoke_requested() {
  local pr="$1" key="$2" sha="$3"
  pr_comments "${pr}" \
    | jq -s -r --arg pat "smoke-request ${key}=${sha}" \
        '[ .[] | select(.body != null and (.body | contains($pat))) ] | length' 2>/dev/null
}

# ── Stage handlers ────────────────────────────────────────────────────────────

# No active sync ticket. Decide whether a hop is due, and open one if so.
stage_idle() {
  local deployed latest orphan
  deployed="$(deployed_version)" || { log "could not read deployed version"; return 0; }
  git fetch --quiet origin main 2>/dev/null || true
  latest="$(latest_upstream_tag)"
  [[ -z "${latest}" ]] && { log "could not resolve latest upstream release"; return 0; }

  local cursor; cursor="$(cursor_tag)"
  log "deployed=${deployed} cursor=${cursor:-none} latest=${latest}"

  # THE quiet no-op. Nothing printed to stdout, so the caller stays silent.
  if [[ "${deployed}" == "${latest}" && ( -z "${cursor}" || "${cursor}" == "${latest}" ) ]]; then
    log "in sync at ${latest}; nothing to do"
    return 0
  fi

  # Single-flight, GitHub side: adopt an orphaned open sync PR rather than
  # opening a second one (§6.3).
  orphan="$(open_sync_pr)"
  if [[ -n "${orphan}" ]]; then
    local onum osha ohead
    onum="$(printf '%s' "${orphan}" | jq -r '.number')"
    osha="$(printf '%s' "${orphan}" | jq -r '.head.sha')"
    ohead="$(printf '%s' "${orphan}" | jq -r '.head.ref')"
    log "adopting orphaned sync PR #${onum} (${ohead})"
    open_ticket "${cursor:-unknown}" "${latest}" "adopted open PR #${onum}"
    mset sync_pr "${onum}" number || true
    mset pr_sha "${osha}" string || true
    ensure_pr_jira_ref "${onum}"
    advance dev_deploying "Adopted the already-open sync PR [#${onum}](https://github.com/${FORK_SLUG}/pull/${onum}) (\`${ohead}\`) instead of opening a second one."
    say "Adopted orphaned sync PR #${onum} into new sync ticket ${TICKET}."
    return 0
  fi

  # No open PR to adopt. Before starting the hop, check that an earlier attempt at
  # this same target did not leave a branch behind on origin.
  #
  # upstream-sync.sh rebuilds the merge commit from scratch, and the rebuilt commit
  # carries a fresh committer timestamp, so it hashes differently from the one on the
  # leftover branch. Its `git push -u` is then a non-fast-forward and is rejected.
  # Starting the hop anyway spends a ticket and a full merge run to land on
  # `sync_failed` with a push error that names none of this, so report it up front
  # instead. Deleting the branch is a human's call: it is unmerged work, and the
  # reason its PR is missing (never opened, or closed unmerged on purpose) is not
  # knowable from here.
  local stale
  stale="$(stale_sync_branch "${latest}")"

  if [[ -n "${DRY_RUN}" ]]; then
    if [[ -n "${stale}" ]]; then
      say "DRY RUN — would NOT start ${cursor:-?} → ${latest}: branch \`${stale}\` is already on origin with no open PR."
    else
      say "DRY RUN — would start a sync hop ${cursor:-?} → ${latest} (deployed ${deployed})."
    fi
    return 0
  fi

  if [[ -n "${stale}" ]]; then
    # Ticket first: block() reports through ${TICKET}, so it needs a surface to
    # report on, and opening one also gives the human the standard "close the
    # ticket to release the guard" affordance.
    open_ticket "${cursor:-unknown}" "${latest}" "stale sync branch"
    block stale_sync_branch "Branch \`${stale}\` is already on \`origin\` from an earlier attempt at this hop, and no open PR points at it. A fresh sync would rebuild the merge commit with a different SHA and its push would be rejected as a non-fast-forward, so this tick did not start one. Either open a PR for \`${stale}\` and let the pipeline adopt it, or delete it (\`git push origin --delete ${stale}\`) and close this ticket to release the guard."
    return 0
  fi

  # Ticket first, so the mutex covers the upstream-sync.sh run itself: two
  # overlapping ticks must never both merge and push.
  open_ticket "${cursor:-unknown}" "${latest}" "starting sync"
  run_sync "${latest}"
}

open_ticket() {
  local from="$1" to="$2" why="$3" resp desc root
  desc="${TMPD}/desc.md"
  {
    printf 'Autonomous upstream sync hop **%s → %s**, opened by `scripts/sync-tick.sh` (%s).\n\n' \
      "${from}" "${to}" "${why}"
    printf 'One ticket per hop. Progress is an append-only thread under the audit root comment, and a state table in the PR body. Machine-readable state is in this issue'"'"'s metadata.\n\n'
    printf 'Design: ANK-34 §6. The pipeline is autonomous up to a green dev smoke, then parks at `awaiting_merge` for a human to merge to `main`.\n'
  } > "${desc}"
  resp="$(multica issue create \
    --title "Upstream sync ${from} → ${to}" \
    --description-file "${desc}" \
    --priority high \
    --status in_progress \
    --output json)" || die "could not create the sync ticket"
  TICKET="$(printf '%s' "${resp}" | jq -r '.id // empty')"
  [[ -n "${TICKET}" ]] || die "sync ticket created but no id returned"
  rm -f "${desc}"
  log "sync ticket ${TICKET}"

  META_JSON='{}'
  mset sync_active true bool || true
  mset sync_from "${from}" string || true
  mset sync_to "${to}" string || true
  set_sync_label sync-active

  # Audit root. Every later transition replies under this id.
  local rootf="${TMPD}/root.md"
  {
    printf '## Upstream sync `%s` → `%s`\n\n' "${from}" "${to}"
    printf 'Audit trail for this hop — each state change is a reply in this thread.\n\n'
    printf '| field | value |\n|---|---|\n'
    printf '| from | `%s` |\n| to | `%s` |\n' "${from}" "${to}"
    printf '| opened by | `scripts/sync-tick.sh` |\n'
  } > "${rootf}"
  root="$(multica issue comment add "${TICKET}" --content-file "${rootf}" --output json 2>/dev/null \
    | jq -r '.id // empty')"
  rm -f "${rootf}"
  if [[ -n "${root}" ]]; then
    mset audit_root_comment_id "${root}" string || true
  else
    log "could not create the audit root comment — transitions will not be threaded"
  fi

  # JIRA mirror. Deliberately before run_sync: the key becomes JIRA_REF on the PR
  # title, which is what satisfies `jira-ref-check-and-description` properly
  # rather than suppressing it with `[NO JIRA]`. A failure here is a no-op — the
  # sync proceeds and falls back to the suppression tag.
  if jira_ensure_item "${from}" "${to}" >/dev/null; then
    jira_transition "In Progress"
  fi

  advance syncing ""
}

run_sync() {
  local to="$1" out rc jref
  out="${TMPD}/sync.log"
  log "running ${SYNC_SCRIPT} → ${to}"
  rc=0
  # JIRA_REF is what keeps `jira-ref-check-and-description` green without a human
  # editing the title. Empty is fine and expected when the mirror is unavailable:
  # upstream-sync.sh then stamps `[NO JIRA]`, which the gate also accepts.
  jref="$(mget jira_key)"
  UPSTREAM_TAG="${to}" FORK_SLUG="${FORK_SLUG}" JIRA_REF="${jref}" \
    bash "${SYNC_SCRIPT}" > "${out}" 2>&1 || rc=$?

  local tail_out; tail_out="$(tail -40 "${out}")"

  case "${rc}" in
    0) : ;;
    2) block sync_conflict "\`${SYNC_SCRIPT}\` hit a merge conflict and aborted (exit 2). Needs a human resolution.

\`\`\`
${tail_out}
\`\`\`"
       return 0 ;;
    3) block sync_invariant "\`${SYNC_SCRIPT}\` refused to push (exit 3) — either a non-fork-owned path still diverges from \`${to}\`, or the sync deleted a path \`${to}\` never owned. No branch and no PR were created.

\`\`\`
${tail_out}
\`\`\`"
       return 0 ;;
    *) block sync_failed "\`${SYNC_SCRIPT}\` failed with exit ${rc}.

\`\`\`
${tail_out}
\`\`\`"
       return 0 ;;
  esac

  if grep -q 'nothing to do' "${out}"; then
    # The version probe said behind but the cursor says otherwise — usually the
    # bakefile bump has merged and the pod has not rolled yet. Retire the ticket
    # quietly rather than leaving the mutex held.
    log "sync reported nothing to do; retiring the ticket"
    jira_comment "Nothing to sync for this hop — the fork is already on the target release. Retiring the hop."
    jira_transition "Done"
    sweep_throwaways
    clear_guard
    set_sync_label sync-passed
    [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" done >/dev/null 2>&1 || true
    return 0
  fi

  local pr sha
  pr="$(sed -n 's#.*github.com/[^/]*/[^/]*/pull/\([0-9]\+\).*#\1#p' "${out}" | tail -1)"
  if [[ -z "${pr}" ]]; then
    block sync_no_pr "\`${SYNC_SCRIPT}\` exited 0 but no PR URL could be parsed from its output. The branch may be pushed without a PR.

\`\`\`
${tail_out}
\`\`\`"
    return 0
  fi
  sha="$(gh_pr_json "${pr}" | jq -r '.head.sha // empty')"
  mset sync_pr "${pr}" number || true
  mset pr_sha "${sha}" string || true
  advance dev_deploying "Sync PR opened: [#${pr}](https://github.com/${FORK_SLUG}/pull/${pr}) at \`${sha:0:8}\`. Applying the \`development\` label to start the dev deploy."
  say "Opened sync PR #${pr} for $(mget sync_from) → $(mget sync_to): https://github.com/${FORK_SLUG}/pull/${pr}"
}

stage_dev_deploying() {
  local pr sha run status conclusion runid
  pr="$(mget sync_pr)"; sha="$(mget pr_sha)"
  [[ -z "${pr}" ]] && { recover_missing_pr; return 0; }

  # Idempotent: POST /labels is additive, so re-adding costs nothing and covers
  # both "just opened" and "adopted an unlabelled orphan".
  gh_add_label "${pr}" development

  run="$(gh_latest_run dev.yml "${sha}")"
  if [[ -z "${run}" ]]; then
    # Only label-skipped runs exist so far, or none at all. The real run arrives
    # within a minute of the label; bound the wait so a workflow that never starts
    # cannot stall the pipeline silently.
    local age; age="$(stage_age_min)"
    if [[ -n "${age}" ]] && (( age > SYNC_ROLLOUT_DEADLINE_MIN )); then
      block dev_deploy_never_started "No \`dev.yml\` run has attempted work for \`${sha:0:8}\` in ${age} min. The \`development\` label is applied, so check whether \`dev.yml\` is firing at all."
      return 0
    fi
    log "no dev.yml run has attempted work for ${sha} yet"
    return 0
  fi
  status="$(printf '%s' "${run}" | jq -r '.status')"
  conclusion="$(printf '%s' "${run}" | jq -r '.conclusion // ""')"
  runid="$(printf '%s' "${run}" | jq -r '.id')"

  if [[ "${status}" != "completed" ]]; then
    log "dev deploy ${runid} ${status}"
    return 0
  fi

  case "${conclusion}" in
    success)
      # Hand off to the dev workspace. The marker line is posted alone and
      # verbatim: the dev autopilot's parser is live and out of reach from here.
      if [[ "$(smoke_requested "${pr}" pr_sha "${sha}")" == "0" ]]; then
        gh_pr_comment "${pr}" "<!-- smoke-request pr_sha=${sha} -->"
      fi
      advance dev_smoke_pending "Dev deploy green ([run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid})). Requested a dev smoke for \`${sha:0:8}\` on the PR — the dev-workspace autopilot picks it up from there and replies on the PR."
      say "Dev deploy green for #${pr}; dev smoke requested for ${sha:0:8}."
      ;;
    cancelled)
      # dev.yml uses concurrency `dev-<ref>` with cancel-in-progress, so a
      # `synchronize` push cancels an in-flight deploy. That is retryable, not a
      # failure. Re-trigger by cycling the label — the `labeled` event refires
      # dev.yml, and the REST label path is the one confirmed to work here.
      log "dev deploy ${runid} cancelled — re-triggering via label cycle"
      gh_remove_label "${pr}" development
      gh_add_label "${pr}" development
      audit "Dev deploy run ${runid} was **cancelled** (dev.yml concurrency cancels an in-flight deploy on a new push). Re-triggered; staying in \`dev_deploying\`."
      ;;
    failure|timed_out|startup_failure)
      block dev_deploy "Dev deploy [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) concluded \`${conclusion}\` for \`${sha:0:8}\`."
      ;;
    *)
      # neutral / action_required / stale and anything GitHub adds later. Not a
      # verdict either way, so wait rather than guess — but do not wait forever.
      local age2; age2="$(stage_age_min)"
      if [[ -n "${age2}" ]] && (( age2 > SYNC_ROLLOUT_DEADLINE_MIN )); then
        block dev_deploy "Dev deploy [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) has sat at conclusion \`${conclusion}\` for ${age2} min for \`${sha:0:8}\`, which is neither a pass nor a failure this script can act on."
        return 0
      fi
      log "dev deploy ${runid} concluded ${conclusion} — waiting"
      ;;
  esac
}

stage_dev_smoke_pending() {
  local pr sha verdict
  pr="$(mget sync_pr)"; sha="$(mget pr_sha)"
  verdict="$(smoke_verdict "${pr}" pr_sha "${sha}")"
  case "${verdict}" in
    PASS)
      advance awaiting_merge "**Dev smoke PASS** for \`${sha:0:8}\` on ${DEV_HOST}.

This is the hand-off point. Merging to \`main\` is deliberately human (ANK-34 Q4) — it triggers \`publish.yml\` and is the one irreversible step in the loop. Once [#${pr}](https://github.com/${FORK_SLUG}/pull/${pr}) is merged, the pipeline resumes on its own: tools deploy, pod roll, tools smoke."
      set_sync_label sync-active
      notify_once awaiting_merge "**Sync $(mget sync_from) → $(mget sync_to) is green in dev and ready for your merge.**

PR: https://github.com/${FORK_SLUG}/pull/${pr}
Dev smoke: PASS for \`${sha:0:8}\` on ${DEV_HOST}

Merge to \`main\` when you are ready; the pipeline takes it from there and will report the tools smoke on this ticket. Nothing else is waiting on you."
      say "Dev smoke PASS for #${pr}. Parked at awaiting_merge — needs a human merge to main."
      ;;
    FAIL)
      block dev_smoke "Dev smoke **FAIL** for \`${sha:0:8}\` on ${DEV_HOST}. PR [#${pr}](https://github.com/${FORK_SLUG}/pull/${pr}) is left open. A later PASS for this same \`pr_sha\` auto-clears this block."
      ;;
    *)
      log "dev smoke for ${sha} still pending"
      ;;
  esac
}

stage_awaiting_merge() {
  local pr prj state merged mergesha
  pr="$(mget sync_pr)"
  prj="$(gh_pr_json "${pr}")"
  [[ -z "${prj}" ]] && { log "could not read PR #${pr}"; return 0; }
  state="$(printf '%s' "${prj}" | jq -r '.state')"
  merged="$(printf '%s' "${prj}" | jq -r '.merged')"
  mergesha="$(printf '%s' "${prj}" | jq -r '.merge_commit_sha // empty')"

  if [[ "${merged}" == "true" ]]; then
    mset merge_sha "${mergesha}" string || true
    advance tools_deploying "PR [#${pr}](https://github.com/${FORK_SLUG}/pull/${pr}) merged as \`${mergesha:0:8}\`. Watching \`publish.yml\`, then waiting for the tools agentrunner to roll onto the new build."
    say "Sync PR #${pr} merged as ${mergesha:0:8}; watching the tools deploy."
    return 0
  fi

  if [[ "${state}" == "closed" ]]; then
    # Abandon path: the human closed it without merging. Release the mutex so a
    # later hop can start.
    audit "PR [#${pr}](https://github.com/${FORK_SLUG}/pull/${pr}) was **closed without merging**. Treating this hop as abandoned and releasing the single-flight guard."
    jira_comment "Hop abandoned: PR #${pr} was closed without merging. Status left as-is for a human to decide."
    set_sync_label sync-failed
    sweep_throwaways
    clear_guard
    [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" cancelled >/dev/null 2>&1 || true
    say "Sync PR #${pr} was closed unmerged; hop abandoned and the guard released."
    return 0
  fi

  # Still open, still unmerged. Notified once on entry — stay silent now.
  log "PR #${pr} still awaiting a human merge"
}

stage_tools_deploying() {
  local mergesha run status conclusion runid deployed target entered age_min
  mergesha="$(mget merge_sha)"; target="$(mget sync_to)"
  [[ -z "${mergesha}" ]] && { block tools_no_merge_sha "Stage is \`tools_deploying\` but \`merge_sha\` is unset."; return 0; }

  run="$(gh_latest_run publish.yml "${mergesha}")"
  if [[ -n "${run}" ]]; then
    status="$(printf '%s' "${run}" | jq -r '.status')"
    conclusion="$(printf '%s' "${run}" | jq -r '.conclusion // ""')"
    runid="$(printf '%s' "${run}" | jq -r '.id')"
    if [[ "${status}" != "completed" ]]; then
      log "tools deploy ${runid} ${status}"
      return 0
    fi
    case "${conclusion}" in
      success) : ;;
      failure|timed_out|startup_failure|cancelled)
        block tools_deploy "Tools deploy [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) concluded \`${conclusion}\` for \`${mergesha:0:8}\`. This is post-merge, so it needs a human — the pipeline deliberately does not roll back."
        return 0 ;;
      *)
        # Ambiguous conclusion. Fall through to the version probe: if the rollout
        # actually happened the runtime will say so, and the staleness guard below
        # bounds the wait if it did not.
        log "tools deploy ${runid} concluded ${conclusion} — deferring to the version probe" ;;
    esac
  else
    log "no publish.yml run for ${mergesha} yet"
  fi

  # publish.yml green is not enough: rollout is a GitOps image bump, so merged is
  # not deployed. Confirm by observing our OWN runtime report the target version.
  deployed="$(deployed_version)" || deployed=""
  if [[ "${deployed}" == "${target}" ]]; then
    dispatch_tools_smoke "${mergesha}"
    return 0
  fi

  # Staleness guard. Without the bakefile bump upstream-sync.sh now makes, the
  # version string would sit at the old value forever and this stage would loop.
  entered="$(mget stage_entered_at)"
  if [[ -n "${entered}" ]]; then
    age_min=$(( ( $(now_epoch) - entered ) / 60 ))
    if (( age_min > SYNC_ROLLOUT_DEADLINE_MIN )); then
      block rollout_stale "Waited ${age_min} min for the tools agentrunner to report \`${target}\` (deadline ${SYNC_ROLLOUT_DEADLINE_MIN} min); it still reports \`${deployed:-unknown}\`.

\`publish.yml\` for \`${mergesha:0:8}\` finished, so this is a rollout or a version-bump problem rather than a build failure — check that \`MULTICA_VERSION\` in \`docker/agent-runtime-base/docker-bake.hcl\` actually moved to \`${target#v}\`, and that the agentrunner rolled."
      return 0
    fi
  fi
  log "tools runtime reports ${deployed:-unknown}, waiting for ${target}"
}

# The tools smoke runs as its own agent task, NOT inline. A tick must stay
# non-blocking, and the smoke waits up to SMOKE_TASK_TIMEOUT for a *second* agent
# to claim and answer the inner marker task — blocking a tick on that risks
# deadlocking against its own task slot and turning a slot shortage into a false
# FAIL. One dispatch per hop, recorded in metadata and reporting onto this ticket,
# so it is not the orphaned per-invocation issue the design rejected.
dispatch_tools_smoke() {
  local mergesha="$1" pr agent_id resp desc issue
  pr="$(mget sync_pr)"
  issue="$(mget tools_smoke_issue_id)"
  if [[ -n "${issue}" ]]; then
    advance tools_smoke_pending ""
    return 0
  fi
  agent_id="$(multica agent list --output json 2>/dev/null \
    | jq -r --arg n "${SYNC_ENGINEER_AGENT}" '.[] | select(.name==$n) | .id' | head -n1)"
  if [[ -z "${agent_id}" ]]; then
    block tools_smoke_dispatch "Could not find the \`${SYNC_ENGINEER_AGENT}\` agent to run the tools smoke."
    return 0
  fi

  if [[ -n "${DRY_RUN}" ]]; then
    say "DRY RUN — would dispatch the tools smoke for ${mergesha:0:8}."
    return 0
  fi

  desc="${TMPD}/smoke.md"
  {
    printf 'Run the tools smoke for upstream sync hop `%s` → `%s`.\n\n' "$(mget sync_from)" "$(mget sync_to)"
    printf 'Run exactly this from a `%s` checkout and do nothing else — the script reports its own result onto the sync ticket and the PR:\n\n' "${FORK_SLUG}"
    printf '```bash\n'
    printf 'SMOKE_SYNC_ISSUE_ID=%s \\\n' "${TICKET}"
    printf '  SMOKE_AUDIT_ROOT_COMMENT_ID=%s \\\n' "$(mget audit_root_comment_id)"
    printf '  SMOKE_PR_REPO=%s SMOKE_PR_NUMBER=%s \\\n' "${FORK_SLUG}" "${pr}"
    printf '  SMOKE_ARTIFACT_KIND=merge_sha SMOKE_ARTIFACT_SHA=%s \\\n' "${mergesha}"
    printf '  SMOKE_LABEL="Tools smoke" \\\n'
    printf '  bash scripts/smoke-test-agentrunner.sh\n'
    printf '```\n\n'
    printf 'Post no separate result comment of your own: `scripts/sync-tick.sh` reads the verdict off the PR marker the script emits. Sync ticket: %s\n' "${TICKET}"
  } > "${desc}"
  resp="$(multica issue create \
    --title "Tools smoke for $(mget sync_to) ($(printf '%s' "${mergesha}" | cut -c1-8))" \
    --description-file "${desc}" \
    --priority high \
    --status todo \
    --assignee-id "${agent_id}" \
    --output json)" || { block tools_smoke_dispatch "Could not create the tools smoke task."; return 0; }
  rm -f "${desc}"
  issue="$(printf '%s' "${resp}" | jq -r '.id // empty')"
  mset tools_smoke_issue_id "${issue}" string || true
  advance tools_smoke_pending "Tools runtime confirmed on \`$(mget sync_to)\`. Dispatched the tools smoke for \`${mergesha:0:8}\`."
  say "Tools rollout confirmed on $(mget sync_to); tools smoke dispatched."
}

stage_tools_smoke_pending() {
  local pr mergesha verdict
  pr="$(mget sync_pr)"; mergesha="$(mget merge_sha)"
  verdict="$(smoke_verdict "${pr}" merge_sha "${mergesha}")"
  case "${verdict}" in
    PASS)
      advance done "**Tools smoke PASS** for \`${mergesha:0:8}\` on ${TOOLS_HOST}.

Hop \`$(mget sync_from)\` → \`$(mget sync_to)\` complete: merged, deployed, rolled and smoked. The single-flight guard is released, so the next tick may open the following hop."
      set_sync_label sync-passed
      # The tools smoke agent has long since exited by the time this tick runs, so
      # the throwaways it left can now be retired without racing it.
      sweep_throwaways
      clear_guard
      [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" done >/dev/null 2>&1 || true
      say "Sync $(mget sync_from) → $(mget sync_to) complete — tools smoke PASS. Guard released."
      ;;
    FAIL)
      block tools_smoke "Tools smoke **FAIL** for \`${mergesha:0:8}\` on ${TOOLS_HOST}. This is post-merge and already deployed, so it needs a human — the pipeline deliberately does not auto-roll-back. A later PASS for this same \`merge_sha\` auto-clears this block."
      ;;
    *)
      log "tools smoke for ${mergesha} still pending"
      ;;
  esac
}

# Smoke gates auto-clear on the exact artifact they attest (Q6). dev_deploy and
# tools_deploy re-examine their latest CI run every tick (ANK-96 postmortem —
# see the header comment) via try_dev_deploy_autofix / try_tools_deploy_autofix.
# Every other reason — conflicts, invariant breaches, rollout_stale, unknown —
# has no automatic re-check; it parks until a human clears blocked_reason,
# though a resolution-looking comment can still resume it once (see
# handle_human_comment).
stage_blocked() {
  local reason pr verdict tstatus run status conclusion runid
  reason="$(mget blocked_reason)"
  pr="$(mget sync_pr)"

  # A human who gives up on a blocked hop closes the ticket rather than editing
  # metadata, and that is the third terminal transition: sweep the throwaways it
  # left and release the guard so the next tick is not wedged behind a dead hop.
  # The label is deliberately left alone — whatever the human set is their intent.
  tstatus="$(issue_status "${TICKET}")"
  case "${tstatus}" in
    cancelled|done)
      log "ticket is ${tstatus} while blocked — sweeping throwaways and releasing the guard"
      jira_comment "The Multica sync ticket was moved to ${tstatus} while blocked on ${reason:-unknown}. Releasing the single-flight guard; no further pipeline updates for this hop."
      sweep_throwaways
      clear_guard
      return 0
      ;;
  esac

  # Read for a new human comment before deciding anything else — see the
  # "Human comments while blocked" header comment. HUMAN_COMMENT_ACTED=1 means
  # this tick already resumed the hop from the comment; the reason-based checks
  # below would just re-evaluate a reason that is no longer current, so skip.
  handle_human_comment "${reason}"
  if [[ "${HUMAN_COMMENT_ACTED}" == "1" ]]; then
    say "Sync ${TICKET} resumed by a human comment on the blocked ticket."
    return 0
  fi

  case "${reason}" in
    dev_smoke)
      verdict="$(smoke_verdict "${pr}" pr_sha "$(mget pr_sha)")"
      if [[ "${verdict}" == "PASS" ]]; then
        mdel blocked_reason
        mdel "notified_blocked_${reason}"
        set_sync_label sync-active
        [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
        advance dev_smoke_pending "A dev smoke for \`$(mget pr_sha | cut -c1-8)\` has since **passed** — clearing the \`dev_smoke\` block and resuming."
        say "Dev smoke now passes; sync ${TICKET} auto-unblocked."
      else
        log "still blocked on dev_smoke"
      fi
      ;;
    tools_smoke)
      verdict="$(smoke_verdict "${pr}" merge_sha "$(mget merge_sha)")"
      if [[ "${verdict}" == "PASS" ]]; then
        mdel blocked_reason
        mdel "notified_blocked_${reason}"
        set_sync_label sync-active
        [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
        advance tools_smoke_pending "A tools smoke for \`$(mget merge_sha | cut -c1-8)\` has since **passed** — clearing the \`tools_smoke\` block and resuming."
        say "Tools smoke now passes; sync ${TICKET} auto-unblocked."
      else
        log "still blocked on tools_smoke"
      fi
      ;;
    dev_deploy)
      # Re-derive against the CURRENT pr_sha, not whatever sha the original
      # block fired on: apply_go_version_autofix advances the stage and clears
      # the block itself on success, so getting here at all means either that
      # sha's run is still the latest word, or an autofix already moved
      # pr_sha and this read is simply confirming the retry's own run.
      run="$(gh_latest_run dev.yml "$(mget pr_sha)")"
      if [[ -z "${run}" ]]; then
        log "no dev.yml run yet for $(mget pr_sha) — waiting"
      else
        status="$(printf '%s' "${run}" | jq -r '.status')"
        conclusion="$(printf '%s' "${run}" | jq -r '.conclusion // ""')"
        runid="$(printf '%s' "${run}" | jq -r '.id')"
        if [[ "${status}" != "completed" ]]; then
          log "dev deploy ${runid} ${status} — waiting"
        elif [[ "${conclusion}" == "success" ]]; then
          # The retry (or autofix) actually landed. Same handoff dev_deploying
          # uses on a green run: request the dev smoke and move on.
          if [[ "$(smoke_requested "${pr}" pr_sha "$(mget pr_sha)")" == "0" ]]; then
            gh_pr_comment "${pr}" "<!-- smoke-request pr_sha=$(mget pr_sha) -->"
          fi
          mdel blocked_reason
          mdel "notified_blocked_${reason}"
          set_sync_label sync-active
          [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
          advance dev_smoke_pending "Dev deploy [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) is now green for \`$(mget pr_sha | cut -c1-8)\` — clearing the \`dev_deploy\` block. Requested a dev smoke; the dev-workspace autopilot picks it up from there."
          say "Dev deploy now green; sync ${TICKET} auto-unblocked."
        else
          try_dev_deploy_autofix "${runid}" "${conclusion}"
        fi
      fi
      ;;
    tools_deploy)
      run="$(gh_latest_run publish.yml "$(mget merge_sha)")"
      if [[ -z "${run}" ]]; then
        log "no publish.yml run yet for $(mget merge_sha) — waiting"
      else
        status="$(printf '%s' "${run}" | jq -r '.status')"
        conclusion="$(printf '%s' "${run}" | jq -r '.conclusion // ""')"
        runid="$(printf '%s' "${run}" | jq -r '.id')"
        if [[ "${status}" != "completed" ]]; then
          log "tools deploy ${runid} ${status} — waiting"
        elif [[ "${conclusion}" == "success" ]]; then
          mdel blocked_reason
          mdel "notified_blocked_${reason}"
          set_sync_label sync-active
          [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
          advance tools_deploying "Tools deploy [run ${runid}](https://github.com/${FORK_SLUG}/actions/runs/${runid}) is now green for \`$(mget merge_sha | cut -c1-8)\` — clearing the \`tools_deploy\` block. Watching for the rollout."
          say "Tools deploy now green; sync ${TICKET} auto-unblocked."
        else
          try_tools_deploy_autofix "${runid}" "${conclusion}"
        fi
      fi
      ;;
    rollout_stale)
      # The staleness guard fires on a slow rollout, not a failed one — the
      # deploy itself may still complete after the deadline (a busy runner, a
      # slow node roll). Re-reading our own runtime version costs nothing and
      # is exactly the signal stage_tools_deploying already trusts for "did it
      # actually roll", so a late-but-successful rollout clears on its own
      # instead of waiting for a human to notice and re-run the same check.
      local deployed target
      deployed="$(deployed_version)" || deployed=""
      target="$(mget sync_to)"
      if [[ -n "${deployed}" && "${deployed}" == "${target}" ]]; then
        mdel blocked_reason
        mdel "notified_blocked_${reason}"
        set_sync_label sync-active
        [[ -n "${DRY_RUN}" ]] || multica issue status "${TICKET}" in_progress >/dev/null 2>&1 || true
        dispatch_tools_smoke "$(mget merge_sha)"
      else
        log "still blocked on rollout_stale — runtime reports ${deployed:-unknown}, waiting for ${target}"
      fi
      ;;
    *)
      log "blocked on ${reason:-unknown} — waits for a human, no action this tick"
      ;;
  esac
}

# Stage says a PR should exist but metadata has none: a tick died mid-sync.
# Prefer adoption; never blind-retry a sync whose branch may already be pushed.
recover_missing_pr() {
  local orphan onum osha
  orphan="$(open_sync_pr)"
  if [[ -n "${orphan}" ]]; then
    onum="$(printf '%s' "${orphan}" | jq -r '.number')"
    osha="$(printf '%s' "${orphan}" | jq -r '.head.sha')"
    mset sync_pr "${onum}" number || true
    mset pr_sha "${osha}" string || true
    ensure_pr_jira_ref "${onum}"
    advance dev_deploying "Recovered: adopted open sync PR [#${onum}](https://github.com/${FORK_SLUG}/pull/${onum}) for this hop."
    say "Recovered sync ${TICKET} by adopting open PR #${onum}."
    return 0
  fi
  block sync_interrupted "This hop has no \`sync_pr\` and there is no open \`upstream-sync/*\` PR to adopt — a previous tick died between creating this ticket and opening the PR. Check for a pushed \`upstream-sync/*\` branch, then either open its PR by hand or close this ticket to release the guard."
}

stage_syncing() {
  local orphan
  orphan="$(open_sync_pr)"
  if [[ -n "${orphan}" ]]; then recover_missing_pr; return 0; fi
  # Nothing to adopt. Re-running the sync in place is only safe if this hop never
  # got as far as pushing — stale_sync_branch is what tells those two apart, scoped
  # to THIS hop's target so the fork's already-merged branches don't all match.
  local target stale
  target="$(mget sync_to)"
  stale="$(stale_sync_branch "${target}")"
  if [[ -n "${stale}" ]]; then
    block sync_interrupted "A previous tick pushed \`${stale}\` but no open PR exists for it. Open the PR by hand, or delete the branch (\`git push origin --delete ${stale}\`) and close this ticket to release the guard."
    return 0
  fi
  run_sync "${target}"
}

# ── Dispatch ──────────────────────────────────────────────────────────────────
main() {
  local active count stage
  require_deps

  # Single-flight, Multica side. `sync_active` is a metadata boolean rather than a
  # label because `issue list` can filter on metadata but not on labels; the
  # labels mirror it for the board.
  active="$(multica issue list --metadata sync_active=true --output json 2>/dev/null \
    | jq -c '[.issues[]] | sort_by(.created_at)' 2>/dev/null || echo '[]')"
  count="$(printf '%s' "${active}" | jq 'length')"

  if (( count == 0 )); then
    log "no active sync"
    # Quiet path: tidy leftovers nobody is waiting on before deciding on a hop.
    # Silent by contract — sweeping writes nothing to stdout, so a tick that only
    # swept still reads as a no-op to the caller.
    sweep_stale_throwaways
    stage_idle
    # stage_idle may have opened a ticket (and with it a JIRA item), so the
    # degradation note has somewhere to land on this path too.
    jira_degraded_flush
    return 0
  fi

  if (( count > 1 )); then
    # Two live syncs is exactly what single-flight exists to prevent. Report it
    # rather than picking one and hiding the breach.
    say "Single-flight breach: ${count} issues carry \`sync_active=true\`. Advancing the oldest only; the rest need a human."
    printf '%s' "${active}" | jq -r '.[] | "- \(.identifier) \(.title) (\(.id))"'
  fi

  TICKET="$(printf '%s' "${active}" | jq -r '.[0].id')"
  load_meta
  stage="$(mget pipeline_stage)"
  log "active sync ${TICKET} at stage ${stage:-unset}"

  # A hop whose JIRA item was never created — Atlassian was down when the ticket
  # was opened, or acli was unauthenticated — retries here, bounded to two attempts
  # per hop by jira_create_attempts. Missed transitions are simply not mirrored;
  # nothing waits on the key.
  if [[ -z "$(mget jira_key)" && "${stage}" != "done" ]]; then
    if jira_ensure_item "$(mget sync_from)" "$(mget sync_to)" >/dev/null; then
      jira_transition "In Progress"
      ensure_pr_jira_ref "$(mget sync_pr)"
    fi
  fi

  case "${stage}" in
    syncing)             stage_syncing ;;
    dev_deploying)       stage_dev_deploying ;;
    dev_smoke_pending)   stage_dev_smoke_pending ;;
    awaiting_merge)      stage_awaiting_merge ;;
    tools_deploying)     stage_tools_deploying ;;
    tools_smoke_pending) stage_tools_smoke_pending ;;
    blocked)             stage_blocked ;;
    done)                log "stage done but guard still held — releasing"
                         sweep_throwaways; clear_guard ;;
    *)                   block unknown_stage "Sync ticket is \`sync_active\` but \`pipeline_stage\` is \`${stage:-unset}\`, which this script does not recognise." ;;
  esac

  jira_degraded_flush
}

# Guarded so the file can be sourced to exercise a single function against live
# data without running a whole tick.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
