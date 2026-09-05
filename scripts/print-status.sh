#!/usr/bin/env bash
# Multica harness status — SESSION + Tier 0/1 file health
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "=== Multica print-status ==="
echo "root: $ROOT"
echo ""

if [[ -f SESSION.md ]]; then
  echo "--- SESSION.md ---"
  grep -E '^\- \*\*(phase|updated|blockers|next)\*\*' -A 6 SESSION.md | head -40 || true
  echo ""
fi

if [[ -f .secondbrain ]]; then
  echo "--- .secondbrain ---"
  head -2 .secondbrain
  echo ""
fi

echo "--- git ---"
git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "no git"
git log -1 --oneline 2>/dev/null || true
echo ""

echo "--- harness files ---"
check() {
  if [[ -e "$1" ]]; then
    echo "[OK] $1"
  else
    echo "[--] $1"
  fi
}
check ".secondbrain"
check "SESSION.md"
check "docs/VAULT-HARNESS.md"
check "docs/KNOWLEDGE-BASE.md"
check "docs/CODE-INDEX.md"
check "docs/SECOND-BRAIN-HERMES-COLLAB.md"
check ".cursor/rules/vault-harness.mdc"
check ".cursor/rules/code-index.mdc"
check "AGENTS.md"
check "CLAUDE.md"
echo ""
echo "Detail: SESSION.md · Vault projects/multica.md"
