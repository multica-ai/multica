# Attention Loop Spec

## 背景

Focus Mode 已经在代码中存在，但它仍偏独立页面。注意力管理的核心目标不是增加计时器，而是降低从“看到任务”到“开始执行”的摩擦，并记录中断、放弃、休息等注意力信号。

## 范围

Phase 1 包含：

- 修正 Focus OpenSpec 状态漂移。
- 将 Start Focus 接入 issue、today、my-time 的主要工作流。
- 补齐 quick start 2 分钟启动闭环。
- 让 focus reason 和 break events 可进入复盘。

Phase 1 不包含：

- 系统级勿扰。
- 网站拦截或浏览器插件。
- 复杂注意力报表。
- 团队强制专注策略。

## ASCII 状态机与页面交互

### Focus 状态机

```text
idle
  |
  | StartFocus(mode=flowtime|pomodoro|quick_start)
  v
focusing
  |
  +-- PauseFocus(reason?) --------------------+
  |                                           |
  v                                           |
paused -- ResumeFocus ------------------------+
  |
  +-- AbandonFocus(reason?) --> abandoned --> idle

focusing
  |
  +-- CompleteFocus(end_reason=completed)
          |
          v
   break_suggested
          |
          +-- StartFocusBreak --> breaking -- CompleteFocusBreak --> idle
          |
          +-- SkipFocusBreak -------------------------------> idle
```

事件写入约束：

- `StartFocus` 写入 focus started 事件。
- `PauseFocus` / `AbandonFocus` 必须保存 reason。
- `CompleteFocus` 创建 `time_entry` 并写入 focus completed 事件。
- break 行为只写 `focus_events`，不写 `time_entry`。

### Quick Start 状态机

```text
idle
  |
  | StartFocus(mode=quick_start, commitment_text, resistance_reason?)
  v
quick_start_countdown(120s)
  |
  +-- countdown_done --> quick_start_completed event
  |                         |
  |                         +--> continue_as_flowtime
  |                         +--> complete
  |                         +--> abandon(reason?)
  |
  +-- abandon(reason?) --> abandoned --> idle
```

Quick Start 的产品含义：

- 2 分钟内目标是“启动”，不是完成任务。
- `quick_start_completed` 只表示用户跨过启动阻力。
- 用户继续工作时应转入 Flowtime，而不是继续挂在 quick_start 语义下。

### Attention 页面交互图

```text
Today / Backlog / Upcoming
  |
  | Start Focus(issue_id)
  v
StartFocusDialog
  |
  +-- choose mode: flowtime / pomodoro / quick_start
  +-- edit next step
  +-- optional resistance reason
  |
  v
FocusPage
  |
  +-- pause(reason) --------+
  +-- abandon(reason) -----+--> FocusEvent summary
  +-- complete ------------+--> TimeEntry
  +-- break start/skip/done+--> FocusEvent summary
                                |
                                v
                         DailyReviewPanel
```

```text
Issue Detail
  |
  +-- Start Focus with current issue
  +-- Keep editing issue description/comments
  |
  v
FocusPage
  |
  +-- Complete focus
  v
My Time / Time Entry history
```

交互约束：

- 列表页只提供轻量入口，不展开完整 Focus 页面。
- 完整计时、暂停、放弃、休息交互仍集中在 `FocusPage`。
- `StartFocusDialog` 必须能接受预填 issue，但允许用户不绑定 issue。
- 如果已有普通 running timer，必须沿用现有冲突处理策略，避免重复计时。

## 当前状态

- 证据：`server/migrations/045_focus_mode.up.sql` `focus_sessions` / `focus_events`
- 当前行为：数据库已有 Focus session 和 event 模型，支持 `pomodoro`、`flowtime`、`quick_start`。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`server/internal/handler/focus.go` `StartFocus` / `CompleteFocus` / `transitionFocusBreak`
- 当前行为：后端支持开始、暂停、完成、放弃、休息开始、休息跳过、休息完成，并支持 `quick_start_completed`。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` `FocusPage`
- 当前行为：前端有 Flowtime、Pomodoro、2 min start、commitment、resistance reason UI；quick start 使用真实倒计时完成态。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`apps/workspace/src/router.tsx` `TodayPage` / `IssueDetailRoute` / `myTimeRoute`
- 当前行为：Today、Issue detail、My Time 都是用户开始工作的自然入口。
- 当前缺口：这些入口已接入统一 Start Focus 组件；Inbox 后置。

## Phase 1 已关闭缺口

1. Focus OpenSpec 状态已回写。
2. Focus 已成为 issue/today/my-time 的可复用 action。
3. Quick Start 已具备真实 2 分钟完成语义。
4. Pause/abandon/break signals 已进入 Daily Review。

## 推荐功能切片

### A1. Focus Reverse Sync

目标：把 Focus 文档更新到当前代码真实状态。

完成定义：

- `focus-mode-flowtime/module-overview.md` 不再把已实现能力标为“未开始”。
- 剩余缺口明确列出：入口贯穿、quick start completed、history/review 汇总。

### A2. Reusable Start Focus Action

目标：让用户从任务直接进入专注。

当前状态：已完成。

最小行为：

- 提供可复用 `StartFocusButton` 或 `StartFocusDialog`。
- 支持传入 `issue_id`、description、默认 mode。
- 在 issue detail、today/workbench row、my-time 中接入。

完成定义：

- 从 issue detail 可一键以当前 issue 开始 Focus。
- 从 Today 可对某个 issue 开始 Focus。
- 不破坏现有 `/focus` 页面直接使用。

验证记录：

- `pnpm typecheck`

### A3. Quick Start 2 Minute Loop

目标：让 quick start 真正成为反拖延启动能力。

当前状态：已完成。

最小行为：

- quick start 模式显示 2 分钟倒计时。
- 倒计时完成后写入 `quick_start_completed` event。
- 完成后引导用户继续 Flowtime、完成或放弃。

完成定义：

- 后端有明确 quick start completed 写入路径。
- 前端能展示倒计时完成态。

验证记录：

- `cd server && go test ./internal/handler`
- `pnpm typecheck`

### A4. Focus Signals To Review

目标：让 Daily Review 能解释注意力状态。

当前状态：已完成。

最小行为：

- Daily Review prompt 包含当天 focus completed、paused、abandoned、break skipped/completed 摘要。
- 不新增复杂图表。

完成定义：

- 复盘草稿能引用当天中断/放弃/休息行为。

验证记录：

- `cd server && go test ./internal/service`

## 交接说明

实现必须先做 A1。A2 和 A3 可以并行，但 A4 依赖 focus event 查询能力稳定。
