# 进度：Channel 会话主模型重构

## 里程碑 1 / 5

### 本阶段实现内容

- 新增 `093_channel_message_model` migration：
  - 新建 `channel_processing_lock`，承接原 `channel_conversation.active_event_id / active_since` 的入站串行化职责。
  - 重塑 `channel_conversation` 为真实外部会话主表，增加 `id`、`conversation_type`、`external_thread_id`、`workspace_id`、`last_message_at` 等字段。
  - 新建 `channel_message`、`channel_message_entity_ref`、`channel_turn`，作为后续 quote/reply、agent handoff、短回复解析的主事实模型。
- 新增 `server/internal/channel/conversation` 包：
  - 提供 conversation/message/entity-ref/turn 的最小 Store 边界。
  - 仅做事实持久化，不做 intent 解析、issue 写入或 provider 发送。
- 调整入站队列锁逻辑：
  - `ConversationKey` 改为真实会话容器 key：群聊按 chat、私聊按 user、thread 按 thread。
  - 新增 `ProcessingKey` 保留原“群聊按 sender 串行”的处理粒度。
  - `AcceptEvent`、`ClaimNext`、active 清理逻辑切换到 `channel_processing_lock`。
- 新增 migration DDL 测试：
  - `TestMigration093DDL` 默认跳过。
  - 设置 `CHANNEL_MIGRATION_TEST_DATABASE_URL` 后，在独立 schema + transaction 中验证 093 up/down。

### 实现思路

- 这阶段先拆清职责：会话主表不再承担处理锁；处理锁不再冒充会话。
- 仍保留 `channel_inbound_event.conversation_key` 作为现有入站队列的 processing key，避免一次性改动完整 pipeline。
- 新 Store 只定义主事实模型的持久化边界，后续里程碑再逐步接入 inbound/outbound/runtime。

### 与 Plan 的差异

- 原 Plan 在里程碑一中保留了“改造现表还是新增处理锁表”的待定项；本阶段已明确选择：改造 `channel_conversation` + 新增 `channel_processing_lock`。
- 没有新增 `messagectx.Store`；显式 message id 反查将由 `channel_message.platform_message_id` 承担。

### 后续待优化

- `channel_inbound_event.conversation_key` 字段名现在实际代表 processing key，后续如果继续深化，可考虑新增 `processing_key` 字段并迁移命名。
- `channel_message` / `channel_turn` 已有 Store，但尚未接入 runtime/outbox，因此当前只是 schema 与持久化边界，不改变用户可见行为。
- 临时测试库的 `schema_migrations` 与实际表状态不一致，无法直接用 `cmd/migrate up` 跑到 093；已用独立 schema DDL 测试替代验证。

### 验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/conversation -run TestMigration093DDL -count=1` 通过。

### 下一步

- 里程碑二：入站消息落主模型。
  - 在 `AcceptEvent` 或 pre phase 为 inbound event 创建/关联 `channel_message`。
  - 保存 platform message id、quote/reply/thread、sender、workspace/chat/conversation。
  - 保持现有 pipeline 行为不变，让主模型先成为事实沉淀。

## 里程碑 2 / 5

### 本阶段实现内容

- `AcceptEvent` 同事务写入 `channel_message`：
  - 每个新 inbound event 先确保 `channel_conversation` 存在。
  - 插入 `channel_inbound_event` 后同步插入 inbound `channel_message`。
  - backpressure/rejected 事件也会保留 message fact，避免队列外事件丢失审计。
- quote/reply/thread 显式信号落主模型：
  - 保存 `platform_message_id`、`reply_to_platform_message_id`、`quoted_platform_message_id`、`thread_id`。
  - 如果被引用/回复的 platform message 已在 `channel_message` 中存在，则写入 `reply_to_message_id` / `quoted_message_id`。
- workspace 信息后补：
  - `SaveEvent`、`MarkQueued`、`MarkWaitingAgent`、`MarkWaitingUser` 在已有 chat binding context 后更新同一条 `channel_message.workspace_id` 和规范化后的 message body。
- 扩展 `conversation.Store`：
  - 支持事务内 Store（`NewTxStore`）。
  - 新增 `UpdateMessageForInboundEvent`，避免 inbound runtime 直接散落 message 表更新 SQL。
- 新增 DB 级测试：
  - `TestDBInboundEventStore_AcceptEventCreatesChannelMessage` 默认跳过。
  - 设置 `CHANNEL_MIGRATION_TEST_DATABASE_URL` 后，在独立 schema 中验证 inbound message 写入、conversation key / processing key 分离、quote/reply 反查和 workspace 后补。

### 实现思路

- 主事实创建放在 `AcceptEvent` 同一事务内，保证 inbound queue 和 `channel_message` 不会一个成功一个失败。
- message 的业务解释仍不在本阶段做：这里只沉淀事实和显式平台关系，不把“同意/重试”解释成 issue comment。
- quote/reply 的 UUID 关联是 opportunistic 的：能找到原 `platform_message_id` 就写强关联，找不到也保留 platform id，供后续降级解析。

### 与 Plan 的差异

- Plan 写的是 “`AcceptEvent` 或 pre phase” 创建/关联 message；本阶段选择 `AcceptEvent`，原因是它和 inbound event 插入处于同一事务，事实一致性最好。
- workspace 在 `AcceptEvent` 时通常还未知，因此采用后补；这符合现有 pipeline 中 chat binding context 的产生时机。

### 后续待优化

- `sender_user_id` 目前还未从 `channel_user_binding` 回填；短回复解析 V1 可先依赖 sender external id + authz，后续再补用户强关联。
- recall event 已按 system message 分类并避免占用原 `platform_message_id`；后续出站消息落库后，可进一步把 recall 标注关联到原消息。
- `channel_inbound_event.conversation_key` 仍保存 processing key，命名收敛留给后续统一迁移。

### 验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/inbound -run TestDBInboundEventStore_AcceptEventCreatesChannelMessage -count=1` 通过。

### 下一步

- 里程碑三：出站/Agent/通知消息落主模型。
  - `ChannelReplySink` 保留 `port.SendResult.PlatformMessageID` 并写入 outbound `channel_message`。
  - Dispatch 回复、失败提示、出站通知统一沉淀 message/entity refs。
  - 为后续“用户引用 bot/agent 消息说同意/重试”提供可反查的原消息上下文。

## 里程碑 3 / 5

### 本阶段实现内容

- `GatewayReplySink` 从“只发送”升级为“发送成功后记录消息事实”：
  - 保留 provider 返回的 `port.SendResult.PlatformMessageID`。
  - 为文本回复、rich/card 回复写入 outbound `channel_message`。
  - 关联原 inbound message：写入 `reply_to_platform_message_id` / `reply_to_message_id`。
  - 对“失败 / Error / 重试 / 审批 / PASS / 继续”等回复写入 `handoff_kind` 和 `suggested_actions`。
- 出站通知 outbox 写入主模型：
  - `RetrySender.SendCard` 返回 `port.SendResult`，outbox worker 发送成功后调用 recorder。
  - 新增 `outbound.ConversationMessageRecorder`，将已发送通知写入 notification 类型的 `channel_message`。
  - 为通知消息写入 issue、comment、inbox item、agent actor 等 `channel_message_entity_ref`。
- 扩展 `channel_outbound_notification` 的上下文字段：
  - `actor_type`
  - `actor_id`
  - `source_comment_id`
  - 这样 agent 评论同步到群里后，用户引用回复时能知道背后的 agent/comment。

### 实现思路

- 发送成功后的记录放在 reply sink / outbox worker 边界，而不是散落到 dispatcher、runtime、各 step 中。
- 记录失败不触发重发，避免 provider 已经发出消息但写库失败时造成重复群消息。
- 聚合多条 outbox 通知时只写一条 `channel_message`，通过多个 entity refs 表示它关联的业务实体；单条通知则额外写 `outbound_notification_id`。

### 与 Plan 的差异

- Plan 没明确要求扩展 `channel_outbound_notification`；实现时发现如果不保留 actor/comment 来源，后续无法把“同意/重试”准确 @ 回对应 agent，因此在 093 migration 中补充了 `actor_type / actor_id / source_comment_id`。
- `ChannelReplySink` 的接口仍保持返回 `error`，没有把 `SendResult` 扩散给所有调用方；PlatformMessageID 在 sink 内被持久化，降低调用面改动。

### 后续待优化

- 当前 agent mention label 在 outbox recorder 中保守使用 `Agent`，后续可在 recorder 里通过 agent id 补真实 agent name。
- 出站通知聚合多条时，平台 message id 只能对应聚合后的 channel message；如果未来要逐条审计 sent platform id，需要给 outbox 增加聚合批次模型。

### 验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- 临时 PostgreSQL 独立 schema：`TestMigration093DDL` 通过。
- 临时 PostgreSQL 独立 schema：`TestDBInboundEventStore_AcceptEventCreatesChannelMessage` 通过。

## 里程碑 4 / 5

### 本阶段实现内容

- Runtime 接入 `ConversationStore`：
  - 可按 `reply_to / quote` 的 platform message id 反查 `channel_message`。
  - 可读取该消息的 `channel_message_entity_ref`。
  - 可在没有显式 quote/reply 时，只在同一 conversation/thread 近期恰好一个 handoff message 的情况下使用时序上下文。
- 实现短回复解析：
  - “同意 / OK / 可以 / 通过 / 批准” → `AddComment`。
  - “继续 / 继续推进” → `AddComment`。
  - “重试 / retry / 再试一次 / 再跑一次” → `AddComment`，内容规范化为“重试一次”。
  - 如果目标 message 有 agent entity ref，自动追加 `[@Agent](mention://agent/<id>)`，让 issue comment 触发对应 agent。
- 增加 channel turn 事实记录：
  - Runtime 在 intent 确认后 `UpsertTurn`。
  - reply sink 成功记录 outbound message 后，通过 inbound event id 完成对应 turn。
- 新增单元测试：
  - 引用 handoff message 回复“重试”会生成 `AddComment`，并携带 issue key + agent mention。
  - 普通群聊中单独说 “OK” 不会被自动解释为 issue comment。

### 实现思路

- 显式平台信号优先：quote/reply 指向的 `platform_message_id` 是第一事实来源。
- 无显式信号时只允许“唯一近期 handoff”作为降级；多个候选或无候选时不猜。
- V1 不直接调用 agent retry API；按已对齐的产品策略，先写 issue comment + agent mention，保持审计和现有任务触发链路一致。

### 与 Plan 的差异

- “重试”没有直接触发 agent retry API，而是落为 issue comment + agent mention；这与 Plan 中的 V1 选择一致。
- `conversationctx` 仍保留为旧流程兼容，但显式 quote/reply 的解析已改为基于 `channel_message` 和 `channel_message_entity_ref`。

### 后续待优化

- 可把短回复 action 识别从 runtime 中抽出为独立 resolver，以便后续加入更多 handoff action。
- 当平台 quote/reply 不传 platform message id 时，仍只能退化到唯一近期 handoff；弱平台体验需要后续按平台能力补齐。

### 验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。

## 里程碑 5 / 5

### 本阶段实现内容

- 主模型链路已经闭合：
  - inbound event → inbound `channel_message`
  - runtime intent/agent turn → `channel_turn`
  - dispatcher/runtime/provider reply → outbound `channel_message`
  - event bus/outbox notification → notification `channel_message`
  - message → issue/agent/comment/inbox entity refs
  - short reply → 基于 message/entity refs 转成 `AddComment`
- `conversationctx` / `replyctx` 保持兼容：
  - 不删除 P0 缓存层，避免扩大重构风险。
  - 新的显式上下文优先走 `channel_message`，时序上下文仍能由现有 `conversationctx` 辅助。
- 完成测试和 migration 校验：
  - 本地 channel 全包。
  - 本地 server 装配。
  - 临时 PostgreSQL 独立 schema migration up/down。

### 实现思路

- 这次没有继续新增能力 Store，而是让 message/turn/entity ref 成为主事实来源。
- 兼容层只保留现有行为，不再作为新增上下文能力的扩展点。
- 所有用户可见写入仍走现有 facade/dispatcher，不跨层直接写 issue/comment 业务表。

### 与 Plan 的差异

- 按用户最新要求，本次没有在里程碑 3/4 之间暂停确认，而是连续推进到最终收口。
- 最终没有删除 `conversationctx`，而是改为“显式上下文走主模型，P0 时序上下文继续兼容”；这是为了避免在同一分支内同时替换缓存语义和主流程语义。

### 后续待优化

- 为 outbox agent refs 补真实 agent display name。
- 将 `channel_inbound_event.conversation_key` 字段改名或迁移为 `processing_key`，彻底消除命名歧义。
- 后续如果已有稳定 agent retry/continue API，可以把 comment+mention 升级为直接 action，同时仍写审计 comment。

### 最终验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/conversation -run TestMigration093DDL -count=1` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/inbound -run TestDBInboundEventStore_AcceptEventCreatesChannelMessage -count=1` 通过。

## 审阅修复

### 本阶段实现内容

- 收紧短回复上下文解析：
  - 显式 `quote/reply` 平台消息 ID 查不到时，不再 fallback 到最近 handoff，避免把用户明确引用的回复落到错误 issue。
  - 只有 outbound 的 handoff message（bot / agent / notification）才能作为“同意 / 继续 / 重试”的目标，普通含 issue key 的聊天消息不会触发写评论。
- 修正 agent ref 语义：
  - outbox 通知正文中显式 `mention://agent/<id>` 的 agent 记录为 `handoff_target`。
  - 事件 actor agent 只记录为 `source`，不再默认当作用户短回复需要 @ 的 agent。
- 补齐 conversation 主表 workspace：
  - inbound message 在绑定 workspace 后，同步回写对应 `channel_conversation.workspace_id`。
  - outbox notification 创建 conversation 时携带聚合后的 workspace id。
- 补齐 turn 失败态：
  - runtime 处理失败、dead、channel turn 超时/失败/空回复时，更新 `channel_turn` 为 `failed` 或 `dead` 并记录错误信息。
- 补充回归测试：
  - 显式引用失配不 fallback。
  - 非 handoff message 不触发短回复解析。
  - actor agent 与显式 mentioned agent 的 entity ref role 区分。
  - conversation workspace 随 inbound workspace 绑定回写。

### 验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- `git diff --check` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/conversation -run TestMigration093DDL -count=1` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/inbound -run TestDBInboundEventStore_AcceptEventCreatesChannelMessage -count=1` 通过。

## PR 前收口

### 本阶段实现内容

- 将 channel message model migration 顺延到 `093` 后的测试名、错误信息和进度记录统一同步为 `093`。
- 确认仓库中不再残留旧 migration 文件名或旧 DDL 测试名引用。

### 最终验证

- `go test ./internal/channel/...` 通过。
- `go test ./cmd/server` 通过。
- `go test ./...`（server）通过。
- `pnpm test` 通过。
- `git diff --check` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/conversation -run TestMigration093DDL -count=1` 通过。
- `CHANNEL_MIGRATION_TEST_DATABASE_URL=... go test ./internal/channel/inbound -run TestDBInboundEventStore_AcceptEventCreatesChannelMessage -count=1` 通过。
- `make test` 未执行完成：该命令会读取当前 `.env` 并对远程 `multica` 数据库执行迁移，风险范围超过本次临时 `test_multica` 验证库。
