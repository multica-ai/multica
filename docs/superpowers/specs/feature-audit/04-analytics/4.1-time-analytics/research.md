# 单能力 Research

## 调研目标

- 确认 4.1 当前有哪些“时间相关页面”已经存在。
- 确认这些页面里哪些属于操作页，哪些属于统计页。
- 确认今日/本周/本月/自定义/项目/标签/任务/日分布八类需求里，当前系统真实支持到哪一层。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/router.tsx` `myTimeRoute`、`teamTimeRoute`；结论：当前时间相关入口只有 `/my-time` 与 `/team-time` 两个主路由。
   - 证据：`apps/workspace/src/features/layout/navigation.ts` `primaryNav`；结论：导航把 `My Time` 与 `Team Time` 直接暴露给用户，没有独立“时间统计”入口。
2. 数据流  
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTimeEntriesQuery`；结论：前端已经支持 `since/until` 范围查询，可作为自定义时间段统计的底层能力。
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTeamTimeStatsQuery`；结论：团队时间页走的是独立聚合查询，不是客户端对列表自行求和。
   - 证据：`apps/workspace/src/shared/api/client.ts` `getTeamTimeStats`；结论：时间统计的现有聚合 API 只有 `/api/time-entries/team-stats`。
3. 状态更新  
   - 证据：`apps/workspace/src/shared/query/keys.ts` `queryKeys.timeTracking.teamStats`；结论：团队统计已有独立 query key。
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`；结论：websocket 只失效 `entries/current/issueEntries`，没有失效 `teamStats`，所以现有统计页刷新并不完整。
4. 输出结果  
   - 证据：`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar`、`MyTimePage`；结论：`My Time` 输出的是计时操作卡片与按天列表，不是统计页。
   - 证据：`apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`；结论：`Team Time` 输出的是“总工时 + 按成员/按项目”两张表，属于轻量统计页。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `myTimeRoute` / `teamTimeRoute` | 现有时间能力以 `My Time` 和 `Team Time` 两个页面承载，没有独立 analytics 路由。 |
| `apps/workspace/src/features/layout/navigation.ts` | `primaryNav` | 导航层也没有“时间统计”独立入口，页面职责还未拆清。 |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` | `StartTimerBar` / `MyTimePage` | `My Time` 核心是开始计时、停止计时、编辑记录与按天浏览，属于操作页。 |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` | `useTimeEntriesQuery` | 列表查询支持 `since` / `until`，说明自定义时间段统计已有底层查询窗口。 |
| `server/pkg/db/queries/time_entry.sql` | `ListTimeEntriesByUserRange` | 后端已有用户维度的范围查询，可支撑今日/本周/本月/自定义窗口。 |
| `apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` | `TeamTimePage` | 当前统计 UI 只支持 `this-week` / `this-month` / `last-month`，且只展示成员/项目聚合。 |
| `server/internal/handler/time_entry.go` | `GetTeamTimeStats` | 服务端返回 `by_user` 和 `by_project`，没有标签、任务、每日分布。 |
| `server/pkg/db/queries/time_entry.sql` | `SumTimeEntriesByUserInWorkspace` / `SumTimeEntriesByProjectInWorkspace` | 现有聚合 SQL 只覆盖用户与项目。 |
| `apps/workspace/src/shared/types/time-entry.ts` | `TimeEntry` | time entry 已有 `issue_id` 与 `labels` 字段，后续可扩展任务/标签维度统计。 |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` | `useTimeTrackingSync` | 统计聚合未被 websocket 同步链路覆盖，是现有统计体验缺口之一。 |

## 数据模型或状态流

- 核心字段  
  - 证据：`apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`；结论：时间统计的原始事实来自 `start_time`、`stop_time`、`duration_seconds`、`issue_id`、`labels`。
  - 证据：`apps/workspace/src/shared/types/time-entry.ts` `TeamTimeStats`；结论：当前聚合输出只定义了 `since`、`until`、`by_user`、`by_project`。
- 状态如何变化  
  - 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useStartTimerMutation` / `useStopTimerMutation`；结论：time entry 的创建与停止会刷新记录列表，但不会自动刷新团队聚合。
- 写入点在哪里  
  - 证据：`server/cmd/server/router.go` `r.Route("/api/time-entries", ...)`；结论：时间数据写入仍由 `/api/time-entries` 系列接口负责，统计能力目前没有独立写入状态。
- 读取点在哪里  
  - 证据：`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `MyTimePage`；结论：个人页读取原始记录列表。
  - 证据：`apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`；结论：团队页读取聚合结果。

## 边界条件

- 权限边界  
  - 证据：`server/internal/handler/time_entry.go` `GetTeamTimeStats`；结论：团队统计依赖 `workspace_id` 和成员校验，属于 workspace 级只读数据。
- 空状态  
  - 证据：`apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`；结论：当前按成员/按项目表都已有空状态文案，可复用到统计页。
- 错误路径  
  - 证据：`server/internal/handler/time_entry.go` `GetTeamTimeStats`；结论：缺失或非法 `since/until` 会返回 `400`，统计页必须处理参数错误。
- 多租户边界  
  - 证据：`apps/workspace/src/shared/api/client.ts` `authHeaders`；结论：所有时间统计都依赖 `X-Workspace-ID`，不能跨 workspace 聚合。

## 未决问题

- 统计入口是否新增独立路由，还是在 `Team Time` 下扩成 analytics 子页；该项在 `design.md` 中给出推荐方案。
- 标签与任务维度是否共用同一个聚合 API；该项在 `design.md` 中统一定义为 group-by 契约，避免执行阶段自行拆口径。
