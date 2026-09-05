#!/usr/bin/env bash
# Weekly harness review: metrics + open candidates + optional verify (doc 32 §5.2 P1).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
QUEUE="${QUEUE:-$MULTICA_ROOT/.ai-company/docs/system-evolution/harness-candidates.md}"
WRITE_METRICS=1
VERIFY=1

usage() {
  cat <<'EOF'
Usage: ceo-weekly-harness-review.sh [options]

Monday ritual: portfolio 派/绿/堵 + harness-candidates 待升格 + verify-opc-design.

Options:
  --no-metrics       Skip ceo-weekly-metrics --write
  --no-verify        Skip verify-opc-design.sh
  --queue PATH       harness-candidates.md
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --no-metrics) WRITE_METRICS=0; shift ;;
    --no-verify) VERIFY=0; shift ;;
    --queue) QUEUE="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

echo "=== CEO weekly harness review — $(date '+%Y-%m-%d %H:%M') ==="
echo ""

if [ "$WRITE_METRICS" -eq 1 ]; then
  echo "## 1. Portfolio metrics"
  bash "$SCRIPT_DIR/ceo-weekly-metrics.sh" --write || true
  echo ""
fi

echo "## 2. Harness candidates (open — promote ≤3 to target docs)"
if [ -f "$QUEUE" ]; then
  python3 - "$QUEUE" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
lines = path.read_text(encoding="utf-8").splitlines()
open_rows = []
for line in lines:
    if not line.startswith("| open |"):
        continue
    open_rows.append(line)
if not open_rows:
    print("  (no open candidates)")
else:
    for row in open_rows:
        print(" ", row)
    print()
    print(f"  Total open: {len(open_rows)} — PATCH target docs then mark promoted in {path.name}")
PY
else
  echo "  missing queue: $QUEUE"
fi
echo ""

echo "## 3. Design verification"
if [ "$VERIFY" -eq 1 ]; then
  bash "$SCRIPT_DIR/verify-opc-design.sh" --skip-hands-off || true
else
  echo "  skipped"
fi
echo ""
echo "Next: edit harness-candidates status · sync-company-norms · portfolio-commit-norms"
