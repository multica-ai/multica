#!/usr/bin/env bash
# Print Feishu event subscription URL and config checklist (reads local env + tunnel URL file).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

URL_FILE="${CEO_FEISHU_CF_TUNNEL_URL_FILE:-$HOME/.multica/ceo-feishu-cloudflare-url.txt}"
PORT="${CEO_FEISHU_APPROVAL_PORT:-9478}"
PUB=""

if [ -f "$URL_FILE" ]; then
  PUB="$(tr -d '[:space:]' <"$URL_FILE")"
fi

echo "=== 飞书 inbound 配置清单 ==="
echo ""
if [ -n "$PUB" ]; then
  echo "Request URL: ${PUB}/feishu/event"
  if curl -fsS --max-time 15 "${PUB}/health" >/dev/null 2>&1; then
    echo "公网 /health: OK"
  else
    echo "公网 /health: 不可达 — bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh status"
  fi
else
  echo "Request URL: https://<公网>/feishu/event"
  echo "  先: bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh quick-install"
  echo "  再: bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh refresh-quick-url"
fi
echo ""
echo "事件: card.action.trigger, im.message.receive_v1"
echo "本机审批: http://127.0.0.1:${PORT}/health"
echo ""
if [ -n "${FEISHU_VERIFICATION_TOKEN:-}" ] && [[ "${FEISHU_VERIFICATION_TOKEN}" != YOUR_* ]]; then
  echo "FEISHU_VERIFICATION_TOKEN: 已配置"
  echo "  若刚写入 token: bash scripts/ai-company/ceo-feishu-approval-service.sh install"
else
  echo "FEISHU_VERIFICATION_TOKEN: 未配置"
  echo "  bash scripts/ai-company/setup-feishu-approval-token.sh"
fi
