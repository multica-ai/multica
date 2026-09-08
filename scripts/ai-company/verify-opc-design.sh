#!/usr/bin/env bash
# Verify OPC harness + knowledge design wiring (docs/32 section 7).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SKIP_HANDS_OFF=0
QUIET=0

usage() {
  cat <<'EOF'
Usage: verify-opc-design.sh [options]

Runs verification from .ai-company/docs/32-opc-harness-knowledge-design.md §7.

Options:
  --skip-hands-off   Skip verify-hands-off.sh (needs cron/feishu)
  --quiet            Only print failures
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-hands-off) SKIP_HANDS_OFF=1; shift ;;
    --quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

ok=0
warn=0
fail=0

pass() { [ "$QUIET" -eq 1 ] || echo "  ✅ $1"; ok=$((ok + 1)); }
note() { echo "  ⚠️  $1"; warn=$((warn + 1)); }
bad() { echo "  ❌ $1"; fail=$((fail + 1)); }

echo "OPC design verification (doc 32) — $(date '+%Y-%m-%d %H:%M')"
echo ""

design="$MULTICA_ROOT/.ai-company/docs/32-opc-harness-knowledge-design.md"
[ -f "$design" ] && pass "32-opc-harness-knowledge-design.md" || bad "missing design doc"

if bash "$SCRIPT_DIR/verify-harness-tier0.sh" ${QUIET:+--quiet} 2>/dev/null; then
  pass "verify-harness-tier0.sh"
else
  bad "verify-harness-tier0.sh"
fi

if bash "$SCRIPT_DIR/verify-harness-learnings.sh" ${QUIET:+--quiet} 2>/dev/null; then
  pass "verify-harness-learnings.sh"
else
  bad "verify-harness-learnings.sh"
fi

if bash "$SCRIPT_DIR/ceo-weekly-metrics.sh" --json --quiet >/dev/null 2>&1; then
  pass "ceo-weekly-metrics.sh --json"
else
  bad "ceo-weekly-metrics.sh"
fi

if [ "$SKIP_HANDS_OFF" -eq 0 ]; then
  if bash "$SCRIPT_DIR/verify-hands-off.sh" >/tmp/verify-opc-hands-off.log 2>&1; then
    pass "verify-hands-off.sh"
  else
    note "verify-hands-off.sh — see /tmp/verify-opc-hands-off.log"
  fi
else
  note "skipped verify-hands-off.sh"
fi

echo ""
echo "────────────────────────────"
printf "结果: %s 通过 · %s 警告 · %s 失败\n" "$ok" "$warn" "$fail"
[ "$fail" -eq 0 ] || exit 1
echo "OPC design wiring OK"
