#!/usr/bin/env bash
# Build Cloud Agent prompt text from a GitHub issue JSON (gh issue view --json).
set -euo pipefail

ISSUE_JSON="${1:?usage: build-prompt.sh <issue.json>}"
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"

title="$(jq -r '.title' "$ISSUE_JSON")"
body="$(jq -r '.body // ""' "$ISSUE_JSON")"
url="$(jq -r '.url' "$ISSUE_JSON")"
number="$(jq -r '.number' "$ISSUE_JSON")"

kickoff="$REPO_ROOT/.delivery/prompts/orchestrator-kickoff.md"

sed \
  -e "s|<GITHUB_ISSUE_URL>|$url|g" \
  -e "s|.delivery/<slug>/|Issue #$number|g" \
  "$kickoff"

cat <<EOF

---

## GitHub Issue #$number: $title

$url

### Issue body

$body
EOF
