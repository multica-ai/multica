#!/usr/bin/env bash
# Verify site-factory prerequisites and smoke-test Cloudflare scaffold (no agent quota).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/site-factory-runtime.sh
source "$SCRIPT_DIR/lib/site-factory-runtime.sh"

ok=0
warn=0
fail=0

pass() { echo "  ✅ $1"; ok=$((ok + 1)); }
note() { echo "  ⚠️  $1"; warn=$((warn + 1)); }
bad() { echo "  ❌ $1"; fail=$((fail + 1)); }

echo "Site Factory 验收 — $(date '+%Y-%m-%d %H:%M')"
echo ""

echo "1. 脚本与模板"
for f in site-factory.sh scaffold-cloudflare.sh lib/site-factory-runtime.sh; do
  if [ -x "$SCRIPT_DIR/$f" ] || [ -f "$SCRIPT_DIR/$f" ]; then
    pass "$f"
  else
    bad "missing $f"
  fi
done
if [ -f "$MULTICA_ROOT/.ai-company/templates/site-factory/research-prompt.md" ]; then
  pass "research/mvp 模板"
else
  bad "site-factory 模板缺失"
fi
if [ -f "$MULTICA_ROOT/.ai-company/templates/competitor_inventory.md" ] \
  && [ -f "$MULTICA_ROOT/.ai-company/templates/wont_do.md" ]; then
  pass "visual replica 模板 (inventory + wont_do)"
else
  bad "缺少 competitor_inventory.md / wont_do.md 模板"
fi
if [ -f "$MULTICA_ROOT/.ai-company/runbooks/visual-replica-gate.md" ]; then
  pass "runbook visual-replica-gate.md"
else
  bad "缺少 visual-replica-gate runbook"
fi
if [ -x "$SCRIPT_DIR/autopilot-dispatch.sh" ] || [ -f "$SCRIPT_DIR/autopilot-dispatch.sh" ]; then
  pass "autopilot-dispatch.sh"
else
  bad "缺少 autopilot-dispatch.sh"
fi
if [ -f "$MULTICA_ROOT/.ai-company/runbooks/employee-autopilot.md" ]; then
  pass "runbook employee-autopilot.md"
else
  bad "缺少 employee-autopilot runbook"
fi
if grep -q 'visual-check' "$SCRIPT_DIR/scaffold-cloudflare.sh"; then
  pass "scaffold 含 visual-check"
else
  bad "scaffold-cloudflare.sh 未接入 visual-check"
fi
if grep -q 'L4-视觉\|Visual Replica' "$MULTICA_ROOT/.ai-company/docs/07-quality-gates.md"; then
  pass "quality-gates L4-视觉"
else
  bad "07-quality-gates.md 缺少 L4-视觉"
fi

echo ""
echo "2. 解析 / dry-run"
if bash "$SCRIPT_DIR/site-factory.sh" --intake "做一个 JSON 格式化网站" --dry-run >/tmp/site-factory-dry-run.log 2>&1; then
  pass "site-factory --dry-run"
  grep -q 'slug:.*json-site' /tmp/site-factory-dry-run.log && pass "slug 解析 json-site" || note "slug 解析非常规"
else
  bad "site-factory --dry-run 失败"
fi

echo ""
echo "3. Multica 运行服务（部分能力由运行中的 self-host + daemon 提供）"
api="$(site_factory_multica_api)"
api="${api:-${MULTICA_SERVER_URL:-http://localhost:8081}}"
if site_factory_runtime_ready "$api"; then
  pass "Multica API ready ($api)"
else
  note "Multica API 未就绪 — bash scripts/local-selfhost-autostart.sh 或 make selfhost"
fi
if site_factory_daemon_running; then
  pass "multica daemon running"
else
  note "daemon 未运行 — multica daemon start"
fi
slots="$(site_factory_dispatch_slots 2)"
pass "dispatch slots (max 2): $slots"

echo ""
echo "4. Cloudflare 脚手架 smoke"
SMOKE="$(mktemp -d)/cf-smoke"
if bash "$SCRIPT_DIR/scaffold-cloudflare.sh" "$SMOKE" cf-smoke "Smoke" >>/tmp/site-factory-scaffold.log 2>&1; then
  pass "scaffold-cloudflare.sh"
else
  bad "scaffold 失败 — /tmp/site-factory-scaffold.log"
fi
if [ -f "$SMOKE/wrangler.toml" ] && [ -f "$SMOKE/.github/workflows/cloudflare-pages-check.yml" ]; then
  pass "wrangler.toml + Pages CI workflow"
else
  bad "CF 产物不完整"
fi
if (cd "$SMOKE" && pnpm install >>/tmp/site-factory-pnpm.log 2>&1 && make check >>/tmp/site-factory-check.log 2>&1); then
  pass "pnpm install + make check"
else
  bad "make check 失败 — /tmp/site-factory-check.log"
fi

echo ""
echo "5. 飞书桥接"
if [ -f "$HOME/Projects/feishu-cursor-claw/server.ts" ] && grep -q matchSiteFactoryIntent "$HOME/Projects/feishu-cursor-claw/server.ts"; then
  pass "feishu-cursor-claw 建站意图已接线"
else
  note "feishu-cursor-claw 未更新或未安装"
fi

echo ""
echo "6. CEO 工作台 API"
if curl -fsS --max-time 2 "http://127.0.0.1:9477/api/site-factory" >/dev/null 2>&1; then
  pass "workbench /api/site-factory 在线"
else
  note "workbench 未运行 — bash scripts/ai-company/ceo-workbench.sh"
fi

echo ""
echo "7. 飞书 intake 路径（模拟 workbench POST dry-run）"
if python3 - <<'PY'
import re
samples = [
    ("做一个 JSON 格式化网站", "JSON 格式化"),
    ("site-factory: 海外 SEO 工具", "海外 SEO 工具"),
    ("建站：极简 Hello 页", "极简 Hello 页"),
]
patterns = [
    re.compile(r"^(?:请)?(?:帮我)?(?:做|建|开发|创建)(?:一个|个)?(.+?)(?:网站|站点|网页)(?:吧|吗|？|\?|!|！|\.|…)*$", re.S),
    re.compile(r"^site-factory[:\uff1a]\s*(.+)$", re.I),
    re.compile(r"^建站[:\uff1a]\s*(.+)$", re.I),
]
def match_intent(text):
    t = text.strip()
    for p in patterns:
        m = p.match(t)
        if m and m.group(1).strip():
            return m.group(1).strip()
    return None
for text, want in samples:
    got = match_intent(text)
    if got != want:
        raise SystemExit(f"intent mismatch: {text!r} -> {got!r} want {want!r}")
print("ok")
PY
then
  pass "飞书建站意图解析"
else
  bad "飞书建站意图解析失败"
fi

if curl -fsS --max-time 2 "http://127.0.0.1:9477/api/site-factory" >/dev/null 2>&1; then
  if curl -fsS --max-time 15 -X POST "http://127.0.0.1:9477/api/site-factory" \
    -H 'Content-Type: application/json' \
    -d '{"intake":"做一个 JSON 格式化网站","dry_run":true,"notify":false,"create_repo":false}' \
    | python3 -c "import json,sys; j=json.load(sys.stdin); assert j.get('mode')=='site-factory' and j.get('id')"; then
    pass "workbench POST dry-run 建站 job"
  else
    bad "workbench POST dry-run 失败（需重启 workbench 加载新 API）"
  fi
else
  note "跳过 workbench POST dry-run（workbench 未运行）"
fi

echo ""
echo "8. 飞书等价路径 smoke"
if bash "$SCRIPT_DIR/feishu-site-factory-smoke.sh" >/tmp/feishu-site-factory-smoke.log 2>&1; then
  pass "feishu-site-factory-smoke.sh"
else
  tail -3 /tmp/feishu-site-factory-smoke.log >&2 || true
  bad "feishu-site-factory-smoke 失败"
fi

echo ""
printf "结果: %s 通过 · %s 警告 · %s 失败\n" "$ok" "$warn" "$fail"
rm -rf "$(dirname "$SMOKE")" 2>/dev/null || true
[ "$fail" -eq 0 ]
