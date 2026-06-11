# 单能力 Research

## 调研目标

1. 确认当前计时历史是否能承载 Flowtime。
2. 确认 Pomodoro duration 计算方式和 Flowtime 的差距。
3. 确认前端历史展示需要哪些改动。

## 现状链路

1. Pomodoro 完成  
`server/internal/handler/pomodoro.go` `CompletePomodoro` 在 work phase 完成时创建 `time_entry`，`DurationSeconds` 使用 `existing.PhaseDurationSeconds`。

2. 普通计时完成  
`server/internal/handler/time_entry.go` `StopTimeEntry` 通过 `TimeEntryService.StopTimer` 停止 running timer，并返回实际 time entry。

3. 历史查询  
`server/pkg/db/queries/time_entry.sql` `ListTimeEntriesByUserRange` 按用户和时间范围列出 `time_entry`。

4. Pomodoro 历史  
`server/pkg/db/queries/pomodoro.sql` `GetPomodoroHistory` 专门查询 `type = 'pomodoro'` 的 `time_entry`。

5. 前端显示  
`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `EntryRow` 对 `entry.type === "pomodoro"` 显示番茄标记。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `server/internal/handler/pomodoro.go` | `CompletePomodoro` | Pomodoro 使用固定 duration，不适合 Flowtime |
| `server/migrations/036_time_entry.up.sql` | `time_entry` | `time_entry` 可承载 Flowtime 历史 |
| `server/pkg/db/queries/time_entry.sql` | `CreateTimeEntry` | 可写入不同 `type` |
| `server/internal/handler/time_entry.go` | `StopTimeEntry` | 普通 timer 可产生实际 duration |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` | `EntryRow` | 前端已有按 type 显示来源的模式 |
| `apps/workspace/src/shared/types/pomodoro.ts` | `CompletePomodoroBody` | 旧完成 body 不含 actual duration 或 mode |

## 数据模型或状态流

Flowtime start：

```text
POST /api/focus/start { mode: "flowtime" }
  -> focus_sessions.phase = focusing
  -> started_at = now
```

Flowtime pause：

```text
POST /api/focus/pause
  -> elapsed_focus_seconds += now - started_at
  -> phase = paused
  -> started_at = null
```

Flowtime complete：

```text
POST /api/focus/complete
  -> actual_focus_seconds = elapsed_focus_seconds + current running segment
  -> create time_entry(type="flowtime")
  -> calculate suggested_break_seconds
  -> focus_sessions.phase = break_suggested
```

## 边界条件

- 小于 60 秒的 Flowtime 是否记录：首版允许完成，但 UI 应提示过短；后端不硬拦。
- pause 累计时长必须参与实际专注时长。
- Flowtime 完成后必须使用实际 elapsed，而不是 preset duration。
- Flowtime 不自动进入 break；只进入 `break_suggested`。

## 未决问题

- `time_entry.type` 是否使用 `focus` + metadata，还是直接使用 `flowtime`。当前推荐首轮使用 `flowtime`，让列表和统计简单可读。
