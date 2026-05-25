#!/usr/bin/env bash
#
# upstream-sync.sh — orchestrates nightly upstream → fork sync.
#
# Modes:
#   --nightly  Full nightly orchestration: fetch, rolling branch, merge,
#              optional auto-resolve, then either open a PR (clean) or
#              emit a conflict report (escalate). Idempotent — safe to
#              run multiple times the same day.
#   --dry-run  Fetch upstream and preview conflicts without touching
#              local branches.
#   --report   Print the latest sync status JSON if present.
#   --help     Show this message.
#
# The script never force-pushes, never rewrites main, and never resolves
# anything outside what scripts/upstream-sync-resolve.sh already handles.
#
# Outputs:
#   - $REPORT_DIR/last-run.json        — machine-readable status
#   - $REPORT_DIR/last-run.log         — full stdout/stderr of the run
#   - $REPORT_DIR/conflicts-<ts>.md    — human-readable conflict report (only when status=conflict)
#
# Exit codes:
#   0  noop | clean | clean-after-resolve
#   2  conflict (report written; needs manual resolve)
#   3  precondition failure (wrong cwd, missing remote, dirty tree)
#   4  push or PR creation failed

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REPORT_DIR="$ROOT/.upstream-sync"
ROLLING_BRANCH="chore/upstream-rolling"
BASE_BRANCH="main"
UPSTREAM_REMOTE="upstream"
ORIGIN_REMOTE="origin"
PR_REVIEWER="${UPSTREAM_SYNC_REVIEWER:-tinetestsen}"
MODE=""

usage() {
  sed -n '3,20p' "$0"
}

log() {
  printf '[upstream-sync] %s\n' "$*"
}

die() {
  log "FATAL: $*" >&2
  exit "${2:-3}"
}

ensure_clean_tree() {
  if ! git diff --quiet || ! git diff --cached --quiet; then
    die "working tree is dirty — commit or stash before running"
  fi
}

ensure_remote() {
  local name="$1"
  git remote get-url "$name" >/dev/null 2>&1 \
    || die "missing git remote '$name'"
}

# Resolve the GitHub `owner/repo` for the fork from the `origin` remote URL.
# Required because `gh` would otherwise default to whatever `gh repo view`
# resolves to (often `upstream`/`multica-ai/multica` in a checkout that has
# both remotes), which makes `gh pr list` miss fork PRs and breaks the
# in-flight guard (see FIR-2169 QA on PR #594).
origin_repo() {
  local url
  url="$(git remote get-url "$ORIGIN_REMOTE" 2>/dev/null)" || return 1
  # strip optional scheme/host and trailing .git so both
  # https://github.com/<owner>/<repo>.git and git@github.com:<owner>/<repo>.git work
  url="${url#git@github.com:}"
  url="${url#https://github.com/}"
  url="${url#http://github.com/}"
  url="${url%.git}"
  printf '%s' "$url"
}

write_status_json() {
  local status="$1" pr_url="${2:-}" conflict_count="${3:-0}" upstream_sha="${4:-}" message="${5:-}"
  mkdir -p "$REPORT_DIR"
  cat > "$REPORT_DIR/last-run.json" <<EOF
{
  "status": "$status",
  "ran_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "pr_url": "$pr_url",
  "conflict_count": $conflict_count,
  "upstream_sha": "$upstream_sha",
  "message": "$message"
}
EOF
}

dry_run() {
  ensure_remote "$UPSTREAM_REMOTE"
  log "fetching $UPSTREAM_REMOTE/$BASE_BRANCH (dry-run)"
  git fetch --quiet "$UPSTREAM_REMOTE" "$BASE_BRANCH"
  local upstream_sha base_sha
  upstream_sha="$(git rev-parse "$UPSTREAM_REMOTE/$BASE_BRANCH")"
  base_sha="$(git rev-parse "$BASE_BRANCH")"
  if [[ "$upstream_sha" == "$base_sha" ]]; then
    log "no new commits on $UPSTREAM_REMOTE/$BASE_BRANCH"
    return 0
  fi
  local behind
  behind="$(git rev-list --count "$BASE_BRANCH..$UPSTREAM_REMOTE/$BASE_BRANCH")"
  log "fork is $behind commits behind upstream"
  log "preview conflict files (merge-base diff):"
  git merge-tree --name-only "$BASE_BRANCH" "$UPSTREAM_REMOTE/$BASE_BRANCH" 2>/dev/null || true
}

# Look up an open PR with the same head branch — exact match avoids
# accidentally treating a different branch ("foo-rolling") as the same PR.
# Pin to the fork repo so checkouts with both `origin` (fork) and `upstream`
# (multica-ai/multica) remotes still scope this lookup to the fork.
existing_pr_url() {
  command -v gh >/dev/null 2>&1 || return 0
  local repo
  repo="$(origin_repo 2>/dev/null)" || return 0
  [[ -z "$repo" ]] && return 0
  gh pr list --repo "$repo" --head "$ROLLING_BRANCH" --state open --limit 1 \
    --json url --jq '.[0].url // ""' 2>/dev/null || true
}

# Look up any open upstream-sync PR in flight — the bot's own rolling branch
# OR a human-driven catch-up branch named `upstream-sync/*`. Used to skip a
# nightly run when a previous batch (manual or bot) is still being merged,
# so the bot does not create a duplicate conflict report on the same commits.
# Pin to the fork repo — `gh` would otherwise default to `upstream` in a
# checkout that has both remotes (FIR-2169 QA on PR #594).
existing_inflight_pr_url() {
  command -v gh >/dev/null 2>&1 || return 0
  local repo
  repo="$(origin_repo 2>/dev/null)" || return 0
  [[ -z "$repo" ]] && return 0
  gh pr list --repo "$repo" --state open --limit 50 \
    --json headRefName,url \
    --jq "[.[] | select(.headRefName == \"$ROLLING_BRANCH\" or (.headRefName | startswith(\"upstream-sync/\")))] | .[0].url // \"\"" \
    2>/dev/null || true
}

nightly() {
  ensure_remote "$UPSTREAM_REMOTE"
  ensure_remote "$ORIGIN_REMOTE"
  ensure_clean_tree

  mkdir -p "$REPORT_DIR"
  local log_file="$REPORT_DIR/last-run.log"
  : > "$log_file"
  exec > >(tee -a "$log_file") 2>&1

  log "fetching $UPSTREAM_REMOTE/$BASE_BRANCH"
  git fetch --quiet "$UPSTREAM_REMOTE" "$BASE_BRANCH"
  git fetch --quiet "$ORIGIN_REMOTE" "$BASE_BRANCH"

  local upstream_sha base_sha
  upstream_sha="$(git rev-parse "$UPSTREAM_REMOTE/$BASE_BRANCH")"
  base_sha="$(git rev-parse "$ORIGIN_REMOTE/$BASE_BRANCH")"

  if [[ "$upstream_sha" == "$base_sha" ]]; then
    log "no new commits on upstream — exiting cleanly"
    write_status_json "noop" "" 0 "$upstream_sha" "Upstream HEAD equals fork main"
    return 0
  fi

  local behind
  behind="$(git rev-list --count "$ORIGIN_REMOTE/$BASE_BRANCH..$UPSTREAM_REMOTE/$BASE_BRANCH")"
  log "fork is $behind commits behind upstream ($upstream_sha)"

  # In-flight guard: if any upstream-sync PR is already open — the bot's own
  # rolling branch OR a manual catch-up on `upstream-sync/*` — skip this run.
  # Without this, the bot races a human-driven catch-up and escalates a
  # duplicate conflict report on the same commits (see FIR-2169 / FIR-2197).
  local inflight_url
  inflight_url="$(existing_inflight_pr_url || true)"
  if [[ -n "$inflight_url" ]]; then
    log "upstream-sync PR already in flight: $inflight_url — skipping"
    write_status_json "noop" "$inflight_url" 0 "$upstream_sha" "Upstream-sync PR already in flight: $inflight_url"
    return 0
  fi

  # Idempotency: if an open PR already exists for this exact branch +
  # this exact upstream SHA, exit early — we already shipped this batch.
  local pr_url
  pr_url="$(existing_pr_url || true)"
  if [[ -n "$pr_url" ]]; then
    if git rev-parse --quiet --verify "$ORIGIN_REMOTE/$ROLLING_BRANCH" >/dev/null; then
      local origin_rolling_sha
      origin_rolling_sha="$(git rev-parse "$ORIGIN_REMOTE/$ROLLING_BRANCH")"
      if git merge-base --is-ancestor "$upstream_sha" "$origin_rolling_sha"; then
        log "open PR $pr_url already includes $upstream_sha — skipping"
        write_status_json "noop" "$pr_url" 0 "$upstream_sha" "Open PR already covers this upstream SHA"
        return 0
      fi
    fi
  fi

  # Reset rolling branch from fork main — never from a previous rolling tip.
  log "resetting $ROLLING_BRANCH from $ORIGIN_REMOTE/$BASE_BRANCH"
  git checkout -B "$ROLLING_BRANCH" "$ORIGIN_REMOTE/$BASE_BRANCH"

  log "merging $UPSTREAM_REMOTE/$BASE_BRANCH (no-ff, no-commit)"
  if git merge --no-ff --no-commit "$UPSTREAM_REMOTE/$BASE_BRANCH"; then
    log "clean merge"
    git commit -m "chore: sync upstream/main ($(echo "$upstream_sha" | cut -c1-12))"
    if push_and_open_pr "$upstream_sha" "$behind" "clean"; then
      return 0
    else
      return 4
    fi
  fi

  log "merge produced conflicts — running auto-resolve"
  if ! bash "$ROOT/scripts/upstream-sync-resolve.sh" --apply; then
    log "auto-resolve script returned non-zero — escalating without further changes"
  fi

  local remaining
  remaining="$(git status --porcelain | grep -cE '^(DD|AU|UD|UA|DU|AA|UU) ' || true)"
  if [[ "$remaining" -eq 0 ]]; then
    log "auto-resolve cleared all conflicts"
    git commit -m "chore: sync upstream/main ($(echo "$upstream_sha" | cut -c1-12)) [auto-resolved]"
    if push_and_open_pr "$upstream_sha" "$behind" "clean-after-resolve"; then
      return 0
    else
      return 4
    fi
  fi

  log "$remaining conflict file(s) remain — escalating"
  local ts
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  local conflict_report="$REPORT_DIR/conflicts-$ts.md"
  write_conflict_report "$conflict_report" "$upstream_sha" "$behind" "$remaining"
  # Abort the in-progress merge so the worktree is left clean for the
  # human (or follow-up bot run) to inspect without surprise state.
  git merge --abort || true
  git checkout "$BASE_BRANCH" || true
  write_status_json "conflict" "" "$remaining" "$upstream_sha" "See $conflict_report"
  log "conflict report: $conflict_report"
  return 2
}

write_conflict_report() {
  local out="$1" upstream_sha="$2" behind="$3" remaining="$4"
  {
    echo "# Upstream sync conflict report"
    echo
    echo "- Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "- Upstream HEAD: \`$upstream_sha\`"
    echo "- Fork is behind by: $behind commit(s)"
    echo "- Conflict files remaining after auto-resolve: $remaining"
    echo
    echo "## Conflicting files"
    echo
    echo '```'
    git status --porcelain | grep -E '^(DD|AU|UD|UA|DU|AA|UU) ' || true
    echo '```'
    echo
    echo "## Upstream commits in this batch"
    echo
    echo '```'
    git log --oneline "$ORIGIN_REMOTE/$BASE_BRANCH..$UPSTREAM_REMOTE/$BASE_BRANCH" || true
    echo '```'
  } > "$out"
}

push_and_open_pr() {
  local upstream_sha="$1" behind="$2" outcome="$3"

  log "pushing $ROLLING_BRANCH to $ORIGIN_REMOTE"
  if ! git push --force-with-lease "$ORIGIN_REMOTE" "$ROLLING_BRANCH"; then
    write_status_json "push-failed" "" 0 "$upstream_sha" "git push failed"
    return 1
  fi

  if ! command -v gh >/dev/null 2>&1; then
    log "gh CLI not available — skipping PR creation"
    write_status_json "$outcome" "" 0 "$upstream_sha" "Branch pushed; gh CLI absent so PR was not opened"
    return 0
  fi

  local existing
  existing="$(existing_pr_url || true)"
  if [[ -n "$existing" ]]; then
    log "existing PR found: $existing — updating in place via push"
    write_status_json "$outcome" "$existing" 0 "$upstream_sha" "Updated existing PR with new upstream batch"
    return 0
  fi

  local short_sha title body
  short_sha="$(echo "$upstream_sha" | cut -c1-12)"
  title="chore: sync upstream/main ($short_sha)"
  body="$(printf 'Nightly upstream sync from \x60multica-ai/multica\x60.\n\n- Outcome: %s\n- Upstream HEAD: \x60%s\x60\n- Commits in batch: %s\n\nAuto-opened by the upstream-sync bot. Review and merge as part of normal QA.\n' \
    "$outcome" "$upstream_sha" "$behind")"

  local pr_url repo
  repo="$(origin_repo 2>/dev/null || true)"
  if pr_url="$(gh pr create \
    ${repo:+--repo "$repo"} \
    --base "$BASE_BRANCH" \
    --head "$ROLLING_BRANCH" \
    --title "$title" \
    --body "$body" \
    --reviewer "$PR_REVIEWER" 2>&1)"; then
    log "PR opened: $pr_url"
    write_status_json "$outcome" "$pr_url" 0 "$upstream_sha" "PR opened"
    return 0
  else
    log "gh pr create failed: $pr_url"
    write_status_json "pr-failed" "" 0 "$upstream_sha" "gh pr create failed: $pr_url"
    return 1
  fi
}

show_report() {
  if [[ ! -f "$REPORT_DIR/last-run.json" ]]; then
    log "no report at $REPORT_DIR/last-run.json"
    return 0
  fi
  cat "$REPORT_DIR/last-run.json"
}

case "${1:-}" in
  --help|-h) usage; exit 0 ;;
  --dry-run) MODE="dry-run" ;;
  --nightly) MODE="nightly" ;;
  --report)  MODE="report" ;;
  "")        usage; exit 0 ;;
  *)         die "unknown argument: $1" ;;
esac

case "$MODE" in
  dry-run) dry_run ;;
  nightly) nightly ;;
  report)  show_report ;;
esac
