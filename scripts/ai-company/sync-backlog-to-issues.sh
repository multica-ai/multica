#!/usr/bin/env bash
# Parse .ai-company/examples/<slug>/backlog.md and create GitHub Issues.
# Requires: gh CLI authenticated, agent-* labels created on the repo.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

usage() {
  cat <<'EOF'
Usage: sync-backlog-to-issues.sh [options]

Options:
  --backlog PATH     Path to backlog.md (required)
  --repo OWNER/NAME  GitHub repo (default: GITHUB_REPOSITORY or git remote)
  --dry-run          Print gh commands without running
  --from TICKET-NNN  Start at ticket id (inclusive)
  --to TICKET-NNN    End at ticket id (inclusive)
  -h, --help

Example:
  sync-backlog-to-issues.sh \
    --backlog .ai-company/examples/music-game-sea/backlog.md \
    --repo your-org/music-game-sea \
    --dry-run
EOF
}

BACKLOG=""
REPO="${GITHUB_REPOSITORY:-}"
DRY_RUN=0
FROM_ID=""
TO_ID=""
SKIP_EXISTING=0

while [ $# -gt 0 ]; do
  case "$1" in
    --backlog) BACKLOG="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --skip-existing) SKIP_EXISTING=1; shift ;;
    --from) FROM_ID="${2:?}"; shift 2 ;;
    --to) TO_ID="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

if [ -z "$BACKLOG" ] || [ ! -f "$BACKLOG" ]; then
  echo "error: --backlog must point to an existing backlog.md" >&2
  exit 1
fi

if [ -z "$REPO" ]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [ -z "$REPO" ]; then
  echo "error: set --repo or GITHUB_REPOSITORY or run inside a gh-linked repo" >&2
  exit 1
fi

ticket_num() {
  echo "$1" | sed -n 's/^TICKET-[A-Z]*\([0-9]*\).*/\1/p'
}

in_range() {
  local id="$1"
  local n from_n to_n
  n="$(ticket_num "$id")"
  [[ "$n" =~ ^[0-9]+$ ]] || return 0
  if [ -n "$FROM_ID" ]; then
    from_n="$(ticket_num "$FROM_ID")"
    [[ "$from_n" =~ ^[0-9]+$ ]] || return 0
    [ "$n" -ge "$from_n" ] || return 1
  fi
  if [ -n "$TO_ID" ]; then
    to_n="$(ticket_num "$TO_ID")"
    [[ "$to_n" =~ ^[0-9]+$ ]] || return 0
    [ "$n" -le "$to_n" ] || return 1
  fi
  return 0
}

labels_for_grade() {
  case "$1" in
    agent-safe) echo "agent-safe" ;;
    agent-assisted) echo "agent-assisted" ;;
    human-only) echo "human-only" ;;
    *) echo "" ;;
  esac
}

create_issue() {
  local ticket_id="$1"
  local grade="$2"
  local title="$3"
  local body="$4"
  local labels
  labels="$(labels_for_grade "$grade")"

  if [ "$SKIP_EXISTING" -eq 1 ]; then
    local exists
    exists="$(
      gh issue list -R "$REPO" --search "[${ticket_id}] in:title" --state all \
        --json number -q 'length' 2>/dev/null || echo 0
    )"
    if [ "${exists:-0}" -gt 0 ]; then
      echo "skip existing: $ticket_id ($REPO)"
      return 0
    fi
  fi

  local gh_labels=()
  if [ -n "$labels" ]; then
    gh_labels=(--label "$labels")
  fi

  local full_title="[$ticket_id] $title"
  local full_body
  full_body="$(cat <<EOF
## $ticket_id

**Grade:** \`$grade\`

$body

---
_Auto-created from backlog.md. Delivery truth: \`.delivery/<slug>/\`_
EOF
)"

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "---"
    echo "gh issue create -R $REPO --title $(printf %q "$full_title") ${gh_labels[@]+"${gh_labels[@]}"} --body <markdown>"
    echo "$full_body"
    return 0
  fi

  gh issue create -R "$REPO" --title "$full_title" ${gh_labels[@]+"${gh_labels[@]}"} --body "$full_body"
}

# Parse backlog: ticket header + bullet body until next ### or ##
current_id=""
current_grade=""
current_title=""
current_body=""
flush() {
  if [ -z "$current_id" ]; then
    return 0
  fi
  if ! in_range "$current_id"; then
    return 0
  fi
  create_issue "$current_id" "$current_grade" "$current_title" "$current_body"
  current_id=""
  current_grade=""
  current_title=""
  current_body=""
}

set +u
while IFS= read -r line || [ -n "$line" ]; do
  if [[ "$line" =~ ^###\ (TICKET-[A-Z]*[0-9]+|PAY-[0-9]+|AUTH-[0-9]+|MIG-[0-9]+)\ \[(agent-safe|agent-assisted|human-only)\]\ (.+)$ ]]; then
    # flush() may run gh/sed and clear BASH_REMATCH — capture first.
    next_id="${BASH_REMATCH[1]}"
    next_grade="${BASH_REMATCH[2]}"
    next_title="${BASH_REMATCH[3]}"
    flush
    current_id="$next_id"
    current_grade="$next_grade"
    current_title="$next_title"
    current_body=""
    continue
  fi
  if [[ "$line" =~ ^##\  ]]; then
    flush
    continue
  fi
  if [ -n "$current_id" ]; then
    current_body+="$line"$'\n'
  fi
done <"$BACKLOG"
set -u
flush

echo "Done. Repo: $REPO"
