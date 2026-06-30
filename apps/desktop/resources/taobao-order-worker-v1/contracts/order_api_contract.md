# 订单 API 契约：淘宝订单员工 1.0 明文版本

本契约用于把已有淘宝订单 API 接到 Order Bridge。Order Bridge 对外提供统一接口给 Multica 订单员工调用。

## 0. Order Bridge 鉴权

除 `/health` 和 `/taobao/order-event` 外，所有 `/api/orders/*` 接口都要求：

```http
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

缺少或错误 token 返回 `401`。`/taobao/order-event` 使用独立的 `X-Order-Event-Secret`，不要混用。

## 1. 获取订单详情

```http
GET /api/orders/{tid}?plain=true
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

返回字段：

```json
{
  "tid": "1234567890",
  "platform": "taobao",
  "shop_id": "shop_001",
  "status": "WAIT_SELLER_SEND_GOODS",
  "payment": "199.00",
  "buyer": {
    "buyer_nick": "tb_user_xxx",
    "buyer_open_id": "xxx",
    "receiver_oaid": "xxx",
    "receiver_name": "张三",
    "receiver_mobile": "13888888888",
    "receiver_phone": "",
    "receiver_state": "浙江省",
    "receiver_city": "杭州市",
    "receiver_district": "西湖区",
    "receiver_address": "某某街道某某小区1幢101室",
    "receiver_zip": "310000"
  },
  "orders": [
    {
      "oid": "111",
      "sku_id": "sku_001",
      "outer_sku_id": "ERP_SKU_001",
      "title": "商品A",
      "sku_properties_name": "颜色:黑色;尺码:L",
      "num": 1,
      "price": "199.00",
      "payment": "199.00",
      "refund_status": "NO_REFUND",
      "inventory_status": "RESERVED"
    }
  ],
  "buyer_message": "请尽快发货",
  "seller_memo": "加急",
  "created": "2026-06-30T10:00:00+08:00",
  "modified": "2026-06-30T10:12:00+08:00"
}
```

## 2. 发货前检查

Order Bridge 已内置基础检查逻辑。如果订单系统已经有风控、库存、物流判断，可以把 `/check-fulfillment` 改为调用自己的接口。

```http
POST /api/orders/{tid}/check-fulfillment
Content-Type: application/json
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

```json
{
  "plain": true
}
```

返回：

```json
{
  "tid": "1234567890",
  "can_ship": true,
  "risk_level": "low",
  "reason_codes": [],
  "required_action": "create_shipping_draft",
  "receiver_check": {
    "name_valid": true,
    "mobile_valid": true,
    "address_complete": true,
    "province": "浙江省",
    "city": "杭州市",
    "district": "西湖区",
    "remote_area": false,
    "logistics_supported": true
  },
  "summary": "订单为待发货状态，未发现退款、禁止发货、库存、SKU、地址、留言或物流异常，可创建发货草稿。",
  "safe_summary": "订单为待发货状态，未发现退款、禁止发货、库存、SKU、地址、留言或物流异常，可创建发货草稿。",
  "metadata": {
    "shop_id": "shop_001",
    "order_status": "WAIT_SELLER_SEND_GOODS",
    "fulfillment_status": "shipping_draft_ready",
    "checked_at": "2026-06-30T10:12:30+00:00"
  }
}
```

## 3. 安全写接口

写内部备注：

```http
POST /api/orders/{tid}/internal-note
Content-Type: application/json
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

```json
{
  "idempotency_key": "taobao:shop_001:1234567890:note:v1",
  "note": "AI检查通过，建议创建发货草稿",
  "actor": "taobao-order-ops"
}
```

打标签：

```http
POST /api/orders/{tid}/tag
Content-Type: application/json
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

```json
{
  "idempotency_key": "taobao:shop_001:1234567890:tag:shipping_draft_ready:v1",
  "tag": "shipping_draft_ready",
  "actor": "taobao-order-ops"
}
```

挂起订单：

```http
POST /api/orders/{tid}/hold
Content-Type: application/json
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

```json
{
  "idempotency_key": "taobao:shop_001:1234567890:hold:refund:v1",
  "reason_code": "REFUND_IN_PROGRESS",
  "reason": "订单存在退款中子订单，禁止进入发货流程",
  "actor": "taobao-order-ops"
}
```

创建发货草稿：

```http
POST /api/orders/{tid}/create-shipping-draft
Content-Type: application/json
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

```json
{
  "idempotency_key": "taobao:shop_001:1234567890:create_shipping_draft:v1",
  "warehouse_id": "WH_HZ_001",
  "logistics_company": "YTO",
  "package_note": "AI检查通过，创建发货草稿",
  "actor": "taobao-order-ops"
}
```

安全写接口先保留幂等记录为 `pending`，只有本地检查和可选上游写入都成功后才更新为 `succeeded`。如果 `ORDER_API_WRITE_THROUGH=false`，Order Bridge 明确处于本地演示模式，可以只保存本地安全动作。如果 `ORDER_API_WRITE_THROUGH=true` 且 `ORDER_API_BASE_URL` 为空，Order Bridge 返回 `500`，并把该 idempotency key 标记为 `failed`。如果 `ORDER_API_WRITE_THROUGH=true` 且上游订单 API 返回非 2xx、超时或网络错误，Order Bridge 返回 `502` 或 `504`，并把该 idempotency key 标记为 `failed`，允许后续用同一 key 重试。

## 4. 禁止接口

1.0 中以下接口必须拒绝 AI 员工直接调用：

```http
POST /api/orders/{tid}/ship
POST /api/orders/{tid}/refund
POST /api/orders/{tid}/close
POST /api/orders/{tid}/modify-address
POST /api/orders/{tid}/modify-price
X-Order-Bridge-Token: <ORDER_BRIDGE_API_TOKEN>
```

预期返回：

```json
{
  "detail": {
    "ok": false,
    "denied": true,
    "action": "ship",
    "reason": "1.0 forbids direct high-risk actions; use safe actions only"
  }
}
```

## 5. 上游订单 API 错误语义

Order Bridge 调用已有订单 API 时，错误映射如下：

- 上游 `404`：返回 `404 order not found`。
- 上游 `401/403`：返回 `502 order API upstream auth failed`。
- 上游其他非 2xx：返回 `502`，包含上游状态码和截断响应体。
- 上游超时：返回 `504`。
- 上游网络错误：返回 `502`。

Multica Webhook 非 2xx 也会返回 `502`，该订单事件会标记为 `failed`，同一 `eventId` 可在修复后重试，不会被错误地当作已处理事件去重。`ENV=dev` 时可以缺省 `MULTICA_AUTOPILOT_WEBHOOK_URL` 以便本地演示；非 dev 环境必须配置该 webhook，否则应用启动失败，或订单事件返回 `500` 并标记为 `failed`。同一 `eventId` 处于 `pending` 时再次推送会返回 `409`，防止并发重复触发 Multica。
