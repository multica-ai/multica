# 单能力 Research

## 调研目标

1. 确认现有 Pomodoro 入口、状态和 API 边界。
2. 确认普通 live timer 与 Pomodoro session 的关系。
3. 确认 Focus Mode 可以复用哪些前端能力。
4. 确认为什么需要新的当前状态模型。

## 现状链路

1. 路由入口  
`apps/workspace/src/router.tsx` `pomodoroRoute` 把 `path: "pomodoro"` 映射到 `PomodoroPage`。

2. 页面结构  
`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage` 渲染 `PomodoroTimer variant="page"`、`PomodoroTodaySummary`、`PomodoroRecentSessions`，并使用固定 `TODAY_TARGET = 6`。

3. Pomodoro 当前状态  
`server/pkg/db/queries/pomodoro.sql` `GetPomodoroSession` 按 `user_id + workspace_id` 读取单条 session。  
`server/migrations/041_pomodoro_session.up.sql` `pomodoro_sessions` 存 `phase`、`phase_duration_seconds`、`status`、`elapsed_seconds`、`started_at`。

4. 普通 live timer 当前状态  
`server/migrations/036_time_entry.up.sql` `running_timer` 以 `user_id` 为主键保存当前 running entry。  
`server/internal/handler/time_entry.go` `CreateTimeEntry` 在无 `stop_time` 时启动 live timer。

5. 历史记录  
`server/internal/handler/pomodoro.go` `CompletePomodoro` 在 work phase 完成时创建 `time_entry(type='pomodoro')`。  
`server/pkg/db/queries/pomodoro.sql` `GetPomodoroHistory` 从 `time_entry` 查询 `type='pomodoro'`。

6. 前端复用能力  
`apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker` 已实现可搜索 issue picker。  
`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `usePomodoroSettings` 已有个人计时和声音设置。  
`apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` `PomodoroStatusPill` 已有全局运行状态入口。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `pomodoroRoute` | 当前只有 `/pomodoro`，没有 `/focus` |
| `apps/workspace/src/features/layout/navigation.ts` | `navigationGroups` | 导航仍显示 `Pomodoro` |
| `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` | `PomodoroPage` | 页面统计以番茄次数为中心 |
| `apps/workspace/src/shared/types/pomodoro.ts` | `PomodoroPhase` | 前端状态强绑定 `work/break/long_break` |
| `server/migrations/041_pomodoro_session.up.sql` | `pomodoro_sessions` | 当前 session 表只表达 Pomodoro phase |
| `server/migrations/036_time_entry.up.sql` | `running_timer` | 普通 live timer 是独立当前状态 |
| `server/internal/handler/pomodoro.go` | `CompletePomodoro` | Pomodoro 历史已落到 `time_entry` |
| `server/internal/handler/worklog.go` | `CreateWorklog` | worklog 不适合当前运行态和 Focus 历史 |

## 数据模型或状态流

当前状态流：

```text
/pomodoro UI
  -> usePomodoroQuery
  -> /api/pomodoro/current
  -> pomodoro_sessions

Pomodoro complete
  -> /api/pomodoro/complete
  -> time_entry(type='pomodoro')
  -> pomodoro_sessions phase transition
```

普通 timer 状态流：

```text
My Time / Issue timer UI
  -> /api/time-entries
  -> time_entry(stop_time=NULL)
  -> running_timer(user_id)
```

Focus Mode 需要的新状态流：

```text
/focus UI
  -> /api/focus/current
  -> focus_sessions

Focus complete
  -> /api/focus/complete
  -> time_entry(type='focus' or 'pomodoro')
  -> focus_events
  -> focus_sessions next state
```

## 边界条件

- 同一用户同一 workspace 只能有一个 active focus session。
- 同一用户仍只能有一个 ordinary running timer。
- 启动 Focus 时如果已有 ordinary running timer，必须明确 stop、convert、cancel 三选一，不能静默双跑。
- `/pomodoro` 的旧入口需要保留，避免现有用户或链接失效。

## 未决问题

- 旧 `/api/pomodoro/*` 是否在第一轮迁移为 Focus service 的兼容 wrapper。当前推荐不在首轮做 wrapper，只新增 `/api/focus/*` 并保留旧 Pomodoro 页面能力到迁移完成。
