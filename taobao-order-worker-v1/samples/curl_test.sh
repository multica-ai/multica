#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8090}"
SECRET="${TAOBAO_EVENT_SECRET:-dev-secret}"
TOKEN="${ORDER_BRIDGE_API_TOKEN:-dev-bridge-token}"
BRIDGE_AUTH=(-H "X-Order-Bridge-Token: $TOKEN")

echo "== health =="
curl -s "$BASE_URL/health" | python -m json.tool

echo "== send event to Order Bridge =="
curl -s -X POST "$BASE_URL/taobao/order-event" \
  -H "Content-Type: application/json" \
  -H "X-Order-Event-Secret: $SECRET" \
  --data-binary @samples/taobao_event_wait_seller_send_goods.json | python -m json.tool

echo "== get order plain =="
curl -s "${BRIDGE_AUTH[@]}" "$BASE_URL/api/orders/1234567890?plain=true" | python -m json.tool

echo "== check fulfillment normal =="
curl -s -X POST "$BASE_URL/api/orders/1234567890/check-fulfillment" \
  "${BRIDGE_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{"plain": true}' | python -m json.tool

echo "== create shipping draft =="
curl -s -X POST "$BASE_URL/api/orders/1234567890/create-shipping-draft" \
  "${BRIDGE_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "taobao:shop_001:1234567890:create_shipping_draft:v1",
    "warehouse_id": "WH_HZ_001",
    "logistics_company": "YTO",
    "package_note": "AI检查通过，创建发货草稿",
    "actor": "taobao-order-ops"
  }' | python -m json.tool

echo "== check refund order =="
curl -s -X POST "$BASE_URL/api/orders/1234567890refund/check-fulfillment" \
  "${BRIDGE_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{"plain": true}' | python -m json.tool

echo "== check address abnormal order =="
curl -s -X POST "$BASE_URL/api/orders/1234567890address/check-fulfillment" \
  "${BRIDGE_AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d '{"plain": true}' | python -m json.tool

echo "== high-risk ship denied =="
curl -s -X POST "$BASE_URL/api/orders/1234567890/ship" \
  "${BRIDGE_AUTH[@]}" | python -m json.tool
