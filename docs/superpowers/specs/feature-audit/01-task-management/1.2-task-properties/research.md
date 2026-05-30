# 1.2 任务属性配置调研

## 1. 调研目标

- 明确 issue 详情页已经承载的属性项。
- 找出“预计工作量”“重复规则入口”缺失在哪里，以及它们与 1.7 的边界。

## 2. 现状链路

1. `apps/workspace/src/features/issues/components/issue-detail.tsx` · `IssueDetail` 已支持标题、描述、状态、优先级、项目、标签、截止日期、父任务。
2. `apps/workspace/src/features/issues/components/attachment-list.tsx` · `AttachmentList` 已是附件入口，说明附件不是空白能力。
3. `apps/workspace/src/shared/types/issue.ts` · `Issue` 没有预计工时或重复规则字段。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ProjectPicker` / `LabelPicker` / `DueDatePicker` / `ParentIssuePicker` | 属性配置主要集中在详情页右侧属性区，新增属性应复用该结构。 |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | 没有 `estimated_minutes`、`recurrence_rule_id` 等字段，说明“预计工作量”和“重复规则”缺少数据承载位。 |
| `apps/workspace/src/features/issues/components/attachment-list.tsx` · `AttachmentList` | 附件列表已经存在，当前缺口不是“从零开始做附件”，而是属性区入口和规则一致性。 |

## 4. 数据模型或状态流

- issue 属性更新当前都汇总到 `apps/workspace/src/features/issues/mutations.ts` · `updateIssueMutation`。
- 结论：预计工作量应沿用 `updateIssue` 的部分更新模式；重复规则应只暴露为 issue 属性入口，其系列与实例逻辑由 1.7 定义。

## 5. 边界条件

- 1.2 不能在没有 1.7 系列模型前先自造重复规则字段语义。
- 1.2 不应重写附件上传后端；附件当前已在 issue 模型内可见。

## 6. 未决问题

1. 预计工作量采用分钟、小时，还是两者都显示。
2. 重复规则入口是内嵌编辑，还是跳转到 1.7 的专用弹层。
