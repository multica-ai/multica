# 1.6 子任务与层级调研

## 1. 调研目标

- 确认现有子任务模型和 UI 入口。
- 找出无限层级、拖拽层级、聚合时间/进度的真实缺口。

## 2. 现状链路

1. `apps/workspace/src/shared/types/issue.ts` · `Issue.parent_issue_id` / `Issue.child_issues` 已表达父子关系。
2. `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker` 提供选择父任务入口。
3. 列表查询仍以平铺 issue 为主，没有递归读模型。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue.parent_issue_id` | 现有模型支持单父节点树。 |
| `apps/workspace/src/shared/types/issue.ts` · `Issue.child_issues` | 前端已经预留子任务数组，但不是递归查询结果的稳定来源。 |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker` | 当前只支持在详情页手动设父任务，没有拖拽层级。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` | 服务端列表仍是平铺分页查询，没有树形查询和层级聚合字段。 |

## 4. 数据模型或状态流

- 当前层级是 adjacency list：子任务记录自己的 `parent_issue_id`。
- 没有 `depth`、`path`、`aggregated_time`、`aggregated_progress` 等派生字段。

## 5. 边界条件

- 单父模型意味着能力范围是树，不是多父依赖图。
- 若直接在前端用 `child_issues` 拼树，会受分页与局部加载影响。

## 6. 未决问题

1. 无限层级是否需要在本阶段就支持拖拽重排。
2. 聚合时间是否以实际 time entry 总和还是预计工作量汇总为准。
