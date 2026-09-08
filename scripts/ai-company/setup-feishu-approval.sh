#!/usr/bin/env bash
# One-shot setup for Multica + Feishu dual CEO approval.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

usage() {
  cat <<'EOF'
Usage: setup-feishu-approval.sh [--test]

Configures:
  1. Feishu bot DM credentials (reuses setup-feishu-bot-notify.sh if missing)
  2. Prints Feishu open-platform event subscription steps
  3. Optional --test: sync + push one approval card (dry-run if no BLOCKED)

Env (optional, in .ai-company/config/local.env):
  MULTICA_FRONTEND_URL=http://localhost:3000
  CEO_FEISHU_APPROVAL_PORT=9478
  FEISHU_VERIFICATION_TOKEN=...
  CEO_FEISHU_APPROVAL_PUSH=1   # nightly brief also pushes cards

Feishu app → 事件订阅:
  Request URL: https://<your-host>/feishu/event
  Events: card.action.trigger, im.message.receive_v1

Run callback server:
  bash scripts/ai-company/ceo-feishu-approval-server.py

Text commands (after server + event subscription):
  /批 beatscape 42 用方案 A
  /打回 beatscape 42 需要补 AC
EOF
}

TEST=0
while [ $# -gt 0 ]; do
  case "$1" in
    --test) TEST=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

if [ ! -f "$MULTICA_ROOT/.ai-company/config/feishu-bot-notify.env" ]; then
  echo ">> configuring Feishu bot DM (feishu-bot-notify.env)"
  bash "$SCRIPT_DIR/setup-feishu-bot-notify.sh"
else
  # shellcheck disable=SC1090
  source "$MULTICA_ROOT/.ai-company/config/feishu-bot-notify.env"
fi

PORT="${CEO_FEISHU_APPROVAL_PORT:-9478}"
FRONTEND="${MULTICA_FRONTEND_URL:-http://localhost:3000}"
URL_FILE="${CEO_FEISHU_CF_TUNNEL_URL_FILE:-$HOME/.multica/ceo-feishu-cloudflare-url.txt}"
PUB_URL=""
if [ -f "$URL_FILE" ]; then
  PUB_URL="$(tr -d '[:space:]' <"$URL_FILE")"
fi

cat <<EOF

=== Multica + 飞书 双向审批 — 下一步 ===

1) 回调服务（LaunchAgent）:
   bash $SCRIPT_DIR/ceo-feishu-approval-service.sh install
   bash $SCRIPT_DIR/ceo-feishu-approval-service.sh status

2) 公网 tunnel（quick 或 named）:
   bash $SCRIPT_DIR/ceo-feishu-cloudflare-tunnel.sh quick-install
   bash $SCRIPT_DIR/ceo-feishu-cloudflare-tunnel.sh refresh-quick-url

3) 飞书开放平台 → 你的 Bot 应用 → 事件订阅:
   - 请求地址: ${PUB_URL:-https://<公网可达>}/feishu/event
   - 订阅事件: card.action.trigger, im.message.receive_v1
   - Verification Token → local.env FEISHU_VERIFICATION_TOKEN，然后:
     bash $SCRIPT_DIR/ceo-feishu-approval-service.sh install

4) 手动推送审批卡片:
   bash $SCRIPT_DIR/ceo-feishu-approval.sh sync
   bash $SCRIPT_DIR/ceo-feishu-approval.sh push

5) 飞书文字命令（卡片按钮也需要第 1–3 步）:
   /批 beatscape 42 说明
   /打回 beatscape 42 说明

6) Multica Web 审批:
   打开 $FRONTEND 收件箱 / issue，评论后:
   bash $SCRIPT_DIR/ceo-feishu-approval.sh approve beatscape 42 说明

7) 夜间自动推卡片（可选）:
   echo 'export CEO_FEISHU_APPROVAL_PUSH=1' >> $MULTICA_ROOT/.ai-company/config/local.env

EOF

if [ "$TEST" -eq 1 ]; then
  echo ">> test: list pending"
  bash "$SCRIPT_DIR/ceo-feishu-approval.sh" list || true
  pending_count="$(bash "$SCRIPT_DIR/ceo-feishu-approval.sh" list 2>/dev/null | wc -l | tr -d ' ')"
  if [ "${pending_count:-0}" -eq 0 ]; then
    echo ">> no pending items — dry-run push skipped"
  else
    echo ">> test: sync + push"
    bash "$SCRIPT_DIR/ceo-feishu-approval.sh" sync
    bash "$SCRIPT_DIR/ceo-feishu-approval.sh" push
  fi
fi
