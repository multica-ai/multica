# Deadline Visibility Research

## 调研目标

确认 Multica 当前日期、任务视图和 schedule 展示能力，判断 deadline banner 是否可以作为小改动闭环实现。

## 现状链路

1. Issue 数据从后端 `ListIssues` 返回，包含 due/start/end。
2. 前端 issue store 或 query 获取 issue list。
3. Workbench 通过 `deriveTodayIssues` / `deriveUpcomingIssues` 派生 Today / Upcoming 页面。
4. 卡片和列表通过 `formatIssueSchedule` 展示日期，通过 `isIssueScheduleOverdue` 标红。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | Issue 类型已有 `due_date`、`start_date`、`end_date`，无需先加字段 |
| `apps/workspace/src/features/issues/utils/workbench-view.ts` | `deriveTodayIssues` | 今日任务可由日期字段前端派生 |
| `apps/workspace/src/features/issues/utils/workbench-view.ts` | `deriveUpcomingIssues` | 未来任务可由日期字段前端派生 |
| `apps/workspace/src/features/issues/utils/workbench-view.ts` | `isIssueScheduleOverdue` | 已有逾期判断，但只处理 due/end |
| `apps/workspace/src/features/issues/components/board-card.tsx` | `BoardCard` | 卡片已消费 schedule label 和 overdue 状态 |
| `apps/workspace/src/features/issues/components/list-row.tsx` | `ListRow` | 列表已消费 schedule label 和 overdue 状态 |
| `server/pkg/db/queries/issue.sql` | `ListIssues` | 支持 due/start/end date filters |
| `apps/workspace/src/router.tsx` | `TodayPage` / `UpcomingPage` | 已有 Today / Upcoming 路由与派生视图 |

## 数据模型或状态流

- 输入：`Issue[]`
- 派生：
  - `overdueIssues`: 非 terminal 且 due/end 早于今天。
  - `todayDeadlineIssues`: 非 terminal 且 due/end 在今天。
  - `upcomingDeadlineIssues`: 非 terminal 且 due/end 在未来 7 天。
  - `activeWindowIssues`: start/end 覆盖今天但未到 end。
- 输出：`DeadlineSummary`
  - `overdue_count`
  - `today_count`
  - `upcoming_count`
  - `top_items`

## 边界条件

- `done` 和 `cancelled` 不进入 active deadline summary。
- `due_date` 优先级高于 `start_date`，因为 deadline banner 表达“必须处理”的时间压力。
- 只有 `start_date` 没有 `end_date` 的任务不算 overdue。
- 没有日期的任务不进入 deadline summary。
- 日期比较第一版沿用当前前端 local day 口径，不引入用户时区设置。

## 未决问题

- 是否需要每个用户独立 dismiss banner？
- 是否要把 agent-assigned issue 纳入个人 deadline banner？
- 是否要把 project deadline 纳入同一 summary？
- 后续提醒是否走已有 notification preference，还是单独 reminder 模型？
