#!/usr/bin/env bash
# PR-review plumbing for a Multica agent.
# - Reads the assigned Multica issue (created by pr-agent-sidecar) to learn
#   which PR to review.
# - Clones the PR at its head SHA into a tmpdir.
# - Prints the diff vs the PR base for the agent to reason over.
# - Posts inline / summary review comments + approve / request-changes back
#   to the PR via `gh`.
#
# The agent does the reviewing. This script is plumbing only.
#
# Subcommands:
#   PrReviewer.sh resolve
#   PrReviewer.sh clone   <owner> <repo> <head_sha>
#   PrReviewer.sh diff    <tmpdir> <base_ref>
#   PrReviewer.sh comment <owner> <repo> <pr_number> <path> <line> <body>
#   PrReviewer.sh review  <owner> <repo> <pr_number> <event> <body_or_dash>
#   PrReviewer.sh push    <tmpdir> <branch> <commit_msg>
#
# Exit codes:
#   2  missing env var / arg
#   3  Multica API call failed
#   4  could not parse PR URL / head SHA from issue body
#   5  repo not in REPO_ALLOWLIST
#   6  git operation failed
#   7  gh API call failed
#   8  gh CLI not installed

set -euo pipefail

die() { echo "$1" >&2; exit "${2:-1}"; }

require_arg() {
  # require_arg "<value>" "<name>"
  [[ -n "${1:-}" ]] || die "missing required argument: $2" 2
}

require_env() {
  # require_env VAR_NAME
  local v="${1:?}"
  [[ -n "${!v:-}" ]] || die "$v is not set" 2
}

require_gh() {
  command -v gh >/dev/null 2>&1 || die "gh CLI not installed in this runtime" 8
}

require_allowlist() {
  # require_allowlist "owner/repo"
  # REPO_ALLOWLIST="*" allows any repo. Otherwise comma-separated owner/repo.
  local target="${1:?}"
  require_env REPO_ALLOWLIST
  [[ "$REPO_ALLOWLIST" == "*" ]] && return 0
  local IFS=,
  for entry in $REPO_ALLOWLIST; do
    entry="${entry// /}"
    [[ "$entry" == "$target" || "$entry" == "*" ]] && return 0
  done
  die "repo $target is not in REPO_ALLOWLIST — refusing" 5
}

# ----- Multica API ------------------------------------------------------------

multica_api() {
  # multica_api <path>
  require_env MULTICA_TOKEN
  require_env MULTICA_WORKSPACE_ID
  require_env MULTICA_SERVER_URL
  local base="${MULTICA_SERVER_URL%/}"
  curl -fsS \
    -H "Authorization: Bearer $MULTICA_TOKEN" \
    -H "X-Workspace-ID: $MULTICA_WORKSPACE_ID" \
    "$base$1" \
    || die "Multica API call failed: $1" 3
}

# ----- gh wrappers ------------------------------------------------------------

# `gh` reads GH_TOKEN from the environment automatically; no `gh auth login`
# needed for non-interactive use. We never echo the token.

gh_pr_view() {
  # gh_pr_view <owner> <repo> <pr_number> <jq_filter>
  local owner="$1" repo="$2" num="$3" filter="$4"
  require_gh
  require_env GH_TOKEN
  gh pr view "$num" --repo "$owner/$repo" --json baseRefName,headRefOid 2>/dev/null \
    | jq -r "$filter" \
    || die "gh pr view failed for $owner/$repo#$num" 7
}

# ----- resolve ---------------------------------------------------------------

cmd_resolve() {
  require_env MULTICA_TASK_ID
  require_env MULTICA_AGENT_ID

  local task issue_id issue body
  task=$(multica_api "/api/agents/$MULTICA_AGENT_ID/tasks" \
         | jq -c --arg id "$MULTICA_TASK_ID" '.[]? | select(.id == $id)' \
         | head -n1)
  [[ -n "$task" ]] || die "task $MULTICA_TASK_ID not found on agent $MULTICA_AGENT_ID" 3

  issue_id=$(echo "$task" | jq -r '.issue_id // empty')
  [[ -n "$issue_id" ]] || die "task has no issue_id — not a PR-review trigger" 4

  issue=$(multica_api "/api/issues/$issue_id")
  body=$(echo "$issue" | jq -r '.description // .body // ""')
  [[ -n "$body" ]] || die "issue $issue_id has no description body" 4

  # Extract a github PR URL: https://github.com/<owner>/<repo>/pull/<n>
  local pr_url owner repo pr_number head_sha base_ref
  pr_url=$(printf '%s' "$body" \
           | grep -oE 'https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/pull/[0-9]+' \
           | head -n1 || true)
  [[ -n "$pr_url" ]] || die "no GitHub PR URL found in issue body" 4

  owner=$(echo "$pr_url" | awk -F/ '{print $4}')
  repo=$(echo  "$pr_url" | awk -F/ '{print $5}')
  pr_number=$(echo "$pr_url" | awk -F/ '{print $7}')

  # head SHA: look for a labelled `head sha: <hex>` (case-insensitive,
  # underscore / dash / space tolerated), 7-40 hex chars.
  head_sha=$(printf '%s' "$body" \
             | grep -oiE 'head[ _-]?sha[: ]+[a-f0-9]{7,40}' \
             | head -n1 \
             | grep -oE '[a-f0-9]{7,40}$' || true)
  [[ -n "$head_sha" ]] || die "no head SHA found in issue body (expected 'head sha: <hex>')" 4

  # base_ref is best-effort — needs gh + GH_TOKEN. Null if unavailable.
  base_ref=null
  if [[ -n "${GH_TOKEN:-}" ]] && command -v gh >/dev/null 2>&1; then
    local b
    b=$(gh pr view "$pr_number" --repo "$owner/$repo" --json baseRefName 2>/dev/null \
        | jq -r '.baseRefName // empty' || true)
    [[ -n "$b" ]] && base_ref=$(printf '%s' "$b" | jq -Rs .)
  fi

  jq -n \
    --arg pr_url "$pr_url" \
    --arg owner "$owner" \
    --arg repo "$repo" \
    --argjson pr_number "$pr_number" \
    --arg head_sha "$head_sha" \
    --argjson base_ref "$base_ref" \
    '{pr_url:$pr_url, owner:$owner, repo:$repo, pr_number:$pr_number, head_sha:$head_sha, base_ref:$base_ref}'
}

# ----- clone -----------------------------------------------------------------

cmd_clone() {
  local owner="${1:-}" repo="${2:-}" head_sha="${3:-}"
  require_arg "$owner" owner
  require_arg "$repo" repo
  require_arg "$head_sha" head_sha
  require_allowlist "$owner/$repo"
  require_env GH_TOKEN

  local tmp
  tmp=$(mktemp -d -t pr-review-XXXXXX) || die "mktemp failed" 6

  # Use the token in the URL but never echo it. Redirect git output to stderr
  # so the only stdout line is the tmpdir path.
  local url="https://x-access-token:${GH_TOKEN}@github.com/${owner}/${repo}.git"

  {
    git -C "$tmp" init -q
    git -C "$tmp" remote add origin "$url"
    git -C "$tmp" fetch --depth=50 origin "$head_sha"
    git -C "$tmp" checkout -q FETCH_HEAD
    # Scrub the tokenised URL so subsequent commands don't leak it via
    # `git remote -v` output captured into logs.
    git -C "$tmp" remote set-url origin "https://github.com/${owner}/${repo}.git"
  } >&2 || die "git clone/fetch/checkout failed" 6

  printf '%s\n' "$tmp"
}

# ----- diff ------------------------------------------------------------------

cmd_diff() {
  local tmpdir="${1:-}" base_ref="${2:-}"
  require_arg "$tmpdir" tmpdir
  require_arg "$base_ref" base_ref
  [[ -d "$tmpdir/.git" ]] || die "$tmpdir is not a git repo" 6

  # Re-inject the token transiently for the fetch, then strip it back out.
  require_env GH_TOKEN
  local origin_url owner_repo
  origin_url=$(git -C "$tmpdir" remote get-url origin)
  owner_repo=$(echo "$origin_url" | sed -E 's#^https?://(x-access-token:[^@]+@)?github\.com/##; s#\.git$##')

  git -C "$tmpdir" remote set-url origin \
    "https://x-access-token:${GH_TOKEN}@github.com/${owner_repo}.git"
  {
    git -C "$tmpdir" fetch --depth=50 origin "$base_ref"
  } >&2 || { git -C "$tmpdir" remote set-url origin "$origin_url"; die "git fetch base failed" 6; }
  git -C "$tmpdir" remote set-url origin "$origin_url"

  git -C "$tmpdir" diff "origin/${base_ref}...HEAD" || die "git diff failed" 6
}

# ----- comment ---------------------------------------------------------------

cmd_comment() {
  local owner="${1:-}" repo="${2:-}" num="${3:-}" path="${4:-}" line="${5:-}" body="${6:-}"
  require_arg "$owner" owner
  require_arg "$repo" repo
  require_arg "$num" pr_number
  require_arg "$path" path
  require_arg "$line" line
  require_arg "$body" body
  require_allowlist "$owner/$repo"
  require_gh
  require_env GH_TOKEN

  local commit_id
  commit_id=$(gh_pr_view "$owner" "$repo" "$num" '.headRefOid')
  [[ -n "$commit_id" ]] || die "could not resolve head commit for $owner/$repo#$num" 7

  local payload
  payload=$(jq -n \
    --arg body "$body" \
    --arg commit_id "$commit_id" \
    --arg path "$path" \
    --argjson line "$line" \
    '{body:$body, commit_id:$commit_id, path:$path, line:$line, side:"RIGHT"}')

  gh api -X POST "repos/${owner}/${repo}/pulls/${num}/comments" \
    --input - <<<"$payload" >/dev/null \
    || die "gh api inline-comment POST failed" 7
}

# ----- review ----------------------------------------------------------------

cmd_review() {
  local owner="${1:-}" repo="${2:-}" num="${3:-}" event="${4:-}" body="${5:-}"
  require_arg "$owner" owner
  require_arg "$repo" repo
  require_arg "$num" pr_number
  require_arg "$event" event
  require_allowlist "$owner/$repo"
  require_gh
  require_env GH_TOKEN

  case "$event" in
    COMMENT|APPROVE|REQUEST_CHANGES) ;;
    *) die "event must be one of COMMENT, APPROVE, REQUEST_CHANGES (got: $event)" 2 ;;
  esac

  # Allow body via stdin when passed as "-"
  if [[ "$body" == "-" || -z "$body" ]]; then
    body=$(cat)
  fi

  local payload
  payload=$(jq -n \
    --arg body "$body" \
    --arg event "$event" \
    '{body:$body, event:$event}')

  gh api -X POST "repos/${owner}/${repo}/pulls/${num}/reviews" \
    --input - <<<"$payload" >/dev/null \
    || die "gh api review POST failed" 7
}

# ----- push ------------------------------------------------------------------

cmd_push() {
  local tmpdir="${1:-}" branch="${2:-}" msg="${3:-}"
  require_arg "$tmpdir" tmpdir
  require_arg "$branch" branch
  require_arg "$msg" commit_msg
  [[ -d "$tmpdir/.git" ]] || die "$tmpdir is not a git repo" 6
  require_env GH_TOKEN

  local origin_url owner_repo
  origin_url=$(git -C "$tmpdir" remote get-url origin)
  owner_repo=$(echo "$origin_url" | sed -E 's#^https?://(x-access-token:[^@]+@)?github\.com/##; s#\.git$##')
  require_allowlist "$owner_repo"

  if git -C "$tmpdir" diff --quiet && git -C "$tmpdir" diff --cached --quiet; then
    die "working tree clean — nothing to push" 6
  fi

  {
    git -C "$tmpdir" -c user.email='noreply@multica' -c user.name='multica-pr-reviewer' add -A
    git -C "$tmpdir" -c user.email='noreply@multica' -c user.name='multica-pr-reviewer' \
      commit -m "$msg"
    git -C "$tmpdir" remote set-url origin \
      "https://x-access-token:${GH_TOKEN}@github.com/${owner_repo}.git"
    git -C "$tmpdir" push origin "HEAD:${branch}"
    git -C "$tmpdir" remote set-url origin "https://github.com/${owner_repo}.git"
  } >&2 || die "git commit/push failed" 6
}

# ----- dispatch --------------------------------------------------------------

sub="${1:-}"; shift || true
case "$sub" in
  resolve) cmd_resolve "$@" ;;
  clone)   cmd_clone   "$@" ;;
  diff)    cmd_diff    "$@" ;;
  comment) cmd_comment "$@" ;;
  review)  cmd_review  "$@" ;;
  push)    cmd_push    "$@" ;;
  ""|-h|--help)
    sed -n '1,30p' "$0" >&2
    exit 0
    ;;
  *) die "unknown subcommand: $sub" 2 ;;
esac
