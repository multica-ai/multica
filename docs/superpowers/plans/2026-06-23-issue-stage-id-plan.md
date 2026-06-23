# Issue Stage ID Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `stage_id` field to all issues that references `multica_workflow_stage`, enforcing that the stage belongs to the issue's workflow.

**Architecture:** A database migration adds `stage_id` to `multica_issue` with a foreign key to `multica_workflow_stage`. sqlc queries are updated to read and write the column. Go handlers validate that `stage_id` belongs to `issue.workflow_id` and clear it when the workflow changes. Workflow-generated sub-issues inherit their node's `stage_id`. The frontend gains a stage selector in the issue detail panel.

**Tech Stack:** Go 1.26.1, Chi, sqlc, PostgreSQL, TypeScript, React, Zod, TanStack Query.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `server/migrations/126_issue_stage_id.up.sql` | Add `stage_id` column and index to `multica_issue`. |
| `server/migrations/126_issue_stage_id.down.sql` | Roll back by dropping the column. |
| `server/pkg/db/queries/issue.sql` | Include `stage_id` in SELECT, INSERT, and UPDATE queries. |
| `server/pkg/db/generated/models.go` | Auto-generated `MulticaIssue` struct with `StageID`. |
| `server/pkg/db/generated/issue.sql.go` | Auto-generated query params/rows with `StageID`. |
| `server/internal/handler/issue.go` | DTOs, validation, create/update/batch handlers, response mapping, sub-issue creation. |
| `server/internal/handler/issue_test.go` | Tests for validation and inheritance behavior. |
| `packages/core/types/issue.ts` | TypeScript `Issue` interface with `stage_id`. |
| `packages/core/api/schemas.ts` | Zod `IssueSchema` with `stage_id`. |
| `packages/views/issues/components/issue-detail.tsx` | Stage selector UI in issue detail. |

---

### Task 1: Database Migration

**Files:**
- Create: `server/migrations/126_issue_stage_id.up.sql`
- Create: `server/migrations/126_issue_stage_id.down.sql`

- [ ] **Step 1: Write up migration**

```sql
-- Add stage_id to issues so they can be grouped by workflow stage.
ALTER TABLE multica_issue
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_stage_id ON multica_issue(stage_id);
```

- [ ] **Step 2: Write down migration**

```sql
DROP INDEX IF EXISTS idx_issue_stage_id;
ALTER TABLE multica_issue DROP COLUMN IF EXISTS stage_id;
```

- [ ] **Step 3: Verify migration number**

Run: `ls server/migrations/*.up.sql | tail -n 5`
Expected output ends with `125_workflow_stage.up.sql`, confirming `126_issue_stage_id.up.sql` is next.

- [ ] **Step 4: Apply migration locally**

Run: `make migrate-up`
Expected: migration `126_issue_stage_id` applied successfully.

---

### Task 2: Update sqlc Queries

**Files:**
- Modify: `server/pkg/db/queries/issue.sql`

- [ ] **Step 1: Add `stage_id` to `ListIssues` SELECT list**

Change line 8-10 from:

```sql
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata
```

To:

```sql
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.workflow_id, i.workflow_run_id, i.stage_id, i.metadata
```

- [ ] **Step 2: Add `stage_id` to `ListOpenIssues` SELECT list**

Change line 150-152 from:

```sql
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata
```

To:

```sql
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.workflow_id, i.workflow_run_id, i.stage_id, i.metadata
```

- [ ] **Step 3: Add `stage_id` to `CreateIssue`**

Change lines 73-81 from:

```sql
INSERT INTO multica_issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    workflow_id, workflow_run_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('workflow_id'), sqlc.narg('workflow_run_id')
) RETURNING *;
```

To:

```sql
INSERT INTO multica_issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    workflow_id, workflow_run_id, stage_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('workflow_id'), sqlc.narg('workflow_run_id'), sqlc.narg('stage_id')
) RETURNING *;
```

- [ ] **Step 4: Add `stage_id` to `CreateIssueWithOrigin`**

Change lines 114-124 from:

```sql
INSERT INTO multica_issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, workflow_id, workflow_run_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'),
    sqlc.narg('workflow_id'), sqlc.narg('workflow_run_id')
) RETURNING *;
```

To:

```sql
INSERT INTO multica_issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, workflow_id, workflow_run_id, stage_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'),
    sqlc.narg('workflow_id'), sqlc.narg('workflow_run_id'), sqlc.narg('stage_id')
) RETURNING *;
```

- [ ] **Step 5: Add `stage_id` to `UpdateIssue`**

Change lines 87-104 from:

```sql
UPDATE multica_issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    workflow_id = sqlc.narg('workflow_id'),
    workflow_run_id = sqlc.narg('workflow_run_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;
```

To:

```sql
UPDATE multica_issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    workflow_id = sqlc.narg('workflow_id'),
    workflow_run_id = sqlc.narg('workflow_run_id'),
    stage_id = sqlc.narg('stage_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 6: Regenerate sqlc**

Run: `make sqlc`
Expected: `server/pkg/db/generated/models.go` and `server/pkg/db/generated/issue.sql.go` updated with `StageID` fields.

- [ ] **Step 7: Verify generated code**

Run: `grep -n "StageID" server/pkg/db/generated/models.go server/pkg/db/generated/issue.sql.go | head -20`
Expected: multiple matches showing `StageID pgtype.UUID` in `MulticaIssue` and query params.

---

### Task 3: Update Go Response DTO and Mapping

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Add `StageID` to `IssueResponse`**

Add after `WorkflowRunID` (around line 51):

```go
WorkflowRunID *string `json:"workflow_run_id"`
StageID       *string `json:"stage_id"`
```

- [ ] **Step 2: Map `StageID` in `issueToResponse`**

Add after `WorkflowRunID` mapping (around line 92):

```go
WorkflowRunID: uuidToPtr(i.WorkflowRunID),
StageID:       uuidToPtr(i.StageID),
```

- [ ] **Step 3: Map `StageID` in `issueListRowToResponse`**

Add after `DueDate` mapping (around line 119):

```go
DueDate:       timestampToPtr(i.DueDate),
StageID:       uuidToPtr(i.StageID),
Metadata:      parseIssueMetadata(i.Metadata),
```

- [ ] **Step 4: Map `StageID` in `openIssueRowToResponse`**

Find `openIssueRowToResponse` around line 157 and add the same mapping after `DueDate`.

---

### Task 4: Add Stage Validation Helper

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Write `validateIssueStage`**

Add near other helpers (after `parseOptionalRuntimeID` around line 2092):

```go
// validateIssueStage ensures a stage_id belongs to the issue's workflow.
// A nil/invalid stageID is always valid. A non-empty stageID requires a
// workflowID and the stage must reference that workflow.
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
        return errors.New("stage_id requires workflow_id")
    }
    stage, err := qtx.GetWorkflowStage(ctx, stageID)
    if err != nil {
        return fmt.Errorf("stage not found: %w", err)
    }
    if stage.WorkflowID != workflowID {
        return errors.New("stage does not belong to workflow")
    }
    return nil
}
```

---

### Task 5: Update `CreateIssue` Handler

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Add `StageID` to `CreateIssueRequest`**

Add after `DueDate` (around line 1696):

```go
DueDate       *string `json:"due_date"`
StageID       *string `json:"stage_id"`
```

- [ ] **Step 2: Parse `stage_id` after dates**

After parsing `dueDate` (around line 1817), add:

```go
var stageID pgtype.UUID
if req.StageID != nil && *req.StageID != "" {
    id, ok := parseUUIDOrBadRequest(w, *req.StageID, "stage_id")
    if !ok {
        return
    }
    stageID = id
}
```

- [ ] **Step 3: Validate stage before creating issue**

After determining `workflowID` (around line 1886), before the `if originType.Valid` block, add:

```go
var workflowID pgtype.UUID
if assigneeType.Valid && assigneeType.String == "workflow" {
    workflowID = assigneeID
}
if err := h.validateIssueStage(r.Context(), qtx, workflowID, stageID); err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
}
```

- [ ] **Step 4: Pass `StageID` to `CreateIssue` and `CreateIssueWithOrigin`**

In the `CreateIssueWithOrigin` call (around line 1906), add:

```go
WorkflowID:    workflowID,
StageID:       stageID,
```

In the `CreateIssue` call (around line 1925), add:

```go
WorkflowID:    workflowID,
StageID:       stageID,
```

---

### Task 6: Update `UpdateIssue` Handler

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Add `StageID` to `UpdateIssueRequest`**

Add after `RuntimeID` (around line 2081):

```go
RuntimeID *string `json:"runtime_id"`
StageID   *string `json:"stage_id"`
```

- [ ] **Step 2: Compute new workflow ID and detect workflow change**

After the existing nullable field prefill block (around line 2129), add:

```go
// Compute the effective workflow ID after this update.
newWorkflowID := prevIssue.WorkflowID
if _, ok := rawFields["assignee_type"]; ok || _, ok := rawFields["assignee_id"]; ok {
    if params.AssigneeType.Valid && params.AssigneeType.String == "workflow" && params.AssigneeID.Valid {
        newWorkflowID = params.AssigneeID
    } else {
        newWorkflowID = pgtype.UUID{}
    }
}
workflowChanged := newWorkflowID != prevIssue.WorkflowID
```

- [ ] **Step 3: Parse and validate `stage_id`**

After the `project_id` block (around line 2239), add:

```go
var stageID pgtype.UUID
var stageTouched bool
if _, ok := rawFields["stage_id"]; ok {
    stageTouched = true
    if req.StageID != nil && *req.StageID != "" {
        id, ok := parseUUIDOrBadRequest(w, *req.StageID, "stage_id")
        if !ok {
            return
        }
        stageID = id
    }
}

// Workflow change clears stage_id; otherwise validate the requested stage.
if workflowChanged {
    stageID = pgtype.UUID{}
} else if stageTouched {
    if err := h.validateIssueStage(r.Context(), h.Queries, newWorkflowID, stageID); err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
}

if stageTouched || workflowChanged {
    params.StageID = stageID
}
```

- [ ] **Step 4: Ensure stage is cleared when workflow changes**

The existing handler already starts/cancels workflow runs when the assignee changes. We only need to clear `stage_id` in the same `UpdateIssue` call. No additional `WorkflowID`/`WorkflowRunID` changes are required here; the existing workflow re-assignment logic handles those.

---

### Task 7: Update `BatchUpdateIssues` Handler

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Update `hasMutation` check to include `stage_id`**

Change the `hasMutation` block (around line 2648) from:

```go
hasMutation := req.Updates.Title != nil ||
    req.Updates.Description != nil ||
    req.Updates.Status != nil ||
    req.Updates.Priority != nil ||
    req.Updates.Position != nil
if !hasMutation {
    for _, k := range []string{"assignee_type", "assignee_id", "start_date", "due_date", "parent_issue_id", "project_id"} {
```

To:

```go
hasMutation := req.Updates.Title != nil ||
    req.Updates.Description != nil ||
    req.Updates.Status != nil ||
    req.Updates.Priority != nil ||
    req.Updates.Position != nil
if !hasMutation {
    for _, k := range []string{"assignee_type", "assignee_id", "start_date", "due_date", "parent_issue_id", "project_id", "stage_id"} {
```

- [ ] **Step 2: Parse `stage_id` per issue**

After the `project_id` handling block inside the loop (around line 2799), add:

```go
var stageID pgtype.UUID
if _, ok := rawUpdates["stage_id"]; ok {
    if req.Updates.StageID != nil && *req.Updates.StageID != "" {
        sid, err := util.ParseUUID(*req.Updates.StageID)
        if err != nil {
            continue
        }
        stageID = sid
    }
}
```

- [ ] **Step 3: Compute effective workflow ID and clear/validate stage per issue**

Before the `UpdateIssue` call inside the loop, add:

```go
newWorkflowID := prevIssue.WorkflowID
if _, ok := rawUpdates["assignee_type"]; ok || _, ok := rawUpdates["assignee_id"]; ok {
    if params.AssigneeType.Valid && params.AssigneeType.String == "workflow" && params.AssigneeID.Valid {
        newWorkflowID = params.AssigneeID
    } else {
        newWorkflowID = pgtype.UUID{}
    }
}
workflowChanged := newWorkflowID != prevIssue.WorkflowID

if workflowChanged {
    stageID = pgtype.UUID{}
} else if _, ok := rawUpdates["stage_id"]; ok {
    if err := h.validateIssueStage(r.Context(), h.Queries, newWorkflowID, stageID); err != nil {
        continue
    }
}

if _, ok := rawUpdates["stage_id"]; ok || workflowChanged {
    params.StageID = stageID
}
```

Note: `BatchUpdateIssues` does not start workflow runs; it only updates fields. We clear `stage_id` when the assignee changes to/from workflow, matching the per-issue semantics without altering existing batch workflow behavior.
```

---

### Task 8: Inherit Stage in Sub-Issue Creation

**Files:**
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Pass `StageID` in `createWorkflowSubIssue`**

In the `CreateIssueWithOrigin` call inside `createWorkflowSubIssue` (around line 2971), add:

```go
OriginType:    pgtype.Text{String: "workflow", Valid: true},
OriginID:      nodeRun.ID,
StageID:       node.StageID,
```

- [ ] **Step 2: Verify `node.StageID` is available**

The `node` is loaded via `qtx.GetWorkflowNode(ctx, nodeRun.WorkflowNodeID)`. `multica_workflow_node` already has `stage_id` (migration 125), so `node.StageID` exists after sqlc regeneration.

---

### Task 9: Update Frontend Types and Schema

**Files:**
- Modify: `packages/core/types/issue.ts`
- Modify: `packages/core/api/schemas.ts`

- [ ] **Step 1: Add `stage_id` to `Issue` interface**

In `packages/core/types/issue.ts` after `workflow_run_id` (around line 51):

```typescript
workflow_id: string | null;
workflow_run_id: string | null;
stage_id: string | null;
```

- [ ] **Step 2: Add `stage_id` to `IssueSchema`**

In `packages/core/api/schemas.ts` after `workflow_run_id` (around line 166):

```typescript
workflow_run_id: z.string().nullable().default(null),
stage_id: z.string().nullable().default(null),
```

- [ ] **Step 3: Run TypeScript type check**

Run: `pnpm typecheck`
Expected: passes (may fail later until issue detail UI is updated).

---

### Task 10: Add Stage Selector to Issue Detail

**Files:**
- Modify: `packages/views/issues/components/issue-detail.tsx`

- [ ] **Step 1: Locate the assignee/workflow selector area**

Search for the `WorkflowDagViewer` usage or assignee selector. The stage selector should be placed nearby.

- [ ] **Step 2: Fetch stages for the issue's workflow**

Use the existing `useWorkflowStages` hook or query (if it exists) keyed by `issue.workflow_id`.

If no hook exists, add a query in `packages/core/workflows/queries.ts`:

```typescript
export function useWorkflowStages(wsId: string, workflowId: string | null) {
  return useQuery({
    queryKey: ["workspaces", wsId, "workflows", workflowId, "stages"],
    queryFn: () => api.workflows.listStages(wsId, workflowId!),
    enabled: !!workflowId,
  });
}
```

- [ ] **Step 3: Render the stage selector**

Add a `Select` component (from `@multica/ui`) near the assignee selector:

```tsx
const { data: stages } = useWorkflowStages(wsId, issue.workflow_id);

// ...

<Select
  value={issue.stage_id ?? "none"}
  disabled={!issue.workflow_id}
  onValueChange={(value) => {
    updateIssue.mutate({
      id: issue.id,
      stage_id: value === "none" ? null : value,
    });
  }}
>
  <SelectTrigger>
    <SelectValue placeholder={issue.workflow_id ? "Select stage" : "Assign workflow first"} />
  </SelectTrigger>
  <SelectContent>
    <SelectItem value="none">No stage</SelectItem>
    {stages?.map((stage) => (
      <SelectItem key={stage.id} value={stage.id}>
        {stage.name}
      </SelectItem>
    ))}
  </SelectContent>
</Select>
```

- [ ] **Step 4: Run type check and lint**

Run: `pnpm typecheck`
Run: `pnpm lint`
Expected: both pass.

---

### Task 11: Go Tests

**Files:**
- Modify: `server/internal/handler/issue_test.go`

- [ ] **Step 1: Add helper to create workflow with a stage**

If not already available, create a test helper or inline the creation:

```go
func createTestWorkflowWithStage(t *testing.T, ctx context.Context, q *db.Queries, wsID pgtype.UUID) (db.MulticaWorkflow, db.MulticaWorkflowStage) {
    workflow, err := q.CreateWorkflow(ctx, db.CreateWorkflowParams{
        WorkspaceID: wsID,
        Title:       "Test Workflow",
    })
    require.NoError(t, err)
    stage, err := q.CreateWorkflowStage(ctx, db.CreateWorkflowStageParams{
        WorkflowID:  workflow.ID,
        Name:        "Test Stage",
        Description: "",
        SortOrder:   0,
    })
    require.NoError(t, err)
    return workflow, stage
}
```

- [ ] **Step 2: Write test for valid stage on create**

```go
func TestCreateIssue_WithStageID(t *testing.T) {
    h, cleanup := newTestHandler(t)
    defer cleanup()
    ctx := context.Background()

    ws := h.createTestWorkspace(t, ctx)
    member := h.createTestMember(t, ctx, ws.ID)
    workflow, stage := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)

    body := map[string]any{
        "title":         "Stage issue",
        "assignee_type": "workflow",
        "assignee_id":   uuidToString(workflow.ID),
        "stage_id":      uuidToString(stage.ID),
    }
    req := newAuthenticatedRequest(t, http.MethodPost, "/api/issues", member.ID, body)
    rr := httptest.NewRecorder()
    h.Handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusCreated, rr.Code)
    var resp IssueResponse
    require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
    require.NotNil(t, resp.StageID)
    require.Equal(t, uuidToString(stage.ID), *resp.StageID)
}
```

- [ ] **Step 3: Write test for stage without workflow**

```go
func TestCreateIssue_StageIDRequiresWorkflow(t *testing.T) {
    h, cleanup := newTestHandler(t)
    defer cleanup()
    ctx := context.Background()

    ws := h.createTestWorkspace(t, ctx)
    member := h.createTestMember(t, ctx, ws.ID)
    _, stage := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)

    body := map[string]any{
        "title":    "Stage without workflow",
        "stage_id": uuidToString(stage.ID),
    }
    req := newAuthenticatedRequest(t, http.MethodPost, "/api/issues", member.ID, body)
    rr := httptest.NewRecorder()
    h.Handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusBadRequest, rr.Code)
}
```

- [ ] **Step 4: Write test for stage from another workflow**

```go
func TestCreateIssue_StageIDMustBelongToWorkflow(t *testing.T) {
    h, cleanup := newTestHandler(t)
    defer cleanup()
    ctx := context.Background()

    ws := h.createTestWorkspace(t, ctx)
    member := h.createTestMember(t, ctx, ws.ID)
    workflow1, _ := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)
    _, stage2 := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)

    body := map[string]any{
        "title":         "Wrong stage",
        "assignee_type": "workflow",
        "assignee_id":   uuidToString(workflow1.ID),
        "stage_id":      uuidToString(stage2.ID),
    }
    req := newAuthenticatedRequest(t, http.MethodPost, "/api/issues", member.ID, body)
    rr := httptest.NewRecorder()
    h.Handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusBadRequest, rr.Code)
}
```

- [ ] **Step 5: Write test for workflow change clearing stage**

```go
func TestUpdateIssue_WorkflowChangeClearsStage(t *testing.T) {
    h, cleanup := newTestHandler(t)
    defer cleanup()
    ctx := context.Background()

    ws := h.createTestWorkspace(t, ctx)
    member := h.createTestMember(t, ctx, ws.ID)
    workflow, stage := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)

    // Create issue assigned to workflow with stage.
    issue := h.createIssue(t, ctx, ws.ID, member.ID, map[string]any{
        "title":         "Parent",
        "assignee_type": "workflow",
        "assignee_id":   uuidToString(workflow.ID),
        "stage_id":      uuidToString(stage.ID),
    })
    require.NotNil(t, issue.StageID)

    // Re-assign to a member.
    req := newAuthenticatedRequest(t, http.MethodPatch, fmt.Sprintf("/api/issues/%s", issue.ID), member.ID, map[string]any{
        "assignee_type": "member",
        "assignee_id":   uuidToString(member.ID),
    })
    rr := httptest.NewRecorder()
    h.Handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusOK, rr.Code)
    var resp IssueResponse
    require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
    require.Nil(t, resp.StageID)
    require.Nil(t, resp.WorkflowID)
}
```

- [ ] **Step 6: Write test for sub-issue inheriting stage**

```go
func TestCreateIssue_SubIssueInheritsStage(t *testing.T) {
    h, cleanup := newTestHandler(t)
    defer cleanup()
    ctx := context.Background()

    ws := h.createTestWorkspace(t, ctx)
    member := h.createTestMember(t, ctx, ws.ID)
    workflow, stage := createTestWorkflowWithStage(t, ctx, h.Queries, ws.ID)

    // Create a node with the stage.
    node, err := h.Queries.CreateWorkflowNode(ctx, db.CreateWorkflowNodeParams{
        WorkflowID: workflow.ID,
        Title:      "Node",
        WorkerType: "human",
        StageID:    stage.ID,
    })
    require.NoError(t, err)

    body := map[string]any{
        "title":         "Parent",
        "assignee_type": "workflow",
        "assignee_id":   uuidToString(workflow.ID),
    }
    req := newAuthenticatedRequest(t, http.MethodPost, "/api/issues", member.ID, body)
    rr := httptest.NewRecorder()
    h.Handler.ServeHTTP(rr, req)

    require.Equal(t, http.StatusCreated, rr.Code)

    // Find the sub-issue by origin.
    var resp IssueResponse
    require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
    parentID, _ := util.ParseUUID(resp.ID)
    children, err := h.Queries.ListChildIssues(ctx, parentID)
    require.NoError(t, err)
    require.Len(t, children, 1)
    require.True(t, children[0].StageID.Valid)
    require.Equal(t, stage.ID, children[0].StageID)
}
```

- [ ] **Step 7: Run Go tests**

Run: `cd server && go test ./internal/handler/ -run 'TestCreateIssue_WithStageID|TestCreateIssue_StageIDRequiresWorkflow|TestCreateIssue_StageIDMustBelongToWorkflow|TestUpdateIssue_WorkflowChangeClearsStage|TestCreateIssue_SubIssueInheritsStage' -v`
Expected: all tests pass.

---

### Task 12: TypeScript Schema Tests

**Files:**
- Modify: `packages/core/api/schemas.test.ts` (or create if absent)

- [ ] **Step 1: Add test for `IssueSchema` with `stage_id`**

```typescript
import { describe, it, expect } from "vitest";
import { IssueSchema } from "./schemas";

describe("IssueSchema", () => {
  it("parses issue with stage_id", () => {
    const parsed = IssueSchema.safeParse({
      id: "issue-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "MUL-1",
      title: "Test",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: "workflow",
      assignee_id: "workflow-1",
      creator_type: "member",
      creator_id: "member-1",
      parent_issue_id: null,
      project_id: null,
      workflow_id: "workflow-1",
      workflow_run_id: "run-1",
      stage_id: "stage-1",
      origin_type: null,
      origin_id: null,
      position: 0,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-06-23T00:00:00Z",
      updated_at: "2026-06-23T00:00:00Z",
    });
    expect(parsed.success).toBe(true);
    expect(parsed.data.stage_id).toBe("stage-1");
  });

  it("defaults missing stage_id to null", () => {
    const parsed = IssueSchema.safeParse({
      id: "issue-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "MUL-1",
      title: "Test",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "member-1",
      parent_issue_id: null,
      project_id: null,
      workflow_id: null,
      workflow_run_id: null,
      origin_type: null,
      origin_id: null,
      position: 0,
      start_date: null,
      due_date: null,
      metadata: {},
      created_at: "2026-06-23T00:00:00Z",
      updated_at: "2026-06-23T00:00:00Z",
    });
    expect(parsed.success).toBe(true);
    expect(parsed.data.stage_id).toBeNull();
  });
});
```

- [ ] **Step 2: Run TypeScript tests**

Run: `pnpm --filter @multica/core exec vitest run api/schemas.test.ts`
Expected: tests pass.

---

### Task 13: Full Verification

- [ ] **Step 1: Run backend tests**

Run: `make test`
Expected: all Go tests pass.

- [ ] **Step 2: Run frontend type check**

Run: `pnpm typecheck`
Expected: no TypeScript errors.

- [ ] **Step 3: Run frontend tests**

Run: `pnpm test`
Expected: all Vitest tests pass.

- [ ] **Step 4: Run lint**

Run: `pnpm lint`
Expected: no ESLint errors.

- [ ] **Step 5: Run full check (optional)**

Run: `make check`
Expected: all checks pass.

---

## Self-Review

### Spec coverage

| Spec requirement | Implementing task |
|------------------|-------------------|
| Add `stage_id` column to `multica_issue` | Task 1 |
| `stage_id` references `multica_workflow_stage(id)` with `ON DELETE SET NULL` | Task 1 |
| Include `stage_id` in issue queries | Task 2 |
| Return `stage_id` in `IssueResponse` | Task 3 |
| Validate `stage_id` belongs to `workflow_id` | Tasks 4, 5, 6, 7 |
| Clear `stage_id` on workflow change | Tasks 6, 7 |
| Sub-issue inherits node's `stage_id` | Task 8 |
| Frontend `Issue` type and schema | Task 9 |
| Stage selector in issue detail | Task 10 |
| Go tests | Task 11 |
| TypeScript schema tests | Task 12 |

### Placeholder scan

No `TBD`, `TODO`, or vague steps. All code blocks contain real code matching the codebase patterns.

### Type consistency

- Go: `StageID pgtype.UUID` in models, `StageID *string` in `IssueResponse`, `stage_id` JSON key.
- TypeScript: `stage_id: string | null` in interface and Zod schema.
- All mappings use `uuidToPtr` / `uuidToString` consistently.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-23-issue-stage-id-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach would you like?
