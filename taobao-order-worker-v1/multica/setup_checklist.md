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
ORDER_API_AUTH_HEADER=Authorization
ORDER_API_AUTH_SCHEME=Bearer
ORDER_API_WRITE_THROUGH=false
ALLOW_PLAIN_RECEIVER_INFO=true
STORE_PLAIN_RECEIVER_IN_ACTION_LOG=false
REMOTE_AREA_KEYWORDS=新疆,西藏,内蒙古,青海,宁夏,甘肃
UNSUPPORTED_AREA_KEYWORDS=香港,澳门,台湾,海外
ALLOW_HIGH_RISK_ACTIONS=false
```

生产环境不能继续使用 `TAOBAO_EVENT_SECRET=dev-secret` 或 `ORDER_BRIDGE_API_TOKEN=dev-bridge-token`。`ORDER_API_WRITE_THROUGH=false` 只适合本地演示；接入真实订单系统时，安全写接口要在上游写成功后才会记录为成功。

启动 Order Bridge 时需要显式加载 `.env`，不要只复制 `.env.example` 后直接启动：

```bash
uvicorn app:app --env-file .env --host 0.0.0.0 --port 8090
```

如果 `ENV` 不是 `dev`，还必须配置 `MULTICA_AUTOPILOT_WEBHOOK_URL`。如果 `ORDER_API_WRITE_THROUGH=true`，还必须配置 `ORDER_API_BASE_URL`，否则安全写接口会失败并把幂等记录标记为 `failed`，不会伪装成成功。

## 六、1.1 真实联调准备

本阶段只做真实订单 API 联调准备，不接触真实淘宝账号密码、Cookie、AppSecret、Access Token 或真实客户隐私。真实凭证只能通过本机 `.env`、系统环境变量或 secret manager 配置，不能写入 Git、Skill、Issue 或聊天。

新增的真实订单适配层：

```text
order_client.py
```

它负责：

- 调用 `ORDER_API_BASE_URL` 指向的 staging 订单 API。
- 把真实订单 API 返回标准化成 Order Bridge 的 `OrderDetail` 结构。
- 统一映射上游 `401/403/404/5xx/timeout`。
- 封装安全写动作：内部备注、打标签、挂起、创建发货草稿。

第一轮联调必须保持只读：

```env
ORDER_API_WRITE_THROUGH=false
```

验收重点：

- 真实待发货订单能读取并进入 `create_shipping_draft` 建议。
- 真实退款中订单能识别为 `hold`。
- 真实地址异常订单能识别为 `manual_review`。
- 真实已关闭订单能识别为 `ignore`。
- 真实禁止发货订单能识别为 `hold`。
- Multica Issue 评论 JSON 结构稳定。

第二轮联调才允许打开安全写动作：

```env
ORDER_API_WRITE_THROUGH=true
```

只允许写：

- `internal-note`
- `tag`
- `hold`
- `create-shipping-draft`

继续禁止：

- 正式发货
- 退款
- 关闭订单
- 修改地址
- 修改价格

参考配置模板：

```text
.env.staging.example
```

## 七、验收标准

- `POST /taobao/order-event` 后，Multica 自动创建 Issue。
- 订单员工带 `X-Order-Bridge-Token` 后能读取 `/api/orders/{tid}?plain=true`。
- 不带 `X-Order-Bridge-Token` 调用 `/api/orders/*` 返回 401。
- 正常订单创建发货草稿。
- 退款中订单被挂起。
- 地址异常订单进入人工审核。
- 直接调用 `/ship`、`/refund`、`/close`、`/modify-address`、`/modify-price` 返回 403。
- `ORDER_API_BASE_URL` 为空时仍走 mock 演示模式。
- `ORDER_API_BASE_URL` 存在时走 `order_client.py` 真实 API 适配层。
- `ORDER_API_WRITE_THROUGH=false` 时不写真实订单系统。
- 上游失败不会把 action 伪装成 succeeded。
