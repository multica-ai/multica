# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `4.1 时间统计`；结论：清单要求时间统计覆盖今日/本周/本月/自定义、项目/标签/任务维度，以及每日分布图表。
- 证据：`apps/workspace/src/router.tsx` `myTimeRoute` / `teamTimeRoute`；结论：仓库当前已有时间相关页面，但页面职责分散在操作页与轻量统计页之间，需要单独设计来补齐产品面。

## 范围

- 本次覆盖：独立时间统计入口、时间范围预设、项目/标签/任务维度、每日分布图表、与现有操作页的边界。
- 本次不覆盖：计时器创建/停止/编辑流程重做，`dashboard.md`，以及与时间统计无关的设置页面。

## 当前状态

- 证据：`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `MyTimePage`；结论：当前个人时间页是操作页，不是统计页。
- 证据：`apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`；结论：当前只存在团队级按成员/项目汇总，属于部分统计能力。
- 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTimeEntriesQuery`；结论：自定义时间段的底层查询已经存在，但还没有稳定的统计产品承接它。

## 证据

- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar`：支持开始计时，说明这是操作页。
- `apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`：只提供 `By Member` / `By Project` 两个汇总表。
- `server/internal/handler/time_entry.go` `GetTeamTimeStats`：后端聚合仅返回 `by_user` 与 `by_project`。
- `server/pkg/db/queries/time_entry.sql` `ListTimeEntriesByUserRange`：已具备自定义时间窗口原始数据查询。
- `apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`：原始记录已有 `issue_id` 和 `labels`，后续能支撑任务/标签维度。

## 缺口

1. 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：没有独立时间统计入口，用户只能在操作页和轻量团队页之间切换。
2. 证据：`server/pkg/db/queries/time_entry.sql` `SumTimeEntriesByUserInWorkspace` / `SumTimeEntriesByProjectInWorkspace`；结论：现有聚合没有标签、任务、日分布 bucket。
3. 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`；结论：统计聚合未纳入实时刷新链路，统计体验存在陈旧风险。

## 交接说明

- 后续优先看：`research.md` 中的“现状链路”和 `design.md` 中的推荐方案。
- 进入实现前必须先确认：统计页独立入口、聚合 API 是否统一为 analytics 契约、以及标签/任务/日分布的口径。
- 当前“自定义时间段统计”仍只算部分完成；证据：`useTimeEntriesQuery` 虽有 `since/until`，但没有独立统计 UI。
