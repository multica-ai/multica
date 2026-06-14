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
| Focus Mode Core | Phase 1 已完成 | P1 | `/focus`、`focus_sessions`、`/api/focus/*` 已存在，并已接入 issue/today/my-time |
| Flowtime Session | Phase 1 已完成 | P1 | Flowtime 可按实际时长写入 `time_entry` 并生成休息建议，review/plan 已消费 focus signals |
| Break Flow | Phase 1 已完成 | P2 | break started/skipped/completed 已写 `focus_events`，review/plan 已汇总 |
| Anti-Procrastination Start | Phase 1 已完成 | P2 | quick start、commitment、reason、真实 2 分钟倒计时和 `quick_start_completed` 写入已存在 |

## Reverse Sync 说明

本文件在 2026-06-12 回写当前代码状态。此前本 change 的能力列表仍按“未开始”描述，但代码已经实现了 Focus 主入口、后端 Focus API、`focus_sessions` / `focus_events`、Flowtime complete、break flow 基础行为。

当前代码证据：

- 证据：`server/migrations/045_focus_mode.up.sql` `focus_sessions` / `focus_events`
- 当前行为：数据库已定义 Focus 当前状态和事件表，mode 支持 `pomodoro`、`flowtime`、`quick_start`。

- 证据：`server/internal/handler/focus.go` `StartFocus` / `CompleteFocus` / `PauseFocus` / `AbandonFocus` / `transitionFocusBreak`
- 当前行为：后端已支持 Focus 生命周期、Flowtime 完成、休息开始/跳过/完成，并写入 `time_entry` 与 `focus_events`。

- 证据：`server/cmd/server/router.go` `/api/focus`
- 当前行为：Focus API route group 已注册。

- 证据：`apps/workspace/src/router.tsx` `focusRoute` / `pomodoroRoute`
- 当前行为：`/focus` 渲染 FocusPage，`/pomodoro` 重定向到 `/focus`。

- 证据：`apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` `FocusPage` / `modeOptions`
- 当前行为：前端已有 Flowtime、Pomodoro、2 min start、next step、start friction、pause/abandon reason、break suggested/breaking UI。

Phase 1 后剩余缺口：

1. Inbox 入口后置，不阻塞 issue/today/my-time 的工作流。
2. Focus history/reporting 后置，不在本轮做复杂行为分析。
3. Pomodoro legacy UI 的全面收敛后置，不作为新 Focus 主线扩展。
4. 旧 `/api/pomodoro/*` 和 `pomodoro_sessions` 仍存在，属于 legacy/parallel 能力，后续不能作为新 Focus 主线扩展。

## 当前状态基线

### `/focus` 已是主入口，`/pomodoro` 已重定向

- 证据：`apps/workspace/src/router.tsx` `focusRoute` / `pomodoroRoute`
- 当前行为：`/focus` 挂载 `FocusPage`，`/pomodoro` 使用 `<Navigate to="/focus" replace />`。
- 当前缺口：Focus 已接入 Issue Detail、Today、My Time；Inbox 入口后置。

### Focus 页面已覆盖 Flowtime、Pomodoro 和 Quick Start

- 证据：`apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` `FocusPage` / `modeOptions`
- 当前行为：页面支持 Flowtime、Pomodoro、2 min start，支持 issue/description/commitment/labels/reason 上下文，并在 quick start 中显示 2 分钟倒计时完成态。
- 当前缺口：无 Phase 1 阻塞缺口。

### 当前 Focus 状态已从 Pomodoro phase 升级为 Focus phase

- 证据：`server/migrations/045_focus_mode.up.sql` `focus_sessions`
- 当前行为：Focus phase 支持 `idle`、`focusing`、`paused`、`break_suggested`、`breaking`、`abandoned`。
- 当前缺口：Quick Start completion 已通过 `quick_start_completed` 事件和 flowtime 续接回写；无 Phase 1 阻塞缺口。

### 完成记录继续落到通用 `time_entry`

- 证据：`server/internal/handler/focus.go` `CompleteFocus`
- 当前行为：Focus complete 按 elapsed 创建 `time_entry`，quick start 会作为 flowtime 历史写入。
- 当前缺口：Daily Review 已消费 focus signals；完整 Focus history/reporting 后置。

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
