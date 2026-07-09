# 任务 Prompt 触发 Goal 模式

## 状态

待审阅草稿。

## 背景

此前讨论过在 Agent 配置上增加 `Execution mode: Normal / Goal`。该方案已回退，因此本设计不依赖 Agent 级别的 `execution_mode` 字段或页面配置。

本设计改为支持**任务级显式指令**：用户在任务 prompt 中使用固定 directive 触发本次任务的 Goal 模式。该能力只影响当前任务，不改变 Agent 的持久化配置，也不影响后续任务。

## 摘要

支持用户在任务 prompt 的开头写入：

```text
/goal
请完成这个后端改造，直到功能完成并验证通过。
```

系统在任务入队前解析该 directive：

- 识别到 `/goal` 时，将本次任务的执行模式持久化为 `goal`。
- 从实际发给模型的用户任务文本中移除 `/goal` directive。
- 未识别到 directive 时，保持默认 `normal`。
- 该模式随任务一起保存，daemon claim 后透传给 provider。

首批 provider 行为：

- Codex：在 `turn/start` 前调用 `thread/goal/set`。
- Claude Code：在发送给 Claude 的用户 prompt 前注入明确的 goal contract。

## 目标

- 用户可以仅通过任务 prompt 触发本次任务的 Goal 模式。
- Goal 是任务级 override，不改变 Agent 配置。
- 默认行为保持不变；没有 `/goal` 时仍为 Normal。
- 支持 Codex 和 Claude Code。
- 解析规则明确、可审计、低误触发。
- 任务重试、daemon 重启、claim 后执行都能保留本次任务的 execution mode。

## 非目标

- 不恢复 Agent 级 `Execution mode` 配置。
- 不增加 Agent 详情页配置项。
- 不增加 issue run/rerun 菜单里的临时覆盖项。
- 不支持自然语言模糊识别，例如“请用 goal 模式执行”。
- 不支持 token budget、自定义 goal 模板或多种执行模式。
- 不改变 sandbox、approval、worktree、reasoning 等其他运行配置。

## 用户体验

### 支持的指令

首版只支持 prompt 起始位置的单行 directive：

```text
/goal
```

合法形式：

```text
/goal
实现这个需求并跑完测试。
```

```text
   /goal

实现这个需求并跑完测试。
```

不支持的形式：

```text
请用 /goal 模式实现这个需求。
```

```text
实现这个需求。
/goal
```

原因：只解析开头 directive 可以避免误触发，也便于用户和系统审计。

### 实际发给 Agent 的任务内容

用户输入：

```text
/goal
实现登录页错误提示优化。
```

持久化任务：

- `execution_mode = "goal"`
- prompt 内容去掉 directive，保留为：

```text
实现登录页错误提示优化。
```

Agent 不应再看到 `/goal` 这一行，避免 provider 同时收到一条普通文本控制指令。

### 默认行为

没有 `/goal` 时：

- `execution_mode = "normal"`
- prompt 原样进入现有任务构建流程。
- provider 行为保持现状。

## 解析规则

新增一个小型解析器，例如：

```go
type PromptDirectiveResult struct {
    ExecutionMode string
    Prompt        string
}
```

规则：

1. 只检查 prompt 起始处。
2. 允许文件开头有空白行。
3. 第一条非空行如果精确等于 `/goal`，则触发 Goal。
4. `/goal` 大小写敏感；`/Goal`、`/goal please`、`goal` 都不触发。
5. 触发后移除该行以及其后的一个空白分隔区。
6. prompt 剩余内容为空时仍允许入队，但实际任务可能由现有校验决定是否拒绝。

建议伪代码：

```go
func parseTaskPromptDirectives(input string) PromptDirectiveResult {
    prompt := normalizeLineEndings(input)
    leadingBlank, firstLine, rest := splitFirstNonEmptyLine(prompt)
    if strings.TrimSpace(firstLine) != "/goal" {
        return PromptDirectiveResult{ExecutionMode: "normal", Prompt: input}
    }
    return PromptDirectiveResult{
        ExecutionMode: "goal",
        Prompt: strings.TrimLeft(rest, "\r\n"),
    }
}
```

解析器需要用单元测试覆盖 CRLF、前导空行、无 directive、非法 directive、directive 后空行等情况。

## 数据模型

在任务表增加任务级字段，而不是 Agent 字段。

建议在 `agent_task_queue` 增加：

```sql
execution_mode TEXT NOT NULL DEFAULT 'normal'
CHECK (execution_mode IN ('normal', 'goal'))
```

理由：

- 任务级行为需要随任务持久化，支持重试和 daemon 重启。
- claim 响应可以直接携带该字段，不需要重新解析 prompt。
- 历史任务默认 `normal`，行为不变。
- 审计时能看到某个任务为什么使用 Goal。

如果现有任务模型已有合适的 metadata JSON，也可以暂存到 metadata，但不推荐。`execution_mode` 是一等执行契约，应该用显式列和 DB constraint。

## API 契约

### 创建任务 / 入队路径

所有会创建 `agent_task_queue` 的入口，在入队前统一调用 directive 解析器。

典型来源包括：

- issue 分配给 Agent 后生成任务。
- comment mention 触发 Agent。
- chat / quick create / rerun / autopilot 等现有任务入口。

首版原则：

- 只解析“用户直接输入的任务 prompt/source text”。
- 不解析系统生成的完整 runtime prompt。

原因：`BuildPrompt` 会包裹大量系统说明，等到 daemon 执行阶段再解析已经太晚，也不一定能准确定位用户原始文本。

### Claim Response

daemon claim 响应的 task payload 增加：

```json
{
  "execution_mode": "normal"
}
```

默认值为 `normal`。

## Daemon 行为

daemon claim 后从 task payload 读取 `execution_mode`，并写入：

```go
agent.ExecOptions{
    ExecutionMode: task.ExecutionMode,
}
```

如果字段为空或未知，daemon 应降级为 `normal` 并记录 warning。正常情况下 DB constraint 会避免未知值进入任务表。

## Provider 行为

### Codex

当 `ExecOptions.ExecutionMode == "goal"` 时：

1. `thread/start` 或 `thread/resume` 成功后获得 `threadId`。
2. 在 `turn/start` 前调用：

```json
{
  "method": "thread/goal/set",
  "params": {
    "threadId": "<thread-id>",
    "objective": "<task prompt>",
    "status": "active",
    "tokenBudget": null
  }
}
```

失败策略：

- 如果 `thread/goal/set` 不支持或调用失败，记录 warning。
- 继续按 Normal 执行，避免旧 Codex 安装直接导致任务失败。

### Claude Code

Claude Code 当前没有接入 Multica 内的原生 goal RPC。Goal 模式通过 prompt contract 表达：

```text
# Execution mode: Goal

Work until this goal is genuinely handled:
<task prompt>

If the goal is complete, say so in the final answer. If blocked, explain the blocking condition and what is needed next.
```

要求：

- 只在 `ExecutionMode == "goal"` 时注入。
- `normal` 不改变 prompt。
- 注入时保留原始任务 prompt 的主体内容，不额外改写用户任务。

## 前端范围

首版不增加新 UI 控件。

可选的轻量提示：

- 在输入框 placeholder 或帮助文案里提示“在开头输入 `/goal` 可让本次任务以 Goal 模式执行”。
- 该提示不是必需项，避免扩大 UI 范围。

如果未来需要更强可见性，可以在任务详情或运行日志里展示 `Execution mode: Goal`，但不属于首版必须范围。

## 兼容性

- 历史任务通过 DB 默认值变为 `normal`。
- 没有 `/goal` 的任务行为不变。
- 旧 daemon 如果不知道 `execution_mode`，会继续按现有行为执行。
- 新 daemon 读取不到字段或字段为空时按 `normal`。
- Codex goal RPC 失败不会中断任务。

## 安全与误触发

只支持固定开头 directive，不做自然语言识别。

不支持以下输入触发：

- “请用 goal 模式”
- “goal: 完成这个任务”
- prompt 中间或末尾的 `/goal`

这样可以避免普通任务内容、代码片段、文档内容中的 `/goal` 被误识别。

## 测试计划

### 后端解析器

- `/goal` 在第一行时返回 `goal`，并移除 directive。
- 前导空行后 `/goal` 也能识别。
- `/goal` 后的空白分隔行会被清理。
- prompt 中间的 `/goal` 不触发。
- `/Goal`、`/goal please` 不触发。
- CRLF 输入能正确处理。

### DB / SQL

- 新任务不显式传 `execution_mode` 时默认 `normal`。
- 显式 `goal` 能持久化。
- 非法值被 DB constraint 拒绝。
- 历史任务迁移后为 `normal`。

### 入队路径

- issue/comment/chat/quick-create 等目标入口中，带 `/goal` 的用户输入会保存 `execution_mode = goal`。
- 保存的 prompt/source text 不包含 `/goal`。
- 无 `/goal` 的路径保存 `normal`。

### Daemon

- claim response 包含 task `execution_mode`。
- daemon 将 task execution mode 传入 `agent.ExecOptions`。
- 空值或未知值降级为 `normal` 并 warning。

### Provider

- Codex Goal 在 `turn/start` 前发送 `thread/goal/set`。
- Codex Normal 不发送 `thread/goal/set`。
- Codex `thread/goal/set` 失败时继续执行。
- Claude Goal 注入 goal contract。
- Claude Normal 不改变 prompt。

### 前端

如果增加帮助文案：

- 文案显示正确。
- 不影响现有输入和提交行为。

## 推荐实施顺序

1. 增加 prompt directive 解析器和单元测试。
2. 增加 `agent_task_queue.execution_mode` 迁移与 sqlc 更新。
3. 在任务入队路径解析 `/goal` 并持久化。
4. 在 claim response 和 daemon task 类型中透传 `execution_mode`。
5. 在 `agent.ExecOptions` 和 Codex / Claude provider 中实现 goal 行为。
6. 补齐 provider、daemon、handler、SQL 测试。

## 待确认问题

1. `/goal` 是否只支持英文固定指令，还是也支持中文别名如 `/目标`？
2. 是否要在任务详情或运行日志中显示本次任务的 execution mode？
3. 首版是否需要覆盖所有入队路径，还是先覆盖 issue/comment/chat 三类用户直接输入路径？

## 推荐决策

- 首版只支持 `/goal`，不支持中文别名，降低误触发和文档成本。
- 首版必须持久化到 `agent_task_queue.execution_mode`，不要只在 daemon 执行时临时解析。
- 首版覆盖所有能创建 Agent task 的入口；如果某些入口没有明确用户 prompt，则默认 `normal`。
- UI 不新增配置项，只在必要位置加轻量提示。
