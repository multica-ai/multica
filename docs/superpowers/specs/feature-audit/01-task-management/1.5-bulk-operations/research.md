# 1.5 批量操作调研

## 1. 调研目标

- 确认现有批量工具栏已覆盖的操作。
- 明确缺失的项目迁移、标签增删、归档与依赖关系。

## 2. 现状链路

1. `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx` · `BatchActionToolbar` 负责选中 issue 后的批量操作。
2. `apps/workspace/src/features/issues/mutations.ts` · `batchUpdateIssuesMutation` / `batchDeleteIssuesMutation` 已支持批量更新与批量删除。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx` · `statusAction` / `priorityAction` / `assigneeAction` / `deleteAction` | 当前仅有状态、优先级、负责人、删除。 |
| `apps/workspace/src/features/issues/mutations.ts` · `batchUpdateIssuesMutation` | 服务端已具备“批量改字段”的技术路径，项目迁移与归档可优先复用。 |
| `docs/superpowers/specs/feature-audit/03-project-and-labels/3.2-label-management/design.md` · `推荐方案` | 批量标签操作已在 3.2 定义为服务端能力，本包应直接引用。 |

## 4. 数据模型或状态流

- 当前批量操作以“先多选 -> 再弹工具栏 -> 最后提交 mutation”为主。
- 缺口集中在操作集合，而不是选中模型。

## 5. 边界条件

- 归档批量操作依赖 1.1 的归档接口。
- 批量标签增删不能通过覆盖 `label_ids` 粗暴实现，否则会丢失并发修改。

## 6. 未决问题

1. 项目迁移与标签增删是继续放工具栏，还是进批量编辑面板。
2. 批量归档是否允许混入已归档任务。
