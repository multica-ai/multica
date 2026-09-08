#!/usr/bin/env bash
# Dispatch one content issue via local Hermes CLI (remote media machine).
set -euo pipefail

REPO="${GITHUB_REPOSITORY:-}"
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
HERMES_BIN="${HERMES_BIN:-hermes}"
WORKTREE_BASE="${WORKTREE_BASE:-main}"
LOG_DIR="${LOG_DIR:-$REPO_ROOT/.delivery/.agent-runs}"
DRY_RUN=0
ISSUE_NUMBER=""
USE_WORKTREE="${USE_WORKTREE:-1}"

usage() {
  cat <<'EOF'
Usage: dispatch-hermes-cli.sh <issue_number> [--dry-run]

Runs Hermes locally against REPO_ROOT for a content-repo issue.
Requires: gh, jq, hermes logged in (`hermes setup` / portal).

Environment:
  GITHUB_REPOSITORY   owner/name
  REPO_ROOT           Content repo path
  HERMES_BIN          Binary (default: hermes)
  WORKTREE_BASE       Base ref for -w worktree (default: main)
  LOG_DIR             Run logs
  USE_WORKTREE        1|0 (default: 1) — hermes -w isolated worktree
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --dry-run) DRY_RUN=1; shift ;;
    *)
      if [ -z "$ISSUE_NUMBER" ]; then
        ISSUE_NUMBER="$1"
        shift
      else
        echo "Unknown argument: $1" >&2
        exit 1
      fi
      ;;
  esac
done

if [ -z "$ISSUE_NUMBER" ]; then
  usage >&2
  exit 1
fi

mkdir -p "$LOG_DIR"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_FILE="$LOG_DIR/issue-${ISSUE_NUMBER}-${TS}.log"
{
  echo "=== dispatch-hermes-cli.sh ==="
  echo "started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "issue=#${ISSUE_NUMBER} root=${REPO_ROOT}"
} >>"$LOG_FILE"

if [ -z "$REPO" ]; then
  REPO="$(gh repo view "$REPO_ROOT" --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [ -z "$REPO" ]; then
  echo "error: set GITHUB_REPOSITORY or run inside a gh-linked repo" >&2
  exit 1
fi

if ! command -v "$HERMES_BIN" &>/dev/null; then
  echo "error: $HERMES_BIN not found on PATH" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export GH_REPO="$REPO"
gh issue view "$ISSUE_NUMBER" --json title,body,url,number >"$TMP/issue.json"
PROMPT="$TMP/prompt.txt"
bash "$(dirname "$0")/build-prompt.sh" "$TMP/issue.json" >"$PROMPT"
PROMPT_KEEP="$LOG_DIR/issue-${ISSUE_NUMBER}-${TS}.prompt.txt"
cp "$PROMPT" "$PROMPT_KEEP"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "repo=$REPO root=$REPO_ROOT worktree=$USE_WORKTREE base=$WORKTREE_BASE"
  echo "log=$LOG_FILE prompt=$PROMPT_KEEP"
  head -30 "$PROMPT"
  exit 0
fi

gh issue edit "$ISSUE_NUMBER" --add-label "agent-running" --remove-label "agent-blocked" 2>/dev/null || true

gh issue comment "$ISSUE_NUMBER" --body "$(cat <<EOF
📝 Hermes content dispatch started.

- Mode: local Hermes CLI
- Worktree: \`$([ "$USE_WORKTREE" = "1" ] && echo "enabled (-w)" || echo "disabled")\`
- Log: \`${LOG_FILE}\`

Publishing to social platforms requires CEO approval unless issue has \`publish-ok\`.
EOF
)"

echo "Dispatching content issue #$ISSUE_NUMBER in $REPO_ROOT (log: $LOG_FILE)"

if git -C "$REPO_ROOT" rev-parse --is-inside-work-tree &>/dev/null; then
  git -C "$REPO_ROOT" fetch origin --quiet 2>/dev/null || true
fi

set +e
(
  cd "$REPO_ROOT"
  if [ "$USE_WORKTREE" = "1" ]; then
    "$HERMES_BIN" -w chat --query-file "$PROMPT_KEEP" --oneshot \
      >"$LOG_FILE" 2>&1
  else
    "$HERMES_BIN" chat --query-file "$PROMPT_KEEP" --oneshot \
      >"$LOG_FILE" 2>&1
  fi
)
exit_code=$?
set -e

if [ "$exit_code" -eq 0 ]; then
  gh issue edit "$ISSUE_NUMBER" --remove-label "agent-running" --add-label "agent-done" 2>/dev/null || true
  gh issue comment "$ISSUE_NUMBER" --body "$(cat <<EOF
✅ Hermes finished (exit 0). Review branch/PR and \`drafts/\` changes before publish.
EOF
)"
else
  gh issue edit "$ISSUE_NUMBER" --remove-label "agent-running" --add-label "agent-blocked" 2>/dev/null || true
  gh issue comment "$ISSUE_NUMBER" --body "$(cat <<EOF
❌ Hermes failed (exit $exit_code). See log: \`${LOG_FILE}\`
EOF
)"
  exit "$exit_code"
fi
