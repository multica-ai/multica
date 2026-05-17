# 任务：Channel 上下文指代闭环

## 背景与目标

- Channel/群聊场景中，用户经常通过“这个”“它”“刚才那个”等表达引用最近出现的 issue，或者直接回复/引用机器人消息继续操作。
- 当前代码已经有部分字段、prompt 和 fake store 骨架，但生产链路没有闭环：上下文没有真正持久化读取，运行时没有注入真实 ContextEntities，部分写入路径会绕过 dispatcher，旧 replyctx 迁移与代码也存在不一致。
- 最终目标是让 Feishu 群/频道里的自然语言连续操作可用：用户提到一个 issue 后，后续在同一聊天/线程/用户 scope 内可以通过指代完成查询、状态变更、分配等动作。

## 需求对齐记录

- 用户原始需求：
  - 查看 `scripts/deploy/out/design.md` 的设计是否合理。
  - 判断当前代码实现到哪里、缺什么、已实现部分有哪些问题。
  - 在需求理解对齐后开始实现。
- 对齐后的理解：
  - 不是只修单点 bug，而是把 Channel 上下文支持做成可上线的 P0 闭环。
  - P0 范围包括平台信号标准化、channel-level context 持久化、Runtime 读取注入、回复结果写回、生产 wiring、相关测试和已知迁移不一致修复。
  - P1 的 message-level context 可以先留出边界，但不能阻塞 P0；如果实现中发现 P0 无法可靠支持 reply/quote，需要暂停重新确认范围。

## 技术方案

- 整体架构：
  - Adapter 层只负责把 Feishu 的 reply、quote、thread 信号标准化为 `port.InboundEvent` 字段。
  - `conversationctx.Store` 作为 Channel 上下文仓储接口，生产实现落 PostgreSQL，测试使用 fake store。
  - Runtime 在构造 intent/agent prompt 前读取 `conversationctx.Store`，把最近实体与显式 quote/reply/thread 信号注入 `IntentRequest`。
  - 执行结果或机器人回复中出现 issue key 后，写回同一 scope 的 conversation context。
  - 指代消解必须发生在 Authz/业务执行之前，避免缺少 `issue_key` 时被提前拒绝。
- 关键模块划分：
  - `server/internal/channel/conversationctx`：上下文实体提取、scope、DB store、fake store。
  - `server/internal/channel/inbound`：Runtime 读取上下文，Dispatcher/Agent turn 写回上下文，生产配置参数传递。
  - `server/internal/channel/intent`：ContextEntities、quote/reply/thread 信号进入 prompt。
  - `server/internal/channel/adapter/feishu`：平台信号解析与标准化。
  - `server/cmd/server` 与 `server/internal/channel/manager`：生产依赖注入与默认 TTL/max entities。
  - `server/migrations`：修复或补齐上下文表与 replyctx 迁移对应代码。

## 里程碑

1. 里程碑一：上下文仓储生产化 → `conversationctx.DBStore` 完整实现 `Get`、`Upsert`、`AppendEntities`、`DeleteExpired`，单元测试覆盖排序、去重、TTL、scope 隔离。
2. 里程碑二：Runtime 读取与指代前置 → intent/agent 构造前读取 conversation context，保证 Authz 前可拿到可解析目标；测试覆盖 group/thread/direct 的上下文注入。
3. 里程碑三：写回链路与生产 wiring → dispatcher 和 agent turn 回复均能写回 context，server/manager 配置真实 store、TTL、max entities；修复 replyctx chat_id 迁移与 store 不一致。
4. 里程碑四：验证与收尾 → 修复 issue key 提取大小写问题，补齐关键回归测试，运行 channel 相关 Go tests，更新 progress.md 和最终审阅摘要。

## 关键决策

- 决策点：上下文存储放在 Channel 层还是业务 issue 层？
- 选择：放在 Channel 层。
- 原因：上下文 scope 来自 connection/chat/thread/sender，属于入口通道语义；issue 层不应理解 Feishu reply/quote/thread。

- 决策点：P0 是否实现 message-level context？
- 选择：P0 不完整实现 messagectx，但保留接口边界；优先完成 channel-level 闭环。
- 原因：当前最大缺口是生产链路没有读写闭环。messagectx 需要平台消息 ID 精确关联，会牵涉 ReplySink 返回值和更多迁移，适合作为 P1。

- 决策点：replyctx 的 chat_id 迁移不一致是否纳入本次？
- 选择：纳入。
- 原因：它会影响当前已存在的 direct reply context，并且迁移后 `ON CONFLICT` 与查询条件不匹配，属于已实现部分的生产风险。

- 决策点：上下文写回放在 Dispatcher 还是 Runtime？
- 选择：保留 Dispatcher 写回，同时补齐 agent turn 绕过 dispatcher 的路径。
- 原因：现有代码已经有 dispatcher 写回骨架，但 agent turn 是真实主路径之一；只修 dispatcher 会造成行为不一致。

## 设计决策思考（强制书面回答）

### 1. 问题的本质是什么？

问题本质不是“给 prompt 加几行最近消息”，而是 Channel 入口缺少一个稳定的上下文状态模型。Feishu 群聊、线程、引用和回复是平台信号；用户意图解析、权限校验和 issue 操作是业务链路。当前缺的是把平台信号转换成业务可用实体上下文，并在正确时机注入，让后续动作能在不显式重复 issue key 的情况下仍然可审计、可测试、可控。

### 2. 为什么这个方案是最优解？

替代方案一是在 prompt 中直接塞最近文本。这实现快，但不可控，无法按 workspace/chat/thread/sender 隔离，也难以做 TTL、去重和权限前置。替代方案二是把上下文放到 issue/service 层。这会让业务层理解通道平台语义，破坏职责边界。当前方案把 scope、TTL、实体提取放在 Channel 层，业务层继续只处理明确实体，是职责边界和生产可维护性之间更稳的选择。

### 3. 这个改动对现有架构有什么影响？

改动主要集中在 `server/internal/channel` 内部、server 启动 wiring 和 migrations，不跨越到 issue internal 模块。Channel adapter 仍只做标准化，Runtime/Dispatcher 负责编排，Store 通过接口注入，测试可以继续使用 fake store。需要注意的是 Authz 前置链路，如果发现现有 Dispatcher/Runtime 的意图解析顺序无法满足，需要在 Channel inbound 内部调整流程，而不是把权限逻辑下沉到 prompt 或仓储。

### 4. 这是根治还是补丁？

这是根治当前 Channel 指代能力缺失的基础问题：建立可持久化、可隔离、可过期、可测试的上下文状态，并将其接入读写闭环。单纯修 prompt、只写 fake store、或者只在 dispatcher 中追加一次实体都属于补丁；本任务要求生产 DB store、Runtime 读取、写回路径和 wiring 一起闭合。

### 5. 是否符合 DDD 分层原则？

符合。上下文属于 Channel inbound 子域内部能力，不让 HTTP handler、issue service 或 adapter 直接承担跨层职责。DB 访问收敛在 `conversationctx.DBStore`，Runtime/Dispatcher 只依赖 Store 抽象，adapter 不接触数据库，业务执行仍通过现有 intent/dispatcher 路径。迁移修复只改变仓储与表结构契约，不引入跨模块 internal 调用。

### 6. 需求变化时，这个代码容易修改吗？

如果后续要支持 Slack、Discord 或更多平台，只需要 adapter 继续填充标准 `InboundEvent` 字段，conversation context 的 scope 和 Runtime 注入逻辑可复用。如果 P1 要支持 message-level 精确回复，可以新增 `messagectx.Store` 并在 Runtime 的上下文装配阶段合并优先级，不需要推翻 channel-level store。如果上下文实体从 issue 扩展到 project/member，也可以扩展 `EntityType` 与提取器，而不改变整体链路。

## 影响范围

- 预计修改文件：
  - `server/internal/channel/conversationctx/store.go`
  - `server/internal/channel/conversationctx/extract.go`
  - `server/internal/channel/conversationctx/store_test.go`
  - `server/internal/channel/inbound/runtime.go`
  - `server/internal/channel/inbound/dispatcher.go`
  - `server/internal/channel/inbound/*_test.go`
  - `server/internal/channel/intent/resolver.go`
  - `server/internal/channel/replyctx/store.go`
  - `server/cmd/server/channel_pipeline.go`
  - `server/internal/channel/manager/manager.go`
  - `server/migrations/091_channel_conversation_context.up.sql`
  - `server/migrations/091_channel_reply_context_chat.up.sql`
- 可能新增文件：
  - conversation context DB 行为测试或 helper。
  - `docs/plans/channel/feature-context-resolution/progress.md`
- 对外部模块的影响：
  - 不改变公开 HTTP API。
  - 不改变 Feishu webhook 入站协议，只增强内部事件标准化后的使用。
  - 数据库迁移与上下文表会影响本地/CI migration。

## 验收标准

- `cd server && go test ./internal/channel/...` 通过。
- `conversationctx.DBStore` 不再返回 `not implemented`。
- 群聊/频道中，用户先提到 `STA-12`，再说“把它改成 done”时，Runtime 能在 intent 阶段拿到 `STA-12` 的 context entity。
- 引用或回复包含 issue key 的消息时，显式 quote/reply 信号优先于普通最近上下文。
- 同 connection 下不同 workspace、chat、sender、thread 之间上下文隔离。
- 已过期上下文不会参与指代消解。
- `replyctx` 的 chat_id 迁移和 store 代码一致，不再出现 conflict target 或跨 chat 误命中问题。
