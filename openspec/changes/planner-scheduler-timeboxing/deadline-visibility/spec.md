# Deadline Visibility Spec

## 背景

Super Productivity 的 deadline 能力不只是日期字段。官方 v17.5 release notes 描述了 task deadline badge、detail panel deadline、approaching reminders、today banner、planner overdue section 和 context menu deadline。Multica 当前已有 due/start/end 日期和 today/upcoming/overdue 派生能力，但这些信息分散在任务卡片、列表和 workbench 视图里，没有统一 deadline visibility。

## 范围

本能力第一版覆盖：

- 顶部 deadline banner：显示已逾期、今日到期、未来 7 天即将到期的数量。
- deadline section：在 Today / My Time / Calendar 相关页面提供可点击的 deadline 摘要入口。
- issue deadline badge：复用现有 schedule label，让 due/end overdue 和 today 状态更明确。
- 只读 summary：第一版不做 reminder，不做用户 dismiss 持久化。

## 当前状态

- Issue 已有 `due_date`、`start_date`、`end_date` 字段。
- 前端已有 today/upcoming/overdue 计算。
- 任务卡片和列表已有 schedule label 展示。
- 后端 issue list 查询已有 due/start/end 日期过滤。

## 证据

- `apps/workspace/src/shared/types/issue.ts` `Issue`：定义 `due_date`、`start_date`、`end_date`。
- `apps/workspace/src/features/issues/utils/workbench-view.ts` `deriveTodayIssues`：根据 due/start/end 推导今日任务。
- `apps/workspace/src/features/issues/utils/workbench-view.ts` `deriveUpcomingIssues`：根据 due/start/end 推导未来任务。
- `apps/workspace/src/features/issues/utils/workbench-view.ts` `isIssueScheduleOverdue`：根据 due/end 判断逾期。
- `apps/workspace/src/features/issues/components/board-card.tsx` `BoardCard`：使用 schedule label 和 overdue 状态展示卡片日期。
- `apps/workspace/src/features/issues/components/list-row.tsx` `ListRow`：使用 schedule label 和 overdue 状态展示列表日期。
- `server/pkg/db/queries/issue.sql` `ListIssues`：支持 due/start/end 日期范围过滤。

## 缺口

- 没有跨页面 deadline summary。
- 没有 today deadline banner。
- 没有 planner overdue section。
- 没有统一 deadline severity 规则。
- 没有后端 deadline summary 聚合接口；第一版可以先前端派生，但后续 reminder 需要后端能力。

## 交接说明

执行 Agent 应优先实现前端派生的最小版本，不新增数据库。若需要后端聚合接口，必须先回写本 spec 和 `design.md`，再进入实现。
