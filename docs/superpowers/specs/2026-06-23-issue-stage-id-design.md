# Issue Stage ID 设计文档

## 背景

当前 workflow 的 `stage_id` 只存在于 `multica_workflow_node` 上，用于在 workflow overview 中按 stage 分组展示 node。Issue 表目前没有 `stage_id` 字段。

本设计目标：给所有 issue 增加一个通用的 `stage_id` 字段，并复用现有的 `multica_workflow_stage` 表。

## 决策摘要

- **Stage 来源：** 复用 `multica_workflow_stage` 表，不新建 issue stage 表。
- **耦合方式：** 紧耦合。`issue.stage_id` 必须属于 `issue.workflow_id` 对应的 workflow；没有 workflow 的 issue 不能设置 stage。
- **赋值方式：** 手动设置为主；workflow 下生成的 sub-issue 自动继承对应 node 的 `stage_id`。
- **Workflow 变更行为：** 当 issue 的 workflow 变更或清空时，`stage_id` 自动清空。

## 数据模型

### 数据库变更

```sql
ALTER TABLE multica_issue
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_stage_id ON multica_issue(stage_id);
```

### 约束规则

1. 外键 `ON DELETE SET NULL`：stage 或 workflow 被删除时，`issue.stage_id` 自动置空。
2. 应用层校验：
   - 如果 `stage_id` 非空，则 `workflow_id` 必须非空。
   - `stage_id` 必须指向一个属于 `issue.workflow_id` 的 stage。
3. Workflow 变更时：如果新的 `workflow_id` 与旧值不同，`stage_id` 强制清空。
4. Sub-issue 继承：由 workflow node run 创建的 sub-issue，其 `stage_id` 等于对应 `workflow_node.stage_id`。

## API 变更

### 请求 DTO

- `CreateIssueRequest.stage_id?: string | null`
- `UpdateIssueRequest.stage_id?: string | null`
- `BatchUpdateIssuesRequest.stage_id?: string | null`

### 响应 DTO

- `IssueResponse.stage_id: string | null`

### 后端校验函数

```go
func (h *Handler) validateIssueStage(
    ctx context.Context,
    qtx *db.Queries,
    workflowID pgtype.UUID,
    stageID pgtype.UUID,
) error {
    if !stageID.Valid {
        return nil
    }
    if !workflowID.Valid {
        return fmt.Errorf("stage_id requires workflow_id")
    }
    stage, err := qtx.GetWorkflowStage(ctx, stageID)
    if err != nil {
        return fmt.Errorf("stage not found: %w", err)
    }
    if stage.WorkflowID != workflowID {
        return fmt.Errorf("stage does not belong to workflow")
    }
    return nil
}
```

### Workflow 变更清空逻辑

在 `UpdateIssue` 中比较旧 `workflow_id` 与新 `workflow_id`：
- 若不同，将 `stage_id` 设为 null。
- 再对新值进行 stage 校验。

### Sub-issue 创建

在 `createWorkflowSubIssue` 中：
- 读取 `workflow_node.stage_id`。
- 创建 sub-issue 时传入 `StageID: node.StageID`。

## 前端变更

### 类型与 Schema

`packages/core/types/issue.ts`：

```typescript
export interface Issue {
  // ... existing fields
  stage_id: string | null;
}
```

`packages/core/api/schemas.ts`：

```typescript
const IssueSchema = z.object({
  // ... existing fields
  stage_id: z.string().nullable().default(null),
}).loose();
```

### Issue 详情页

- 在 assignee/workflow 选择器附近新增 stage 下拉选择器。
- 未分配 workflow 时禁用，提示"先分配 workflow"。
- 已分配 workflow 时，下拉列表只展示该 workflow 的 stages。
- Workflow 变更后自动清空 stage 选择。

### 列表/Board 视图

- 本次不变更 board 分组逻辑，仅预留数据字段。
- 可选在 issue row/card 上显示 stage 名称标签（需后端返回或额外查询）。

## 错误处理

| 场景 | 响应 |
|------|------|
| `stage_id` 非空但 `workflow_id` 为空 | `400 Bad Request` |
| `stage_id` 指向不存在 stage | `400 Bad Request` |
| `stage_id` 不属于当前 workflow | `400 Bad Request` |
| workflow/stage 被删除 | 外键自动置空 `stage_id` |
| workflow 变更 | `stage_id` 清空 |

## 测试计划

### Go 测试

在 `server/internal/handler/issue_test.go` 中新增：

1. 创建 issue 时传入合法 `stage_id`，响应包含该 `stage_id`。
2. 创建 issue 时 `stage_id` 非空但 `workflow_id` 为空，期望 400。
3. 创建 issue 时 `stage_id` 属于其他 workflow，期望 400。
4. 更新 issue 的 workflow 后，验证 `stage_id` 被清空。
5. 分配 workflow 后生成 sub-issue，验证 sub-issue `stage_id` 等于 node `stage_id`。

### TypeScript 测试

1. `IssueSchema` 正确解析带 `stage_id` 的 issue。
2. 缺失 `stage_id` 时默认回退为 `null`。

### E2E 测试（可选）

1. 在 issue 详情页为 workflow 下的 issue 选择 stage，刷新后仍显示正确。

## 文件清单

### 新增

- `server/migrations/126_issue_stage_id.up.sql`
- `server/migrations/126_issue_stage_id.down.sql`

### 修改

- `server/pkg/db/queries/issue.sql`
- `server/pkg/db/generated/models.go`（sqlc 自动生成）
- `server/pkg/db/generated/issue.sql.go`（sqlc 自动生成）
- `server/internal/handler/issue.go`
- `server/internal/handler/issue_test.go`
- `packages/core/types/issue.ts`
- `packages/core/api/schemas.ts`
- `packages/views/issues/components/issue-detail.tsx`

## 回滚策略

`.down.sql` 删除 `stage_id` 列。该字段为新功能，历史数据为空，回滚无数据丢失风险。

## 不在本次范围内

- Board 按 stage 分组（预留字段，后续实现）。
- 独立的 issue stage 管理 UI（复用 workflow stage）。
