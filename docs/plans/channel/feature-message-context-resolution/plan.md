# 任务：Channel 会话主模型重构

## 背景与目标

- 当前 channel 主流程已经能处理飞书入站消息、intent、authz、dispatch、agent turn、出站通知和 P0 的 conversation context。
- 但现有模型更像“入站事件队列 + issue intent/dispatch + 出站通知 outbox”，还不是面向长期产品方向的会话系统。
- `channel_conversation` 目前主要用于同一 conversation key 的 active inbound event 串行化，不是完整的会话主表。
- `channel_inbound_event` 同时承担入站事件日志、处理队列、phase 状态机和 dispatch reply checkpoint，不适合作为长期 channel message 主模型。
- P0 的 `conversationctx` 是战术补洞；如果继续新增 `messagectx.Store`，会把 channel 推向多个能力 Store 拼装，不利于长期维护。

本任务目标是把 V1 从“补 messagectx”改成“建立 channel 会话主模型”，支撑 Agent-to-human handoff：

- Agent 在 issue 中产生的重要消息可以同步到群里。
- 用户可以用“同意”“继续”“重试”“OK @Agent”这类短回复推进对应 issue/agent。
- quote/reply/thread 等平台显式信号能精确定位到被回复的 channel message。
- 系统能把短回复转换为明确的 issue comment / agent mention / 后续动作，而不是要求用户说完整命令。
- 未来 Slack、Teams、Discord 等平台只需要标准化事件和能力降级，不在适配器里维护业务上下文。

## 需求对齐记录

- 用户原始需求：继续完成 Channel 上下文能力，但不希望在现有技术债上继续缝补；长期要把 channel 演进成人与 agent 沟通的渠道。
- 对齐后的理解：
  - Channel V1 不是“在群里复制完整 Multica”，也不是“把每个 agent 都真实拉进群”。
  - V1 是“Multica bot 作为 workspace 会话入口，把 agent 工作回路带到群里，让用户用最短上下文回复推动 issue/agent 继续前进”。
  - 群里的每句话首先是 channel message，只有具备明确业务语义时才投影为 issue comment、issue mutation、agent retry/continue 等动作。

## 第一版用户故事

1. 作为 workspace 成员，我希望 agent 在 issue 中产生的重要评论、任务完成、任务失败、审批请求、需要用户决策的消息能同步到绑定群里。
2. 作为用户，我希望群里的 agent 消息能显示它来自哪个 issue、哪个 agent、当前状态是什么、需要我做什么。
3. 作为用户，当 agent 说“同意的话我继续推进”时，我直接回复“同意 / OK / 可以 / 继续”，系统应理解为对这条 agent 消息的确认。
4. 作为用户，当我引用或回复这条 agent 消息说“同意”时，系统应知道这句话对应的是原消息背后的 issue 和 agent。
5. 作为用户，我不想写“给 STA-123 新增评论：同意，并 @Orion”；我只想说“同意”，系统自动在对应 issue 下新增评论，并 @ 对应 agent。
6. 作为用户，当 agent 任务失败并提示可重试时，我回复“重试 / retry / 再试一次”，系统应自动在对应 issue 下新增类似“重试一次 @对应 agent”的评论或触发对应后续动作。
7. 作为用户，当 ReviewBot 完成 review 并等待我批准进入 QA-Bot 验收时，我回复“OK @Orion”或只回复“OK”，系统应把它转换成对对应 issue/agent 的有效回复，而不是普通聊天。
8. 作为用户，如果我引用一条 agent 消息回复“同意”，系统必须优先使用被引用消息的上下文，而不是最近聊天里提到的其他 issue。
9. 作为用户，如果平台不支持 quote/reply/thread，系统可以退化到最近上下文，但多个候选时不能猜。
10. 作为用户，我在群里普通聊天不会自动变成 issue comment。
11. 作为用户，只有当我的消息明确回复/引用了一个 issue/agent 相关消息，或明确表达“评论到某个 issue”，系统才应该写 issue comment。
12. 作为系统维护者，我希望每条用户消息、Multica 回复、agent 回复、出站通知都被记录成统一 channel message。
13. 作为系统维护者，我希望每条 channel message 能记录它关联的 issue、agent、agent task、issue comment、推荐用户动作等结构化上下文。
14. 作为系统维护者，我希望一次用户输入到系统/agent 回复之间有 channel turn 记录，能追踪 intent、authz、dispatch、agent task 和最终回复。

## 明确非目标

- 不把每个 agent 都作为真实群成员拉进群；V1 可以由 Multica bot 代发，但消息模型必须知道该消息代表哪个 agent。
- 不做完整多 agent 群聊编排。
- 不把所有群聊自动写成 issue comment。
- 不在 channel 内复制完整项目管理 UI、筛选、批量编辑、看板能力。
- 不做历史聊天导入。
- 不做长期记忆、个人画像或跨 channel 上下文同步。
- 不要求所有平台能力一致；弱平台可以降级到文本和时序上下文。

## 技术方案

### 核心模型

新增或重塑四类主模型：

1. `channel_conversation`
   - 表示外部聊天空间中的会话容器。
   - direct/group/thread 都应落到同一套模型。
   - 与当前 active event 锁职责解耦，避免“会话主表”和“处理锁”混在一起。

2. `channel_message`
   - 统一记录 inbound、outbound、agent、system、notification 消息。
   - 保存 platform message id、reply/quote/thread 关系、text/body、发送者类型、代表的 agent、所属 workspace/chat/conversation。
   - 是 quote/reply 精确定位的事实来源。

3. `channel_message_entity_ref`
   - 记录一条 message 关联的业务实体。
   - V1 先支持 issue 和 agent；保留扩展 project / PR / task / comment 的类型空间。
   - 用于显式上下文、时序上下文、后续搜索和审计。

4. `channel_turn`
   - 记录一次用户消息到系统/agent 回复的处理闭环。
   - 关联 inbound message、outbound message、intent、authz 结果、dispatch 结果、agent task、状态和错误。
   - 替代把所有 checkpoint 都塞进 `channel_inbound_event` 的趋势。

### 与现有模型关系

- `channel_inbound_event` 短期保留，继续承担入站可靠队列和 phase 状态机；但新增 message/turn 后，不再把它当长期消息主表。
- `channel_outbound_notification` 短期保留，继续承担出站通知 outbox；发送成功后补写 `channel_message` 和 entity refs。
- `conversationctx` 短期保留为 P0 兼容缓存；后续可从 `channel_message_entity_ref` 聚合或替代。
- `replyctx` 短期保留 direct chat 兼容；后续由 reply/quote message relation 和 turn state 吸收。
- 不新增 `messagectx.Store`；显式 message id 反查直接基于 `channel_message.platform_message_id` 和 `channel_message_entity_ref`。

### Agent-to-human handoff 语义

V1 需要在 channel message 上表达“这条 agent 消息期待用户做什么”。建议引入结构化字段或 JSONB：

- `handoff_kind`：`approval` / `retry` / `continue` / `need_input` / `review_pass` / `failure` / `none`
- `suggested_actions`：如 `approve`、`retry`、`continue`、`comment`
- `agent_id` / `agent_name`
- `issue_id` / `issue_identifier`
- `source_comment_id` 或 `agent_task_id`

用户短回复进入 Runtime 时，解析优先级：

1. quote/reply 指向的 `channel_message`。
2. 当前 thread 内最近待响应 handoff message。
3. 当前 chat/sender scope 的最近上下文。
4. 无明确上下文则反问，不自动猜。

当上下文明确时，短回复可转换为：

- 写 issue comment：例如“同意 @Orion”“重试一次 @ReviewBot”。
- 触发 agent continue/retry：如果已有明确 agent task/action 接口。
- 若 V1 还没有直接 retry API，则先落为 issue comment + mention，保持行为可审计。

## 里程碑

1. 里程碑一：主模型设计与 migration
   - 输出并实现 `channel_message`、`channel_message_entity_ref`、`channel_turn` 的最小 schema。
   - 将 `channel_conversation` 改造为真实会话主表，新增 `channel_processing_lock` 承接 active event。
   - 验收：migration 可执行/回滚；现有入站队列行为不变。

2. 里程碑二：入站消息落主模型
   - `AcceptEvent` 或 pre phase 为每条 inbound event 创建/关联 `channel_message`。
   - 保存平台 message id、quote/reply/thread、sender、workspace/chat/conversation。
   - 验收：用户消息在主模型中可查；现有 pipeline 测试通过。

3. 里程碑三：出站/Agent/通知消息落主模型
   - `ChannelReplySink` 保留 `port.SendResult.PlatformMessageID` 并写入 outbound `channel_message`。
   - Dispatch 回复、channel agent turn 回复、失败提示、出站通知都落同一套 message/entity refs。
   - 验收：机器人发到群里的消息可按 platform message id 反查到 issue/agent 上下文。

4. 里程碑四：短回复和显式上下文解析
   - Runtime 通过 quote/reply message id 查 `channel_message` + entity refs + handoff metadata。
   - “同意 / OK / 继续 / 重试”在明确上下文下转换为 issue comment + agent mention 或后续动作。
   - 多候选或无上下文时反问。
   - 验收：截图里的 approval/retry 场景可用单词短回复完成。

5. 里程碑五：收敛 P0 上下文和最终审阅
   - 保持 `conversationctx` 兼容，同时让它的写入来源靠近 `channel_message_entity_ref`。
   - 更新 progress、测试、最终 What / Why / Impact / Delta。
   - 验收：`go test ./internal/channel/...` 与 `go test ./cmd/server` 通过。

## 关键决策

- 决策点：继续新增 `messagectx.Store` 还是重构主模型？
- 选择：重构主模型，不新增 `messagectx.Store`。
- 原因：显式上下文不是独立能力，而是 message 作为事实来源后自然具备的能力。继续新增 Store 会形成旁路状态。

- 决策点：群聊消息是否自动写 issue comment？
- 选择：不自动写。只有明确回复/引用 issue/agent handoff 消息，或用户明确要求评论到某 issue，才写 comment。
- 原因：聊天是协作界面，不是 issue comment 的镜像；自动全量写入会污染 issue 历史。

- 决策点：Agent 是否作为真实群成员存在？
- 选择：V1 不要求。可以由 Multica bot 代发，但 message 模型必须记录 represented agent。
- 原因：不同平台对多 bot、多身份、card 能力支持不同；业务语义不应依赖平台是否能拉真实 agent 入群。

- 决策点：`channel_conversation` 是否继续保留 active event 锁字段？
- 选择：不保留。新增 `channel_processing_lock` 专门负责入站处理串行化，`channel_conversation` 回到外部会话容器职责。
- 原因：会话主表和处理锁的生命周期、粒度、查询方向都不同；继续混在一起会让 message/turn 的归属模型长期不清晰。

- 决策点：短回复“重试”是否直接触发 agent retry？
- 选择：V1 优先生成明确 issue comment + @agent；如已有稳定 retry/continue API，再接直接动作。
- 原因：comment + mention 是最小闭环且可审计；直接 retry 需要更完整的 agent task/action 契约。

## 设计决策思考（强制书面回答）

### 1. 问题的本质是什么？

问题的本质不是 quote/reply 缺少一个 message id 映射表，而是 channel 层没有把“会话、消息、实体引用、用户响应、agent handoff”作为主流程事实沉淀下来。现有实现把入站事件当队列，把出站通知当 outbox，把上下文当附属 Store，因此用户短回复无法稳定映射到“我正在回应哪条 agent 消息、哪个 issue、哪个 agent、期待什么动作”。V1 应该先补主模型，再在这个模型上实现上下文解析。

### 2. 为什么这个方案是最优解？

替代方案一是继续实现 `messagectx.Store`，成本最低，但会制造新的旁路读模型。替代方案二是把逻辑塞进飞书适配器，能快速满足截图场景，但平台适配器会承担业务上下文，后续 Slack/Teams 重复实现。选定方案把会话消息模型放在 channel 核心层，短期改动更大，但能同时承接入站、出站、通知、agent handoff 和未来多平台能力降级，是长期收益最大的方向。

### 3. 这个改动对现有架构有什么影响？

它会把 channel 从“pipeline + store 拼接”推进到更明确的领域模型：adapter 仍只做标准化，Runtime/Dispatcher 仍编排流程，新增 message/turn/entity 存储作为 channel 核心事实来源。现有 `channel_inbound_event` 和 `channel_outbound_notification` 先保留，不一次性推翻可靠队列；新模型先旁路写入并逐步成为读取来源，降低风险。

### 4. 这是根治还是补丁？

这是根治方向。P0 的 `conversationctx` 解决了最近 issue 指代，但它不是完整事实来源。本方案把事实来源前移到每条 channel message 和其实体引用上，quote/reply、短回复、上下文、通知引用都能基于同一套数据解析，而不是为每个能力继续新增 Store。

### 5. 是否符合 DDD 分层原则？

当前 channel 模块不是典型 `system/<module>/internal/{app,service,dao}` 结构，而是现有项目内的 pipeline/port/facade 架构。本方案仍遵守边界：平台 adapter 不写业务上下文；Runtime/Dispatcher 依赖抽象存储接口；数据库细节封装在 channel 内部 store；跨 issue/agent 能力继续走 facade/API，不直接跨模块 internal。

### 6. 需求变化时，这个代码容易修改吗？

如果后续要支持真实多 agent 入群，只需把 represented agent 从 metadata 升级为 participant 关系；如果要支持 PR/project，只扩展 entity ref 类型；如果某个平台不支持 quote/reply/thread，则同一套模型保留空关系并退化到时序上下文。主模型稳定后，新能力是添加 message 类型、entity 类型和 handoff kind，而不是继续新增能力专用 Store。

## 影响范围

- 新增或重塑：
  - `channel_processing_lock`
  - `channel_message`
  - `channel_message_entity_ref`
  - `channel_turn`
  - 调整 `channel_conversation` 职责为外部聊天会话主表
- 修改：
  - `server/internal/channel/inbound/runtime_store.go`
  - `server/internal/channel/inbound/runtime.go`
  - `server/internal/channel/inbound/dispatcher.go`
  - `server/internal/channel/inbound/reply_sink.go`
  - `server/internal/channel/outbound/outbox.go`
  - `server/internal/channel/manager/manager.go`
  - `server/cmd/server/channel_pipeline.go`
  - channel 相关测试
- 保留兼容：
  - `channel_inbound_event` 继续作为可靠入站处理队列
  - `channel_outbound_notification` 继续作为通知 outbox
  - `conversationctx` / `replyctx` 短期保留，后续收敛
- 对外影响：
  - 不改变 HTTP API
  - 不要求 provider `port.Channel` 立刻改变
  - 对弱平台保持功能降级
