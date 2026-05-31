# 1.4 筛选与排序设计

## 1. 目标

1. 明确各类筛选的归属边界。
2. 补齐“最后更新时间排序”。
3. 保证 1.3 视图扩展时不复制筛选逻辑。

## 2. 非目标

- 不把全部筛选立即迁移到后端。
- 不重写 3.2 的标签筛选设计。
- 不新建独立的“高级筛选 DSL”。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `queryParams` | 搜索/项目/日期已服务端化。 |
| `apps/workspace/src/features/issues/stores/view-store.ts` · `IssueViewState` | 状态、优先级、负责人、创建者与排序已前端本地化。 |
| `apps/workspace/src/features/issues/utils/sort.ts` · `sortIssues` | `updated_at` 是唯一明确缺失的排序维度。 |

## 4. 缺口定义

- 缺明确的筛选归属说明。
- 缺 `updated_at` 排序状态、比较逻辑和 UI 入口。

## 5. 方案与权衡

### 方案 A：只补 `updated_at` 排序，并把边界写进文档，推荐

优点：与现有实现最一致，影响最小。  
缺点：仍保留“服务端过滤 + 前端过滤”混合模型。

### 方案 B：全部筛选排序统一迁到后端

优点：单一事实源。  
缺点：明显超出当前缺口，也会打断 3.2 的设计包边界。

## 6. 推荐方案

采用方案 A，并把当前阶段的筛选归属固定为：

- 服务端：搜索、项目、日期、标签。
- 前端：状态、优先级、负责人、创建者。
- 前端排序：position、priority、due_date、created_at、updated_at、title。

## 7. 数据模型或状态模型

- `SortField` 新增 `updated_at`。
- 默认排序方向：`updated_at` 使用降序，保证最近变更优先。

## 8. 接口契约

- 服务端无需新增字段；继续返回 `Issue.updated_at`。
- 视图状态新增 `sortBy = updated_at`。
- 四象限、时间轴、列表视图都复用同一排序枚举。

## 9. UI 或交互流程

- 列表排序菜单新增“最后更新时间”。
- 切换排序后立即更新本地视图状态，并对当前结果集重排。

### 页面交互流

```text
任务列表页
  -> 打开排序菜单
  -> 选择“最后更新时间”
  -> 更新 view-store
  -> 重新执行 sortIssues
  -> 列表重排
```

### 状态机

```text
默认排序
  -> priority
  -> due_date
  -> created_at
  -> updated_at
  -> title
```

### 数据变化流

```text
Sort UI
  -> useIssueViewStore.setSortBy
  -> sortIssues(updated_at)
  -> ListView / FlatIssueList
  -> 用户看到新顺序
```

## 10. 权限、边界条件、异常路径

- 如果 issue 缺少 `updated_at`，回退到 `created_at` 比较。
- 标签筛选在 UI 上可出现在同一栏位，但数据来源必须遵循 3.2 的服务端查询契约。

## 11. 实现约束

- 不得在 `filterIssues` 中再追加标签的本地过滤分支。
- `SortField`、排序菜单文案、`sortIssues` 必须保持同一枚举集合。

## 12. 风险与对策

- 风险：列表和其他视图排序枚举不一致。  
  对策：只允许从 `view-store` 导出排序枚举。
- 风险：未来改服务端排序时与本包冲突。  
  对策：届时先更新本设计的归属表，再迁实现。

## 13. 验收检查

1. 列表页可选择“最后更新时间”排序。
2. 最近编辑的任务默认排在更前。
3. 筛选归属在实现中与本设计一致。
4. 标签筛选没有在本地重复实现。
