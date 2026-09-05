#!/usr/bin/env bash
# Smoke-test the Feishu → workbench site-factory path (no live Feishu message, no agent quota).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FEISHU_DIR="${FEISHU_CURSOR_CLAW_DIR:-$HOME/Projects/feishu-cursor-claw}"
WB="${CEO_WORKBENCH_URL:-http://127.0.0.1:9477}"

echo "Feishu site-factory smoke — $(date '+%Y-%m-%d %H:%M')"

if [ ! -f "$FEISHU_DIR/server.ts" ]; then
  echo "❌ feishu-cursor-claw not found: $FEISHU_DIR" >&2
  exit 1
fi
if ! grep -q matchSiteFactoryIntent "$FEISHU_DIR/server.ts"; then
  echo "❌ matchSiteFactoryIntent missing in feishu-cursor-claw" >&2
  exit 1
fi
echo "✅ feishu-cursor-claw 建站意图已接线"

if pgrep -f "feishu-cursor-claw/start.ts" >/dev/null 2>&1 || \
   { [ -x "$FEISHU_DIR/service.sh" ] && bash "$FEISHU_DIR/service.sh" status 2>/dev/null | grep -qE '运行中|PID:'; }; then
  echo "✅ feishu-cursor-claw 服务运行中"
else
  echo "⚠️  feishu-cursor-claw 未运行 — bash $FEISHU_DIR/service.sh start"
fi

if ! curl -fsS --max-time 2 "$WB/api/health" >/dev/null; then
  echo "❌ CEO workbench 未运行 ($WB) — bash scripts/ai-company/ceo-workbench.sh" >&2
  exit 1
fi
echo "✅ CEO workbench 在线"

# Same fetch contract as feishu-cursor-claw tryWorkbenchSiteFactory(), with dry_run to avoid agents.
if command -v bun >/dev/null 2>&1; then
  bun -e "
const intake = '做一个 JSON 格式化网站';
const res = await fetch('$WB/api/site-factory', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    intake,
    create_repo: false,
    notify: false,
    max_dispatch: 2,
    dry_run: true,
  }),
  signal: AbortSignal.timeout(4000),
});
if (!res.ok) throw new Error('workbench POST failed: ' + res.status);
const job = await res.json();
if (job.mode !== 'site-factory' || !job.id) throw new Error('unexpected job: ' + JSON.stringify(job));
console.log('✅ 飞书等价 workbench POST dry-run job ' + job.id);
" || exit 1
else
  curl -fsS --max-time 15 -X POST "$WB/api/site-factory" \
    -H 'Content-Type: application/json' \
    -d '{"intake":"做一个 JSON 格式化网站","dry_run":true,"notify":false,"create_repo":false}' \
    | python3 -c "import json,sys; j=json.load(sys.stdin); assert j.get('mode')=='site-factory' and j.get('id'); print('✅ 飞书等价 workbench POST dry-run job', j['id'])"
fi

echo ""
echo "Live 验收：在飞书私聊 Bot 发送「做一个 XX 网站」，应收到「建站流水线已提交 CEO 工作台」卡片。"
