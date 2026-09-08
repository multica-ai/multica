#!/usr/bin/env bash
# After sending a Feishu site-factory message, run this to confirm intake → workbench job.
set -euo pipefail

JOBS_DIR="${HOME}/.multica/ceo-workbench/jobs"
FEISHU_LOG="${FEISHU_CURSOR_LOG:-/tmp/feishu-cursor.log}"
SINCE="${1:-5}"

echo "Feishu live 验收监视 — 最近 ${SINCE} 分钟"
echo ""

echo "1. 服务"
curl -fsS --max-time 2 http://127.0.0.1:9477/api/health >/dev/null && echo "  ✅ workbench :9477" || echo "  ❌ workbench 未运行"
pgrep -f "feishu-cursor-claw/start.ts" >/dev/null && echo "  ✅ feishu-cursor-claw" || echo "  ❌ feishu Bot 未运行"
curl -fsS --max-time 2 http://localhost:8081/readyz >/dev/null && echo "  ✅ Multica API" || echo "  ⚠️  Multica API 未就绪"

echo ""
echo "2. 最近 site-factory workbench jobs（非 dry_run 优先）"
found=0
if [ -d "$JOBS_DIR" ]; then
  while IFS= read -r path; do
    [ -f "$path" ] || continue
    python3 - <<PY
import json, datetime, sys
from pathlib import Path
j = json.loads(Path("$path").read_text())
if j.get("mode") != "site-factory":
    sys.exit(0)
started = j.get("started_at", "")
dry = j.get("dry_run", False)
print(f"  · {j.get('id')} dry_run={dry} status={j.get('status')} intake={j.get('intake','')[:50]}")
if not dry and j.get("status") in ("running", "success"):
    sys.exit(2)
PY
    code=$?
    if [ "$code" -eq 2 ]; then found=1; fi
  done < <(find "$JOBS_DIR" -name '*.json' -mmin "-${SINCE}" 2>/dev/null | sort -r)
fi
if [ "$found" -eq 1 ]; then
  echo "  ✅ 发现 live site-factory job（非 dry_run）"
else
  echo "  ⚠️  最近 ${SINCE} 分钟内无 live site-factory job"
  echo "     请在飞书发：做一个 XX 网站"
fi

echo ""
echo "3. 飞书日志尾部（建站相关）"
if [ -f "$FEISHU_LOG" ]; then
  if tail -200 "$FEISHU_LOG" | grep -E '\[site-factory\]|建站流水线已提交 CEO 工作台' | tail -5; then
    :
  else
    echo "  （无建站关键词 — 可能尚未从飞书触发）"
  fi
else
  echo "  ⚠️  无日志 $FEISHU_LOG"
fi

echo ""
echo "4. 活跃流水线"
REGISTRY="/Users/zhenhuachen/Projects/multica/.ai-company/run-logs/active-pipelines.txt"
if [ -f "$REGISTRY" ]; then tail -3 "$REGISTRY"; else echo "  （无 active-pipelines.txt）"; fi
