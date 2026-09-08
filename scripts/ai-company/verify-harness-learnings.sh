#!/usr/bin/env bash
# Verify harness learnings loop (routing doc, queue, scripts, manifest, Tier-0 pointers).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANIFEST="${MANIFEST:-$MULTICA_ROOT/.ai-company/config/company-os-sync-manifest.yaml}"
QUIET=0

usage() {
  cat <<'EOF'
Usage: verify-harness-learnings.sh [options]

Checks harness experience feedback loop wiring (HQ).

Options:
  --quiet   Only print failures
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

ok=0
fail=0

pass() { [ "$QUIET" -eq 1 ] || echo "  ✅ $1"; ok=$((ok + 1)); }
bad() { echo "  ❌ $1"; fail=$((fail + 1)); }

echo "Harness learnings loop — $(date '+%Y-%m-%d %H:%M')"
echo ""

routing="$MULTICA_ROOT/.ai-company/docs/31-harness-learnings-routing.md"
queue="$MULTICA_ROOT/.ai-company/docs/system-evolution/harness-candidates.md"
record="$SCRIPT_DIR/record-harness-learning.sh"
hq_mdc="$MULTICA_ROOT/.cursor/rules/company-harness.mdc"
scaffold_mdc="$MULTICA_ROOT/.ai-company/harness/scaffold/.cursor/rules/company-harness.mdc"

[ -f "$routing" ] && pass "31-harness-learnings-routing.md" || bad "missing $routing"
[ -f "$queue" ] && pass "harness-candidates.md queue" || bad "missing $queue"
[ -x "$record" ] && pass "record-harness-learning.sh executable" || bad "missing or not executable: $record"

if grep -q "31-harness-learnings-routing" "$MANIFEST" 2>/dev/null; then
  pass "doc 31 in company-os-sync-manifest.yaml"
else
  bad "docs/31-harness-learnings-routing.md not in manifest"
fi

if [ -f "$hq_mdc" ] && grep -q "31-harness-learnings-routing\|记录 harness 经验" "$hq_mdc"; then
  pass "HQ company-harness.mdc points to learnings routing"
else
  bad "HQ company-harness.mdc missing learnings pointer"
fi

if [ -f "$scaffold_mdc" ] && grep -q "31-harness-learnings-routing\|record-harness-learning" "$scaffold_mdc"; then
  pass "scaffold company-harness.mdc points to learnings routing"
else
  bad "scaffold company-harness.mdc missing learnings pointer"
fi

if bash "$record" --content "verify-harness-learnings smoke" --dry-run >/dev/null 2>&1; then
  pass "record-harness-learning.sh --dry-run"
else
  bad "record-harness-learning.sh --dry-run failed"
fi

if bash "$record" --content "bad ghp_abcdefghijklmnopqrstuvwxyz1234567890" --dry-run >/dev/null 2>&1; then
  bad "record-harness-learning.sh should reject secret-like content"
else
  pass "record-harness-learning.sh rejects secrets"
fi

if grep -q "harness-candidates\|31-harness" "$MULTICA_ROOT/.ai-company/docs/system-evolution/README.md" 2>/dev/null; then
  pass "system-evolution README wired"
else
  bad "system-evolution README missing harness-candidates section"
fi

echo ""
echo "────────────────────────────"
printf "结果: %s 通过 · %s 失败\n" "$ok" "$fail"
[ "$fail" -eq 0 ] || exit 1
echo "Harness learnings loop OK"
