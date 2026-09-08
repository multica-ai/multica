#!/usr/bin/env bash
# Reconcile stale agent labels so portfolio dispatch can continue hands-off.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/agent-queue.sh
source "$SCRIPT_DIR/lib/agent-queue.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
DRY_RUN=0
INCLUDE_PAUSED=0

usage() {
  cat <<'EOF'
Usage: ceo-reconcile-queue.sh [options]

Fixes common label drift:
  - open PR with merge conflicts → agent-blocked (CEO must resolve)
  - agent-done on open issues with no open/merged PR → back to agent-safe
  - agent-running on open issues with no open PR → back to agent-safe
  - merged PR linked to issue → strip agent-* labels

Runs automatically in ceo-nightly.sh before auto-merge.

Options:
  --include-paused   Also reconcile paused registry projects (stale labels only)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --include-paused) INCLUDE_PAUSED=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if ! command -v gh &>/dev/null; then
  echo "error: gh CLI required" >&2
  exit 1
fi

repos="$(
  python3 - "$REGISTRY" "$GITHUB_ORG" "$INCLUDE_PAUSED" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
org = sys.argv[2]
include_paused = sys.argv[3] == "1"
current = None
paused = False
for line in registry.read_text(encoding="utf-8").splitlines():
    stripped = line.strip()
    if stripped.startswith("- id:"):
        current = stripped.split(":", 1)[1].strip()
        paused = False
        continue
    if current and stripped.startswith("paused:"):
        paused = stripped.split(":", 1)[1].strip() == "true"
        continue
    if current and stripped.startswith("repo:"):
        repo = stripped.split(":", 1)[1].strip()
        repo = repo.replace("github.com/", "").replace("https://github.com/", "")
        if repo.startswith("your-org/"):
            repo = repo.replace("your-org/", f"{org}/", 1)
        if not paused or include_paused:
            print(repo)
        current = None
PY
)"

fixed=0
while IFS= read -r repo; do
  [ -n "$repo" ] || continue
  root="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --repo "$repo" --quiet 2>/dev/null || true)"

  reconcile_stale_running_labels "$repo" "$root" "$DRY_RUN"
  reconcile_auth_blocked_retries "$repo" "$DRY_RUN"

  open_prs="$(
    gh pr list -R "$repo" -s open \
      --json number,closingIssuesReferences \
      -q '.[] | select(.closingIssuesReferences | length > 0) | . as $pr | .closingIssuesReferences[] | "\(.number)\t\($pr.number)"' \
      2>/dev/null || true
  )"
  while IFS=$'\t' read -r issue_num pr_num; do
    [ -z "$issue_num" ] && continue
    if issue_dispatch_active "$issue_num" "$root"; then
      continue
    fi
    mergeable="$(
      gh pr view "$pr_num" -R "$repo" --json mergeable -q '.mergeable' 2>/dev/null || echo UNKNOWN
    )"
    if [ "$mergeable" = "CONFLICTING" ]; then
      if [ "$DRY_RUN" -eq 1 ]; then
        echo "would block: $repo#$issue_num (PR #$pr_num has merge conflicts)"
      else
        gh issue edit "$issue_num" -R "$repo" \
          --remove-label "agent-done" 2>/dev/null || true
        gh issue edit "$issue_num" -R "$repo" \
          --remove-label "agent-running" 2>/dev/null || true
        gh issue edit "$issue_num" -R "$repo" \
          --add-label "agent-blocked" 2>/dev/null || true
        gh issue comment "$issue_num" -R "$repo" --body \
          "🔴 PR #$pr_num has merge conflicts — CEO or agent must resolve before auto-merge." 2>/dev/null || true
        echo "reconcile: $repo#$issue_num → agent-blocked (PR #$pr_num conflicting)"
      fi
      fixed=$((fixed + 1))
      continue
    fi
    labels="$(
      gh issue view "$issue_num" -R "$repo" --json labels -q '[.labels[].name] | join(",")' 2>/dev/null || true
    )"
    case "$labels" in
      *agent-done*) continue ;;
    esac
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "would tag agent-done: $repo#$issue_num (open PR #$pr_num)"
    else
      gh issue edit "$issue_num" -R "$repo" --remove-label "agent-running" --add-label "agent-done" 2>/dev/null || true
      echo "reconcile: $repo#$issue_num → agent-done (open PR #$pr_num)"
    fi
    fixed=$((fixed + 1))
  done <<<"$open_prs"

  numbers="$(
    {
      gh issue list -R "$repo" -s open -l agent-done --json number -q '.[].number' 2>/dev/null || true
      gh issue list -R "$repo" -s open -l agent-running --json number -q '.[].number' 2>/dev/null || true
    } | sort -u
  )"
  for num in $numbers; do
    [ -z "$num" ] && continue
    if issue_dispatch_active "$num" "$root"; then
      echo "reconcile: skip $repo#$num (dispatch in progress)"
      continue
    fi
    merged_refs="$(
      gh issue view "$num" -R "$repo" \
        --json closedByPullRequestsReferences -q '.closedByPullRequestsReferences | length' 2>/dev/null || echo 0
    )"
    if [ "${merged_refs:-0}" -gt 0 ]; then
      if [ "$DRY_RUN" -eq 1 ]; then
        echo "would strip labels: $repo#$num (merged)"
      else
        strip_agent_labels "$repo" "$num"
        echo "reconcile: $repo#$num stripped agent labels (merged)"
      fi
      fixed=$((fixed + 1))
      continue
    fi

    open_pr="$(
      gh pr list -R "$repo" -s open --search "issue $num" --json number -q 'length' 2>/dev/null || echo 0
    )"
    if [ "${open_pr:-0}" -gt 0 ]; then
      labels="$(
        gh issue view "$num" -R "$repo" --json labels -q '[.labels[].name] | join(",")' 2>/dev/null || true
      )"
      if [ "$DRY_RUN" -eq 1 ]; then
        echo "would keep: $repo#$num (open PR, labels: $labels)"
      else
        gh issue edit "$num" -R "$repo" --remove-label "agent-running" --add-label "agent-done" 2>/dev/null || true
      fi
      continue
    fi

    labels="$(
      gh issue view "$num" -R "$repo" --json labels -q '[.labels[].name] | join(",")' 2>/dev/null || true
    )"
    case "$labels" in
      *agent-done*|*agent-running*)
        if [ "$DRY_RUN" -eq 1 ]; then
          echo "would re-queue: $repo#$num ($labels)"
        else
          gh issue edit "$num" -R "$repo" --remove-label "agent-done" 2>/dev/null || true
          gh issue edit "$num" -R "$repo" --remove-label "agent-running" 2>/dev/null || true
          gh issue edit "$num" -R "$repo" --add-label "agent-safe" 2>/dev/null || true
          echo "reconcile: $repo#$num → agent-safe (no open PR)"
        fi
        fixed=$((fixed + 1))
        ;;
    esac
  done
done <<<"$repos"

echo "reconcile: $fixed issue(s)"
