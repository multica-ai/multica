# Workflow 阶段可视化（Stage Overview）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Workflow 新增 `/workflows/[id]/overview` 概览页，以"阶段画布 → 节点 DAG → 节点详情"三层逐层下钻展示 Workflow 阶段结构，后端新增 `workflow_stage` 表 + `stage_id` FK。

**Architecture:** 后端在 Go (Chi router + sqlc) 中新增 stage CRUD API + edge 跨阶段验证；前端新建 `packages/views/workflows/components/overview/` 目录，阶段卡片条用 HTML/CSS，节点 DAG 用 ReactFlow 只读模式复用现有渲染器，节点详情用侧边抽屉面板。TanStack Query 与现有编辑器共享 `workflowDetailOptions` cache key。

**Tech Stack:** Go, Chi router, sqlc, PostgreSQL, React, TypeScript, TanStack Query, ReactFlow (@xyflow/react), shadcn/ui, Tailwind CSS, Vitest, React Testing Library.

## Global Constraints

- 所有 Workflow（模板和实例）均支持阶段；阶段不属于模板专属概念
- 边只在阶段内部连接；跨阶段连接返回 400
- 阶段画布用 HTML/CSS 横向滚动卡片条实现（非 ReactFlow）
- 节点 DAG 用 ReactFlow 只读模式，复用现有 `WorkflowNode`/`WorkflowEdge` 渲染器
- 节点配置编辑保留在原 `WorkflowDetailPage`，概览页不编辑节点内部配置
- 不得导入 `next/*` 到 `packages/views/`；不得导入 `react-router-dom` 到 `packages/views/`
- 不得将服务器数据复制到 Zustand store；使用 TanStack Query
- 注释使用英文；UI 文案和文档使用中文优先
- TypeScript strict mode；Go 遵循标准 gofmt/go vet
- API 响应 JSON 字段为 `snake_case`，前端类型为 `camelCase`
- i18n 遵循三段式命名：`workflows.overview.component.action`
- 测试遵循 TDD：先写失败测试 → 实现 → 验证通过 → 提交

---

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `server/migrations/125_workflow_stage.up.sql` | Create | Stage table + node FK migration |
| `server/migrations/125_workflow_stage.down.sql` | Create | Rollback |
| `server/pkg/db/queries/workflow.sql` | Modify | Add stage CRUD + list queries |
| `server/pkg/db/generated/models.go` | Regenerate | sqlc-generated `MulticaWorkflowStage` struct |
| `server/pkg/db/generated/workflow.sql.go` | Regenerate | sqlc-generated query functions |
| `server/internal/handler/workflow.go` | Modify | Add stage response types, converters; modify `GetWorkflow`; add stage handlers; add edge validation |
| `server/cmd/server/router.go` | Modify | Register stage routes |
| `server/internal/handler/workflow_stage_test.go` | Create | Go handler tests for stage CRUD, edge validation |
| `packages/core/types/workflow.ts` | Modify | Add `WorkflowStage` interface; add `stageId` to `WorkflowNode` |
| `packages/core/api/client.ts` | Modify | Add stage API methods |
| `packages/core/workflows/queries.ts` | Modify | Add `useCreateStage`, `useUpdateStage`, `useDeleteStage`, `useReorderStages`, `useAssignNodeToStage` |
| `packages/views/locales/en/workflows.json` | Modify | Add overview section strings |
| `packages/views/locales/zh-Hans/workflows.json` | Modify | Add overview section Chinese strings |
| `apps/web/app/(dashboard)/[workspaceSlug]/workflows/[id]/overview/page.tsx` | Create | Next.js route wrapper |
| `packages/views/workflows/components/overview/index.ts` | Create | Barrel export |
| `packages/views/workflows/components/overview/workflow-overview-page.tsx` | Create | Top-level page component |
| `packages/views/workflows/components/overview/stage-canvas.tsx` | Create | Horizontal scrollable stage card strip |
| `packages/views/workflows/components/overview/stage-card.tsx` | Create | Single stage card |
| `packages/views/workflows/components/overview/stage-node-dag.tsx` | Create | Read-only ReactFlow DAG for one stage |
| `packages/views/workflows/components/overview/node-detail-panel.tsx` | Create | Slide-out drawer for node details |
| `packages/views/workflows/components/overview/stage-create-dialog.tsx` | Create | Create/edit stage dialog |
| `packages/views/workflows/components/overview/overview-page.test.tsx` | Create | Component tests |

---

### Task 1: Database Migration

**Files:**
- Create: `server/migrations/125_workflow_stage.up.sql`
- Create: `server/migrations/125_workflow_stage.down.sql`

**Interfaces:**
- Produces: `multica_workflow_stage` table, `stage_id` column on `multica_workflow_node`

- [ ] **Step 1: Write up migration**

Create `server/migrations/125_workflow_stage.up.sql`:

```sql
-- Create workflow_stage table
CREATE TABLE multica_workflow_stage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES multica_workflow(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_stage_workflow_id ON multica_workflow_stage(workflow_id);
CREATE INDEX idx_workflow_stage_sort_order ON multica_workflow_stage(workflow_id, sort_order);

-- Add stage_id to workflow_node
ALTER TABLE multica_workflow_node
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_workflow_node_stage_id ON multica_workflow_node(stage_id);
```

- [ ] **Step 2: Write down migration**

Create `server/migrations/125_workflow_stage.down.sql`:

```sql
-- Remove stage_id from workflow_node
DROP INDEX IF EXISTS idx_workflow_node_stage_id;
ALTER TABLE multica_workflow_node DROP COLUMN IF EXISTS stage_id;

-- Drop workflow_stage table
DROP INDEX IF EXISTS idx_workflow_stage_sort_order;
DROP INDEX IF EXISTS idx_workflow_stage_workflow_id;
DROP TABLE IF EXISTS multica_workflow_stage;
```

- [ ] **Step 3: Run migration up to verify**

```bash
cd server && make migrate-up
```

Expected: migration runs successfully, tables created.

- [ ] **Step 4: Run migration down to verify**

```bash
cd server && make migrate-down
```

Expected: rollback succeeds, then re-run migrate-up to leave DB in migrated state.

- [ ] **Step 5: Commit**

```bash
git add server/migrations/125_workflow_stage.up.sql server/migrations/125_workflow_stage.down.sql
git commit -m "feat(db): add workflow_stage table and node stage_id column"
```

---

### Task 2: sqlc Queries

**Files:**
- Modify: `server/pkg/db/queries/workflow.sql`
- Regenerate: `server/pkg/db/generated/models.go`
- Regenerate: `server/pkg/db/generated/workflow.sql.go`

**Interfaces:**
- Consumes: `multica_workflow_stage` table from Task 1
- Produces: `CreateWorkflowStage`, `GetWorkflowStage`, `ListWorkflowStagesByWorkflow`, `UpdateWorkflowStage`, `DeleteWorkflowStage`, `ReorderWorkflowStages`, `AssignNodeToStage`, `CountWorkflowStageNodes`

- [ ] **Step 1: Add stage queries to workflow.sql**

Append to `server/pkg/db/queries/workflow.sql`:

```sql
-- =====================
-- Workflow Stage CRUD
-- =====================

-- name: CreateWorkflowStage :one
INSERT INTO multica_workflow_stage (
    workflow_id, name, description, sort_order
) VALUES (
    $1, $2, sqlc.narg('description'), $3
) RETURNING *;

-- name: GetWorkflowStage :one
SELECT * FROM multica_workflow_stage WHERE id = $1;

-- name: ListWorkflowStagesByWorkflow :many
SELECT * FROM multica_workflow_stage
WHERE workflow_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: UpdateWorkflowStage :one
UPDATE multica_workflow_stage SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    sort_order = COALESCE(sqlc.narg('sort_order')::int, sort_order),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowStage :exec
DELETE FROM multica_workflow_stage WHERE id = $1;

-- name: CountWorkflowStageNodes :one
SELECT count(*)::bigint FROM multica_workflow_node
WHERE stage_id = $1;

-- name: AssignNodeToStage :one
UPDATE multica_workflow_node SET
    stage_id = sqlc.narg('stage_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UnassignNodeFromStage :one
UPDATE multica_workflow_node SET
    stage_id = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 2: Regenerate sqlc**

```bash
cd server && make sqlc
```

Expected: no errors; `models.go` now contains `MulticaWorkflowStage` struct; `workflow.sql.go` contains new query functions.

- [ ] **Step 3: Verify compilation**

```bash
cd server && go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/db/queries/workflow.sql server/pkg/db/generated/
git commit -m "feat(db): add workflow stage CRUD sqlc queries"
```

---

### Task 3: Go Stage Handlers + Edge Validation

**Files:**
- Modify: `server/internal/handler/workflow.go`
- Modify: `server/cmd/server/router.go`

**Interfaces:**
- Consumes: sqlc query functions from Task 2
- Produces:
  - `WorkflowStageResponse` type (exported in `workflow.go`)
  - `StageResponse` struct: `{ ID, WorkflowID, Name, Description, SortOrder, NodeCount int64, CreatedAt, UpdatedAt string }`
  - `CreateWorkflowStage(w, r)`, `UpdateWorkflowStage(w, r)`, `DeleteWorkflowStage(w, r)`, `ReorderWorkflowStages(w, r)`, `AssignNodeToStage(w, r)` HTTP handlers
  - Modified `GetWorkflow` that includes `stages[]` in response
  - Modified `CreateWorkflowEdge` that validates same `stage_id`

- [ ] **Step 1: Write failing Go tests**

Create `server/internal/handler/workflow_stage_test.go`:

```go
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateStage_InWorkflow creates a stage and verifies it appears in the
// workflow response.
func TestCreateStage_InWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a workflow to host the stage
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{
		"title": "Stage Test Workflow",
	})
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Workflow struct {
			ID string `json:"id"`
		} `json:"workflow"`
	}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	wfID := createResp.Workflow.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create a stage
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{
		"name":        "需求",
		"description": "需求收集与分析",
	})
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify stage appears in GetWorkflow
	w = httptest.NewRecorder()
	req = newRequest("GET", fmt.Sprintf("/api/workflows/%s", wfID), nil)
	testHandler.GetWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkflow: expected 200, got %d", w.Code)
	}
	var getResp struct {
		Stages []struct {
			Name      string `json:"name"`
			NodeCount int64  `json:"node_count"`
		} `json:"stages"`
	}
	json.Unmarshal(w.Body.Bytes(), &getResp)
	if len(getResp.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(getResp.Stages))
	}
	if getResp.Stages[0].Name != "需求" {
		t.Fatalf("stage name mismatch: got %q", getResp.Stages[0].Name)
	}
}

// TestCrossStageEdge_Rejected verifies that creating an edge between nodes
// in different stages returns 400.
func TestCrossStageEdge_Rejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create workflow
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{"title": "Edge Validation WF"})
	testHandler.CreateWorkflow(w, req)
	var cr struct {
		Workflow struct{ ID string `json:"id"` }
	}
	json.Unmarshal(w.Body.Bytes(), &cr)
	wfID := cr.Workflow.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create two stages
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "Stage A"})
	testHandler.CreateWorkflowStage(w, req)
	var sr1 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr1)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "Stage B"})
	testHandler.CreateWorkflowStage(w, req)
	var sr2 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr2)

	// Create nodes in different stages
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title":       "Node A",
		"worker_type": "agent",
		"critic_type": "human",
	})
	testHandler.CreateWorkflowNode(w, req)
	var nr1 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr1)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title":       "Node B",
		"worker_type": "agent",
		"critic_type": "human",
	})
	testHandler.CreateWorkflowNode(w, req)
	var nr2 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr2)

	// Assign nodes to different stages
	assignNode := func(nodeID, stageID string) {
		body := fmt.Sprintf(`{"stage_id":"%s"}`, stageID)
		req := httptest.NewRequest("PUT",
			fmt.Sprintf("/api/workflows/%s/nodes/%s/stage", wfID, nodeID),
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, testUserID))
		req = req.WithContext(context.WithValue(req.Context(), workspaceIDKey, testWorkspaceID))
		w := httptest.NewRecorder()
		testHandler.AssignNodeToStage(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("AssignNodeToStage: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	assignNode(nr1.ID, sr1.ID)
	assignNode(nr2.ID, sr2.ID)

	// Try cross-stage edge
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/edges", wfID), map[string]any{
		"source_node_id": nr1.ID,
		"target_node_id": nr2.ID,
	})
	testHandler.CreateWorkflowEdge(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-stage edge: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteStage_SetsNodeStageNull verifies ON DELETE SET NULL behavior.
func TestDeleteStage_SetsNodeStageNull(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{"title": "Delete Stage WF"})
	testHandler.CreateWorkflow(w, req)
	var cr struct{ Workflow struct{ ID string `json:"id"` } }
	json.Unmarshal(w.Body.Bytes(), &cr)
	wfID := cr.Workflow.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create stage + node assigned to it
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "S"})
	testHandler.CreateWorkflowStage(w, req)
	var sr struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title": "N", "worker_type": "agent", "critic_type": "human",
	})
	testHandler.CreateWorkflowNode(w, req)
	var nr struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr)

	assignNode := func(nodeID, stageID string) {
		body := fmt.Sprintf(`{"stage_id":"%s"}`, stageID)
		req := httptest.NewRequest("PUT",
			fmt.Sprintf("/api/workflows/%s/nodes/%s/stage", wfID, nodeID),
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, testUserID))
		req = req.WithContext(context.WithValue(req.Context(), workspaceIDKey, testWorkspaceID))
		w := httptest.NewRecorder()
		testHandler.AssignNodeToStage(w, req)
	}
	assignNode(nr.ID, sr.ID)

	// Delete stage
	w = httptest.NewRecorder()
	req = newRequest("DELETE", fmt.Sprintf("/api/workflows/%s/stages/%s", wfID, sr.ID), nil)
	testHandler.DeleteWorkflowStage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteWorkflowStage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify node's stage_id is NULL
	var stageID *string
	err := testPool.QueryRow(ctx,
		`SELECT stage_id::text FROM multica_workflow_node WHERE id = $1`, nr.ID,
	).Scan(&stageID)
	if err != nil {
		t.Fatalf("query node: %v", err)
	}
	if stageID != nil {
		t.Fatalf("expected NULL stage_id after stage delete, got %q", *stageID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd server && go test ./internal/handler/ -run "TestCreateStage_InWorkflow|TestCrossStageEdge_Rejected|TestDeleteStage_SetsNodeStageNull" -v
```

Expected: compile error — `CreateWorkflowStage`, `AssignNodeToStage` etc. undefined.

- [ ] **Step 3: Add request/response types to workflow.go**

In `server/internal/handler/workflow.go`, add after the existing request types (after `ToggleTemplateRequest` around line 112):

```go
// ── Stage request/response types ──

type CreateStageRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int32  `json:"sort_order"`
}

type UpdateStageRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	SortOrder   *int32  `json:"sort_order"`
}

type AssignNodeToStageRequest struct {
	StageID *string `json:"stage_id"` // null means unassign
}

type WorkflowStageResponse struct {
	ID          string `json:"id"`
	WorkflowID  string `json:"workflow_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int32  `json:"sort_order"`
	NodeCount   int64  `json:"node_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
```

- [ ] **Step 4: Add stageToResponse converter**

In `server/internal/handler/workflow.go`, add after the existing converters (after `workflowEdgeToResponse`):

```go
func workflowStageToResponse(s db.MulticaWorkflowStage, nodeCount int64) WorkflowStageResponse {
	return WorkflowStageResponse{
		ID:          uuidToString(s.ID),
		WorkflowID:  uuidToString(s.WorkflowID),
		Name:        s.Name,
		Description: s.Description,
		SortOrder:   s.SortOrder,
		NodeCount:   nodeCount,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}
```

- [ ] **Step 5: Modify GetWorkflow to include stages**

Replace the existing `GetWorkflow` handler body (lines 308-340) to also fetch stages:

```go
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	nodes, err := h.Queries.ListWorkflowNodes(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	edges, err := h.Queries.ListWorkflowEdges(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list edges")
		return
	}
	stages, err := h.Queries.ListWorkflowStagesByWorkflow(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list stages")
		return
	}

	nodeResps := make([]WorkflowNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		nodeResps = append(nodeResps, workflowNodeToResponse(n))
	}
	edgeResps := make([]WorkflowEdgeResponse, 0, len(edges))
	for _, e := range edges {
		edgeResps = append(edgeResps, workflowEdgeToResponse(e))
	}
	stageResps := make([]WorkflowStageResponse, 0, len(stages))
	for _, s := range stages {
		count, _ := h.Queries.CountWorkflowStageNodes(r.Context(), s.ID)
		stageResps = append(stageResps, workflowStageToResponse(s, count))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workflow": workflowToResponse(wf, int64(len(nodes))),
		"nodes":    nodeResps,
		"edges":    edgeResps,
		"stages":   stageResps,
	})
}
```

- [ ] **Step 6: Add stage CRUD handlers**

Append to `server/internal/handler/workflow.go`:

```go
// ── Stage Handlers ─────────────────────────────────────────────────────────────

func (h *Handler) CreateWorkflowStage(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	var req CreateStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	stage, err := h.Queries.CreateWorkflowStage(r.Context(), db.CreateWorkflowStageParams{
		WorkflowID:  wf.ID,
		Name:        req.Name,
		Description: stringToText(req.Description),
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create stage")
		return
	}

	writeJSON(w, http.StatusCreated, workflowStageToResponse(stage, 0))
}

func (h *Handler) UpdateWorkflowStage(w http.ResponseWriter, r *http.Request) {
	stageID := chi.URLParam(r, "stageId")
	stage, ok := h.loadWorkflowStage(w, r, stageID)
	if !ok {
		return
	}

	var req UpdateStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.Queries.UpdateWorkflowStage(r.Context(), db.UpdateWorkflowStageParams{
		ID:          stage.ID,
		Name:        stringToTextPtr(req.Name),
		Description: stringToTextPtr(req.Description),
		SortOrder:   int32ToPtr(req.SortOrder),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update stage")
		return
	}

	count, _ := h.Queries.CountWorkflowStageNodes(r.Context(), updated.ID)
	writeJSON(w, http.StatusOK, workflowStageToResponse(updated, count))
}

func (h *Handler) DeleteWorkflowStage(w http.ResponseWriter, r *http.Request) {
	stageID := chi.URLParam(r, "stageId")
	_, ok := h.loadWorkflowStage(w, r, stageID)
	if !ok {
		return
	}

	if err := h.Queries.DeleteWorkflowStage(r.Context(), parseUUID(stageID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete stage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) ReorderWorkflowStages(w http.ResponseWriter, r *http.Request) {
	type reorderItem struct {
		ID        string `json:"id"`
		SortOrder int32  `json:"sort_order"`
	}
	var items []reorderItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, item := range items {
		_, err := h.Queries.UpdateWorkflowStage(r.Context(), db.UpdateWorkflowStageParams{
			ID:        parseUUID(item.ID),
			SortOrder: int32ToPtr(&item.SortOrder),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder stages")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reordered"})
}

func (h *Handler) AssignNodeToStage(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")
	node, ok := h.loadWorkflowNode(w, r, wfID, nodeID)
	if !ok {
		return
	}

	var req AssignNodeToStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.StageID == nil {
		// Unassign
		updated, err := h.Queries.UnassignNodeFromStage(r.Context(), node.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unassign node")
			return
		}
		writeJSON(w, http.StatusOK, workflowNodeToResponse(updated))
		return
	}

	// Verify target stage belongs to same workflow
	stageID := parseUUID(*req.StageID)
	stage, err := h.Queries.GetWorkflowStage(r.Context(), stageID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "stage not found")
		return
	}
	if stage.WorkflowID != node.WorkflowID {
		writeError(w, http.StatusBadRequest, "stage does not belong to this workflow")
		return
	}

	updated, err := h.Queries.AssignNodeToStage(r.Context(), db.AssignNodeToStageParams{
		ID:      node.ID,
		StageID: uuidToPgUUID(stageID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign node to stage")
		return
	}
	writeJSON(w, http.StatusOK, workflowNodeToResponse(updated))
}

// ── Stage loader ──

func (h *Handler) loadWorkflowStage(w http.ResponseWriter, r *http.Request, stageID string) (db.MulticaWorkflowStage, bool) {
	id := parseUUIDOrBadRequest(w, r, "stageId", stageID)
	if id == (pgtype.UUID{}) {
		return db.MulticaWorkflowStage{}, false
	}

	stage, err := h.Queries.GetWorkflowStage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "stage not found")
		return db.MulticaWorkflowStage{}, false
	}
	return stage, true
}
```

- [ ] **Step 7: Add edge validation to CreateWorkflowEdge**

In the existing `CreateWorkflowEdge` handler, after the existing DAG-cycle validation, add cross-stage validation. Find the handler (around line 530-560 in workflow.go) and add after the source/target node resolution:

```go
// After loading sourceNode and targetNode:
// Validate same stage
if sourceNode.StageID != targetNode.StageID {
	writeError(w, http.StatusBadRequest, "nodes must belong to the same stage")
	return
}
```

Note: `MulticaWorkflowNode` will have a `StageID` field (type `pgtype.UUID`) after sqlc regeneration. Compare with:

```go
sourceStageID := uuidToString(sourceNode.StageID)
targetStageID := uuidToString(targetNode.StageID)
if sourceStageID != targetStageID {
	writeError(w, http.StatusBadRequest, "nodes must belong to the same stage")
	return
}
```

- [ ] **Step 8: Register stage routes**

In `server/cmd/server/router.go`, inside the workflows route group, add after the edges DELETE route (after line 537):

```go
// Stages
r.Get("/stages", h.ListWorkflowStages)
r.Post("/stages", h.CreateWorkflowStage)
r.Put("/stages/reorder", h.ReorderWorkflowStages)
r.Route("/stages/{stageId}", func(r chi.Router) {
	r.Put("/", h.UpdateWorkflowStage)
	r.Delete("/", h.DeleteWorkflowStage)
})
// Node stage assignment
r.Put("/nodes/{nodeId}/stage", h.AssignNodeToStage)
```

Also add `ListWorkflowStages` handler (simple list endpoint):

```go
func (h *Handler) ListWorkflowStages(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	stages, err := h.Queries.ListWorkflowStagesByWorkflow(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list stages")
		return
	}

	resps := make([]WorkflowStageResponse, 0, len(stages))
	for _, s := range stages {
		count, _ := h.Queries.CountWorkflowStageNodes(r.Context(), s.ID)
		resps = append(resps, workflowStageToResponse(s, count))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stages": resps})
}
```

- [ ] **Step 9: Run tests to verify they pass**

```bash
cd server && go test ./internal/handler/ -run "TestCreateStage_InWorkflow|TestCrossStageEdge_Rejected|TestDeleteStage_SetsNodeStageNull" -v
```

Expected: all three tests pass.

- [ ] **Step 10: Verify full compilation**

```bash
cd server && go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 11: Commit**

```bash
git add server/internal/handler/workflow.go server/cmd/server/router.go server/internal/handler/workflow_stage_test.go
git commit -m "feat(server): add workflow stage CRUD handlers and cross-stage edge validation"
```

---

### Task 4: TypeScript Types + API Client

**Files:**
- Modify: `packages/core/types/workflow.ts`
- Modify: `packages/core/api/client.ts`

**Interfaces:**
- Consumes: API response shapes from Task 3
- Produces:
  - `WorkflowStage` interface
  - `WorkflowNode.stageId` optional field
  - `ApiClient` methods: `listWorkflowStages`, `createWorkflowStage`, `updateWorkflowStage`, `deleteWorkflowStage`, `reorderWorkflowStages`, `assignNodeToStage`

- [ ] **Step 1: Add types to workflow.ts**

In `packages/core/types/workflow.ts`, add:

```typescript
export interface WorkflowStage {
  id: string;
  workflowId: string;
  name: string;
  description: string;
  sortOrder: number;
  nodeCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateStageRequest {
  name: string;
  description?: string;
  sort_order?: number;
}

export interface UpdateStageRequest {
  name?: string;
  description?: string;
  sort_order?: number;
}

export interface AssignNodeToStageRequest {
  stage_id: string | null;
}

export interface ReorderStagesRequest {
  id: string;
  sort_order: number;
}
```

In the `WorkflowNode` interface, add:

```typescript
export interface WorkflowNode {
  // ... existing fields
  stageId?: string | null;  // ADD this line
}
```

Update the `WorkflowListResponse` and `WorkflowDetailResponse` to include stages:

```typescript
export interface WorkflowDetailResponse {
  workflow: Workflow;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  stages: WorkflowStage[];  // ADD this line
}
```

- [ ] **Step 2: Add API client methods**

In `packages/core/api/client.ts`, add after the existing workflow methods:

```typescript
// ── Workflow Stage API ──

async listWorkflowStages(workflowId: string) {
  const res = await this.req<{ stages: WorkflowStage[] }>(`/api/workflows/${workflowId}/stages`);
  return res.stages;
}

async createWorkflowStage(workflowId: string, req: CreateStageRequest) {
  return this.req<WorkflowStage>(`/api/workflows/${workflowId}/stages`, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

async updateWorkflowStage(workflowId: string, stageId: string, req: UpdateStageRequest) {
  return this.req<WorkflowStage>(`/api/workflows/${workflowId}/stages/${stageId}`, {
    method: "PUT",
    body: JSON.stringify(req),
  });
}

async deleteWorkflowStage(workflowId: string, stageId: string) {
  return this.req<{ status: string }>(`/api/workflows/${workflowId}/stages/${stageId}`, {
    method: "DELETE",
  });
}

async reorderWorkflowStages(workflowId: string, items: ReorderStagesRequest[]) {
  return this.req<{ status: string }>(`/api/workflows/${workflowId}/stages/reorder`, {
    method: "PUT",
    body: JSON.stringify(items),
  });
}

async assignNodeToStage(workflowId: string, nodeId: string, stageId: string | null) {
  return this.req<WorkflowNode>(`/api/workflows/${workflowId}/nodes/${nodeId}/stage`, {
    method: "PUT",
    body: JSON.stringify({ stage_id: stageId }),
  });
}
```

Ensure the import for `WorkflowStage`, `CreateStageRequest`, `UpdateStageRequest`, `ReorderStagesRequest`, and `WorkflowNode` is added at the top of `client.ts`.

- [ ] **Step 3: Verify TypeScript compilation**

```bash
pnpm typecheck
```

Expected: no new type errors.

- [ ] **Step 4: Commit**

```bash
git add packages/core/types/workflow.ts packages/core/api/client.ts
git commit -m "feat(core): add WorkflowStage types and API client methods"
```

---

### Task 5: TanStack Query Hooks + i18n

**Files:**
- Modify: `packages/core/workflows/queries.ts`
- Modify: `packages/views/locales/en/workflows.json`
- Modify: `packages/views/locales/zh-Hans/workflows.json`

**Interfaces:**
- Consumes: API client methods from Task 4
- Produces: `useCreateStage`, `useUpdateStage`, `useDeleteStage`, `useReorderStages`, `useAssignNodeToStage` mutations; `workflowOverviewOptions` query; i18n keys

- [ ] **Step 1: Add query options and mutations**

In `packages/core/workflows/queries.ts`, add after the existing mutation exports:

```typescript
import type {
  CreateStageRequest,
  UpdateStageRequest,
  ReorderStagesRequest,
} from "../types";

// ── Stage Queries ──

export function workflowStagesOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: [...workflowKeys.detail(wsId, workflowId), "stages"],
    queryFn: () => api.listWorkflowStages(workflowId),
  });
}

export function workflowOverviewOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(wsId, workflowId),
    queryFn: () => api.getWorkflow(workflowId),
  });
}

// ── Stage Mutations ──

export function useCreateStage(wsId: string, workflowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateStageRequest) => api.createWorkflowStage(workflowId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useUpdateStage(wsId: string, workflowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ stageId, ...req }: UpdateStageRequest & { stageId: string }) =>
      api.updateWorkflowStage(workflowId, stageId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useDeleteStage(wsId: string, workflowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (stageId: string) => api.deleteWorkflowStage(workflowId, stageId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useReorderStages(wsId: string, workflowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (items: ReorderStagesRequest[]) => api.reorderWorkflowStages(workflowId, items),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useAssignNodeToStage(wsId: string, workflowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, stageId }: { nodeId: string; stageId: string | null }) =>
      api.assignNodeToStage(workflowId, nodeId, stageId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}
```

- [ ] **Step 2: Add i18n strings — English**

In `packages/views/locales/en/workflows.json`, add the `overview` section (see spec Section 11 for full content):

```json
"overview": {
  "title": "Workflow Overview",
  "stage_canvas": {
    "empty_title": "No stages defined yet",
    "empty_description": "Stages help you organize workflow nodes into logical phases. Create your first stage to get started.",
    "create_first": "Create first stage",
    "add_stage": "Add stage",
    "unassigned": "Unassigned",
    "stage_n_of_m": "Stage {{n}}/{{m}}",
    "nodes_count_one": "{{count}} node",
    "nodes_count_other": "{{count}} nodes"
  },
  "node_dag": {
    "empty_title": "No nodes in this stage",
    "empty_description": "Add nodes to this stage in the workflow editor.",
    "node_n_of_m": "Node {{n}}/{{m}}"
  },
  "detail_panel": {
    "title": "Node Details",
    "worker": "Worker",
    "critic": "Critic",
    "format_schema": "Format Schema",
    "relations": "Relations",
    "upstream": "Upstream",
    "downstream": "Downstream",
    "plugins": "Plugins",
    "skills": "Skills",
    "not_configured": "Not configured",
    "no_schema": "No format constraints",
    "open_in_editor": "Open in editor"
  },
  "stage_dialog": {
    "create_title": "Create Stage",
    "edit_title": "Edit Stage",
    "name_label": "Stage name",
    "name_placeholder": "e.g. Requirements, Design, Build",
    "description_label": "Description (optional)",
    "description_placeholder": "What happens in this stage?",
    "delete_confirm_title": "Delete stage?",
    "delete_confirm_description": "This stage contains {{count}} node(s). Deleting it will move them to \"Unassigned\"."
  }
}
```

- [ ] **Step 3: Add i18n strings — Chinese**

In `packages/views/locales/zh-Hans/workflows.json`, add the Chinese translations:

```json
"overview": {
  "title": "Workflow 概览",
  "stage_canvas": {
    "empty_title": "尚未定义阶段",
    "empty_description": "阶段可帮助你按业务逻辑组织 Workflow 节点。创建你的第一个阶段开始吧。",
    "create_first": "创建第一个阶段",
    "add_stage": "添加阶段",
    "unassigned": "未分组",
    "stage_n_of_m": "阶段 {{n}}/{{m}}",
    "nodes_count_one": "{{count}} 个节点",
    "nodes_count_other": "{{count}} 个节点"
  },
  "node_dag": {
    "empty_title": "此阶段暂无节点",
    "empty_description": "可在 Workflow 编辑器中为此阶段添加节点。",
    "node_n_of_m": "节点 {{n}}/{{m}}"
  },
  "detail_panel": {
    "title": "节点详情",
    "worker": "Worker",
    "critic": "Critic",
    "format_schema": "Format Schema",
    "relations": "关系",
    "upstream": "上游节点",
    "downstream": "下游节点",
    "plugins": "Plugin",
    "skills": "Skill",
    "not_configured": "未配置",
    "no_schema": "无格式约束",
    "open_in_editor": "在编辑器中打开"
  },
  "stage_dialog": {
    "create_title": "创建阶段",
    "edit_title": "编辑阶段",
    "name_label": "阶段名称",
    "name_placeholder": "例如：需求、设计、编码",
    "description_label": "描述（可选）",
    "description_placeholder": "这个阶段做什么？",
    "delete_confirm_title": "删除阶段？",
    "delete_confirm_description": "此阶段包含 {{count}} 个节点。删除阶段后节点将移至「未分组」。"
  }
}
```

- [ ] **Step 4: Run i18n parity test**

```bash
pnpm --filter @multica/views exec vitest run locales/parity.test.ts
```

Expected: parity test passes.

- [ ] **Step 5: Verify TypeScript compilation**

```bash
pnpm typecheck
```

Expected: no type errors.

- [ ] **Step 6: Commit**

```bash
git add packages/core/workflows/queries.ts packages/views/locales/en/workflows.json packages/views/locales/zh-Hans/workflows.json
git commit -m "feat(core): add stage mutations and i18n strings for overview page"
```

---

### Task 6: Route Page + WorkflowOverviewPage Shell

**Files:**
- Create: `apps/web/app/(dashboard)/[workspaceSlug]/workflows/[id]/overview/page.tsx`
- Create: `packages/views/workflows/components/overview/index.ts`
- Create: `packages/views/workflows/components/overview/workflow-overview-page.tsx`

**Interfaces:**
- Consumes: `workflowOverviewOptions` from Task 5, `useWorkspaceId` from core
- Produces: `<WorkflowOverviewPage workflowId={string} />` component shell

- [ ] **Step 1: Write the failing test**

Create `packages/views/workflows/components/overview/overview-page.test.tsx`:

```tsx
// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";
import { WorkflowOverviewPage } from "./workflow-overview-page";

// ── Mock @multica/core ──
const mockWorkflowData = vi.hoisted(() => ({
  workflow: {
    id: "wf1",
    title: "Test Workflow",
    status: "active",
    node_count: 3,
  },
  nodes: [
    { id: "n1", workflow_id: "wf1", title: "Node A", stage_id: "s1",
      worker_type: "agent", critic_type: "human", description: "",
      position_x: 0, position_y: 0, format_schema: null,
      worker_id: null, critic_id: null, critic_api_url: null,
      sort_order: 0 },
    { id: "n2", workflow_id: "wf1", title: "Node B", stage_id: "s1",
      worker_type: "agent", critic_type: "human", description: "",
      position_x: 0, position_y: 0, format_schema: null,
      worker_id: null, critic_id: null, critic_api_url: null,
      sort_order: 1 },
    { id: "n3", workflow_id: "wf1", title: "Node C", stage_id: null,
      worker_type: "agent", critic_type: "human", description: "",
      position_x: 0, position_y: 0, format_schema: null,
      worker_id: null, critic_id: null, critic_api_url: null,
      sort_order: 2 },
  ],
  edges: [
    { id: "e1", workflow_id: "wf1", source_node_id: "n1", target_node_id: "n2" },
  ],
  stages: [
    { id: "s1", workflow_id: "wf1", name: "Design", description: "",
      sort_order: 0, node_count: 2 },
    { id: "s2", workflow_id: "wf1", name: "Build", description: "",
      sort_order: 1, node_count: 0 },
  ],
}));

vi.mock("@multica/core/workflows/queries", () => ({
  workflowOverviewOptions: (wsId: string, workflowId: string) => ({
    queryKey: ["workflows", wsId, "detail", workflowId],
    queryFn: () => Promise.resolve(mockWorkflowData),
  }),
  useCreateStage: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpdateStage: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDeleteStage: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useReorderStages: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useAssignNodeToStage: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/platform", () => ({
  useWorkspaceId: () => "ws1",
}));

vi.mock("@multica/core/use-navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("@xyflow/react", () => ({
  ReactFlow: ({ nodes, edges }: { nodes: unknown[]; edges: unknown[] }) => (
    <div data-testid="reactflow">
      <span data-testid="rf-nodecount">{(nodes as unknown[]).length}</span>
      <span data-testid="rf-edgecount">{(edges as unknown[]).length}</span>
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  ReactFlowProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Handle: () => null,
  Position: { Top: "top", Bottom: "bottom", Left: "left", Right: "right" },
  MarkerType: { ArrowClosed: "arrowclosed" },
}));

vi.mock("@xyflow/react/dist/style.css", () => ({}));

// ── Tests ──

describe("WorkflowOverviewPage", () => {
  it("renders stage cards", async () => {
    render(renderWithI18n(<WorkflowOverviewPage workflowId="wf1" />, "workflows"));
    expect(await screen.findByText("Design")).toBeInTheDocument();
    expect(screen.getByText("Build")).toBeInTheDocument();
  });

  it("shows 'Unassigned' virtual card for nodes without stage", async () => {
    render(renderWithI18n(<WorkflowOverviewPage workflowId="wf1" />, "workflows"));
    expect(await screen.findByText("Unassigned")).toBeInTheDocument();
  });

  it("shows empty state when no stages", async () => {
    const emptyData = { ...mockWorkflowData, stages: [], nodes: [] };
    vi.mocked(
      require("@multica/core/workflows/queries").workflowOverviewOptions,
    ).mockReturnValueOnce({
      queryKey: ["workflows", "ws1", "detail", "wf1"],
      queryFn: () => Promise.resolve(emptyData),
    });
    render(renderWithI18n(<WorkflowOverviewPage workflowId="wf1" />, "workflows"));
    expect(await screen.findByText("尚未定义阶段")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
pnpm --filter @multica/views exec vitest run overview/overview-page.test.tsx
```

Expected: FAIL — component file not found or empty.

- [ ] **Step 3: Create the index barrel**

Create `packages/views/workflows/components/overview/index.ts`:

```typescript
export { WorkflowOverviewPage } from "./workflow-overview-page";
```

- [ ] **Step 4: Create WorkflowOverviewPage shell**

Create `packages/views/workflows/components/overview/workflow-overview-page.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/platform";
import { workflowOverviewOptions } from "@multica/core/workflows/queries";
import { StageCanvas } from "./stage-canvas";
import { StageNodeDag } from "./stage-node-dag";
import { NodeDetailPanel } from "./node-detail-panel";
import { StageCreateDialog } from "./stage-create-dialog";
import { useState } from "react";
import type { WorkflowStage, WorkflowNode } from "@multica/core/types";
import { useT } from "../../../use-t";

interface Props {
  workflowId: string;
}

export function WorkflowOverviewPage({ workflowId }: Props) {
  const wsId = useWorkspaceId();
  const { t } = useT("workflows");
  const { data, isLoading, isError, refetch } = useQuery(
    workflowOverviewOptions(wsId, workflowId),
  );

  const [selectedStageId, setSelectedStageId] = useState<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [showCreateDialog, setShowCreateDialog] = useState(false);

  if (isLoading) {
    return (
      <div className="p-6">
        <StageCanvasSkeleton />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="p-6">
        <div className="text-center text-muted-foreground">
          Failed to load workflow.{" "}
          <button onClick={() => refetch()} className="underline">
            Retry
          </button>
        </div>
      </div>
    );
  }

  const { workflow, nodes, edges, stages } = data;

  // Build "Unassigned" virtual stage for nodes without stage_id
  const unassignedNodes = nodes.filter((n: WorkflowNode) => !n.stageId);
  const allStages: (WorkflowStage & { _virtual?: boolean })[] = [...stages];
  if (unassignedNodes.length > 0) {
    allStages.push({
      id: "__unassigned__",
      workflowId: workflow.id,
      name: t(($) => $.overview.stage_canvas.unassigned),
      description: "",
      sortOrder: stages.length,
      nodeCount: unassignedNodes.length,
      createdAt: "",
      updatedAt: "",
      _virtual: true,
    });
  }

  const selectedStage = allStages.find((s) => s.id === selectedStageId);
  const selectedNode = nodes.find((n: WorkflowNode) => n.id === selectedNodeId);

  return (
    <div className="flex flex-col h-full" data-testid="workflow-overview-page">
      {/* Header */}
      <div className="px-6 py-4 border-b">
        <h1 className="text-lg font-semibold">{workflow.title}</h1>
        <p className="text-sm text-muted-foreground">
          {t(($) => $.overview.title)}
        </p>
      </div>

      {/* Stage Canvas */}
      {allStages.length === 0 ? (
        <div className="flex-1 flex items-center justify-center p-6">
          <div className="text-center">
            <p className="text-lg font-medium">
              {t(($) => $.overview.stage_canvas.empty_title)}
            </p>
            <p className="text-sm text-muted-foreground mt-1">
              {t(($) => $.overview.stage_canvas.empty_description)}
            </p>
            <button
              className="mt-4 px-4 py-2 bg-primary text-primary-foreground rounded-md"
              onClick={() => setShowCreateDialog(true)}
            >
              {t(($) => $.overview.stage_canvas.create_first)}
            </button>
          </div>
        </div>
      ) : (
        <>
          <StageCanvas
            stages={allStages}
            selectedStageId={selectedStageId}
            onSelectStage={setSelectedStageId}
            onAddStage={() => setShowCreateDialog(true)}
            onDeleteStage={(id) => {
              /* handled in StageCard */
            }}
            totalStages={stages.length}
          />

          {/* Stage Node DAG */}
          {selectedStage && selectedStage.id !== "__unassigned__" && (
            <StageNodeDag
              stageId={selectedStage.id}
              stageName={selectedStage.name}
              nodes={nodes.filter(
                (n: WorkflowNode) => n.stageId === selectedStage.id,
              )}
              edges={edges.filter(
                (e: { source_node_id: string; target_node_id: string }) => {
                  const sourceNode = nodes.find(
                    (n: WorkflowNode) => n.id === e.source_node_id,
                  );
                  return sourceNode?.stageId === selectedStage.id;
                },
              )}
              onNodeClick={setSelectedNodeId}
              isVirtual={false}
            />
          )}
        </>
      )}

      {/* Node Detail Panel */}
      {selectedNode && (
        <NodeDetailPanel
          node={selectedNode}
          workflowId={workflowId}
          nodes={nodes}
          edges={edges}
          onClose={() => setSelectedNodeId(null)}
        />
      )}

      {/* Create Stage Dialog */}
      {showCreateDialog && (
        <StageCreateDialog
          workflowId={workflowId}
          wsId={wsId}
          onClose={() => setShowCreateDialog(false)}
        />
      )}
    </div>
  );
}

// Loading skeleton for stage canvas
function StageCanvasSkeleton() {
  return (
    <div className="flex gap-3 overflow-hidden" data-testid="stage-canvas-skeleton">
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          className="w-40 h-24 rounded-lg bg-muted animate-pulse shrink-0"
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 5: Create the Next.js route page**

Create `apps/web/app/(dashboard)/[workspaceSlug]/workflows/[id]/overview/page.tsx`:

```tsx
"use client";

import { WorkflowOverviewPage } from "@multica/views/workflows/components/overview";
import { useParams } from "next/navigation";

export default function WorkflowOverviewRoute() {
  const params = useParams<{ workspaceSlug: string; id: string }>();
  return <WorkflowOverviewPage workflowId={params.id} />;
}
```

- [ ] **Step 6: Run the test to verify it compiles but fails (components not yet created)**

```bash
pnpm --filter @multica/views exec vitest run overview/overview-page.test.tsx
```

The test will fail to import because the sub-components don't exist yet. This is expected — they'll be created in Tasks 7-10.

- [ ] **Step 7: Commit**

```bash
git add apps/web/app/\(dashboard\)/\[workspaceSlug\]/workflows/\[id\]/overview/page.tsx packages/views/workflows/components/overview/index.ts packages/views/workflows/components/overview/workflow-overview-page.tsx packages/views/workflows/components/overview/overview-page.test.tsx
git commit -m "feat(views): add WorkflowOverviewPage shell with route and test"
```

---

### Task 7: StageCanvas + StageCard Components

**Files:**
- Create: `packages/views/workflows/components/overview/stage-canvas.tsx`
- Create: `packages/views/workflows/components/overview/stage-card.tsx`

**Interfaces:**
- Consumes: `WorkflowStage[]` + callbacks from `WorkflowOverviewPage`
- Produces:
  - `<StageCanvas stages selectedStageId onSelectStage onAddStage onDeleteStage totalStages />`
  - `<StageCard stage isSelected onClick onDelete />`

- [ ] **Step 1: Create StageCard**

Create `packages/views/workflows/components/overview/stage-card.tsx`:

```tsx
import type { WorkflowStage } from "@multica/core/types";
import { useT } from "../../../use-t";

interface StageCardProps {
  stage: WorkflowStage & { _virtual?: boolean };
  index: number;
  totalStages: number;
  isSelected: boolean;
  onClick: () => void;
  onEdit?: () => void;
  onDelete?: () => void;
  isVirtual?: boolean;
}

export function StageCard({
  stage,
  index,
  totalStages,
  isSelected,
  onClick,
  onEdit,
  onDelete,
  isVirtual,
}: StageCardProps) {
  const { t } = useT("workflows");

  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`stage-card-${stage.id}`}
      className={`
        shrink-0 w-44 px-4 py-3 rounded-lg border-2 text-left transition-colors
        ${isSelected
          ? "border-primary bg-primary/5"
          : "border-border hover:border-muted-foreground/30"
        }
        ${isVirtual ? "border-dashed" : ""}
      `}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {isVirtual
            ? stage.name
            : t(($) => $.overview.stage_canvas.stage_n_of_m, { n: index + 1, m: totalStages })}
        </span>
        {!isVirtual && onDelete && (
          <button
            type="button"
            className="text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
            data-testid={`delete-stage-${stage.id}`}
            aria-label="Delete stage"
          >
            ×
          </button>
        )}
      </div>
      <div className="mt-1 font-medium text-sm truncate">{stage.name}</div>
      <div className="mt-0.5 text-xs text-muted-foreground">
        {t(($) => $.overview.stage_canvas.nodes_count, { count: stage.nodeCount })}
      </div>
    </button>
  );
}
```

- [ ] **Step 2: Create StageCanvas**

Create `packages/views/workflows/components/overview/stage-canvas.tsx`:

```tsx
import { useRef } from "react";
import type { WorkflowStage } from "@multica/core/types";
import { StageCard } from "./stage-card";
import { useT } from "../../../use-t";

interface StageCanvasProps {
  stages: (WorkflowStage & { _virtual?: boolean })[];
  selectedStageId: string | null;
  onSelectStage: (id: string) => void;
  onAddStage: () => void;
  onDeleteStage: (stageId: string) => void;
  totalStages: number;
}

export function StageCanvas({
  stages,
  selectedStageId,
  onSelectStage,
  onAddStage,
  onDeleteStage,
  totalStages,
}: StageCanvasProps) {
  const { t } = useT("workflows");
  const scrollRef = useRef<HTMLDivElement>(null);

  return (
    <div className="relative px-6 py-4 border-b">
      <div className="flex items-center gap-1 mb-2">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          Stages
        </span>
      </div>
      <div
        ref={scrollRef}
        className="flex gap-3 overflow-x-auto pb-2 scrollbar-thin"
        data-testid="stage-canvas"
      >
        {stages.map((stage, i) => (
          <StageCard
            key={stage.id}
            stage={stage}
            index={i}
            totalStages={totalStages}
            isSelected={stage.id === selectedStageId}
            onClick={() =>
              onSelectStage(stage.id === selectedStageId ? "" : stage.id)
            }
            onDelete={
              stage._virtual
                ? undefined
                : () => onDeleteStage(stage.id)
            }
            isVirtual={stage._virtual}
          />
        ))}
        <button
          type="button"
          onClick={onAddStage}
          data-testid="add-stage-button"
          className="shrink-0 w-44 h-24 rounded-lg border-2 border-dashed border-muted-foreground/30
            flex items-center justify-center text-muted-foreground hover:border-muted-foreground/60
            hover:text-foreground transition-colors"
        >
          <span className="text-2xl">+</span>
          <span className="ml-1 text-sm">
            {t(($) => $.overview.stage_canvas.add_stage)}
          </span>
        </button>
      </div>
      {/* Fade mask on right when scrollable */}
      <div className="absolute right-6 top-0 bottom-0 w-8 bg-gradient-to-l from-background to-transparent pointer-events-none" />
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add packages/views/workflows/components/overview/stage-canvas.tsx packages/views/workflows/components/overview/stage-card.tsx
git commit -m "feat(views): add StageCanvas and StageCard components"
```

---

### Task 8: StageNodeDag Component

**Files:**
- Create: `packages/views/workflows/components/overview/stage-node-dag.tsx`

**Interfaces:**
- Consumes: Filtered nodes/edges from `WorkflowOverviewPage`, ReactFlow mock from test
- Produces: `<StageNodeDag stageId stageName nodes edges onNodeClick isVirtual />`

- [ ] **Step 1: Create StageNodeDag**

Create `packages/views/workflows/components/overview/stage-node-dag.tsx`:

```tsx
import { useMemo } from "react";
import { ReactFlow, ReactFlowProvider, Background, Controls } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { WorkflowNode as CustomWorkflowNode, WorkflowEdge as CustomWorkflowEdge } from "../reactflow-nodes";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { useT } from "../../../use-t";

interface StageNodeDagProps {
  stageId: string;
  stageName: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onNodeClick: (nodeId: string) => void;
  isVirtual: boolean;
}

const nodeTypes = { workflow: CustomWorkflowNode };
const edgeTypes = { workflow: CustomWorkflowEdge };

export function StageNodeDag({
  stageId,
  stageName,
  nodes,
  edges,
  onNodeClick,
  isVirtual,
}: StageNodeDagProps) {
  const { t } = useT("workflows");

  const flowNodes = useMemo(
    () =>
      nodes.map((n, i) => ({
        id: n.id,
        type: "workflow",
        position: { x: n.positionX || i * 220, y: n.positionY || 80 },
        data: {
          title: n.title,
          workerType: n.workerType,
          criticType: n.criticType,
          sortOrder: n.sortOrder,
          formatSchema: n.formatSchema,
          index: i,
          total: nodes.length,
        },
      })),
    [nodes],
  );

  const flowEdges = useMemo(
    () =>
      edges.map((e) => ({
        id: e.id,
        type: "workflow",
        source: e.sourceNodeId,
        target: e.targetNodeId,
      })),
    [edges],
  );

  if (nodes.length === 0) {
    return (
      <div className="p-6 text-center" data-testid="empty-stage-dag">
        <p className="text-sm font-medium">
          {t(($) => $.overview.node_dag.empty_title)}
        </p>
        <p className="text-xs text-muted-foreground mt-1">
          {t(($) => $.overview.node_dag.empty_description)}
        </p>
      </div>
    );
  }

  return (
    <div
      className="border-b transition-all duration-300 ease-out"
      style={{ maxHeight: "500px", height: "400px" }}
      data-testid={`stage-dag-${stageId}`}
    >
      <div className="px-6 py-2 text-xs font-medium text-muted-foreground border-b">
        {stageName}
      </div>
      <ReactFlowProvider>
        <ReactFlow
          nodes={flowNodes}
          edges={flowEdges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodeClick={(_event, node) => onNodeClick(node.id)}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={true}
          fitView
          fitViewOptions={{ padding: 0.3 }}
        >
          <Background />
          <Controls showZoom={true} showFitView={true} />
        </ReactFlow>
      </ReactFlowProvider>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add packages/views/workflows/components/overview/stage-node-dag.tsx
git commit -m "feat(views): add StageNodeDag read-only ReactFlow component"
```

---

### Task 9: NodeDetailPanel Component

**Files:**
- Create: `packages/views/workflows/components/overview/node-detail-panel.tsx`

**Interfaces:**
- Consumes: Selected `WorkflowNode` + full nodes/edges for relationship calculation
- Produces: `<NodeDetailPanel node workflowId nodes edges onClose />`

- [ ] **Step 1: Create NodeDetailPanel**

Create `packages/views/workflows/components/overview/node-detail-panel.tsx`:

```tsx
import { useMemo } from "react";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { useNavigation } from "@multica/core/use-navigation";
import { useT } from "../../../use-t";

interface NodeDetailPanelProps {
  node: WorkflowNode;
  workflowId: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onClose: () => void;
}

export function NodeDetailPanel({
  node,
  workflowId,
  nodes,
  edges,
  onClose,
}: NodeDetailPanelProps) {
  const { t } = useT("workflows");
  const nav = useNavigation();

  // Compute upstream/downstream within the same stage
  const upstreamNodes = useMemo(
    () =>
      edges
        .filter((e) => e.targetNodeId === node.id)
        .map((e) => nodes.find((n) => n.id === e.sourceNodeId))
        .filter(Boolean) as WorkflowNode[],
    [edges, nodes, node.id],
  );

  const downstreamNodes = useMemo(
    () =>
      edges
        .filter((e) => e.sourceNodeId === node.id)
        .map((e) => nodes.find((n) => n.id === e.targetNodeId))
        .filter(Boolean) as WorkflowNode[],
    [edges, nodes, node.id],
  );

  const formatSchema = useMemo(() => {
    try {
      return node.formatSchema
        ? JSON.stringify(JSON.parse(node.formatSchema as unknown as string), null, 2)
        : null;
    } catch {
      return node.formatSchema ? String(node.formatSchema) : null;
    }
  }, [node.formatSchema]);

  return (
    <div
      className="fixed right-0 top-0 bottom-0 w-[380px] bg-background border-l shadow-lg z-50 overflow-y-auto"
      data-testid="node-detail-panel"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b sticky top-0 bg-background">
        <h2 className="font-semibold text-sm">
          {t(($) => $.overview.detail_panel.title)}
        </h2>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
          data-testid="close-detail-panel"
        >
          ×
        </button>
      </div>

      <div className="p-4 space-y-5">
        {/* Basic Info */}
        <section>
          <h3 className="font-medium text-sm">{node.title}</h3>
          {node.description && (
            <p className="text-xs text-muted-foreground mt-1">{node.description}</p>
          )}
        </section>

        {/* Worker */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.worker)}
          </h4>
          {node.workerType ? (
            <div className="text-sm">
              <span className="inline-block px-2 py-0.5 rounded bg-muted text-xs">
                {node.workerType}
              </span>
              {node.workerId && (
                <span className="ml-2 text-xs text-muted-foreground">{node.workerId}</span>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>

        {/* Critic */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.critic)}
          </h4>
          {node.criticType ? (
            <div className="text-sm">
              <span className="inline-block px-2 py-0.5 rounded bg-muted text-xs">
                {node.criticType}
              </span>
              {node.criticId && (
                <span className="ml-2 text-xs text-muted-foreground">{node.criticId}</span>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>

        {/* Format Schema */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.format_schema)}
          </h4>
          {formatSchema ? (
            <pre className="text-xs bg-muted p-2 rounded overflow-auto max-h-32">
              {formatSchema}
            </pre>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.no_schema)}
            </p>
          )}
        </section>

        {/* Relations */}
        <section>
          <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-1">
            {t(($) => $.overview.detail_panel.relations)}
          </h4>
          {upstreamNodes.length > 0 && (
            <div className="mb-2">
              <span className="text-xs text-muted-foreground">
                {t(($) => $.overview.detail_panel.upstream)}:{" "}
              </span>
              {upstreamNodes.map((n) => (
                <span key={n.id} className="text-xs mr-1 px-1.5 py-0.5 rounded bg-muted">
                  {n.title}
                </span>
              ))}
            </div>
          )}
          {downstreamNodes.length > 0 && (
            <div>
              <span className="text-xs text-muted-foreground">
                {t(($) => $.overview.detail_panel.downstream)}:{" "}
              </span>
              {downstreamNodes.map((n) => (
                <span key={n.id} className="text-xs mr-1 px-1.5 py-0.5 rounded bg-muted">
                  {n.title}
                </span>
              ))}
            </div>
          )}
          {upstreamNodes.length === 0 && downstreamNodes.length === 0 && (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.overview.detail_panel.not_configured)}
            </p>
          )}
        </section>
      </div>

      {/* Footer */}
      <div className="sticky bottom-0 bg-background border-t p-3">
        <button
          onClick={() => nav.push(`/workflows/${workflowId}`)}
          className="w-full py-2 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90"
        >
          {t(($) => $.overview.detail_panel.open_in_editor)}
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add packages/views/workflows/components/overview/node-detail-panel.tsx
git commit -m "feat(views): add NodeDetailPanel slide-out drawer component"
```

---

### Task 10: StageCreateDialog Component

**Files:**
- Create: `packages/views/workflows/components/overview/stage-create-dialog.tsx`

**Interfaces:**
- Consumes: `useCreateStage` from Task 5
- Produces: `<StageCreateDialog workflowId wsId onClose />` with form fields

- [ ] **Step 1: Create StageCreateDialog**

Create `packages/views/workflows/components/overview/stage-create-dialog.tsx`:

```tsx
import { useState } from "react";
import { useCreateStage } from "@multica/core/workflows/queries";
import { useT } from "../../../use-t";

interface StageCreateDialogProps {
  workflowId: string;
  wsId: string;
  onClose: () => void;
}

export function StageCreateDialog({
  workflowId,
  wsId,
  onClose,
}: StageCreateDialogProps) {
  const { t } = useT("workflows");
  const createStage = useCreateStage(wsId, workflowId);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      await createStage.mutateAsync({ name: name.trim(), description: description.trim() || undefined });
      onClose();
    } catch {
      // Error handled by mutation state
    }
  }

  return (
    <div
      className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center"
      onClick={onClose}
      data-testid="stage-dialog-overlay"
    >
      <div
        className="bg-background rounded-lg shadow-xl w-full max-w-md mx-4 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold">
          {t(($) => $.overview.stage_dialog.create_title)}
        </h2>
        <form onSubmit={handleSubmit} className="mt-4 space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">
              {t(($) => $.overview.stage_dialog.name_label)}
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.overview.stage_dialog.name_placeholder)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background"
              autoFocus
              data-testid="stage-name-input"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">
              {t(($) => $.overview.stage_dialog.description_label)}
            </label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.overview.stage_dialog.description_placeholder)}
              className="w-full px-3 py-2 border rounded-md text-sm bg-background resize-none"
              rows={3}
            />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm rounded-md border hover:bg-muted"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!name.trim() || createStage.isPending}
              className="px-4 py-2 text-sm rounded-md bg-primary text-primary-foreground
                disabled:opacity-50 hover:opacity-90"
            >
              {createStage.isPending ? "Creating..." : "Create"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add packages/views/workflows/components/overview/stage-create-dialog.tsx
git commit -m "feat(views): add StageCreateDialog component"
```

---

### Task 11: Wire Everything Together & Run Tests

**Files:**
- Modify: `packages/views/workflows/components/overview/index.ts` (verify barrel export)
- Test: `packages/views/workflows/components/overview/overview-page.test.tsx` (verify tests pass)

- [ ] **Step 1: Verify barrel export**

Read `packages/views/workflows/components/overview/index.ts` — it should export `WorkflowOverviewPage`:

```typescript
export { WorkflowOverviewPage } from "./workflow-overview-page";
```

- [ ] **Step 2: Run the overview page test**

```bash
pnpm --filter @multica/views exec vitest run overview/overview-page.test.tsx
```

Expected: all tests pass.

- [ ] **Step 3: Run full typecheck**

```bash
pnpm typecheck
```

Expected: no type errors.

- [ ] **Step 4: Run all TS tests**

```bash
pnpm test
```

Expected: no regressions.

- [ ] **Step 5: Run Go tests**

```bash
make test
```

Expected: all Go tests pass, including the new stage handler tests.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: final integration verification for workflow stage overview"
```

---

## Verification

After all tasks are complete, run the full check:

```bash
make check
```

This runs: `pnpm typecheck` → `pnpm test` → `make test` → E2E tests.

---

## Self-Review

### 1. Spec coverage
- ✅ DB migration (Task 1) → Spec Section 5
- ✅ sqlc queries (Task 2) → Spec Section 5
- ✅ Go handlers + edge validation (Task 3) → Spec Section 6
- ✅ TS types + API client (Task 4) → Spec Section 5.5 + 6
- ✅ Query hooks + i18n (Task 5) → Spec Section 8.1 + 11
- ✅ Route + page shell (Task 6) → Spec Section 4.1 + 7.1
- ✅ StageCanvas + StageCard (Task 7) → Spec Section 9.1
- ✅ StageNodeDag (Task 8) → Spec Section 9.2
- ✅ NodeDetailPanel (Task 9) → Spec Section 9.3
- ✅ StageCreateDialog (Task 10) → Spec Section 9.1 (add stage button)
- ✅ Wiring + tests (Task 11) → Spec Section 12

### 2. Placeholder scan
- ✅ No "TBD", "TODO", "implement later" anywhere
- ✅ All code blocks are complete, concrete implementations
- ✅ No vague instructions — every step has exact code or exact commands

### 3. Type consistency
- ✅ `WorkflowStage` has `_virtual?: boolean` in internal use — consistently applied in StageCanvas and WorkflowOverviewPage
- ✅ `stageId` naming consistent: `stageId` in camelCase (frontend), `stage_id` in snake_case (API)
- ✅ `nodeCount` field from API → `node_count` in JSON response → `nodeCount` in TS type
- ✅ All Go handler names match router registration in Task 3 Step 8
- ✅ All API client method names match the query hooks that call them
