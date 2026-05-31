# 单能力 Research

## 调研目标

- 确认 4.2 任务统计在当前仓库里是否存在任何已实现链路。
- 确认可以复用哪些 issue 原始字段。
- 确认为什么现有 workbench / issue list 不能直接算作任务统计页。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：当前没有任何任务统计专属路由。
   - 证据：`apps/workspace/src/features/layout/navigation.ts` `primaryNav`；结论：导航没有任务统计入口。
2. 数据流  
   - 证据：`server/pkg/db/queries/issue.sql` `ListIssues`；结论：后端 issue 查询只支持筛选和列表，不返回统计聚合。
   - 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`；结论：前端只是对 `allIssues` 做筛选和展示，没有完成率或分布统计计算。
3. 状态更新  
   - 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `filterIssues` 调用链；结论：状态变化只影响列表可见项，不影响任何统计状态。
4. 输出结果  
   - 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPage`；结论：Today / Upcoming / Backlog 是工作视图，不是统计视图。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `routeTree` | 没有任务统计路由。 |
| `apps/workspace/src/features/layout/navigation.ts` | `primaryNav` | 没有任务统计导航入口。 |
| `server/pkg/db/queries/issue.sql` | `ListIssues` | issue 查询支持状态/优先级/日期筛选，但没有任何 count、distribution、completion rate 聚合。 |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | 原始任务数据已有 `status`、`priority`、`due_date`、`start_date`、`end_date`，可作为未来统计事实源。 |
| `apps/workspace/src/features/issues/components/workbench-issues-page.tsx` | `WorkbenchIssuesPageContent` | 工作台页面是基于 issue 列表的筛选结果，不是统计页。 |
| `apps/workspace/src/features/issues/components/issues-header.tsx` | `BulkImportButton` | 任务页当前强化的是导入与筛选操作，而不是统计摘要。 |
| 代码搜索 `apps/workspace/src`、`server` | `rg(task analytics|任务统计|completion rate|priority distribution|完成率统计|优先级分布)` | 未找到匹配，说明 4.2 在代码侧没有现成实现。 |

## 数据模型或状态流

- 核心字段  
  - 证据：`apps/workspace/src/shared/types/issue.ts` `Issue`；结论：任务统计可以直接复用 `status`、`priority`、`due_date`、`updated_at` 等字段。
- 状态如何变化  
  - 证据：`server/pkg/db/queries/issue.sql` `ListIssues`；结论：当前状态变化只影响筛选结果集，没有统计聚合缓存或快照。
- 写入点在哪里  
  - 证据：`server/cmd/server/router.go` `r.Route("/api/issues", ...)`；结论：任务数据写入集中在 issue API，统计目前没有独立 handler。
- 读取点在哪里  
  - 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`；结论：当前前端读取点仍是 issue 列表 store。

## 边界条件

- 权限边界  
  - 证据：`server/cmd/server/router.go` `RequireWorkspaceMember`；结论：未来任务统计也必须保持 workspace 级权限边界。
- 空状态  
  - 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `emptyTitle` / `emptyDescription`；结论：现有工作台有空状态，但统计页要有自己的空状态定义。
- 错误路径  
  - 证据：`server/pkg/db/queries/issue.sql` `ListIssues`；结论：当前列表接口没有统计参数校验逻辑，未来聚合接口要单独定义非法范围/非法口径报错。
- 多租户边界  
  - 证据：`apps/workspace/src/shared/api/client.ts` `authHeaders`；结论：任务统计仍应以 workspace 维度隔离。

## 未决问题

- “完成率”是按任务数、按状态映射、还是按计划窗口计算；该项在 `design.md` 内固定为明确口径。
- 逾期任务数是否按当前时刻实时计算还是按所选时间窗口末端计算；该项在 `design.md` 内统一。
