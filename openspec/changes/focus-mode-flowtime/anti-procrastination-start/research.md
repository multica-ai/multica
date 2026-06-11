# 单能力 Research

## 调研目标

1. 确认当前 start/pause/reset 是否记录原因。
2. 确认现有 UI 是否已有下一步输入入口。
3. 确认原因采集应落在哪里。

## 现状链路

1. 普通 timer start  
`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar` 使用 description 输入框，Enter 或 Start 后调用 `requestStart`。

2. Pomodoro start  
`server/internal/handler/pomodoro.go` `StartPomodoro` 只创建或恢复 session，不接收原因和 commitment。

3. Pomodoro pause  
`server/internal/handler/pomodoro.go` `PausePomodoro` 只累计 elapsed 并设置 paused。

4. Pomodoro reset  
`server/internal/handler/pomodoro.go` `ResetPomodoro` 重置为 idle，不记录放弃原因。

5. 事件基础  
当前没有 `focus_events`；`server/pkg/protocol/events.go` 只有 time entry 和 worklog 等事件常量，没有 focus reason 事件。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` | `StartTimerBar` | 当前启动输入只有 description |
| `server/internal/handler/pomodoro.go` | `StartPomodoro` | Pomodoro start 不记录原因 |
| `server/internal/handler/pomodoro.go` | `PausePomodoro` | pause 不记录原因 |
| `server/internal/handler/pomodoro.go` | `ResetPomodoro` | reset 不记录 abandon reason |
| `server/migrations/041_pomodoro_session.up.sql` | `pomodoro_sessions` | 无 commitment/reason 字段 |

## 数据模型或状态流

2 分钟启动：

```text
POST /api/focus/start
  mode = quick_start
  preset = two_minute_start
  commitment_text = "open failing CI log"
  resistance_reason = "unclear_next_step"
  -> focus_sessions.phase = focusing
  -> focus_events(event_type = focus_started, reason = resistance_reason)
```

Pause with reason：

```text
POST /api/focus/pause
  reason = "interruption"
  note = optional
  -> focus_sessions.phase = paused
  -> focus_events(event_type = focus_paused)
```

Abandon with reason：

```text
POST /api/focus/abandon
  reason = "too_large"
  note = optional
  -> focus_sessions.phase = abandoned
  -> focus_events(event_type = focus_abandoned)
```

## 边界条件

- 用户可以不填原因直接开始。
- 如果选择 `other`，note 仍然可选，不强制。
- 2 分钟启动完成后不应强制写成长时间专注。
- 原因枚举必须稳定，便于后续统计。

## 未决问题

- 2 分钟启动完成后默认建议 break 还是提示“继续 Flowtime”。当前推荐提示继续 Flowtime 为主，break 为次。
