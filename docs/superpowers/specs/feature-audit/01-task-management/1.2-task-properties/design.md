# 1.2 任务属性配置设计

## 1. 目标

1. 在 issue 属性区补齐预计工作量。
2. 为重复规则提供稳定入口，但不抢 1.7 的系列建模职责。
3. 保持附件、标签、项目等既有属性入口不被重构打散。

## 2. 非目标

- 不重写附件存储。
- 不在本能力中完成重复任务生成逻辑。
- 不引入新的“任务扩展属性表单框架”。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `IssueDetail` | 属性编辑入口已集中。 |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | 缺少预计工作量与重复规则字段。 |
| `apps/workspace/src/features/issues/mutations.ts` · `updateIssueMutation` | 现有属性更新走部分更新 mutation，适合继续追加字段。 |

## 4. 缺口定义

- 缺预计工作量字段和 UI。
- 缺重复规则摘要、入口和与 1.7 的对接位。

## 5. 方案与权衡

### 方案 A：把预计工作量与重复规则都直接做进 issue 表

优点：单表直观。  
缺点：重复规则会与 1.7 的系列/实例语义耦死。

### 方案 B：预计工作量写入 issue；重复规则只保留引用和摘要，推荐

优点：最贴合 `updateIssue` 现状；1.7 仍可独立演进。  
缺点：需要跨文档引用 1.7。

## 6. 推荐方案

采用方案 B：在 issue 上新增 `estimated_minutes`；重复规则只在 issue 上保留 `recurrence_rule_id` 与 `recurrence_summary` 的展示契约，真正的规则结构、单次/全系列修改仍由 1.7 的系列模型负责。

## 7. 数据模型或状态模型

- `estimated_minutes: number | null`
- `recurrence_rule_id: string | null`
- `recurrence_summary: string | null`（可由后端派生返回）

## 8. 接口契约

- `updateIssue` 允许提交 `estimated_minutes`
- `getIssue` / `listIssues` 返回 `estimated_minutes`、`recurrence_rule_id`、`recurrence_summary`
- 1.7 落地后，详情页点击重复规则入口可打开共用编辑器

## 9. UI 或交互流程

- 在 `IssueDetail` 的属性区新增“预计工作量”和“重复规则”两行。
- “预计工作量”支持快捷输入与清空。
- “重复规则”在未配置时显示“未设置”；已配置时显示摘要并可跳入编辑。

### 页面交互流

```text
任务详情页
  -> 编辑“预计工作量”
  -> updateIssue
  -> 属性区回显

任务详情页
  -> 点击“重复规则”
  -> 打开 1.7 共用编辑器
  -> 保存后回显摘要
```

### 状态机

```text
未设置预计工作量 -> 已设置预计工作量 -> 已清空
未设置重复规则   -> 已绑定规则       -> 已移除规则
```

### 数据变化流

```text
IssueDetail
  -> updateIssue / recurrence editor
  -> issue handler
  -> issue table + recurrence rule relation
  -> issue query 返回属性摘要
  -> IssueDetail 回显
```

## 10. 权限、边界条件、异常路径

- `estimated_minutes` 为空表示未估算，不应强制为 0。
- 重复规则入口在 1.7 未实现前可显示只读占位，但契约字段必须先固定。
- 附件区继续由 `AttachmentList` 管理，不和预计工作量混在同一控件。

## 11. 实现约束

- 预计工作量继续复用 `updateIssue` 的部分更新，不新增独立 API。
- 重复规则字段名必须与 1.7 统一，避免“双字段迁移”。

## 12. 风险与对策

- 风险：1.2 先定义了错误的重复规则字段。  
  对策：只在 issue 上保留引用与摘要，不定义规则细节。
- 风险：预计工作量单位不一致。  
  对策：存储统一用分钟，显示层再做小时换算。

## 13. 验收检查

1. 详情页可设置、修改、清空预计工作量。
2. 列表和详情可回显预计工作量。
3. 详情页存在重复规则入口，能显示已配置摘要。
4. 1.7 落地后无需改动字段名即可接入同一入口。
