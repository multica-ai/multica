# 单能力 Research

## 调研目标

- 确认当前番茄能力里哪些是操作页能力，哪些是统计能力。
- 确认今日/本周/本月/完成率/日分布五类需求当前各自的实现程度。
- 确认“完成率”为什么还没有统一口径。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/router.tsx` `pomodoroRoute`；结论：当前番茄能力只有 `/pomodoro` 一个入口。
   - 证据：`apps/workspace/src/features/layout/navigation.ts` `primaryNav`；结论：导航直接暴露 `Pomodoro`，没有统计子入口。
2. 数据流  
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-history.ts` `usePomodoroHistoryQuery`；结论：页面一次读取“历史 entries + aggregate stats”。
   - 证据：`apps/workspace/src/shared/api/client.ts` `getPomodoroHistory`；结论：客户端番茄统计接口只有 `/api/pomodoro/history`。
3. 状态更新  
   - 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：页面的核心状态仍是计时器与历史展开，而不是统计筛选。
4. 输出结果  
   - 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：当前只输出今日摘要和近期历史，没有月统计、完成率和图表。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` | `PomodoroPage` | 当前页面以 `PomodoroTimer` 为核心，属于操作页。 |
| `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` | `TODAY_TARGET` | 页面把今日目标写成常量 `6`，说明完成率缺少稳定配置契约。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-history.ts` | `usePomodoroHistoryQuery` | 当前统计读取只依赖历史接口，没有独立 analytics 接口。 |
| `apps/workspace/src/shared/api/client.ts` | `PomodoroHistoryStats` / `getPomodoroHistory` | 客户端只声明了 `today_count`、`week_count`、`total_seconds` 三个聚合字段。 |
| `server/pkg/db/queries/pomodoro.sql` | `GetPomodoroStats` | 后端聚合只有今日数、周数、总时长，没有月数与日分布。 |
| `server/internal/handler/pomodoro.go` | `GetPomodoroHistory` 响应构造 | 服务端确实把 `week_count` 返回给前端，但当前页面没有消费周统计。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` | `PomodoroSettings` | 设置里没有 daily target 字段，说明完成率目标来源尚未产品化。 |

## 数据模型或状态流

- 核心字段  
  - 证据：`apps/workspace/src/shared/api/client.ts` `PomodoroHistoryStats`；结论：当前聚合只含 `today_count`、`week_count`、`total_seconds`。
  - 证据：`apps/workspace/src/shared/types/time-entry.ts` `TimeEntry.type`；结论：番茄完成记录落在 `time_entry.type = "pomodoro"`。
- 状态如何变化  
  - 证据：`apps/workspace/src/shared/api/client.ts` `completePomodoro`；结论：完成一个工作 phase 会生成 pomodoro time entry，再由历史接口读取出来。
- 写入点在哪里  
  - 证据：`apps/workspace/src/shared/api/client.ts` `completePomodoro`；结论：写入入口仍是番茄会话接口，而非统计接口。
- 读取点在哪里  
  - 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：当前读取点只有操作页。

## 边界条件

- 权限边界  
  - 证据：`server/internal/handler/pomodoro.go` `GetPomodoroHistory`；结论：番茄统计是当前用户 + 当前 workspace 范围。
- 空状态  
  - 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroRecentSessions` 渲染链；结论：当前可以处理无历史数据，但缺少统计页空状态。
- 错误路径  
  - 证据：`server/internal/handler/pomodoro.go` `GetPomodoroHistory`；结论：历史查询失败会返回服务端错误，统计页需要单独错误提示。
- 多租户边界  
  - 证据：`apps/workspace/src/shared/api/client.ts` `authHeaders`；结论：番茄统计同样依赖 workspace header。

## 未决问题

- 完成率目标值来自用户设置、团队默认值还是固定常量；该项在 `design.md` 中定义为前置契约。
- 统计入口是扩展现有 `PomodoroPage` 还是新增独立统计页；该项在 `design.md` 中给出推荐方案。
