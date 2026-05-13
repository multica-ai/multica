# TUI 使用说明

这份文档面向日常使用 Multica 的成员，说明 Issue 页面中的本地 Agent TUI / Stream 面板如何使用、能看到什么、以及在 `plan` 和 `approval` 场景下应该怎么理解界面反馈。

## 适用范围

- 任务在**本地 daemon** 上执行
- Agent 开启了 `stream`
- 当前 Issue 已经有正在运行或最近一次运行的任务

> [截图占位：Issue 页面右侧的 Agent stream 面板总览]

## 这是什么

`Agent stream` 是本地 daemon 暴露出来的实时执行视图。它不是最终评论的替代物，而是用来帮助你观察 Agent 在执行过程中的真实状态，包括：

- 正在输出的回复
- 工具调用和工具结果
- 命令执行与文件改动
- `plan` 阶段状态
- 需要人工确认的 `approval`

它的目标是让你在任务尚未结束时，就能判断：

- Agent 是否真的在工作
- 当前卡在什么步骤
- 是继续等待，还是应该介入

## 打开前的准备

在 Agent 详情侧栏中，先确认以下设置：

1. `Stream` 处于开启状态
2. 如果需要人工确认，再设置 `Approval policy`

注意：

- `Approval policy` 只有在 `Stream` 开启时才会显示
- `Stream` 关闭时，`Approval policy` 默认按 `auto` 处理，不展示审批卡片

> [截图占位：Agent 详情中的 Stream 开关与 Approval policy]

## 在哪里看

进入某个 Issue 后，右侧侧边栏会显示 `Agent stream`。

它有三种常见状态：

- `live`：任务正在执行，面板会持续刷新
- `paused`：任务正在等待你的确认
- `recent`：当前没有活跃任务，展示最近一次本地运行记录

如果当前 Issue 还没有本地执行记录，会看到空状态。

## 你会看到哪些内容

默认视图会优先显示更适合阅读的执行事件，而不是原始日志。

常见内容包括：

- `Assistant`：Agent 对外可见的过程说明或结果输出
- 思考 / 推理片段：模型在执行过程中的内部推理内容
- `Command` / `Command output`：执行的命令和输出
- `File change`：文件改动开始或完成
- `Tool result`：工具调用结果
- `Plan mode` / `Plan accepted` / `Plan revision requested`：计划流转状态
- `approval` / `plan decision`：等待你处理的交互卡片

右上角的 `Raw` 按钮会切换到更接近底层事件流的视图，适合排查问题。

> [截图占位：默认视图与 Raw 视图对比]

## 常规使用流程

### 1. 发起任务

常见触发方式：

- 直接把 Issue 分配给 Agent
- 在评论框里输入内容并发送
- 在已有线程里继续追问

只要任务落到本地 daemon，上述动作都可能在右侧看到实时流。

### 2. 观察执行过程

建议先看三个信号：

- 是否处于 `live`
- 最近几条事件在做什么
- 是否出现 `approval` 或 `plan decision`

如果事件在持续推进，通常说明任务还在正常执行。

### 3. 判断是否需要介入

你通常只需要在两类情况下介入：

- 任务进入审批等待
- `plan` 输出与你预期不一致，需要要求修改

## Plan only 怎么用

在 Issue 评论输入框右下角，有一个 `Plan only` 按钮。它会发起一次只产出方案、不直接实施的请求。

典型流程：

1. 输入你的需求
2. 点击 `Plan only`
3. 右侧 `Agent stream` 进入计划阶段
4. Agent 输出方案
5. 如果进入 `plan decision`，你可以批准、要求修改或取消

`plan` 模式适合：

- 需求还不够稳定
- 先看拆解方案再决定是否执行
- 需要先审一下实施路径、风险或范围

> [截图占位：Plan only 按钮位置]
> [截图占位：plan 阶段输出与 plan decision 卡片]

### 关于 provider 支持

当前原生 `plan mode` 以 Claude runtime 为主。若当前 Agent 使用的 runtime 不支持原生 `plan mode`，流里会给出提示，并按普通执行模式继续。

这意味着：

- 想稳定使用 `plan` 审批流，优先选择支持该模式的 runtime
- 如果只是普通执行，不会出现 `plan decision`

## Approval 怎么用

当 Agent 需要你确认某个动作时，右侧流里会出现审批卡片。

常见场景：

- 执行敏感命令前确认
- 写文件前确认
- 计划阶段等待你决定是否继续

你可以直接在卡片上操作。处理完成后，任务会继续向下执行，面板状态也会从 `paused` 恢复。

> [截图占位：普通 approval 卡片]
> [截图占位：plan approval / revise 输入框]

## 怎么理解“最终结果”和“过程输出”

右侧的 `Agent stream` 用来观察过程，Issue 评论或最终回复才是任务完成后的正式产物。

建议这样理解：

- **过程输出**：帮助你判断 Agent 当前在做什么
- **最终输出**：这次任务真正交付的结果

过程里的中间文本可能会反复修正，也可能只是执行中的阶段性说明，不应直接当作最终结论。

## 常见问题

### 右侧没有任何 stream

先检查：

1. 任务是不是跑在本地 daemon 上
2. Agent 的 `Stream` 是否开启
3. 当前 Issue 是否有本地执行记录

### 提示本地 trace port 不可用

这通常说明前端已经更新，但本地 daemon 的运行时元数据还没刷新，或者 daemon 版本还不支持本地 trace 接口。

处理方式：

1. 重启本地 daemon
2. 再次打开 Issue 页面确认

> [截图占位：trace port 不可用提示]

### 为什么没看到 approval

先确认两件事：

1. `Stream` 已开启
2. Agent 的 `Approval policy` 不是 `auto`

如果 `Stream` 关闭，审批默认按 `auto` 处理，不会展示审批卡片。

## 建议的使用习惯

- 日常执行任务时保持 `Stream` 开启，便于观察过程
- 需要人工介入的 Agent，再开启非 `auto` 的审批策略
- 需求复杂或范围不明确时，优先走一次 `Plan only`
- 看不懂事件时先用默认视图，需要排查再切到 `Raw`

## 截图清单

建议补以下截图，方便同步到平台 Wiki：

- [ ] Issue 页面右侧 `Agent stream` 总览
- [ ] Agent 设置中的 `Stream` 与 `Approval policy`
- [ ] 默认视图与 `Raw` 视图
- [ ] `Plan only` 按钮位置
- [ ] `plan` 输出与 `plan decision`
- [ ] 普通 `approval` 卡片
- [ ] `trace port` 不可用提示
