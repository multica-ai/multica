#!/usr/bin/env bash
# Dispatch one issue via local cursor-agent CLI (session auth).
set -euo pipefail

REPO="${GITHUB_REPOSITORY:-}"
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
MULTICA_ROOT="${MULTICA_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
# PATH + local.env for cron/nohup (machine credential + ~/.local/bin).
if [ -f "$MULTICA_ROOT/scripts/ai-company/lib/source-local-env.sh" ]; then
  # shellcheck disable=SC1090
  source "$MULTICA_ROOT/scripts/ai-company/lib/source-local-env.sh"
elif [ -f "$MULTICA_ROOT/.ai-company/config/local.env" ]; then
  # shellcheck disable=SC1090
  source "$MULTICA_ROOT/.ai-company/config/local.env"
fi
if [ -f "$MULTICA_ROOT/scripts/ai-company/lib/agent-queue.sh" ]; then
  # shellcheck disable=SC1090
  source "$MULTICA_ROOT/scripts/ai-company/lib/agent-queue.sh"
fi
CURSOR_AGENT_BIN="${CURSOR_AGENT_BIN:-cursor-agent}"
WORKTREE_BASE="${WORKTREE_BASE:-main}"
LOG_DIR="${LOG_DIR:-$REPO_ROOT/.delivery/.agent-runs}"
DRY_RUN=0
ISSUE_NUMBER=""

usage() {
  cat <<'EOF'
Usage: dispatch-cursor-agent-cli.sh <issue_number> [--dry-run]

Runs cursor-agent locally against REPO_ROOT using CLI session auth.
Requires: gh, jq, cursor-agent logged in (`cursor-agent status`).

Environment:
  GITHUB_REPOSITORY   owner/name (default: infer from gh)
  REPO_ROOT           Product repo path (default: parent of scripts/agent-delivery)
  CURSOR_AGENT_BIN        Binary (default: cursor-agent)
  WORKTREE_BASE       Base ref for --worktree (default: main)
  LOG_DIR             Where to write run logs
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

WORKTREE_NAME="${WORKTREE_NAME:-cursor-issue-${ISSUE_NUMBER}}"
USE_WORKTREE="${USE_WORKTREE:-1}"

mkdir -p "$LOG_DIR"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_FILE="$LOG_DIR/issue-${ISSUE_NUMBER}-${TS}.log"
{
  echo "=== dispatch-cursor-agent-cli.sh ==="
  echo "started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "issue=#${ISSUE_NUMBER} root=${REPO_ROOT}"
} >>"$LOG_FILE"

if [ -z "$REPO" ]; then
  REPO="$(gh repo view "$REPO_ROOT" --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [ -z "$REPO" ]; then
  echo "error: set GITHUB_REPOSITORY or run inside a gh-linked repo" >&2
  echo "error: could not resolve GITHUB_REPOSITORY" >>"$LOG_FILE"
  exit 1
fi
echo "repo=$REPO" >>"$LOG_FILE"

if ! command -v "$CURSOR_AGENT_BIN" &>/dev/null; then
  echo "error: $CURSOR_AGENT_BIN not found on PATH" >&2
  echo "error: $CURSOR_AGENT_BIN not found" >>"$LOG_FILE"
  exit 1
fi

if ! "$CURSOR_AGENT_BIN" status &>/dev/null; then
  echo "error: cursor-agent not logged in — run: cursor-agent login" >&2
  echo "error: cursor-agent not logged in" >>"$LOG_FILE"
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export GH_REPO="$REPO"
gh issue view "$ISSUE_NUMBER" --json title,body,url,number >"$TMP/issue.json"
PROMPT="$TMP/prompt.txt"
BUILD_PROMPT="$REPO_ROOT/scripts/agent-delivery/build-prompt.sh"
if [ ! -f "$BUILD_PROMPT" ]; then
  BUILD_PROMPT="$(dirname "$0")/build-prompt.sh"
fi
REPO_ROOT="$REPO_ROOT" bash "$BUILD_PROMPT" "$TMP/issue.json" >"$PROMPT"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "repo=$REPO root=$REPO_ROOT worktree=$WORKTREE_NAME base=$WORKTREE_BASE"
  echo "log=$LOG_FILE"
  head -20 "$PROMPT"
  exit 0
fi

gh issue edit "$ISSUE_NUMBER" --add-label "agent-running" --remove-label "agent-blocked" 2>/dev/null || true

COMMENT="🤖 Local cursor-agent dispatched for this issue.

- Mode: CLI (\`cursor-agent\` session)
- Worktree: \`${WORKTREE_NAME}\` (base \`${WORKTREE_BASE}\`)
- Log: \`${LOG_FILE}\`

Verifier must pass acceptance commands before merge."

gh issue comment "$ISSUE_NUMBER" --body "$COMMENT"

echo "Dispatching issue #$ISSUE_NUMBER in $REPO_ROOT (log: $LOG_FILE)"

LOCK_FILE="$LOG_DIR/.dispatch-issue-${ISSUE_NUMBER}.lock"
if [ -f "$LOCK_FILE" ] && ! _dispatch_lock_expired "$LOCK_FILE" 2>/dev/null; then
  stale_pid="$(sed -n '1p' "$LOCK_FILE" 2>/dev/null || true)"
  echo "error: issue #$ISSUE_NUMBER dispatch already running (pid ${stale_pid:-unknown})" >&2
  exit 1
fi
rm -f "$LOCK_FILE"
write_dispatch_lock "$REPO_ROOT" "$ISSUE_NUMBER" "$$"
trap 'rm -f "$LOCK_FILE"' EXIT

# Resolve base ref: explicit WORKTREE_BASE, else origin/HEAD, else main/master.
WORKTREE_BASE_REF="$WORKTREE_BASE"
if git -C "$REPO_ROOT" rev-parse --is-inside-work-tree &>/dev/null; then
  if [ "$WORKTREE_BASE" = "main" ] || [ -z "${WORKTREE_BASE_SET:-}" ]; then
    default_remote="$(git -C "$REPO_ROOT" symbolic-ref -q --short refs/remotes/origin/HEAD 2>/dev/null || true)"
    if [ -n "$default_remote" ]; then
      WORKTREE_BASE="${default_remote#origin/}"
    elif git -C "$REPO_ROOT" show-ref --verify --quiet refs/remotes/origin/master; then
      WORKTREE_BASE="master"
    elif git -C "$REPO_ROOT" show-ref --verify --quiet refs/remotes/origin/main; then
      WORKTREE_BASE="main"
    fi
  fi
  if ! git -C "$REPO_ROOT" show-ref --verify --quiet "refs/remotes/origin/$WORKTREE_BASE"; then
    # Best-effort refresh; don't hang the dispatcher on flaky network.
    GIT_TERMINAL_PROMPT=0 git -C "$REPO_ROOT" -c http.lowSpeedLimit=1000 -c http.lowSpeedTime=15 \
      fetch origin "$WORKTREE_BASE" --quiet 2>/dev/null || true
  fi
  if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/remotes/origin/$WORKTREE_BASE"; then
    WORKTREE_BASE_REF="origin/$WORKTREE_BASE"
  elif git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/$WORKTREE_BASE"; then
    WORKTREE_BASE_REF="$WORKTREE_BASE"
  else
    echo "error: cannot resolve worktree base '$WORKTREE_BASE' in $REPO_ROOT" >&2
    gh issue edit "$ISSUE_NUMBER" --remove-label "agent-running" --add-label "agent-blocked" 2>/dev/null || true
    exit 1
  fi
fi
echo "Using worktree base: $WORKTREE_BASE_REF"

set +e
(
  cd "$REPO_ROOT"
  # Keep prompt outside TMP so EXIT trap cannot race stdin; write log directly.
  PROMPT_KEEP="$LOG_DIR/issue-${ISSUE_NUMBER}-${TS}.prompt.txt"
  cp "$PROMPT" "$PROMPT_KEEP"
  if [ "$USE_WORKTREE" = "1" ]; then
    echo "agent: starting cursor-agent (worktree=$WORKTREE_NAME)" >>"$LOG_FILE"
    nohup "$CURSOR_AGENT_BIN" -p --force --trust \
      --worktree "$WORKTREE_NAME" \
      --worktree-base "$WORKTREE_BASE_REF" \
      --output-format text \
      <"$PROMPT_KEEP" >>"$LOG_FILE" 2>&1 &
    agent_pid=$!
    echo "agent: pid=$agent_pid" >>"$LOG_FILE"
    if declare -f record_dispatch_agent_pid >/dev/null 2>&1; then
      record_dispatch_agent_pid "$REPO_ROOT" "$ISSUE_NUMBER" "$agent_pid"
    fi
    if ! wait "$agent_pid"; then
      echo "agent: exited $?" >>"$LOG_FILE"
      exit 1
    fi
  else
    echo "agent: starting cursor-agent (branch=$WORKTREE_NAME)" >>"$LOG_FILE"
    git checkout -B "$WORKTREE_NAME" "$WORKTREE_BASE" 2>/dev/null || git checkout "$WORKTREE_NAME" 2>/dev/null
    nohup "$CURSOR_AGENT_BIN" -p --force --trust \
      --output-format text \
      <"$PROMPT_KEEP" >>"$LOG_FILE" 2>&1 &
    agent_pid=$!
    echo "agent: pid=$agent_pid" >>"$LOG_FILE"
    if declare -f record_dispatch_agent_pid >/dev/null 2>&1; then
      record_dispatch_agent_pid "$REPO_ROOT" "$ISSUE_NUMBER" "$agent_pid"
    fi
    if ! wait "$agent_pid"; then
      echo "agent: exited $?" >>"$LOG_FILE"
      exit 1
    fi
  fi
  echo "agent: exited 0" >>"$LOG_FILE"
)
exit_code=$?
if [ "$exit_code" -eq 0 ] && grep -qE 'Authentication required|Please run .agent login|CURSOR_API_KEY' "$LOG_FILE" 2>/dev/null; then
  echo "agent: auth failure detected in log — treating as failure" >>"$LOG_FILE"
  exit_code=1
fi
set -e

if [ "$exit_code" -eq 0 ]; then
  gh issue edit "$ISSUE_NUMBER" --remove-label "agent-running" --add-label "agent-done" 2>/dev/null || true
  FINALIZE_NOTE=""
  if [ "${AUTO_FINALIZE_MAIN:-0}" = "1" ]; then
    if bash "$(dirname "$0")/finalize-to-main.sh" --issue "$ISSUE_NUMBER"; then
      FINALIZE_NOTE=$'\n\nMerged to `main` and primary checkout is on `main`.'
    else
      FINALIZE_NOTE=$'\n\n⚠️ finalize-to-main failed — merge manually, then `git checkout main`.'
    fi
  fi
  gh issue comment "$ISSUE_NUMBER" --body "$(cat <<EOF
✅ Local cursor-agent finished (exit 0). Check worktree \`${WORKTREE_NAME}\` and open PR if not auto-created.

Next: merge into \`main\`, then return to \`main\`:
\`\`\`bash
bash scripts/agent-delivery/finalize-to-main.sh --issue ${ISSUE_NUMBER}
\`\`\`
Or set \`AUTO_FINALIZE_MAIN=1\` on dispatch to run this automatically.${FINALIZE_NOTE}
EOF
)"
else
  blocked_note=""
  if grep -qE 'Authentication required|Please run .agent login|CURSOR_API_KEY' "$LOG_FILE" 2>/dev/null; then
    blocked_note=" Auth failure — run \`cursor-agent login\` or set CURSOR_API_KEY for cron."
  fi
  gh issue edit "$ISSUE_NUMBER" --remove-label "agent-running" --add-label "agent-blocked" 2>/dev/null || true
  gh issue comment "$ISSUE_NUMBER" --body "$(cat <<EOF
❌ Local cursor-agent failed (exit $exit_code). See log: \`${LOG_FILE}\`${blocked_note}
EOF
)"
  exit "$exit_code"
fi
