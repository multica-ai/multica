#!/usr/bin/env bash
# Light content-repo checks before merge (brand + structure). Extend per project.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
FAIL=0

warn() { echo "WARN: $*" >&2; }
bad() { echo "FAIL: $*" >&2; FAIL=1; }
pass() { echo "OK: $*"; }

if [ ! -d "$REPO_ROOT/drafts" ]; then
  warn "no drafts/ directory yet (optional for early repos)"
else
  pass "drafts/ exists"
fi

if [ -f "$REPO_ROOT/brand/voice.md" ]; then
  pass "brand/voice.md present"
else
  warn "missing brand/voice.md — add before scaling content agents"
fi

for dir in drafts calendar; do
  if [ -d "$REPO_ROOT/$dir" ] && command -v rg >/dev/null 2>&1; then
    if rg -n 'sk-[a-zA-Z0-9]{20,}' "$REPO_ROOT/$dir" 2>/dev/null; then
      bad "possible API key in $dir/"
    fi
  fi
done

exit "$FAIL"
