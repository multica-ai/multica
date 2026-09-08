#!/usr/bin/env bash
# Ensure GitHub labels for AI company agent delivery exist on a repo.
set -euo pipefail

REPO="${1:-}"
if [ -z "$REPO" ]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [ -z "$REPO" ]; then
  echo "usage: ensure-github-labels.sh [OWNER/NAME]" >&2
  exit 1
fi

declare -a SPECS=(
  "agent-safe|0E8A16|Autonomous queue eligible"
  "agent-assisted|FBCA04|Agent PR, CEO must merge"
  "human-only|D93F0B|Never auto-dispatch"
  "agent-running|1D76DB|Agent currently working"
  "agent-blocked|B60205|Needs CEO clarification"
  "agent-done|5319E7|PR open and CI green"
)

for spec in "${SPECS[@]}"; do
  name="${spec%%|*}"
  rest="${spec#*|}"
  color="${rest%%|*}"
  desc="${rest#*|}"
  if gh label list -R "$REPO" --json name -q ".[] | select(.name==\"$name\") | .name" 2>/dev/null | grep -q "^${name}$"; then
    echo "label exists: $name"
  else
    gh label create "$name" -R "$REPO" --color "$color" --description "$desc"
    echo "created: $name"
  fi
done

echo "Labels ready on $REPO"
