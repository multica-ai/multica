# Multica 配置清单：淘宝订单员工 1.0

## 一、工作空间

```text
Workspace：淘宝电商运营中控
Project：订单运营
```

## 二、Agent

```text
Agent 名称：淘宝订单员工
Agent ID：taobao-order-ops
职责：待发货订单检查、异常识别、发货草稿、挂起、人工审核
Runtime：选择在线的本地 Runtime
Provider：Claude Code / Codex / OpenCode / Cursor Agent 等任选一个可用工具
```

Agent Runtime 必须能读取以下环境变量：

```env
ORDER_BRIDGE_BASE_URL=http://localhost:8090
ORDER_BRIDGE_API_TOKEN=替换为强随机字符串
```

Agent 调用所有 `/api/orders/*` 接口时必须带：

```text
X-Order-Bridge-Token: ${ORDER_BRIDGE_API_TOKEN}
```

## 三、Skill

导入：

```text
multica/SKILL.md
```

配置：

```text
Skill 名称：taobao-order-ops
挂载 Agent：淘宝订单员工
```

## 四、Autopilot

```text
名称：淘宝订单事件入口
执行智能体：淘宝订单员工
模式：create_issue
触发器：Webhook
事件过滤：taobao.trade.modified
Prompt：复制 multica/autopilot_prompt.md
Issue 标题：[淘宝订单] {{date}} 订单事件处理
```

复制生成的 Webhook URL，写入 Order Bridge 的 `.env`：

```env
MULTICA_AUTOPILOT_WEBHOOK_URL=https://<your-host>/api/webhooks/autopilots/awt_xxx
```

## 五、Order Bridge 环境变量

```env
TAOBAO_EVENT_SECRET=强随机字符串
ORDER_BRIDGE_API_TOKEN=强随机字符串
REQUIRE_ORDER_BRIDGE_API_AUTH=true
CORS_ALLOW_ORIGINS=http://localhost:3000,http://localhost:8080
ORDER_BRIDGE_BASE_URL=http://localhost:8090
ORDER_API_BASE_URL=https://你的订单API域名
ORDER_API_TOKEN=订单API访问Token
ORDER_API_GET_ORDER_PATH=/api/orders/{tid}
ORDER_API_WRITE_THROUGH=false
ALLOW_PLAIN_RECEIVER_INFO=true
STORE_PLAIN_RECEIVER_IN_ACTION_LOG=false
REMOTE_AREA_KEYWORDS=新疆,西藏,内蒙古,青海,宁夏,甘肃
UNSUPPORTED_AREA_KEYWORDS=香港,澳门,台湾,海外
ALLOW_HIGH_RISK_ACTIONS=false
```

生产环境不能继续使用 `TAOBAO_EVENT_SECRET=dev-secret` 或 `ORDER_BRIDGE_API_TOKEN=dev-bridge-token`。`ORDER_API_WRITE_THROUGH=false` 只适合本地演示；接入真实订单系统时，安全写接口要在上游写成功后才会记录为成功。

## 六、验收标准

- `POST /taobao/order-event` 后，Multica 自动创建 Issue。
- 订单员工带 `X-Order-Bridge-Token` 后能读取 `/api/orders/{tid}?plain=true`。
- 不带 `X-Order-Bridge-Token` 调用 `/api/orders/*` 返回 401。
- 正常订单创建发货草稿。
- 退款中订单被挂起。
- 地址异常订单进入人工审核。
- 直接调用 `/ship`、`/refund`、`/close`、`/modify-address`、`/modify-price` 返回 403。
