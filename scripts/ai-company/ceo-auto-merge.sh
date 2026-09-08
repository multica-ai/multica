#!/usr/bin/env bash
# Merge open agent PRs when CI is green (optional nightly step for hands-off ops).
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
MAX_MERGE="${CEO_AUTO_MERGE_MAX:-5}"

usage() {
  cat <<'EOF'
Usage: ceo-auto-merge.sh [options]

Squash-merge open PRs whose checks succeeded. Skips draft PRs and PRs with
pending/failed checks. Enable in nightly via CEO_AUTO_MERGE=1 in local.env.

Options:
  --registry PATH
  --org ORG
  --max N           Max merges this run (default: 5)
  --dry-run
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --max) MAX_MERGE="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if ! command -v gh &>/dev/null; then
  echo "error: gh CLI required" >&2
  exit 1
fi

repos="$(
  python3 - "$REGISTRY" "$GITHUB_ORG" <<'PY'
import sys
from pathlib import Path

registry = Path(sys.argv[1])
org = sys.argv[2]
current = None
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
        if not paused:
            print(repo)
        current = None
PY
)"

merged=0
while IFS= read -r repo; do
  [ -n "$repo" ] || continue
  [ "$merged" -ge "$MAX_MERGE" ] && break
    numbers="$(
    gh pr list -R "$repo" -s open --json number,isDraft,mergeable,statusCheckRollup \
      --jq '.[] | select(.isDraft == false) | select(.mergeable == "MERGEABLE") | select(
        (.statusCheckRollup | length) == 0
        or ([.statusCheckRollup[]? | select(.status != "COMPLETED" or (.conclusion == "FAILURE" or .conclusion == "TIMED_OUT" or .conclusion == "ACTION_REQUIRED"))] | length == 0)
      ) | .number' 2>/dev/null || true
  )"
  for num in $numbers; do
    [ "$merged" -ge "$MAX_MERGE" ] && break
    [ -z "$num" ] && continue
    head="$(gh pr view "$num" -R "$repo" --json headRefName -q .headRefName 2>/dev/null || echo "")"
    root="$(bash "$SCRIPT_DIR/resolve-repo-path.sh" --repo "$repo" --quiet 2>/dev/null || true)"
    policy=""
    branch_prefix="cursor-issue"
    if [ -n "$root" ] && [ -f "$root/.delivery/config/merge-policy.json" ]; then
      policy="$root/.delivery/config/merge-policy.json"
      branch_prefix="$(jq -r '.branchNamePrefix // "cursor-issue"' "$policy")"
    fi
    if [ -n "$policy" ]; then
      if [[ "$head" != ${branch_prefix}* ]]; then
        echo "skip non-agent: $repo#$num (head=$head prefix=$branch_prefix)"
        continue
      fi
      if ! GITHUB_REPOSITORY="$repo" REPO_ROOT="$root" \
        bash "$MULTICA_ROOT/scripts/agent-delivery/check-merge-eligible.sh" "$num" 2>/dev/null | grep -q '^merge_eligible=true'; then
        echo "skip policy: $repo#$num (not merge_eligible)"
        continue
      fi
    elif [[ "$head" != cursor/* ]] && [[ "$head" != cursor-issue* ]]; then
      echo "skip non-agent: $repo#$num (head=$head)"
      continue
    fi
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "would merge: $repo#$num"
    else
      echo ">> merge $repo#$num"
      gh pr merge "$num" -R "$repo" --squash --delete-branch || {
        echo "warn: merge failed $repo#$num" >&2
        continue
      }
      linked="$(
        gh pr view "$num" -R "$repo" \
          --json closingIssuesReferences -q '.closingIssuesReferences[].number' 2>/dev/null || true
      )"
      for issue_num in $linked; do
        [ -z "$issue_num" ] && continue
        strip_agent_labels "$repo" "$issue_num"
        echo "  cleaned labels on linked issue #$issue_num"
      done
    fi
    merged=$((merged + 1))
  done
done <<<"$repos"

echo "auto-merge: $merged PR(s)"
