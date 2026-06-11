# 单能力 Research

## 调研目标

1. 确认现有 Pomodoro break 行为。
2. 确认 break 是否已有历史记录。
3. 确认 break 不应写入 `time_entry` 的原因。

## 现状链路

1. Work complete 进入 break  
`server/internal/handler/pomodoro.go` `CompletePomodoro` 在 work phase 后根据 `pomodoro_count` 选择 `break` 或 `long_break`。

2. Break complete 回到 work  
`server/internal/handler/pomodoro.go` `CompletePomodoro` 对 `break` 或 `long_break` 只更新下一 phase 为 `work`。

3. History 只记录 work  
`server/pkg/db/queries/pomodoro.sql` `GetPomodoroHistory` 查询 `time_entry.type = 'pomodoro'`。

4. `time_entry` 用于时间记录  
`server/migrations/036_time_entry.up.sql` `time_entry` 存 issue、description、start/stop、duration、type，用于 My Time 和 team/project stats。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `server/internal/handler/pomodoro.go` | `CompletePomodoro` | break 是 phase transition，不是独立历史 |
| `server/pkg/db/queries/pomodoro.sql` | `GetPomodoroHistory` | Pomodoro history 不包含 break |
| `server/pkg/db/queries/time_entry.sql` | `SumTimeEntriesByUserInWorkspace` | time entry 参与时间统计 |
| `server/pkg/db/queries/time_entry.sql` | `SumTimeEntriesByProjectInWorkspace` | time entry 参与项目统计 |
| `server/migrations/036_time_entry.up.sql` | `time_entry` | 表语义是工作/时间记录，不是事件日志 |

## 数据模型或状态流

建议 break flow：

```text
Focus complete
  -> focus_sessions.phase = break_suggested
  -> focus_sessions.suggested_break_seconds = N
  -> focus_events(event_type = break_suggested)

Start break
  -> focus_sessions.phase = breaking
  -> started_at = now
  -> focus_events(event_type = break_started)

Skip break
  -> focus_sessions.phase = idle
  -> focus_events(event_type = break_skipped)

Complete break
  -> focus_sessions.phase = idle
  -> focus_events(event_type = break_completed, duration_seconds = actual)
```

## 边界条件

- 用户可以跳过建议休息，但需要记录事件。
- 用户开始休息后可以提前完成。
- break 页面刷新后必须能恢复剩余时间。
- break 不写 `time_entry`。

## 未决问题

- 首轮是否支持自定义 break duration。当前推荐不支持，只允许使用建议时长或 skip。
