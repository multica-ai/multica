# 任务：Channel 自然语言收敛到 Agent Turn

## 背景与目标

- Channel 当前产品方向已经明确：除 slash/显式命令外，普通自然语言消息应该交给 channel agent turn 处理。
- 代码中仍保留 `server/internal/channel/intent` 这条旧抽象，里面混杂了 slash/rule 解析、旧 chat intent planner、agent turn prompt、intent/action 类型。
- 这会让自然语言路径继续受旧 intent 思维影响：例如“关掉吧”后系统反问候选 issue，用户再回 `sta-82` 时，下一轮只有实体上下文，没有 pending action，于是容易退化成查询状态。
- 本任务目标是彻底切断旧 intent 对自然语言 channel turn 的影响：
  - slash/command 保留确定性解析。
  - 普通自然语言只走 channel agent turn。
  - 多轮澄清保存为 channel turn/pending action 状态，而不是靠 pattern 或 prompt 猜。
  - `intent` 包拆分/改名，避免继续把 agent turn 当 intent classifier 维护。

## 需求对齐记录

- 用户原始需求：把现在的 prompt 修正提交后，新开分支处理“除了 slash 命令，都走 agent”，彻底去掉旧 intent 的影响。
- 对齐后的理解：
  - 不是删除所有结构化动作。平台仍需要结构化 action/status/confirmation 来做权限、审计、幂等和 slash 命令。
  - 要删除的是普通自然语言进入旧 `IntentKind` 分类、rule fallback、pattern 补丁和 dispatcher 旧路径的影响。
  - Agent turn 仍可以执行 CLI mutation，但多轮澄清的 pending action 必须由 channel 层持久化，否则全走 agent 也会丢“上一轮想做什么”。

## 技术方案

### 1. 路由边界收敛

- `Runtime.resolveIntent` 保持一条清晰分支：
  - `/...` 或 `SourceCommand`：进入 deterministic command parser。
  - 普通自然语言：必须进入 `ChannelAgentTurnClient.StartAgentTurn`。
- 自然语言路径不再 fallback 到 rule resolver。channel agent 不可用时返回明确失败提示，而不是让旧规则“凑一下”。
- 测试锁定该边界：自然语言即使看起来匹配旧 pattern，也不能进入 `RuleResolver`。

### 2. 拆分旧 `intent` 包职责

目标包职责：

- `server/internal/channel/command`
  - slash/source command 的 parser。
  - 可以继续输出结构化 command/action。
  - 只服务确定性输入，不处理普通自然语言。

- `server/internal/channel/turn`
  - channel agent turn request、prompt、context signal、result parsing。
  - 不再暴露 `BuildChatIntentPrompt` 这类旧 planner 概念。

- `server/internal/channel/action`
  - 结构化动作类型、confirmation/action code、pending action 状态。
  - 供 command 和 turn 共用，但不等同于自然语言 intent classifier。

### 3. 移除旧 chat intent planner 影响

- 删除或隔离 `ChatIntentResolver`、`BuildChatIntentPrompt`、`ChannelTurnPlanner` 这条旧自然语言规划路径。
- `TaskBackedChatIntentClient` 只保留 channel agent turn 能力；旧 `StartIntent/ParseIntentResult` 若无外部调用则删除。
- daemon/task context 中的 `ChannelIntentPrompt` 只在确认仍有内部调用时保留；若无自然语言入口依赖则移除相关 capability 和测试。

### 4. 引入 pending clarification/action

- 当 agent turn 需要向用户追问关键参数时，channel 层保存结构化 pending state，例如：

```json
{
  "kind": "set_status",
  "params": { "status": "cancelled" },
  "missing": ["issue_key"],
  "candidates": ["STA-82", "AC-2", "AC-5", "AC-7", "AC-11"]
}
```

- 下一条同一 sender/conversation/thread 的短回复先尝试匹配 pending state。
- 如果用户回复唯一候选 issue key，则直接恢复原动作，不再重新让 agent 自由理解成查询。
- 多候选或不匹配时继续交给 agent turn 或追问，不猜。

### 5. Prompt 收敛为执行契约

- Channel turn prompt 只描述：
  - 如何读写 workspace。
  - 哪些操作允许/不允许。
  - 回复语言和输出格式。
  - 何时请求澄清并返回可持久化 pending state。
- 不再在 prompt 里堆某一种语言的 pattern。
- daemon runtime config 与 turn prompt 保持同一套语义边界，避免两层指令漂移。

## 里程碑

1. 里程碑一：路由边界测试与自然语言 fallback 移除
   - 验收标准：普通自然语言没有 channel agent 时返回失败提示，不进入 rule resolver；slash/source command 仍可走确定性解析。

2. 里程碑二：拆包并迁移命名
   - 验收标准：agent turn prompt/type 不再位于 `channel/intent`；slash command 与 action 类型边界清楚；编译通过。

3. 里程碑三：删除旧 chat intent planner
   - 验收标准：`BuildChatIntentPrompt`、旧 async intent task、自然语言 planner 测试被删除或迁移；没有普通自然语言入口依赖旧 planner。

4. 里程碑四：pending clarification/action 状态
   - 验收标准：“关掉吧”→“你想关哪个 issue？”→“sta-82” 能恢复为 `STA-82` status `cancelled`，不会变成查询状态。

5. 里程碑五：最终验证与审阅
   - 验收标准：`go test ./internal/channel/...` 通过；输出 What / Why / Impact / Delta；确认未触碰无关用户改动。

## 关键决策

- 决策点：是否完全删除结构化 intent/action？
- 选择：不删除结构化 action，只删除自然语言 intent classifier 影响。
- 原因：slash、权限、确认码、审计和幂等仍需要结构化动作；问题在于普通自然语言不该先被旧 classifier/pattern 截断。

- 决策点：channel agent 不可用时是否 fallback 到 rule resolver？
- 选择：不 fallback。
- 原因：fallback 会把自然语言重新拉回旧 intent 行为，产生“有时像 agent，有时像命令 parser”的不可预测体验。

- 决策点：多轮澄清靠 prompt 还是靠持久化状态？
- 选择：靠 pending clarification/action 状态。
- 原因：prompt 可以提醒 agent，但不能保证下一轮 task 继承上一轮动作语义；状态必须由 channel 层保存。

- 决策点：先大删包还是先切运行边界？
- 选择：先切运行边界，再拆包删除。
- 原因：边界测试能防止重构中旧路径继续漏进自然语言；直接删包风险大且难定位行为回归。

## 设计决策思考（强制书面回答）

### 1. 问题的本质是什么？

问题的本质不是某个中文 pattern 缺失，也不是 `intent` 这个包名难看，而是自然语言 channel turn 还没有成为唯一事实路径。旧 intent classifier、rule fallback、prompt 补丁和缺失的 pending action 状态共同导致系统在多轮对话里丢失“用户上一轮想执行什么”。因此根因修法必须从路由边界和状态模型下手。

### 2. 为什么这个方案是最优解？

替代方案一是继续补 prompt，让 agent 更懂“关掉/取消/close”。这能改善单轮消息，但无法解决用户下一轮只补 `sta-82` 的状态丢失。替代方案二是给 old intent 增加更多规则，这会与“自然语言走 agent”的产品方向相反。选定方案把自然语言入口统一交给 agent turn，同时把跨轮状态从 prompt 中提取为可持久化 pending action，是行为最稳定、长期技术债最少的方案。

### 3. 这个改动对现有架构有什么影响？

改动集中在 channel 子域内部。adapter 仍只负责平台事件标准化；runtime 负责路由和 turn 生命周期；command/parser 只处理 slash；agent turn 负责自然语言；action/pending state 负责结构化动作边界。issue/status/comment 等跨模块写操作继续通过现有 CLI/facade/API，不引入跨层调用。

### 4. 这是根治还是补丁？

这是根治。它不再为某一种语言或某个动词添加补丁，而是移除自然语言旧 intent 路径，并补上多轮澄清缺失的状态模型。prompt 仍会存在，但它是执行契约和兜底，不是业务状态的唯一载体。

### 5. 是否符合 DDD 分层原则？

符合。channel runtime 不直接访问 issue DAO；结构化 action 是 channel 与 issue/comment 能力之间的应用层契约；pending state 属于 channel turn 生命周期，不放到平台 adapter。拆包后每个包职责更单一，减少 `intent` 包同时承担 parser、planner、turn prompt 和动作类型的混杂。

### 6. 需求变化时，这个代码容易修改吗？

如果未来要支持 Slack/Teams，平台只要提供标准化 message/reply/quote/thread 信号，pending action 仍可复用。如果后续新增“改优先级”“指派给某人”等多轮澄清，只扩展 pending action schema 和 resolver，不需要再增加语言 pattern。如果未来完全迁移到 typed agent tool，也可以把 pending action 的执行端替换成 tool call，而不改 slash/turn 路由边界。

## 影响范围

- 预计修改：
  - `server/internal/channel/inbound/runtime.go`
  - `server/internal/channel/facadeimpl/chat_intent_client.go`
  - `server/internal/channel/intent/*`（拆分、迁移或删除）
  - `server/internal/channel/conversation/*`（pending state 持久化方案，如需扩展 turn/message metadata）
  - `server/internal/daemon/prompt.go`
  - `server/internal/daemon/execenv/runtime_config.go`
  - channel 相关测试
- 预计新增：
  - `server/internal/channel/command`
  - `server/internal/channel/turn`
  - `server/internal/channel/action`
  - pending clarification/action 测试
- 不应修改：
  - 前端 UI
  - Feishu adapter 的平台协议逻辑，除非发现它错误标记 slash/source command
  - issue/comment 底层业务模型
