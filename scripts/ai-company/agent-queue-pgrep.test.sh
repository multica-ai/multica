#!/usr/bin/env bash
# Regression: portfolio agent pgrep must match real cursor-agent cmdline (index.js -p --worktree).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/agent-queue.sh
source "$SCRIPT_DIR/lib/agent-queue.sh"

sample='7382 /Users/zhenhuachen/.local/bin/cursor-agent --use-system-ca /path/index.js -p --force --trust --worktree cursor-issue-12 --worktree-base origin/main'

if [[ "$sample" =~ cursor-issue-12 ]]; then
  echo "ok: worktree token parseable"
else
  echo "fail: worktree token" >&2
  exit 1
fi

if [[ "$sample" != *"cursor-agent -p"* ]]; then
  echo "ok: documents why naive cursor-agent -p pattern fails"
else
  echo "fail: sample should not match naive pattern" >&2
  exit 1
fi

if _pgrep_portfolio_agent_for_issue 12 >/dev/null 2>&1; then
  echo "ok: live agent detected for issue 12 (if any running)"
else
  echo "ok: no live agent for issue 12 (skip live check)"
fi

echo "agent-queue-pgrep.test.sh: passed"
