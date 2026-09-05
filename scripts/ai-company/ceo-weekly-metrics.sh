#!/usr/bin/env bash
# Aggregate portfolio pulse metrics (派/绿/堵) for weekly system-evolution review.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
SINCE="${SINCE:-@yesterday}"
WRITE=0
OUT=""
QUIET=0
JSON=0

usage() {
  cat <<'EOF'
Usage: ceo-weekly-metrics.sh [options]

Summarize BLOCKED / RUNNING / QUEUE / MERGED across project-registry (硅谷 派/绿/堵).

Options:
  --registry PATH    project-registry.yaml
  --org ORG          GitHub org (default: chenzh)
  --since DATE       gh merged search window (default: @yesterday)
  --write            Append/update metrics in system-evolution/YYYY-MM-DD-weekly.md
  --out PATH         Write markdown metrics block to PATH
  --json             Single JSON object on stdout
  --quiet            Suppress markdown stdout
  -h, --help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --since) SINCE="${2:?}"; shift 2 ;;
    --write) WRITE=1; shift ;;
    --out) OUT="${2:?}"; shift 2 ;;
    --json) JSON=1; shift ;;
    --quiet) QUIET=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

export MULTICA_ROOT SINCE="$SINCE" GITHUB_ORG PORTFOLIO_DISPATCH_ASYNC="${PORTFOLIO_DISPATCH_ASYNC:-1}"

metrics="$(
  bash "$SCRIPT_DIR/ceo-dashboard.sh" --registry "$REGISTRY" --org "$GITHUB_ORG" --since "$SINCE" --json \
    | python3 -c "
import json, sys, os
from datetime import datetime, timezone
blocked=running=queue=merged=projects=paused=0
for line in sys.stdin:
    line=line.strip()
    if not line:
        continue
    row=json.loads(line)
    if str(row.get('paused')).lower()=='true':
        paused+=1
        continue
    if str(row.get('accessible')).lower()=='false':
        continue
    projects+=1
    blocked+=int(row.get('blocked') or 0)
    running+=int(row.get('running') or 0)
    queue+=int(row.get('agent_safe') or 0)
    merged+=int(row.get('merged_prs') or 0)
async_mode=os.environ.get('PORTFOLIO_DISPATCH_ASYNC','1')
print(json.dumps({
  'generated_at': datetime.now(timezone.utc).astimezone().isoformat(timespec='seconds'),
  'since': os.environ.get('SINCE','@yesterday'),
  'org': os.environ.get('GITHUB_ORG','chenzh'),
  'projects_active': projects,
  'projects_paused': paused,
  'blocked': blocked,
  'running': running,
  'queue_agent_safe': queue,
  'merged_prs': merged,
  'portfolio_dispatch_mode': 'local-cli-async' if async_mode=='1' else 'local-cli-sync',
  'cursor_usd_per_pr': None,
}, ensure_ascii=False))
"
)"

if [ "$JSON" -eq 1 ]; then
  echo "$metrics"
  exit 0
fi

md="$(METRICS_JSON="$metrics" python3 <<'PY'
import json, os
m = json.loads(os.environ["METRICS_JSON"])
print("## 本周脉搏（自动）")
print()
print(f"> generated: {m['generated_at']} · since merged: `{m['since']}` · dispatch: `{m['portfolio_dispatch_mode']}`")
print()
print("| 指标 | 值 | 说明 |")
print("|------|-----|------|")
print(f"| 堵 BLOCKED | {m['blocked']} | 需 CEO 介入 |")
print(f"| 跑 RUNNING | {m['running']} | agent-running |")
print(f"| 队 QUEUE | {m['queue_agent_safe']} | 可派 agent-safe |")
print(f"| 绿 MERGED | {m['merged_prs']} | cursor/* since {m['since']} |")
print(f"| 活跃项目 | {m['projects_active']} | 非 paused 且可访问 |")
print()
print("$/merged PR：Cursor 账单 ÷ 绿 MERGED（见 [10-cost-and-budget.md](../10-cost-and-budget.md)）。")
PY
)"

[ "$QUIET" -eq 0 ] && echo "$md"

target="$OUT"
if [ "$WRITE" -eq 1 ]; then
  target="$MULTICA_ROOT/.ai-company/docs/system-evolution/$(date +%Y-%m-%d)-weekly.md"
  if [ ! -f "$target" ]; then
    cat >"$target" <<EOF
# 系统进化周回顾 — $(date +%Y-%m-%d)

> 由 \`ceo-weekly-metrics.sh --write\` 初始化；补「最大摩擦」「建议改动」后 CEO 拍板。

EOF
  fi
fi

if [ -n "$target" ]; then
  python3 - "$target" "$md" <<'PY'
import sys
from pathlib import Path
path = Path(sys.argv[1])
md = sys.argv[2]
text = path.read_text(encoding="utf-8") if path.exists() else ""
marker = "## 本周脉搏（自动）"
if marker in text:
    start = text.index(marker)
    end = text.find("\n## ", start + 1)
    if end < 0:
        path.write_text(text[:start] + md + "\n", encoding="utf-8")
    else:
        path.write_text(text[:start] + md + "\n" + text[end:].lstrip("\n"), encoding="utf-8")
else:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text((text.rstrip() + "\n\n" + md + "\n") if text else md + "\n", encoding="utf-8")
PY
  [ "$QUIET" -eq 0 ] && echo "[OK] metrics -> $target"
fi
