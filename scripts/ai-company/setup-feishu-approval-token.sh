#!/usr/bin/env bash
# Guide user to copy Verification Token into feishu-approval.env (not available via API).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT="$MULTICA_ROOT/.ai-company/config/feishu-approval.env"
EXAMPLE="$MULTICA_ROOT/.ai-company/config/feishu-approval.env.example"

# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

APP_ID="${FEISHU_BOT_APP_ID:-}"

echo "=== Feishu Verification Token ==="
echo ""
echo "Verification Token is console-only (not available via API)."
echo ""
if [ -n "$APP_ID" ]; then
  echo "1. Open: https://open.feishu.cn/app/${APP_ID}/event"
  echo "   Events and callbacks -> Encryption -> Verification Token"
else
  echo "1. Open Feishu developer console -> your bot -> Events and callbacks"
fi
echo ""
echo "2. Copy Verification Token into:"
echo "   $OUT"
echo ""
if [ -f "$OUT" ]; then
  if grep -q 'YOUR_FEISHU_VERIFICATION_TOKEN' "$OUT" 2>/dev/null; then
    echo "   WARN: $OUT exists but still has placeholder"
    bash "$SCRIPT_DIR/print-feishu-inbound-setup.sh" 2>/dev/null | sed 's/^/   /' || true
  else
    echo "   OK: $OUT configured"
    bash "$SCRIPT_DIR/ceo-feishu-approval-service.sh" install
    bash "$SCRIPT_DIR/print-feishu-inbound-setup.sh"
    exit 0
  fi
else
  cp "$EXAMPLE" "$OUT"
  echo "   Created $OUT -- edit and paste token"
fi
echo ""
echo "3. After saving:"
echo "   bash $SCRIPT_DIR/ceo-feishu-approval-service.sh install"
echo "   bash $SCRIPT_DIR/print-feishu-inbound-setup.sh"
