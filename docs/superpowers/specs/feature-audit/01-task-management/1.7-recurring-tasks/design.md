# 1.7 重复任务设计

## 1. 目标

1. 支持每日、每周、每月、每年和自定义重复规则。
2. 在完成当前实例时自动生成下一次实例。
3. 支持修改单次实例与修改全部未来实例。

## 2. 非目标

- 不实现自然语言规则解析。
- 不实现节假日/工作日复杂日历。
- 不实现跨工作区共享系列。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | issue 是任务主模型，重复任务必须继续产出 issue 实例。 |
| `server/pkg/db/queries/issue.sql` · `UpdateIssue` | 当前状态更新在完成任务时就会触发，是生成下一实例的自然时机。 |
| 搜索关键词 `recurr` / `repeat` / `recurring` / `recurrence` | 未找到匹配，当前必须从零定义系列模型。 |

## 4. 缺口定义

- 缺系列与实例模型。
- 缺规则编辑入口、生成时机和改单次/改全部未来实例语义。

## 5. 方案与权衡

### 方案 A：把规则字段直接放在 issue 上，实例复用同一条记录

优点：实现看似简单。  
缺点：会丢失历史实例，不支持改单次/改全部未来实例。

### 方案 B：引入“系列 + 实例”模型，实例仍然是 issue，推荐

优点：兼容现有 issue 生态；能保留历史；可支持改单次/改未来。  
缺点：需要新表与生成逻辑。

## 6. 推荐方案

采用方案 B：新增 `issue_recurrence_rules`（系列规则）与 `issue_recurrence_overrides`（单次例外）等价结构；每个实际待办仍然是一条 issue，并通过 `recurrence_rule_id`、`recurrence_instance_at` 与系列关联。当前实例转为完成时，由 handler 同步创建下一实例。

## 7. 数据模型或状态模型

- `issue_recurrence_rules`
  - `id`
  - `anchor_issue_id`
  - `frequency`（daily/weekly/monthly/yearly/custom）
  - `interval`
  - `by_weekday`
  - `by_monthday`
  - `timezone`
  - `ends_at`
- `issue_recurrence_overrides`
  - `rule_id`
  - `instance_at`
  - `action`（skip / replace）
- `issues`
  - `recurrence_rule_id`
  - `recurrence_instance_at`

## 8. 接口契约

- `POST /api/issues/:id/recurrence-rule`
- `PATCH /api/issues/:id/recurrence-rule?scope=this|future`
- `DELETE /api/issues/:id/recurrence-rule?scope=this|future`
- `UpdateIssue(status=done)` 时，如存在规则则尝试生成下一实例

## 9. UI 或交互流程

- 入口放在 1.2 的属性区。
- 保存规则时可选择“仅本次”“本次及未来”。
- 完成实例后自动提示“已生成下一次任务”。

### 页面交互流

```text
任务详情页
  -> 打开重复规则编辑器
  -> 选择频率 / 周期 / 结束条件
  -> 保存
  -> issue 显示规则摘要

完成当前实例
  -> UpdateIssue(status=done)
  -> 生成下一实例
  -> 列表出现新任务
```

### 状态机

```text
无重复规则
  -> 已绑定系列
已绑定系列
  -> 当前实例进行中
当前实例完成
  -> 下一实例已生成
已绑定系列
  -> 单次例外 / 未来规则已更新
```

### 数据变化流

```text
Recurrence Editor
  -> recurrence rule API
  -> recurrence rule table / override table
  -> issue query 返回 recurrence_summary
  -> 完成当前实例
  -> UpdateIssue(status=done)
  -> handler 生成下一实例
  -> issue list 刷新
```

## 10. 权限、边界条件、异常路径

- 若当前实例被取消而非完成，不自动生成下一实例，由规则设置决定是否允许取消后补生成。
- 单次修改只能生成 override，不得直接改写系列源规则。
- 为避免与 1.1 冲突，归档实例不影响系列存活；系列停用需显式关闭规则。

## 11. 实现约束

- 不能通过扩展 `IssueStatus` 表达系列状态。
- 生成下一实例时必须复制项目、标签、预计工作量等属性，但保留实例独立完成历史。

## 12. 风险与对策

- 风险：同步生成下一实例导致并发重复创建。  
  对策：以 `(rule_id, recurrence_instance_at)` 做唯一约束。
- 风险：改单次和改未来语义混淆。  
  对策：所有编辑弹窗都必须显式要求 scope 选择。

## 13. 验收检查

1. 支持每日、每周、每月、每年和自定义规则。
2. 完成当前实例后会生成下一实例。
3. 可区分“改单次”和“改未来全部实例”。
4. 实例仍然可参与项目、标签、时间记录与归档。
