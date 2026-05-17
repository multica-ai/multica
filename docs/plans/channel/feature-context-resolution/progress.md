# 进度：Channel 上下文指代闭环

## 里程碑 1 / 总 4

### 本阶段实现内容

- 实现 `conversationctx.DBStore` 的 `Get`、`Upsert`、`AppendEntities`、`DeleteExpired`。
- `AppendEntities` 使用事务内 `SELECT ... FOR UPDATE` 合并实体，避免并发写入丢失最近 issue。
- 统一 DBStore 和 FakeStore 的实体合并规则：key 归一化为大写、按 key 去重、保留最新 `MentionedAt`、按时间倒序、按 `max` 截断。
- `ExtractEntityKeys` 支持小写 issue key 输入，并归一化为大写。
- 测试支持通过 `CHANNEL_CONTEXT_TEST_DATABASE_URL` 指定真实 PostgreSQL。
- 补充排序截断、去重更新时间、小写 key 提取等回归测试。

### 实现思路

- 仓储层直接对应 `channel_conversation_context` 的五维 scope 主键，避免 Runtime/Dispatcher 关心 JSONB 细节。
- 并发 append 采用“先插入空上下文占位，再锁定该 scope 行读取合并，最后更新”的方式，保证同一 scope 内不会互相覆盖。
- 合并逻辑收敛到 package 内 helper，FakeStore 与 DBStore 复用，减少测试与生产行为漂移。

### 与 Plan 的差异

- 原计划里大小写提取修复放在里程碑四；本阶段顺手完成，因为它与实体归一化属于同一处根因。
- 临时测试库缺少 `091_channel_conversation_context` 表，并且 `090_channel_integration` 已存在但未记录 migration；已仅对临时库补执行缺失的 091 migration 以完成 DBStore 验证。

### 后续待优化

- `DeleteExpired` 目前是 Store 能力，尚未接入定时 sweeper。
- Runtime 仍未读取 conversation context，这属于里程碑二。
- 生产 wiring 尚未注入 DBStore，这属于里程碑三。

### 验证

- `cd server && go test ./internal/channel/...` 通过。
- 使用临时 PostgreSQL 测试库运行 `go test -v ./internal/channel/conversationctx` 通过，DBStore 测试真实执行，覆盖 roundtrip、TTL、scope isolation、dedup、sort/trim、concurrent append。

### 下一步

- 进入里程碑二：在 Runtime 构造 intent/agent prompt 前读取 conversation context，并确保该上下文在 Authz 前可用于目标解析。

## 里程碑 2 / 总 4

### 本阶段实现内容

- `RuntimeConfig` 增加 `ConversationCtx` 与 `ContextMaxEntities`，默认最多读取 5 个上下文实体。
- Runtime 构造 `IntentRequest` 时读取 conversation context，并注入 `ContextEntities`、`ContextIssueKey`、`ContextMode`。
- quote 文本中的 issue key 会作为显式上下文优先进入请求；当 quote 中只有一个 issue key 时优先作为当前目标。
- 自然语言 channel agent turn 的 `StartAgentTurn` 请求现在携带 conversation context。
- 规则解析或异步 intent 结果缺少 `issue_key` 时，如果请求上下文能唯一确定 issue，会在保存到 post phase 前补齐目标，保证后续 authz/dispatch 看到的是完整 intent。
- 补充 Runtime 测试，覆盖 conversation context 读取、quote 优先级、唯一上下文补 target、多上下文不猜测、agent turn 请求注入、post phase 前补齐 `issue_key`。

### 实现思路

- 将 Runtime 内重复构造 `IntentRequest` 的代码收敛到 `buildIntentRequest`，让 resolve、fallback rule、resume intent 都走同一套上下文装配。
- 上下文 scope 使用 `connection_id + workspace_id + chat_id + sender_id + thread_id`，与 Store 的主键保持一致。
- 为了避免误操作，只有在上下文能唯一确定目标时才自动补 `issue_key`；如果有多个候选实体，则保留缺失状态，让后续 prompt/clarify 处理。

### 与 Plan 的差异

- 本阶段除了 prompt 注入，还额外补了规则/intent 结果的目标回填。原因是仅把 `ContextEntities` 放进 prompt 不能保证 authz 前一定有 `issue_key`，这会复现之前指出的 fail-closed 问题。
- P1 message-level context 仍未实现；本阶段只把 quote 文本中的 issue key 当作显式实体优先信号。

### 后续待优化

- 生产 wiring 尚未注入 `conversationctx.DBStore`，当前 Runtime 能读，但生产启动配置还没接上。
- Agent turn 回复写回 conversation context 仍未完成，下一阶段处理。
- `DeleteExpired` sweeper 仍未接入。

### 验证

- `cd server && go test ./internal/channel/inbound` 通过。
- `cd server && go test ./internal/channel/...` 通过。

### 下一步

- 进入里程碑三：把 DBStore 接入生产 wiring，并补齐 dispatcher 与 agent turn 回复写回链路，同时修复 replyctx chat_id 迁移与 store 不一致。

## 里程碑 3 / 总 4

### 本阶段实现内容

- `DispatchStep` 为 conversation context 的 `max entities` 与 `TTL` 补默认值，移除未接入 TODO。
- 生产入口创建 `conversationctx.DBStore`，并通过 `channel_pipeline -> manager -> RuntimeConfig / DispatchConfig` 注入 Runtime 与 Dispatcher。
- channel agent turn 回复发送成功后，会从回复文本提取 issue key，并按相同五维 scope 写回 conversation context。
- 修复旧 `replyctx.Store` 与 `091_channel_reply_context_chat` 迁移不一致的问题：`Context`、DBStore、InMemoryStore、调用点和测试都纳入 `chat_id` 维度。
- 补充测试覆盖 agent turn 回复写回 conversation context，以及 reply context 的 chat 维度隔离。

### 实现思路

- 写回逻辑仍以 `conversationctx.Store` 作为抽象边界，Runtime/Dispatcher 只负责提取回复中的实体与构造 scope，不直接关心 JSONB 或并发合并。
- 旧 reply context 的 `chat_id` 是数据模型主键的一部分，代码层必须同样显式携带，否则真实 DB 上 `ON CONFLICT` 目标不匹配，并且 direct chat 的历史上下文可能跨 chat 串用。
- Runtime 写回优先使用事件记录里的 `WorkspaceID`，缺失时再查 chat binding，保证 wait/resume 场景也能拿到稳定 workspace。

### 与 Plan 的差异

- 原计划只说修复 replyctx store 不一致；实际改动扩展到了 Store 接口签名和所有测试 fake。原因是只在 DBStore 内塞默认 `chat_id` 仍会保留跨 chat 串用的根因。

### 后续待优化

- `DeleteExpired` sweeper 仍未接入，这是里程碑四范围。
- 还需要最终全链路审阅，确认是否有未覆盖的 dispatcher rich reply / runtime fallback 边界。

### 验证

- `cd server && go test ./internal/channel/...` 通过。
- `cd server && go test ./cmd/server` 通过。

### 下一步

- 进入里程碑四：补最终清理与审阅，包括过期 context 清理、必要的集成/回归测试，以及最终 What / Why / Impact / Delta 总结。

## 里程碑 4 / 总 4

### 本阶段实现内容

- 将 conversation context 的过期清理接入 Runtime sweeper，周期性调用 `DeleteExpired` 清理已过期 scope。
- 抽出 `sweepOnce` 便于单元测试直接覆盖清理逻辑，避免测试依赖 30 秒 ticker。
- 补充过期 conversation context 被 sweeper 删除的回归测试。
- 完成最终 channel 相关 Go 测试与 server 入口 wiring 测试。

### 实现思路

- 清理仍然放在 Channel inbound runtime 内部，和 waiting_user 过期、stale processing requeue 共用现有 sweeper，不新增独立 goroutine。
- `DeleteExpired` 是 Store 抽象能力，Runtime 只传入当前时间，不理解表结构。
- 过期清理失败只记录错误，不影响入站消息处理主链路。

### 与 Plan 的差异

- issue key 大小写提取在里程碑一已经提前完成，本阶段不重复改动。
- 额外接入了 `DeleteExpired` sweeper，因为只实现仓储方法但不清理会让 P0 闭环留下可预见的长期数据堆积。

### 后续待优化

- P1 可以继续做 message-level context，用平台 message id 精确绑定机器人回复与用户引用。
- 可视运行情况后，再决定是否把 context TTL / max entities 暴露成环境变量；当前先使用代码默认值。

### 验证

- `cd server && go test ./internal/channel/...` 通过。
- `cd server && go test ./cmd/server` 通过。

### 最终审阅摘要

- What：完成 Channel 上下文实体的持久化、读取注入、唯一目标前置解析、回复写回、生产 wiring、过期清理与 replyctx chat_id 一致性修复。
- Why：让群聊/频道里“它”“这个”“刚才那个”能在同一 connection/workspace/chat/sender/thread scope 内稳定指向最近 issue，并在 Authz 前形成明确目标。
- Impact：不改变 HTTP API 和平台 webhook 协议；影响范围集中在 `server/internal/channel`、`server/cmd/server` 与计划文档。
- Delta：相比原实现，`conversationctx.DBStore` 不再是空实现，Runtime/Dispatcher/agent turn 构成读写闭环，旧 direct reply context 不再跨 chat 串用。

## 总体设计对齐补充

### 本阶段实现内容

- `IntentRequest` 补齐 `ExplicitEntities` 字段，与总体设计文档中的显式上下文契约对齐。
- Runtime 将 quoted text 中提取到的实体放入 `ExplicitEntities`，并保持显式实体优先于普通 conversation context。
- Prompt 新增 `Explicit context` 段落，明确显式上下文是最高优先级。
- 当显式上下文存在多个候选时，不再 fallback 到普通 context 自动补目标，避免 quote/reply 歧义下误操作。
- `replyctx` lookup 增加历史兼容 fallback：当前 `chat_id` 查不到时，可读取迁移前默认 `chat_id=''` 的未过期 direct reply context。

### 验证

- `cd server && go test ./internal/channel/intent ./internal/channel/inbound ./internal/channel/replyctx` 通过。
- `cd server && go test ./internal/channel/...` 通过。
- `cd server && go test ./cmd/server` 通过。

### 仍未实现

- P1 的 `messagectx` / `channel_message_entity_context` 仍未实现，`ReplyToMessageID` 和 `QuotedMessageID` 还不能通过 platform message id 精确反查实体。
- PR / project 类型实体提取仍未实现，目前 P0 只处理 issue key。
- 多候选消歧目前保持“不自动猜”，尚未做 deterministic 的候选问题模板。
