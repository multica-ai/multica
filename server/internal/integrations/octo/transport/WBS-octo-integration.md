# Octo IM 集成 — 阶段 4-10 WBS（工作分解）

> 前置已完成：阶段 0-3（方案评估、协议 spike、`im` 传输层包 + 真机 PoC 三项全绿）。
> 本文把"接入 multica 业务系统"拆成可独立提 PR 的工作单元。
>
> **对标模板**：飞书集成 `server/internal/integrations/lark/`（已生产可用）。
> Octo 比飞书省事的地方：无设备流扫码注册、无双云 region、单 bot_token（非 app_id+secret）、
> 出站用文本/markdown + message-edit（非交互卡片渲染）。
>
> **核心复用点**（无需改动）：入站终点是通用 `TaskService.EnqueueChatTask(ctx, chatSession, initiatorUserID)`
> （`internal/service/task.go:702`）；出站监听通用事件 `EventChatDone` / `EventTaskFailed`
> （`pkg/protocol/events.go:73-74`）。**agent runtime / daemon / 任务队列完全不动**——
> Octo 只是 chat_session 的一个新「来源」，与飞书平级。

---

## 依赖关系总览

```
阶段4 (DB)  ─┬─► 阶段5 (入站)  ─┐
             ├─► 阶段6 (出站)  ─┤
             ├─► 阶段7 (绑定)  ─┼─► 阶段9 (骨架接入) ─► 阶段3.5 (集成测试)
             └─► 阶段8 (Hub)   ─┘                      └─► 阶段10 (前端)
```

阶段 4 是所有后端工作的前置。阶段 5-8 可在 4 完成后**并行**。阶段 9 把它们串起来。
阶段 10 前端可在阶段 9 的 API 定型后并行。

---

## 阶段 4 — 数据库层【PR #1，前置，~1 天】

新建 migration `119_octo_integration.up.sql` / `.down.sql`（编号续 118）。
对标 `migrations/109_lark_integration.up.sql`，建 6 张表 + 1 处 issue origin 扩展：

| 表 | 职责 | Octo 化裁剪（相对 lark） |
|---|---|---|
| `octo_installation` | bot 绑定记录：`workspace_id`、`agent_id`、`bot_token_encrypted`、`robot_id`、`bot_name`、`owner_uid`、`api_url`、`ws_url`、租约字段（`ws_lease_owner`/`ws_lease_expires_at`） | **无 region 列、无 app_id/app_secret**（单 token）；token 加密存储 |
| `octo_user_binding` | Octo `uid` ↔ multica user：`workspace_id`、`multica_user_id`、`installation_id`、`octo_uid` | UNIQUE(installation_id, octo_uid)；复合 FK 保工作区一致性 |
| `octo_chat_session_binding` | Octo `channel_id` ↔ `chat_session`：`chat_session_id`(UNIQUE)、`installation_id`、`octo_channel_id`、`octo_channel_type` | UNIQUE(installation_id, octo_channel_id)；照搬 lark |
| `octo_inbound_dedup` | `message_id` 去重 + claim 栅栏：`installation_id`、`message_id`、`claim_token`、`processed_at` | PK(installation_id, message_id)，照搬 lark 113 的 per-installation 设计 |
| `octo_inbound_audit` | drop 原因审计（非内容）：`installation_id`、`message_id`、`drop_reason`、`created_at` | 照搬 lark |
| `octo_outbound_message` | `task_id`(UNIQUE) ↔ 已发消息：`octo_channel_id`、`octo_message_id`、`octo_message_seq`、`status`、`last_edited_at` | lark 叫 card；Octo 存 message_id+seq 供 message-edit 流式 |
| `octo_binding_token` | 一次性绑定令牌：`token_hash`(PK)、`workspace_id`、`installation_id`、`octo_uid`、`expires_at`(≤+15min)、`consumed_at` | 照搬 lark `binding_token` |

**额外改动**：`issue.origin_type` 的 CHECK 约束加 `'octo_chat'`（对标 migration 111）。

**交付物**：
- `migrations/119_octo_integration.up.sql` + `.down.sql`
- `pkg/db/queries/octo.sql`（对标 `queries/lark.sql`）：每张表的 Create/Get/Delete + claim/mark dedup + EnsureBinding 等
- 跑 `make sqlc` 生成 `pkg/db/generated/octo.sql.go`
- **测试**：迁移 up/down 往返；几个关键 query 的 Go 测试（建库 → 插入 → 查回）

**验收**：`make migrate-up && make migrate-down && make migrate-up` 干净；sqlc 无 drift。

---

## 阶段 5 — 入站链路【PR #2，依赖 #1，~2 天】

`server/internal/integrations/octo/` 下新建（对标 lark 同名文件）：

| 文件 | 职责 | 关键函数 |
|---|---|---|
| `types.go` | `Outcome` 枚举（Ingested/NeedsBinding/AgentOffline/Dropped...）、`DispatchResult`、`InboundMessage` | 对标 `lark/types.go` |
| `dispatcher.go` | 入站核心管道 | `Dispatcher.Handle(msg im.BotMessage)` |
| `chat_service.go` | 会话确保 + 消息落库 | `EnsureChatSession`、`AppendUserMessage`（对标 `lark/chat_service.go:94/193`） |
| `audit.go` | drop 审计 | `AuditLogger.Log(...)` |
| `issue_command.go` | `/issue <title>` 命令解析（可选，放阶段 5b） | — |

**Dispatcher.Handle 管道顺序**（对标 lark `dispatcher.go`）：
1. 按 `robot_id` 路由到 `octo_installation`（未知/已撤销 → audit drop）
2. dedup claim（`message_id` 冲突 → drop）
3. 群聊门槛：`channel_type==2` 且 `payload.mention.uids` 不含 bot robot_id → drop
4. 身份检查：`GetOctoUserBindingByUID` → 未绑定 → `OutcomeNeedsBinding`；非工作区成员 → drop
5. `EnsureChatSession(channel_id → chat_session)`（竞态靠 UNIQUE 兜底）
6. `AppendUserMessage`（事务内：插 chat_message + mark dedup processed）
7. `TaskService.EnqueueChatTask(ctx, chatSession, initiatorUserID)` ← **复用,agent 开始干活**

**桥接**：`im.Socket.OnMessage` 回调 → `Dispatcher.Handle`。注意 `im.BotMessage` → 内部
`InboundMessage` 的字段映射（im 包是纯传输,dispatcher 是业务,两者解耦）。

**交付物 + 测试**：dispatcher 各分支单测（mock queries）；EnsureChatSession 并发竞态测试。

**验收**：单测覆盖 7 个分支；一条入站消息能落库并触发 EnqueueChatTask（mock TaskService 验证被调用）。

---

## 阶段 6 — 出站链路【PR #3，依赖 #1，~1.5 天】

`octo/outbound.go`（对标 `lark/outbound.go` + `outcome_replier.go`）：

| 组件 | 职责 |
|---|---|
| `Patcher` | 订阅事件总线 → 回 Octo。`bus.Subscribe(EventChatDone, ...)` + `bus.Subscribe(EventTaskFailed, ...)` |
| `handleEvent(e)` | 提取 `chat_session_id` → `GetOctoChatSessionBindingBySession`（无绑定=Web 会话,跳过）|
| 成功路径 | `EventChatDone` → `im.HTTPClient.SendMessage`（首次,存 `octo_outbound_message`）/ 流式则 `EditMessage` |
| 失败路径 | `EventTaskFailed` → 发错误文本消息 |
| `OutcomeReplier` | 同步回复 `NeedsBinding`（发绑定链接私聊卡片）/ `AgentOffline` 提示 |

**复用 im 传输层**：出站全部走阶段 2 已验证的 `im.HTTPClient`(register 拿的 token 解密后用)。

**交付物 + 测试**：Patcher 事件路由单测；流式 edit 的 message_id 复用逻辑测试。

**验收**：mock 一个 `EventChatDone` → 验证调用 `SendMessage` 且参数正确；无绑定时跳过。

---

## 阶段 7 — 身份绑定【PR #4，依赖 #1，~1 天】

`octo/binding_token.go`（对标 `lark/binding_token.go`）：
- `BindingTokenService.Mint(installation_id, octo_uid)` → 随机 32 字节,存 SHA256 hash,15min TTL
- `RedeemAndBind(rawToken, multicaUserID)` → 校验未过期/未消费 + 工作区成员 → 落 `octo_user_binding`(幂等)
- 绑定链接：`{MULTICA_PUBLIC_URL}/octo/bind?token=<raw>`,由 OutcomeReplier 私聊发给用户

**交付物 + 测试**：Mint/Redeem 往返;过期/重放/非成员拒绝。

---

## 阶段 8 — 生命周期 Hub【PR #5,依赖 #1,~1.5 天】

`octo/hub.go` + `octo/installation.go` + `octo/client.go`（对标 lark）:
- `InstallationService`:bot_token 用 `secretbox` 加密存取(对标 `lark.NewInstallationService`)
- `Hub.Run(ctx)`:扫描所有 `octo_installation` → 为每个起一个 `im.Socket` 长连接
  - 多实例下用 DB 租约(`ws_lease_owner`)保证一个 installation 只有一个节点持有 WS(对标 lark hub 租约)
  - `im.Socket` 的 `OnError(errRapidDisconnect)` → `Register(forceRefresh)` 刷 token → 重连
- 安装/卸载:bot 配置后动态拉起/停止对应 Socket

**这是 im 传输层与业务层的接合处**:Hub 负责"哪些 bot 要连、连接生命周期",im.Socket 负责"单条连接的协议"。

**交付物 + 测试**:Hub 启停;租约获取/释放;installation 增删触发 Socket 增减。

---

## 阶段 9 — 骨架接入【PR #6,依赖 #2-5,~1 天】

把前面各件接进 multica 主干(对标 lark 在 `router.go:189-272` 的接入块):

| 文件 | 改动 |
|---|---|
| `cmd/server/router.go` | 读 `MULTICA_OCTO_SECRET_KEY` → 构造 InstallationService/Dispatcher/Patcher/Hub;挂 `/api/workspaces/{id}/octo/*` 路由 |
| `cmd/server/main.go` | `go h.OctoHub.Run(ctx)` + 优雅关闭(对标 lark main.go:354) |
| `pkg/protocol/events.go` | 加 `EventOctoInstallationCreated/Revoked`(对标 134-135) |
| `internal/handler/octo.go` | HTTP handler:列安装/绑定 bot/解绑/redeem binding token |
| `realtime/use-realtime-sync.ts` | `octo_installation:created/revoked` → invalidate 前端 query |

**环境变量**:`MULTICA_OCTO_SECRET_KEY`(激活开关+加密)、`MULTICA_OCTO_API_URL`(可选默认)、`MULTICA_PUBLIC_URL`(绑定链接,通用已有)。

**API 端点**(对标 lark):
- `GET  /api/workspaces/{id}/octo/installations`
- `POST /api/workspaces/{id}/octo/installations`（配置 bot:填 bot_token → 后端 register 验证 → 落库）
- `DELETE /api/workspaces/{id}/octo/installations/{installationId}`
- `POST /api/octo/binding/redeem`

> Octo 比飞书简单:**没有设备流扫码**,配置 bot 就是"贴一个 bf_ token"。所以砍掉 lark 的
> `registration_service.go` / `BeginInstall` / 轮询 status 那一整套。

---

## 阶段 3.5 — 端到端集成测试【PR #6 内,~0.5 天】

把阶段 2 的手动 PoC 升级为半自动集成测试(仍 `//go:build manual`):
- 配置一个真实 bot → 模拟 Octo 用户私聊 → 验证 issue/chat_session 创建 + agent 入队 → agent 完成 → 验证回到 Octo
- 复用已验证的 `im` 包 + 真机 token

---

## 阶段 10 — 前端【PR #7,依赖 #6 的 API,~1.5 天】

对标 `packages/views/settings/components/lark-tab.tsx`:

| 文件 | 改动 |
|---|---|
| `packages/core/types/octo.ts` | `OctoInstallation`、`ListOctoInstallationsResponse` 等(`parseWithFallback` + zod) |
| `packages/core/api/client.ts` | `listOctoInstallations`/`createOctoInstallation`/`deleteOctoInstallation`/`redeemOctoBindingToken` |
| `packages/core/octo/queries.ts` | TanStack Query keys + options |
| `packages/views/settings/components/octo-tab.tsx` | 设置页:列已连 bot、贴 token 配置、解绑(admin/owner) |
| `packages/views/agents/components/tabs/integrations-tab.tsx` | agent 详情加 Octo 绑定入口 |

> 比飞书简单:无二维码弹窗,就是一个"贴 bot_token + 选 agent"的表单。

---

## PR 拆分与里程碑

| PR | 阶段 | 依赖 | 规模 | 可独立合并 |
|---|---|---|---|---|
| #1 | 4 DB | — | 中 | ✅ |
| #2 | 5 入站 | #1 | 大 | ✅(mock TaskService) |
| #3 | 6 出站 | #1 | 中 | ✅(mock 事件) |
| #4 | 7 绑定 | #1 | 小 | ✅ |
| #5 | 8 Hub | #1 | 中 | ✅(im 包已就绪) |
| #6 | 9 接入 + 3.5 | #2-5 | 中 | ✅(打通闭环) |
| #7 | 10 前端 | #6 | 中 | ✅ |
| (#0) | 2 im 包 + PoC | — | — | **建议先单独提,作为所有后续的基础** |

**关键里程碑**:PR #6 合并后即达成「Octo 用户 @bot → agent 干活 → 结果回 Octo」**MVP 闭环**(无前端,靠 API/CLI 配置 bot)。PR #7 补齐前端配置体验。

**总估**:后端约 8.5 人日 + 前端 1.5 人日 ≈ **10 人日**。阶段 5-8 并行可压到日历约 5-6 天。

---

## 几个已被真机 PoC 确认、需带进实现的细节

1. **message_id 是裸 int64**:`im.SendMessageResult` 已修复兼容,出站存 `octo_outbound_message` 时按字符串存。
2. **message_seq 在 DM 可能为 0**:`EditMessage` 已设计成 seq=0 时省略,服务端用 message_id 兜底——流式编辑安全。
3. **im_token == bot_token**(本环境):但仍按 register 返回的 `im_token` 用,不要假设恒等(其他部署可能不同)。
4. **owner_uid / owner_channel_id**:register 返回,可用于绑定提示的私聊兜底。
5. **群聊前提**:bot 必须先被拉进群(server 端 `group_member` 校验),需写进配置文档。
