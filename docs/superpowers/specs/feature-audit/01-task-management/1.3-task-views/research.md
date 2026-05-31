# 1.3 任务视图调研

## 1. 调研目标

- 盘点现有任务视图入口与服务端视图参数。
- 找出“已完成 / 已归档 / 四象限 / 时间轴”缺口和依赖。

## 2. 现状链路

1. `apps/workspace/src/router.tsx` · `issuesRoute` / `backlogRoute` / `todayRoute` / `upcomingRoute` / `boardRoute` / `issueCalendarRoute` 已提供列表、看板、日历、backlog、today、upcoming 路由。
2. `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` 负责列表视图数据加载与筛选。
3. `server/pkg/db/queries/issue.sql` · `ListIssues` 已支持 `view = backlog|today|upcoming`，但没有 `completed`、`archived`、`timeline`、`quadrant`。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/router.tsx` · `boardRoute` / `issueCalendarRoute` | 看板和日历已经是独立路由，新增视图更适合继续走路由化模式。 |
| `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` | 主列表页已经有搜索、项目、日期与 chip 反馈，适合承接“已完成 / 已归档”类视图。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` | 服务端视图枚举目前只覆盖 backlog/today/upcoming，没有已完成与已归档。 |

## 4. 数据模型或状态流

- `Issue.status` 已可表达 done/cancelled，因此“已完成视图”可直接建立在状态过滤上。
- “已归档视图”依赖 1.1 的 `archived_at` 或等价字段。
- 四象限与时间轴当前没有专用 persisted 视图状态，可视为展示层组合能力。

## 5. 边界条件

- 四象限需要同时依赖优先级与时间字段；当前 issue 只有 `due_date`，没有独立“重要性矩阵”字段。
- 时间轴若只靠 `due_date`，无法表达持续区间；需要与 `start_date` / `end_date` 一致。

## 6. 未决问题

1. 已完成视图是否默认同时包含 `done` 与 `cancelled`。
2. 四象限是否允许无截止日期 issue 进入“重要不紧急”默认区。
