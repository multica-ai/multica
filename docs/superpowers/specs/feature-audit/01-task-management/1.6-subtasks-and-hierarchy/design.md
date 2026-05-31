# 1.6 子任务与层级设计

## 1. 目标

1. 在现有单父树模型上补齐递归层级读取。
2. 让任务详情和列表都能稳定展示多层级子任务。
3. 为进度与时间聚合定义统一口径。

## 2. 非目标

- 不做跨项目 DAG 依赖图。
- 不在首阶段实现复杂拖拽排序动画。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue.parent_issue_id` / `Issue.child_issues` | 数据模型已经是树形雏形。 |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker` | 父任务入口已存在。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` | 缺递归查询，因此无限层级仍未真正成立。 |

## 4. 缺口定义

- 缺树形查询与层级展示。
- 缺聚合时间、聚合进度的定义。
- 缺拖拽/移动层级的后续扩展位。

## 5. 方案与权衡

### 方案 A：继续只靠 `parent_issue_id`，前端递归拼树

优点：改动少。  
缺点：分页下不可靠，无法做聚合统计。

### 方案 B：保持 adjacency list 存储，但增加递归读模型，推荐

优点：兼容现有 schema；能补深度、路径、聚合字段。  
缺点：服务端查询复杂度提高。

## 6. 推荐方案

采用方案 B：继续以 `parent_issue_id` 为写入事实源，但由服务端补充递归查询与树形响应；前端统一消费 `depth`、`path`、`aggregated_time_seconds`、`aggregated_progress` 等派生字段。

## 7. 数据模型或状态模型

- 存储层：保留 `parent_issue_id`
- 读模型：
  - `depth`
  - `path`
  - `child_count`
  - `aggregated_time_seconds`
  - `aggregated_progress`

## 8. 接口契约

- `GET /api/issues/tree`
- `PATCH /api/issues/:id/parent`
- 列表响应可带 `depth`、`has_children` 等只读字段

## 9. UI 或交互流程

- 列表页支持缩进展开层级。
- 详情页子任务区支持显示多层级和聚合摘要。
- 拖拽重排作为低优先级第二阶段，仅在树形读模型稳定后再做。

### 页面交互流

```text
列表页
  -> 展开父任务
  -> 加载子树
  -> 点击子任务
  -> 打开详情页
  -> 修改父任务
  -> 列表树刷新
```

### 状态机

```text
平铺列表
  -> 树形已展开
  -> 修改层级中
  -> 树形已刷新
```

### 数据变化流

```text
ParentIssuePicker / Tree View
  -> update parent API
  -> issue tree query
  -> 聚合进度/时间计算
  -> Tree UI / Detail UI 回显
```

## 10. 权限、边界条件、异常路径

- 禁止把任务设为自己的祖先节点，避免环。
- 聚合时间默认汇总子孙任务的实际 time entry；预计工作量汇总留作后续附加指标。
- 拖拽层级被标记为低优先级，因为当前仅有 `ParentIssuePicker` 入口，没有现成树形拖拽基线。

## 11. 实现约束

- 写入事实源只能是 `parent_issue_id`，不能再建第二套父子关系表。
- 层级查询必须支持分页或懒加载，避免一次性加载整棵大树。

## 12. 风险与对策

- 风险：递归查询性能抖动。  
  对策：先按项目或根节点分段加载。
- 风险：前端树形与列表排序冲突。  
  对策：层级模式与平铺排序模式分开定义。

## 13. 验收检查

1. 能展示多层级子任务。
2. 修改父任务后不会形成环。
3. 列表与详情都能展示聚合进度或时间摘要。
4. 拖拽重排被明确标为第二阶段低优先级。
