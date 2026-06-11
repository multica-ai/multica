# 单能力 Spec

## 背景

Pomodoro 适合固定节奏，但用户在真实工作中经常需要开放式专注：先进入 flow，结束后根据实际专注时长决定休息。Flowtime 的目标是让用户不被 25 分钟切断，同时仍然保留可记录、可复盘、可建议休息的闭环。

## 范围

- 本次覆盖：
  - `mode = flowtime` 的 Focus session
  - 开放式开始、暂停、恢复、完成
  - 按实际专注时长写入 `time_entry`
  - 首版动态休息建议
  - Flowtime 历史在 Focus 页面和 My Time 中可识别
- 本次不覆盖：
  - 用户自定义休息算法
  - 自动检测 flow 中断
  - 跨天 session 分摊统计
  - break 事件完整 UI，交给 `break-flow`

## 当前状态

- 当前 Pomodoro completion 使用固定 `phase_duration_seconds` 写 duration。
- 普通 live timer 可记录实际 start/stop，但没有 Focus Mode 语义。
- `time_entry.type` 当前主要是 `manual` 和 `pomodoro`。

## 证据

- `server/internal/handler/pomodoro.go` `CompletePomodoro`：work 完成时 `DurationSeconds` 使用 `existing.PhaseDurationSeconds`。
- `server/internal/handler/time_entry.go` `CreateTimeEntry`：无 `stop_time` 时启动 live timer，有 `stop_time` 时创建历史记录。
- `server/migrations/036_time_entry.up.sql` `time_entry`：支持 start_time、stop_time、duration_seconds、type。
- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `EntryRow`：已根据 `entry.type === "pomodoro"` 展示 Pomodoro 来源标记。
- `apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts` `useCompletePomodoroMutation`：当前乐观切 phase 时写死 25/5 duration。

## 缺口

1. 实际时长缺口  
Pomodoro completion 不记录实际 elapsed，而是记录固定 phase duration。

2. Flowtime 模式缺口  
普通 live timer 可以开放式计时，但缺少 Focus commitment、mode、break suggestion 和事件。

3. 历史类型缺口  
`time_entry.type` 没有 `focus` 或 `flowtime` 类型，My Time 不能区分 Flowtime。

4. 休息建议缺口  
系统没有根据实际专注时长计算建议休息时长的契约。

## 交接说明

- Flowtime 依赖 `focus-mode-core` 的 `focus_sessions` 和 `/api/focus/*`。
- 本能力只负责 Flowtime session 完成和休息建议，不负责 break 行为持久化 UI。
