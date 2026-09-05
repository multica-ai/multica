#!/usr/bin/env bash
# Remote media machine: pull agent-safe issues from GitHub and dispatch via Hermes.
# Install into content repo via install-content-harness.sh; run from cron on Hermes host.
set -euo pipefail

MAX_TASKS="${1:-1}"
DRY_RUN=0
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

while [ $# -gt 0 ]; do
  case "$1" in
    --max-tasks) MAX_TASKS="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help)
      cat <<'EOF'
Usage: pull-dispatch.sh [--max-tasks N] [--dry-run]

Picks open agent-safe issues (no agent-running/blocked/done) and runs dispatch-hermes-cli.sh.
Run on the Hermes media machine from the content repo root (cron-friendly).
EOF
      exit 0
      ;;
    *)
      if [[ "$1" =~ ^[0-9]+$ ]]; then
        MAX_TASKS="$1"
        shift
      else
        echo "Unknown option: $1" >&2
        exit 1
      fi
      ;;
  esac
done

REPO="$(gh repo view "$REPO_ROOT" --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
if [ -z "$REPO" ]; then
  echo "error: cannot resolve repo from $REPO_ROOT" >&2
  exit 1
fi

mapfile -t ISSUES < <(
  gh issue list -R "$REPO" \
    --label "agent-safe" \
    --state open \
    --json number,labels \
    --jq '.[] | select([.labels[].name] | (index("agent-running") | not) and (index("agent-blocked") | not) and (index("agent-done") | not)) | .number' \
    | head -n "$MAX_TASKS"
)

if [ "${#ISSUES[@]}" -eq 0 ]; then
  echo "No eligible content issues in $REPO"
  exit 0
fi

dispatched=0
for n in "${ISSUES[@]}"; do
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "would dispatch $REPO#$n"
    continue
  fi
  if GITHUB_REPOSITORY="$REPO" REPO_ROOT="$REPO_ROOT" \
    bash "$(dirname "$0")/dispatch-hermes-cli.sh" "$n"; then
    dispatched=$((dispatched + 1))
  else
    echo "warning: dispatch failed for $REPO#$n" >&2
  fi
done

echo "Dispatched $dispatched issue(s) from $REPO"
