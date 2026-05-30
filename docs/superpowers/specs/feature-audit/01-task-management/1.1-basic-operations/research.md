# 1.1 基础任务操作调研

## 1. 调研目标

- 明确“归档 / 恢复 / 永久删除”在现有 issue 链路中的缺口。
- 确认当前创建、编辑、删除主链路，以便为归档生命周期设计推荐方案。

## 2. 现状链路

1. `apps/workspace/src/features/issues/mutations.ts` · `useIssueMutations.createIssueMutation` 负责创建 issue，说明“新增任务”已在前后端闭环。
2. `apps/workspace/src/features/issues/components/issue-detail.tsx` · `IssueDetail` 在详情页内直接编辑标题、描述、状态、优先级、项目、父任务等字段，说明“编辑任务”入口已存在。
3. `apps/workspace/src/features/issues/components/issue-detail.tsx` · `handleDelete` 直接调用删除 mutation，没有归档前置动作。
4. `server/internal/handler/issue.go` · `DeleteIssue` 直接执行删除请求。
5. `server/pkg/db/queries/issue.sql` · `DeleteIssue` 为物理删除 SQL，说明当前不存在归档态。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | issue 模型没有 `archived_at`、`archived_by` 或 `is_archived` 字段，归档尚无数据承载位。 |
| `apps/workspace/src/features/issues/mutations.ts` · `deleteIssueMutation` | 前端删除行为会使 React Query 中的 issue 列表直接失效重取，没有“先归档再确认删除”的中间状态。 |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `handleDelete` | 详情页只有删除入口，没有归档和恢复入口。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` / `CountListedIssues` | 列表查询没有归档谓词，因此即使未来补字段，也需要同步改主查询。 |
| `server/migrations/001_init.up.sql` · `issues` | 初始 issue 表结构只定义 `status`、`priority`、`project_id`、`parent_issue_id` 等业务字段，没有生命周期字段。 |

## 4. 数据模型或状态流

- `apps/workspace/src/shared/types/issue.ts` · `IssueStatus` 当前只表达工作状态，不表达生命周期。
- `server/pkg/db/queries/issue.sql` · `DeleteIssue` 当前直接移除记录，状态流实际是 `active -> deleted`，中间没有 `archived`。
- 结论：如果要支持“归档视图、恢复、批量归档”，必须补一条独立于 `status` 的生命周期流。

## 5. 边界条件

- `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker` 表明 issue 可能存在父子关系；归档父任务时必须定义对子任务的处理策略。
- `apps/workspace/src/shared/types/issue.ts` · `Issue.attachments` 表明归档不能破坏附件可见性。
- `server/pkg/db/queries/issue.sql` · `DeleteIssue` 现为物理删除，若继续允许“未归档直接永久删除”，会绕过恢复能力。

## 6. 未决问题

1. 归档是否允许对子任务级联，还是只阻止含未完成子任务的父任务归档。
2. 永久删除是否必须要求 issue 已处于归档态。
3. 已归档 issue 是否继续参与项目统计与时间统计。
