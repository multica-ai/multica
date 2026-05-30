# 1.5 批量操作设计

## 1. 目标

1. 补齐批量项目迁移、批量标签增删、批量归档。
2. 保持现有“快速操作”效率，不把工具栏做成不可用的按钮堆。

## 2. 非目标

- 不实现跨页面长期批量选择。
- 不在本能力中设计 AI 规则批处理。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx` · `BatchActionToolbar` | 已有统一批量交互入口。 |
| `apps/workspace/src/features/issues/mutations.ts` · `batchUpdateIssuesMutation` / `batchDeleteIssuesMutation` | 批量 mutation 已存在，可承接更多字段。 |
| `docs/superpowers/specs/feature-audit/03-project-and-labels/3.2-label-management/design.md` · `推荐方案` | 标签增删必须复用 3.2 的服务端批量标签契约。 |

## 4. 缺口定义

- 工具栏没有项目迁移、标签增删、归档。
- 服务端没有“批量标签增删而非覆盖”的明确契约。

## 5. 方案与权衡

### 方案 A：继续往工具栏堆按钮

优点：改动少。  
缺点：`BatchActionToolbar` 已有快速按钮，继续堆会让复杂操作难发现。

### 方案 B：保留快捷动作，复杂动作进入批量编辑面板，推荐

优点：兼顾效率与可扩展性。  
缺点：需要新增面板组件。

## 6. 推荐方案

采用方案 B：保留“状态、优先级、负责人、删除”为快捷动作，新增“批量编辑”入口；在批量编辑面板中承载项目迁移、标签增加、标签移除、归档。

## 7. 数据模型或状态模型

- `batchUpdateIssues`：支持 `project_id`、`archived`。
- `batchLabelOperation`：支持 `add_label_ids[]`、`remove_label_ids[]`。
- 选中状态继续复用现有 selection store。

## 8. 接口契约

- 复用或扩展 `batchUpdateIssues`
- 新增 `POST /api/issues/batch-labels`
- 批量归档复用 1.1 的归档语义

## 9. UI 或交互流程

- 多选 issue 后展示工具栏。
- 点击“批量编辑”打开面板。
- 面板中选择目标项目、要新增/移除的标签、是否归档。

### 页面交互流

```text
多选任务
  -> 打开批量编辑
  -> 选择项目 / 标签 / 归档
  -> 提交批量 mutation
  -> 工具栏提示成功并刷新列表
```

### 状态机

```text
未选择
  -> 已选择
  -> 批量编辑中
  -> 提交成功
  -> 未选择
```

### 数据变化流

```text
Selection Store
  -> Batch Edit Panel
  -> batchUpdateIssues / batchLabelOperation
  -> issue handler / label relation handler
  -> invalidate issue queries
  -> 列表刷新
```

## 10. 权限、边界条件、异常路径

- 已归档任务默认不允许再次批量归档。
- 标签操作必须是增量语义，不能覆盖未知标签。
- 若部分 issue 因权限或状态失败，面板应返回逐项失败信息。

## 11. 实现约束

- 快捷动作和批量编辑面板不能分别维护不同的选中源。
- 标签批量能力必须直接引用 3.2 的接口定义。

## 12. 风险与对策

- 风险：工具栏过重。  
  对策：复杂动作收敛到批量编辑面板。
- 风险：批量标签覆盖误删。  
  对策：只允许 add/remove 增量接口。

## 13. 验收检查

1. 批量编辑面板可修改项目。
2. 批量标签支持增加与移除，不覆盖无关标签。
3. 批量归档只能作用于未归档任务。
4. 成功后列表与选中状态同步刷新。
