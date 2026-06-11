# 单能力 Spec

## 背景

当前 Pomodoro 的休息只是 phase 自动切换：work 完成后进入 break，break 完成后回到 work。这个模型无法回答用户是否真的休息、是否跳过、实际休息多久，也无法支持 Flowtime 的动态休息建议。

## 范围

- 本次覆盖：
  - break 建议持久化
  - break started/skipped/completed 事件
  - 当前 session 的 break phase 恢复
  - break 不写入 `time_entry`
  - Focus 页面中的休息建议和休息计时
- 本次不覆盖：
  - 休息质量打分
  - 复杂休息类型推荐
  - 团队级休息合规统计
  - 系统通知和桌面提醒

## 当前状态

- Pomodoro break 不写历史。
- break completion 只更新 session phase。
- 没有 break skipped 或 break started 数据。
- Flowtime 没有 break 建议。

## 证据

- `server/internal/handler/pomodoro.go` `CompletePomodoro`：work 完成后设置 `nextPhase = "break"` 或 `long_break`。
- `server/internal/handler/pomodoro.go` `CompletePomodoro`：break 或 long_break 完成后直接回到 `work`。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroHistory`：历史只查 `time_entry(type='pomodoro')`，break 不在历史里。
- `server/migrations/036_time_entry.up.sql` `time_entry`：time entry 是工时/专注历史，不适合记录休息事件。

## 缺口

1. 休息建议缺口  
Pomodoro 只有固定短休/长休，Flowtime 需要按实际专注时长建议休息。

2. 休息行为缺口  
用户是否开始休息、跳过休息、完成休息没有持久化证据。

3. 统计口径缺口  
如果把 break 写入 `time_entry`，会污染工时和专注时长统计。

## 交接说明

- Break flow 依赖 `focus-mode-core` 的 `focus_sessions`。
- Flowtime complete 会产生 `break_suggested` 状态和建议休息时长。
- Break 行为写 `focus_events`，不写 `time_entry`。
