#!/usr/bin/env bash
# Build Hermes prompt text from a GitHub issue JSON (gh issue view --json).
set -euo pipefail

ISSUE_JSON="${1:?usage: build-prompt.sh <issue.json>}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

title="$(jq -r '.title' "$ISSUE_JSON")"
body="$(jq -r '.body // ""' "$ISSUE_JSON")"
url="$(jq -r '.url' "$ISSUE_JSON")"
number="$(jq -r '.number' "$ISSUE_JSON")"

kickoff="$REPO_ROOT/.delivery/prompts/orchestrator-kickoff.md"
if [ ! -f "$kickoff" ]; then
  echo "error: missing $kickoff — run install-content-harness.sh" >&2
  exit 1
fi

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

### Content delivery rules

- Read \`.delivery/CONTENT-HQ-SPLIT.md\` — you are the **remote worker**; CEO HQ owns queue and publish.
- Work in this repository only. Commit to a branch named \`content/issue-${number}\`.
- Deliverables live under \`drafts/\`, \`calendar/\`, or paths named in the issue — not ad-hoc chat output only.
- Do **not** publish to social platforms or call posting APIs unless the issue explicitly says \`publish-ok\`.
- When done: open a PR (or push branch) and summarize files changed in a short comment on the issue.
- If blocked: comment \`BLOCKED: <reason>\` on the issue and stop.
EOF
