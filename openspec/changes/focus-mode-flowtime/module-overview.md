# 模块级设计总览

## 目标与范围

- 目标：把现有 Pomodoro 能力升级为通用 Focus Mode，让用户可以在 Multica 中完成“开始专注、保持上下文、按实际专注获得休息建议、记录休息行为、降低启动阻力”的闭环。
- 本次包含：
  - Focus Mode 主入口和当前专注上下文
  - Flowtime 开放式专注模式
  - Break flow 休息建议、开始、跳过、完成事件
  - 反拖延启动：2 分钟启动、下一步承诺、阻力原因记录
  - Pomodoro 作为 Focus Mode 的一种 preset 继续存在
- 本次明确不包含：
  - 团队级强制专注策略
  - AI 自动判断用户是否分心
  - 桌面通知、系统级网站拦截、浏览器插件
  - 复杂行为分析报表
  - 对 legacy `worklog` 的新增写入

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| Focus Mode Core | 未开始 | P1 | 统一入口、上下文、状态模型、与普通 timer 互斥 |
| Flowtime Session | 未开始 | P1 | 开放式专注、按实际时长生成记录、动态建议休息 |
| Break Flow | 未开始 | P2 | 休息行为持久化到 `focus_events`，不写入 `time_entry` |
| Anti-Procrastination Start | 未开始 | P2 | 2 分钟启动、下一步承诺、阻力/暂停/放弃原因 |

## 当前状态基线

### `/pomodoro` 已是独立入口，但还不是 Focus Mode

- 证据：`apps/workspace/src/router.tsx` `pomodoroRoute`
- 当前行为：`path: "pomodoro"` 挂载 `PomodoroPage`。
- 当前缺口：路由和导航仍以 Pomodoro 命名，无法表达 Flowtime、反拖延启动和通用 Focus Mode。

### 页面已接近 Focus 工作台，但能力仍以番茄钟为中心

- 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`
- 当前行为：页面渲染 `PomodoroTimer variant="page"`、`PomodoroTodaySummary`、`PomodoroRecentSessions`，并固定 `TODAY_TARGET = 6`。
- 当前缺口：今日目标以番茄个数衡量，不适合 Flowtime 的实际专注时长和启动成功率。

### 当前计时状态强绑定 Pomodoro phase

- 证据：`apps/workspace/src/shared/types/pomodoro.ts` `PomodoroPhase`
- 当前行为：前端 phase 只有 `work | break | long_break`。
- 当前缺口：没有 `flow`, `suggested_break`, `two_minute_start`, `abandoned` 等 Focus Mode 需要的状态语义。

### 完成记录已经落到通用 `time_entry`

- 证据：`server/internal/handler/pomodoro.go` `CompletePomodoro`
- 当前行为：work phase 完成时创建 `time_entry`，并设置 `type = "pomodoro"`。
- 当前缺口：duration 写入固定 `phase_duration_seconds`，不是实际专注时长；缺少 Flowtime、启动方式、结束原因、休息建议等结构化字段。

### `time_entry` 是新的时间记录主线

- 证据：`server/migrations/036_time_entry.up.sql` `time_entry`
- 当前行为：`time_entry` 支持 start/stop、issue 绑定、description、duration、type。
- 当前缺口：缺少 Focus Mode 元数据，但比 `worklog` 更适合作为 Focus/Flowtime 历史主表。

### `worklog` 不适合作为 Focus Mode 核心模型

- 证据：`server/internal/handler/worklog.go` `CreateWorklog`
- 当前行为：worklog 是 issue-bound duration model，只支持按 issue 创建和查询。
- 当前缺口：不支持当前运行态、pause、break、Flowtime、全局 Focus 历史。

### 普通 live timer 与 Pomodoro session 可能并存

- 证据：`server/migrations/036_time_entry.up.sql` `running_timer`
- 当前行为：`running_timer` 以 `user_id` 为主键，同一用户只能有一个普通 live timer。
- 证据：`server/pkg/db/queries/pomodoro.sql` `GetPomodoroSession`
- 当前行为：Pomodoro session 按 `user_id + workspace_id` 存在。
- 当前缺口：Focus Mode 需要定义与普通 timer 的互斥或接管规则，避免双计时和重复记录。

### 前端已有可复用上下文选择能力

- 证据：`apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker`
- 当前行为：已有基于 `PropertyPicker`、`useIssueStore`、`useIssuesListQuery` 的 issue 搜索选择模式。
- 当前缺口：Pomodoro completion 中仍有 `window.prompt` 绑定 issue 的路径。

## 非目标

- 不在本轮设计中实现团队强制专注、leaderboard 或组织层专注政策。
- 不把 break 写入 `time_entry`，避免污染工时和项目时间统计。
- 不继续扩展 legacy `worklog` 作为 Focus/Flowtime 历史来源。
- 不要求执行 Agent 同时重构所有 time tracking UI。
- 不加入系统级拦截、浏览器插件或第三方通知集成。

## 优先级与推进顺序

1. 先做 Focus Mode Core  
统一数据模型、入口、上下文和与普通 timer 的互斥规则，否则后续 Flowtime 和反拖延会各自实现状态。

2. 再做 Flowtime Session  
Flowtime 是核心差异能力，决定专注历史如何落 `time_entry`，也决定 break 建议算法。

3. 再做 Break Flow  
Break flow 依赖 Flowtime 产生的实际专注时长和建议休息时长，应在 session model 稳定后实现。

4. 最后做 Anti-Procrastination Start  
2 分钟启动可以复用 Focus Mode Core 的 session start 能力；原因采集也依赖 `focus_events` 或 session 元数据。

## 共享约束

- 数据约束：
  - 当前运行态使用新的 `focus_sessions`。
  - 专注历史继续写 `time_entry`。
  - break 行为写 `focus_events`，不写 `time_entry`。
  - legacy `worklog` 不新增写入路径。
- 权限约束：
  - 所有 Focus API 必须位于 workspace member 保护组。
  - 查询和写入必须过滤 `workspace_id`。
- 交互约束：
  - Focus Mode 启动前必须处理已有普通 running timer。
  - Pomodoro 是 Focus preset，不是独立产品主概念。
  - `/pomodoro` 保留重定向到 `/focus`，导航显示 Focus。
- 技术约束：
  - 后端新增 `/api/focus/*`，不把 Flowtime 语义塞进 `/api/pomodoro/*`。
  - 前端使用 `@/` alias 和 time-tracking feature 内现有 hook/query 模式。
  - 优先复用现有 `PropertyPicker`、time entry label、sound settings、React Query 模式。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| `pomodoro_sessions` 和 `focus_sessions` 并存 | 用户可能看到两个当前专注状态 | 新 Focus Core 实现后，UI 只读 Focus session；旧 Pomodoro API 进入兼容或迁移设计 |
| 普通 timer 与 Focus 同时运行 | 造成双计时和重复 `time_entry` | Focus start 必须 stop 或接管已有 running timer，并在 UI 中确认 |
| Flowtime 动态休息算法过复杂 | 首轮难验证 | 首轮使用固定分段规则，后续再配置化 |
| break 行为不写 `time_entry` | 早期报表看不到休息详情 | 使用 `focus_events` 持久化，后续从事件聚合 |
| 原因采集太重 | 反拖延功能反而增加阻力 | 启动原因和暂停/放弃原因都做轻量可选，核心 start 不阻塞 |

## 回写规则

- 实现阶段发现模型偏差时，先更新对应能力包的 `design.md` 和 `tasks.md`，再改代码。
- 如果执行阶段决定继续复用 `/api/pomodoro/*` 而不是新增 `/api/focus/*`，必须先更新 `focus-mode-core/design.md`。
- 如果 break 被改为写入 `time_entry`，必须先更新 `break-flow/design.md` 并说明统计口径变化。
- 如果反拖延原因字段增加或枚举变化，必须同步更新 `anti-procrastination-start/design.md` 和 `tasks.md`。
