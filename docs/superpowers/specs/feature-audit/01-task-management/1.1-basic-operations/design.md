# 1.1 基础任务操作设计

## 1. 目标

1. 为 issue 增加独立的归档 / 恢复生命周期。
2. 保持创建、编辑、删除主链路不变，只在删除前补“归档”缓冲层。
3. 为 1.3 已归档视图和 1.5 批量归档提供统一后端契约。

## 2. 非目标

- 不实现跨实体回收站。
- 不把 `IssueStatus` 扩成生命周期状态。
- 不在本能力中处理重复任务实例生成。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | 当前缺少归档字段，只能靠删除移出列表。 |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `handleDelete` | 详情页已具备删除动作插槽，可复用为“归档 / 恢复 / 永久删除”入口。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` / `DeleteIssue` | 主查询与删除 SQL 都需要同时感知新生命周期，避免“能归档但查不出来”。 |

## 4. 缺口定义

- 归档缺少数据字段、API、列表谓词和 UI 入口。
- 删除缺少“仅允许对已归档项执行永久删除”的保护规则。
- 已归档任务缺少恢复路径和专用视图契约。

## 5. 方案与权衡

### 方案 A：把 `archived` 塞进 `IssueStatus`

优点：字段少。  
缺点：`status` 现在已经被 `server/pkg/db/queries/issue.sql:ListIssues` 用于工作流过滤；混入生命周期会污染 1.7 重复任务和 1.6 层级统计。

### 方案 B：新增独立归档字段，推荐

优点：工作状态与生命周期解耦；1.3、1.5 可直接复用；恢复时无需猜测原状态。  
缺点：需要改 schema、handler、查询参数和 UI。

## 6. 推荐方案

采用方案 B：在 issue 上新增 `archived_at`、`archived_by`（或等价操作者字段），保留现有 `status` 语义不变；新增 `archiveIssue` / `restoreIssue` 服务端动作，并让 `ListIssues`、`CountListedIssues` 支持 `archived_only` / `include_archived` 谓词。

## 7. 数据模型或状态模型

- `Issue.status`：继续表示 `todo`、`in_progress`、`done` 等工作状态。
- `Issue.archived_at`：`null` 表示活跃；非空表示已归档。
- `DeleteIssue`：仅对 `archived_at != null` 的 issue 开放永久删除。

## 8. 接口契约

- 新增 `POST /api/issues/:id/archive`
- 新增 `POST /api/issues/:id/restore`
- `GET /api/issues` 增加 `archived_only?: boolean`、`include_archived?: boolean`
- `DELETE /api/issues/:id` 在 issue 未归档时返回 409，提示先归档

## 9. UI 或交互流程

- `apps/workspace/src/features/issues/components/issue-detail.tsx` · `IssueDetail`：在更多操作菜单加入“归档”“恢复”“永久删除”。
- `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent`：补已归档视图查询参数。

### 页面交互流

```text
任务详情页
  -> 点击“归档”
  -> 调用 archiveIssue
  -> 列表移出活跃视图
  -> 用户进入“已归档”
  -> 点击“恢复”或“永久删除”
```

### 状态机

```text
active
  -> archived        (archive)
archived
  -> active          (restore)
archived
  -> permanently_deleted (delete)
```

### 数据变化流

```text
IssueDetail / BatchAction
  -> API Client
  -> handler/archive|restore|delete
  -> issue.sql(ListIssues/CountListedIssues/DeleteIssue)
  -> React Query invalidate
  -> 列表 / 详情页刷新
```

## 10. 权限、边界条件、异常路径

- 归档父任务时若存在未归档子任务，默认阻止并提示；原因见 `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker`，当前层级仍是显式父子关系。
- 恢复已归档任务时保留原 `status`、`project_id`、`labels`。
- 永久删除失败时必须保留已归档状态，不得回退成活跃态。

## 11. 实现约束

- 不得新增第二套“任务生命周期状态枚举”。
- `ListIssues` 与 `CountListedIssues` 必须共用相同归档谓词。
- 详情页与批量归档必须共用同一服务端动作。

## 12. 风险与对策

- 风险：归档和删除入口混淆。  
  对策：未归档项只显示“归档”，已归档项才显示“恢复 / 永久删除”。
- 风险：统计口径漂移。  
  对策：在 1.3 和 3.1 文档中显式规定默认统计仅计算未归档 issue。

## 13. 验收检查

1. 活跃任务可从详情页归档。
2. 已归档任务不会出现在默认列表。
3. 已归档任务可在专用视图恢复，恢复后保留原工作状态。
4. 未归档任务不能直接永久删除。
5. 列表条数与总数在归档过滤下保持一致。
