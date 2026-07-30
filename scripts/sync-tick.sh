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
#
# ── GitHub capability notes, verified from the tools agentrunner pod ───────────
# The GitHub App CANNOT reach GraphQL mutations: `gh pr create`, `gh pr comment`
# and `gh pr edit` all return "Resource not accessible by integration". Every
# write below therefore goes through REST. GraphQL *reads* work, but always pass
# `--repo` explicitly — without it `gh` infers the repo from the checkout and can
# answer about an entirely different PR.
set -euo pipefail

FORK_SLUG="${FORK_SLUG:-g2crowd/agentfarm}"
SYNC_REQUESTER_ID="${SYNC_REQUESTER_ID:-b97bf628-51c0-417a-8d15-b5bdd8789ceb}"
SYNC_ROLLOUT_DEADLINE_MIN="${SYNC_ROLLOUT_DEADLINE_MIN:-45}"
SYNC_ENGINEER_AGENT="${SYNC_ENGINEER_AGENT:-Engineer}"
DRY_RUN="${SYNC_TICK_DRY_RUN:-}"

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

command -v jq >/dev/null 2>&1 || die "jq is required"
command -v gh >/dev/null 2>&1 || die "gh is required"
command -v multica >/dev/null 2>&1 || die "multica is required"

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
  local f="${TMPD}/state.md" msha br
  msha="$(mget merge_sha)"
  br="$(mget blocked_reason)"
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
  [[ -n "${note}" ]] && audit "${note}"
  local pr; pr="$(mget sync_pr)"
  [[ -n "${pr}" ]] && pr_body_update "${pr}" "$(render_state_table)"
  return 0
}

block() {
  local reason="$1" detail="$2"
  mset blocked_reason "${reason}" string || true
  advance blocked "**Blocked — \`${reason}\`**

${detail}"
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
    advance dev_deploying "Adopted the already-open sync PR [#${onum}](https://github.com/${FORK_SLUG}/pull/${onum}) (\`${ohead}\`) instead of opening a second one."
    say "Adopted orphaned sync PR #${onum} into new sync ticket ${TICKET}."
    return 0
  fi

  if [[ -n "${DRY_RUN}" ]]; then
    say "DRY RUN — would start a sync hop ${cursor:-?} → ${latest} (deployed ${deployed})."
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
  advance syncing ""
}

run_sync() {
  local to="$1" out rc
  out="${TMPD}/sync.log"
  log "running ${SYNC_SCRIPT} → ${to}"
  rc=0
  UPSTREAM_TAG="${to}" FORK_SLUG="${FORK_SLUG}" bash "${SYNC_SCRIPT}" > "${out}" 2>&1 || rc=$?

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
    set_sync_label sync-failed
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

# Only smoke gates auto-clear, and only for the exact artifact they attest (Q6).
# Everything else — conflicts, invariant breaches, deploy failures, rollout_stale
# — parks until a human clears blocked_reason.
stage_blocked() {
  local reason pr verdict
  reason="$(mget blocked_reason)"
  pr="$(mget sync_pr)"
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
  # No branch pushed yet — safe to (re)run the sync.
  if git ls-remote --exit-code --heads origin 'upstream-sync/*' >/dev/null 2>&1; then
    block sync_interrupted "A previous tick pushed an \`upstream-sync/*\` branch but no open PR exists for it. Open the PR by hand, or delete the branch and close this ticket to release the guard."
    return 0
  fi
  run_sync "$(mget sync_to)"
}

# ── Dispatch ──────────────────────────────────────────────────────────────────
main() {
  local active count stage

  # Single-flight, Multica side. `sync_active` is a metadata boolean rather than a
  # label because `issue list` can filter on metadata but not on labels; the
  # labels mirror it for the board.
  active="$(multica issue list --metadata sync_active=true --output json 2>/dev/null \
    | jq -c '[.issues[]] | sort_by(.created_at)' 2>/dev/null || echo '[]')"
  count="$(printf '%s' "${active}" | jq 'length')"

  if (( count == 0 )); then
    log "no active sync"
    stage_idle
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

  case "${stage}" in
    syncing)             stage_syncing ;;
    dev_deploying)       stage_dev_deploying ;;
    dev_smoke_pending)   stage_dev_smoke_pending ;;
    awaiting_merge)      stage_awaiting_merge ;;
    tools_deploying)     stage_tools_deploying ;;
    tools_smoke_pending) stage_tools_smoke_pending ;;
    blocked)             stage_blocked ;;
    done)                log "stage done but guard still held — releasing"; clear_guard ;;
    *)                   block unknown_stage "Sync ticket is \`sync_active\` but \`pipeline_stage\` is \`${stage:-unset}\`, which this script does not recognise." ;;
  esac
}

# Guarded so the file can be sourced to exercise a single function against live
# data without running a whole tick.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
