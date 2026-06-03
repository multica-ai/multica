#!/usr/bin/env bash
#
# upstream-sync-deploy.sh — nightly auto-deploy pass for the upstream-sync bot.
#
# Runs AFTER scripts/upstream-sync.sh has opened/updated any sync PRs. For every
# open PR that (a) has a Multica tracking-issue where Tine has posted a
# "Resultat: PASS" QA comment referencing that PR, and (b) is CI-green and
# mergeable, this script merges the PR into main. Merging main IS the prod
# deploy (a webhook on the runner runs .deploy/deploy.sh). After each merge it
# smoke-checks the public site; if the site does not return 200 within the
# budget it auto-opens-and-merges a revert PR (webhook redeploys the previous
# version) and files a Multica incident issue pinging Sara.
#
# Safety fences (FIR-2217):
#   a) Tine approval is required   — no PASS comment ⇒ the PR is skipped.
#   b) CI-green is required        — red/pending CI ⇒ skipped, blocker filed.
#   c) Auto-rollback on smoke fail — revert PR auto-merged + incident issue.
#   d) Freeze switch               — a `deploy-freeze` label on the daily-status
#                                    issue pauses ALL merges (e.g. release freeze).
#   e) Per-repo opt-in             — this script only runs against firtal-cerebro.
#
# The bot never approves its own PRs — Tine's PASS is the only approval signal.
#
# Modes:
#   --pass            Full pass: freeze check → scan → gate → deploy each
#                     approved PR. Default nightly entrypoint. Writes
#                     .upstream-sync/last-deploy.json.
#   --scan            Print JSON of deployable-candidate PRs (open, non-draft,
#                     CI-green, mergeable). No Multica, no merge. Pure `gh`.
#   --check <PR>      Print the Tine-approval gate result for one PR.
#                     Exit 0 = approved, 10 = not approved.
#   --deploy <PR>     Merge one already-gated PR, smoke-check, rollback on fail.
#   --smoke           Run the smoke-check once. Exit 0 = 200 within budget.
#   --report          Print the latest .upstream-sync/last-deploy.json.
#   --help
#
# Exit codes:
#   0  ok (deployed, or nothing to deploy, or smoke-passed)
#   2  frozen | no candidates | not-approved (--check)
#   3  precondition failure (wrong cwd, missing remote/tool, dirty tree)
#   4  merge/push/PR failure
#   5  smoke failed AND rollback executed (handled, but a human should look)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPORT_DIR="$ROOT/.upstream-sync"
BASE_BRANCH="${UPSTREAM_SYNC_BASE_BRANCH:-main}"
ORIGIN_REMOTE="origin"

# Multica identities — overridable for testing. Defaults are the live IDs.
TINE_AGENT_ID="${DEPLOY_TINE_AGENT_ID:-8bfccab3-89ea-40cc-b57d-c7eabaa30f50}"
SARA_AGENT_ID="${DEPLOY_SARA_AGENT_ID:-43501ed6-0b4d-489b-b05e-e5d07e665d91}"
DAILY_STATUS_ISSUE="${DEPLOY_STATUS_ISSUE:-08225ab6-bb2d-404a-b8cf-ab27159a9d32}"
FREEZE_LABEL="${DEPLOY_FREEZE_LABEL:-deploy-freeze}"

# Smoke-check config.
SMOKE_URL="${DEPLOY_SMOKE_URL:-https://sara.tailbde0.ts.net/}"
SMOKE_TIMEOUT="${DEPLOY_SMOKE_TIMEOUT:-600}"   # total budget, seconds (10 min)
SMOKE_INTERVAL="${DEPLOY_SMOKE_INTERVAL:-30}"  # poll cadence, seconds
SMOKE_GRACE="${DEPLOY_SMOKE_GRACE:-60}"        # wait before first poll (build window)

# Approval pattern Tine uses in her QA comments.
PASS_PATTERN="${DEPLOY_PASS_PATTERN:-Resultat:\\s*PASS}"

MODE=""

# Logs go to stderr so functions can return data on stdout via $(...) capture
# (deploy_pr / rollback_merge) without log lines polluting the result.
usage() { sed -n '3,40p' "$0"; }
log()   { printf '[deploy-pass] %s\n' "$*" >&2; }
die()   { log "FATAL: $*"; exit "${2:-3}"; }

require() { command -v "$1" >/dev/null 2>&1 || die "missing required tool '$1'"; }

# Resolve the GitHub owner/repo for the fork from the `origin` remote. Pinning
# every `gh` call to this avoids `gh` defaulting to the `upstream`
# (multica-ai/multica) remote in a checkout that has both (see FIR-2169 QA).
origin_repo() {
  local url
  url="$(git remote get-url "$ORIGIN_REMOTE" 2>/dev/null)" || return 1
  url="${url#git@github.com:}"
  url="${url#https://github.com/}"
  url="${url#http://github.com/}"
  url="${url%.git}"
  printf '%s' "$url"
}

gh_repo() { origin_repo 2>/dev/null || true; }

# --- freeze (fence d) ------------------------------------------------------
# A `deploy-freeze` label on the daily-status issue pauses all merges. Chosen
# over an autopilot metadata key because a label is a one-click human toggle
# that needs no redeploy and no code change — exactly the "release freeze"
# ergonomics FIR-2217 asked for.
is_frozen() {
  command -v multica >/dev/null 2>&1 || { log "multica CLI absent — cannot read freeze label, treating as FROZEN (fail safe)"; return 0; }
  local labels
  labels="$(multica issue label list "$DAILY_STATUS_ISSUE" --output json 2>/dev/null || echo '[]')"
  printf '%s' "$labels" | jq -e --arg l "$FREEZE_LABEL" 'any(.[]; (.name // "") == $l)' >/dev/null 2>&1
}

# --- candidate scan (fence b + e) ------------------------------------------
# Open, non-draft PRs on the fork that are mergeable and CI-green. Revert PRs
# the bot itself opened (head `revert/pr-*`) are excluded so a failed deploy
# never feeds its own rollback back into the candidate list.
scan_candidates() {
  require gh; require jq
  local repo; repo="$(gh_repo)"
  gh pr list ${repo:+--repo "$repo"} --state open --limit 50 \
    --json number,title,url,isDraft,mergeable,mergeStateStatus,headRefName,body \
    --jq '[.[]
            | select(.isDraft == false)
            | select(.headRefName | startswith("revert/pr-") | not)
            | select(.mergeable == "MERGEABLE")
            | select(.mergeStateStatus != "DIRTY" and .mergeStateStatus != "BEHIND")]'
}

# True when every reported check is pass/skipping and at least one check ran.
# Pending or failing checks ⇒ not green. No checks at all ⇒ not green (cerebro
# always runs CI; absence means the run has not started, so we wait).
pr_ci_green() {
  local pr="$1" repo buckets
  repo="$(gh_repo)"
  buckets="$(gh pr checks "$pr" ${repo:+--repo "$repo"} --json bucket --jq '[.[].bucket]' 2>/dev/null || echo '[]')"
  printf '%s' "$buckets" | jq -e '
    (length > 0)
    and (all(.[]; . == "pass" or . == "skipping"))
  ' >/dev/null 2>&1
}

# --- Tine approval gate (fence a) ------------------------------------------
# Pull the Multica tracking-issue id out of a PR body. Accepts either a
# `Tracking-Issue: <FIR-key|uuid>` trailer or a `mention://issue/<uuid>` link.
tracking_issue_from_body() {
  local body="$1" id
  id="$(printf '%s' "$body" | grep -oiE 'Tracking-Issue:[[:space:]]*[A-Za-z0-9-]+' | head -1 | sed -E 's/.*:[[:space:]]*//')"
  if [[ -z "$id" ]]; then
    id="$(printf '%s' "$body" | grep -oE 'mention://issue/[0-9a-fA-F-]+' | head -1 | sed -E 's#mention://issue/##')"
  fi
  printf '%s' "$id"
}

# Approved ⇔ the tracking issue has a comment authored by Tine that matches the
# PASS pattern AND references this PR (by `#<num>` or full URL). Requiring the
# PR reference prevents a stale PASS from an earlier PR on the same issue from
# green-lighting a different PR.
pr_approved_by_tine() {
  local pr="$1"
  command -v multica >/dev/null 2>&1 || { log "multica CLI absent — cannot verify Tine approval"; return 1; }
  require jq; require gh
  local repo body issue url
  repo="$(gh_repo)"
  body="$(gh pr view "$pr" ${repo:+--repo "$repo"} --json body,url --jq '.body' 2>/dev/null || echo '')"
  url="$(gh pr view "$pr" ${repo:+--repo "$repo"} --json url --jq '.url' 2>/dev/null || echo '')"
  issue="$(tracking_issue_from_body "$body")"
  if [[ -z "$issue" ]]; then
    log "PR #$pr has no Tracking-Issue reference — out of scope, skipping"
    return 1
  fi
  local comments
  comments="$(multica issue comment list "$issue" --output json 2>/dev/null || echo '[]')"
  printf '%s' "$comments" | jq -e \
    --arg tine "$TINE_AGENT_ID" \
    --arg pat "$PASS_PATTERN" \
    --arg num "#$pr" \
    --arg url "$url" '
      any(.[];
        (.author_id == $tine)
        and ((.content // "") | test($pat; "i"))
        and (((.content // "") | contains($num))
             or ($url != "" and ((.content // "") | contains($url)))))
    ' >/dev/null 2>&1
}

# --- smoke check (fence c, detection half) ---------------------------------
run_smoke() {
  require curl
  local deadline code
  log "smoke grace: waiting ${SMOKE_GRACE}s for the deploy webhook + build to start"
  sleep "$SMOKE_GRACE"
  deadline=$(( $(date +%s) + SMOKE_TIMEOUT ))
  log "smoke: polling $SMOKE_URL for 200 (budget ${SMOKE_TIMEOUT}s, every ${SMOKE_INTERVAL}s)"
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$SMOKE_URL" 2>/dev/null || echo 000)"
    if [[ "$code" == "200" ]]; then
      log "smoke OK: $SMOKE_URL returned 200"
      return 0
    fi
    log "smoke: got $code, retrying in ${SMOKE_INTERVAL}s"
    sleep "$SMOKE_INTERVAL"
  done
  log "smoke FAIL: $SMOKE_URL never returned 200 within ${SMOKE_TIMEOUT}s"
  return 1
}

# --- rollback (fence c, recovery half) -------------------------------------
# Open and auto-merge a revert of the merge commit. Merging the revert is the
# redeploy of the previous version (webhook fires again). Echoes the revert PR
# URL on success.
rollback_merge() {
  local pr="$1" merge_sha="$2" repo branch ts revert_url
  repo="$(gh_repo)"
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  branch="revert/pr-${pr}-${ts}"
  git fetch --quiet "$ORIGIN_REMOTE" "$BASE_BRANCH"
  git checkout -B "$branch" "$ORIGIN_REMOTE/$BASE_BRANCH"
  # -m 1 reverts relative to the first parent (main) — correct for a merge commit.
  if ! git revert --no-edit -m 1 "$merge_sha"; then
    git revert --abort 2>/dev/null || true
    log "rollback FAILED: could not auto-revert $merge_sha — manual intervention needed" >&2
    return 1
  fi
  git push --quiet "$ORIGIN_REMOTE" "$branch"
  revert_url="$(gh pr create ${repo:+--repo "$repo"} --base "$BASE_BRANCH" --head "$branch" \
    --title "revert: auto-rollback PR #$pr (smoke-check failed)" \
    --body "Automated rollback by the upstream-sync deploy pass. Smoke-check on $SMOKE_URL failed after merging #$pr, so the merge commit \`$merge_sha\` is reverted to restore the previous production version." 2>&1)" || {
      log "rollback: gh pr create failed: $revert_url" >&2; return 1; }
  gh pr merge "$revert_url" ${repo:+--repo "$repo"} --merge >/dev/null 2>&1 || {
    log "rollback: gh pr merge failed for $revert_url" >&2; return 1; }
  printf '%s' "$revert_url"
}

# Create a Multica incident issue (smoke fail) or blocker issue (red CI).
# Best-effort: logs and returns the issue ref if the CLI is present.
file_incident() {
  local title="$1" desc="$2"
  command -v multica >/dev/null 2>&1 || { log "multica absent — incident not filed: $title"; return 0; }
  multica issue create --title "$title" --priority high \
    --assignee-id "$SARA_AGENT_ID" --description "$desc" 2>/dev/null || \
    log "WARN: failed to file incident issue '$title'"
}

write_deploy_json() {
  mkdir -p "$REPORT_DIR"
  printf '%s\n' "$1" > "$REPORT_DIR/last-deploy.json"
}

# --- single-PR deploy ------------------------------------------------------
deploy_pr() {
  local pr="$1" repo merge_sha
  repo="$(gh_repo)"
  require gh; require jq
  log "merging PR #$pr into $BASE_BRANCH (= prod deploy)"
  if ! gh pr merge "$pr" ${repo:+--repo "$repo"} --merge >/dev/null 2>&1; then
    die "gh pr merge failed for #$pr" 4
  fi
  git fetch --quiet "$ORIGIN_REMOTE" "$BASE_BRANCH"
  merge_sha="$(git rev-parse "$ORIGIN_REMOTE/$BASE_BRANCH")"
  log "merged #$pr — main is now $merge_sha; running smoke-check"
  if run_smoke; then
    local now; now="$(date '+%H:%M %Z')"
    log "deploy of #$pr verified at $now"
    printf 'deployed\t%s\t%s\t%s' "$pr" "$merge_sha" "$now"
    return 0
  fi
  log "smoke failed for #$pr — rolling back"
  local revert_url=""
  revert_url="$(rollback_merge "$pr" "$merge_sha" || true)"
  file_incident \
    "Auto-deploy incident — PR #$pr smoke-check failed $(date -u +%Y-%m-%d)" \
    "$(printf 'Nightly auto-deploy merged PR #%s (commit %s) but the smoke-check on %s did not return 200 within %ss.\n\nRollback: %s\n\n[@Sara - CTO](mention://agent/%s) — please investigate the failed deploy and the rollback.' \
        "$pr" "$merge_sha" "$SMOKE_URL" "$SMOKE_TIMEOUT" "${revert_url:-MANUAL ROLLBACK REQUIRED — auto-revert failed}" "$SARA_AGENT_ID")"
  printf 'rolled-back\t%s\t%s\t%s' "$pr" "$merge_sha" "${revert_url:-manual}"
  return 5
}

# --- full pass -------------------------------------------------------------
run_pass() {
  require gh; require jq
  git remote get-url "$ORIGIN_REMOTE" >/dev/null 2>&1 || die "missing git remote '$ORIGIN_REMOTE'"

  if is_frozen; then
    log "deploy-freeze label present on status issue — skipping all merges"
    write_deploy_json '{"status":"frozen","deployed":[],"rolled_back":[],"skipped":[],"ran_at":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}'
    exit 2
  fi

  local candidates count
  candidates="$(scan_candidates)"
  count="$(printf '%s' "$candidates" | jq 'length')"
  log "found $count CI-green mergeable candidate PR(s)"

  local deployed='[]' rolled='[]' skipped='[]'
  local i pr
  for ((i = 0; i < count; i++)); do
    pr="$(printf '%s' "$candidates" | jq -r ".[$i].number")"
    if ! pr_ci_green "$pr"; then
      log "PR #$pr CI not green — skipping (blocker fence)"
      skipped="$(jq -c --argjson n "$pr" --arg r "ci-not-green" '. + [{pr:$n,reason:$r}]' <<<"$skipped")"
      continue
    fi
    if ! pr_approved_by_tine "$pr"; then
      log "PR #$pr lacks Tine PASS — skipping (approval fence)"
      skipped="$(jq -c --argjson n "$pr" --arg r "no-tine-approval" '. + [{pr:$n,reason:$r}]' <<<"$skipped")"
      continue
    fi
    log "PR #$pr approved by Tine + CI green — deploying"
    local result rc=0
    result="$(deploy_pr "$pr")" || rc=$?
    local kind; kind="$(printf '%s' "$result" | cut -f1)"
    if [[ "$kind" == "deployed" ]]; then
      deployed="$(jq -c --argjson n "$pr" --arg sha "$(printf '%s' "$result" | cut -f3)" --arg at "$(printf '%s' "$result" | cut -f4)" '. + [{pr:$n,sha:$sha,deployed_at:$at}]' <<<"$deployed")"
    else
      rolled="$(jq -c --argjson n "$pr" --arg sha "$(printf '%s' "$result" | cut -f3)" --arg revert "$(printf '%s' "$result" | cut -f4)" '. + [{pr:$n,sha:$sha,revert:$revert}]' <<<"$rolled")"
    fi
    [[ "$rc" -eq 4 ]] && die "merge failure on #$pr" 4
  done

  local out
  out="$(jq -nc \
    --argjson d "$deployed" --argjson r "$rolled" --argjson s "$skipped" \
    --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{status: (if ($r|length)>0 then "rolled-back" elif ($d|length)>0 then "deployed" else "noop" end), deployed:$d, rolled_back:$r, skipped:$s, ran_at:$t}')"
  write_deploy_json "$out"
  log "pass complete: $out"
  if [[ "$(printf '%s' "$out" | jq -r '.status')" == "rolled-back" ]]; then exit 5; fi
  exit 0
}

show_report() {
  if [[ -f "$REPORT_DIR/last-deploy.json" ]]; then cat "$REPORT_DIR/last-deploy.json"; else log "no report at $REPORT_DIR/last-deploy.json"; fi
}

# Allow the test harness to source this file (functions only, no dispatch).
(return 0 2>/dev/null) && return 0

case "${1:-}" in
  --help|-h) usage; exit 0 ;;
  --pass)    MODE="pass" ;;
  --scan)    MODE="scan" ;;
  --check)   MODE="check"; PR_ARG="${2:-}" ;;
  --deploy)  MODE="deploy"; PR_ARG="${2:-}" ;;
  --smoke)   MODE="smoke" ;;
  --report)  MODE="report" ;;
  "")        usage; exit 0 ;;
  *)         die "unknown argument: $1" ;;
esac

case "$MODE" in
  pass)   run_pass ;;
  scan)   scan_candidates ;;
  check)  [[ -n "${PR_ARG:-}" ]] || die "--check requires a PR number"
          if pr_approved_by_tine "$PR_ARG"; then log "PR #$PR_ARG is Tine-approved"; exit 0; else log "PR #$PR_ARG is NOT Tine-approved"; exit 10; fi ;;
  deploy) [[ -n "${PR_ARG:-}" ]] || die "--deploy requires a PR number"
          deploy_pr "$PR_ARG" >/dev/null ;;
  smoke)  run_smoke ;;
  report) show_report ;;
esac
