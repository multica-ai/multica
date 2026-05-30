# 1.4 筛选与排序调研

## 1. 调研目标

- 确认现有筛选、排序分别由哪一层负责。
- 明确剩余缺口，尤其是“最后更新时间排序”和标签筛选归属。

## 2. 现状链路

1. `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` 把搜索词、项目、日期区间放进服务端查询参数。
2. `apps/workspace/src/features/issues/stores/view-store.ts` · `IssueViewState` 维护状态、优先级、负责人、创建者与排序状态。
3. `apps/workspace/src/features/issues/utils/filter.ts` · `filterIssues` 在前端执行状态、优先级、负责人、创建者过滤。
4. `apps/workspace/src/features/issues/utils/sort.ts` · `SortField` / `sortIssues` 在前端执行排序。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `queryParams` | 搜索、项目、日期过滤已经服务端化。 |
| `apps/workspace/src/features/issues/stores/view-store.ts` · `SortField` | 当前排序枚举只有 `position`、`priority`、`due_date`、`created_at`、`title`，没有 `updated_at`。 |
| `apps/workspace/src/features/issues/utils/sort.ts` · `sortIssues` | 排序实现没有最后更新时间分支。 |
| `docs/superpowers/specs/feature-audit/03-project-and-labels/3.2-label-management/design.md` · `推荐方案` | 标签筛选已经在 3.2 被定义为服务端过滤能力，本包不应重复定义。 |

## 4. 数据模型或状态流

- 服务端过滤：搜索、项目、日期。
- 前端过滤：状态、优先级、负责人、创建者。
- 前端排序：position、priority、due_date、created_at、title。
- 缺口：`updated_at` 排序，以及筛选职责的文档化边界。

## 5. 边界条件

- `apps/workspace/src/shared/types/issue.ts` · `Issue.updated_at` 已存在，因此“最后更新时间排序”不需要新字段，只缺状态枚举和 UI 接线。
- 标签筛选本轮必须引用 3.2；若再在 1.4 自造前端标签筛选，会与服务端标签过滤冲突。

## 6. 未决问题

1. `updated_at` 排序是否默认降序。
2. board/calendar 视图是否同步暴露 `updated_at` 排序入口。
