# DAG Canvas 工作流编排 — 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Phase 1 完成 — 手动创建 DAG 并运行的最小可行闭环：数据库新建 4 表 → Go CRUD → React Flow 画布 → AgentTask 关联运行。

**Architecture:** 前后端完全解耦，后端通过 REST API 提供数据，前端通过 TanStack Query 消费。WorkflowNode 通过 task_id 直接复用现有 AgentTask 执行层，无产物传递。

**Tech Stack:** React Flow（@xyflow/react）、TanStack Query、Zustand、Go Chi、sqlc、pgx/v5

---

## 预备知识：项目结构

- **数据库 Migration**: `server/migrations/` — 顺序编号（当前最大 111，下一个 112）
- **SQL 查询**: `server/pkg/db/queries/*.sql` — sqlc 注解生成 Go 代码
- **生成的 Go 代码**: `server/pkg/db/generated/` — `make sqlc` 触发
- **Handler 注册**: `server/cmd/server/router.go` — chi 路由
- **前端页面导出**: `packages/views/xxx/index.ts` — barrel export
- **Next.js 路由**: `apps/web/app/[workspaceSlug]/(dashboard)/xxx/page.tsx`

---

## Phase 1 实现任务

---

### Task 1: 数据库 Migration（112）

**Files:**
- Create: `server/migrations/112_plan_workflow.up.sql`
- Create: `server/migrations/112_plan_workflow.down.sql`

**Step 1: 创建 migration 文件**

`server/migrations/112_plan_workflow.up.sql`:
```sql
-- Plan: PRD 确认后的快照
CREATE TABLE plan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    creator_id UUID NOT NULL REFERENCES member(id),
    title TEXT NOT NULL,
    content TEXT,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'confirmed', 'running', 'done', 'cancelled')),
    workflow_id UUID,  -- 暂时 NULL，workflow 创建后再回填
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX plan_workspace_id_idx ON plan(workspace_id);

-- Workflow: DAG 画布容器
CREATE TABLE workflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'running', 'paused', 'done')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX workflow_plan_id_idx ON workflow(plan_id);

-- WorkflowNode: DAG 节点
CREATE TABLE workflow_node (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id),
    title TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    position_x FLOAT NOT NULL DEFAULT 0,
    position_y FLOAT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'queued', 'running', 'completed', 'failed', 'skipped')),
    task_id UUID REFERENCES agent_task(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX workflow_node_workflow_id_idx ON workflow_node(workflow_id);
CREATE INDEX workflow_node_task_id_idx ON workflow_node(task_id);

-- WorkflowEdge: DAG 边（执行顺序约束）
CREATE TABLE workflow_edge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    target_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workflow_edge_no_self CHECK (source_node_id != target_node_id),
    CONSTRAINT workflow_edge_unique UNIQUE (workflow_id, source_node_id, target_node_id)
);

CREATE INDEX workflow_edge_workflow_id_idx ON workflow_edge(workflow_id);

-- 回填 plan.workflow_id 为 plan 自引用（先创建 workflow 再回填，这里先加外键）
-- 由于 plan 和 workflow 同时创建，我们先不清空外键约束，在 service 层处理
```

**Step 2: 创建 rollback migration**

`server/migrations/112_plan_workflow.down.sql`:
```sql
DROP TABLE IF EXISTS workflow_edge;
DROP TABLE IF EXISTS workflow_node;
DROP TABLE IF EXISTS workflow;
DROP TABLE IF EXISTS plan;
```

**Step 3: 运行 migration**

Run: `make migrate-up`
Expected: `Migration 112_plan_workflow applied successfully`

**Step 4: 验证表创建**

Run: `psql $DATABASE_URL -c "\dt plan" -c "\dt workflow" -c "\dt workflow_node" -c "\dt workflow_edge"`
Expected: 4 个表列出

---

### Task 2: SQL 查询（plan + workflow）

**Files:**
- Create: `server/pkg/db/queries/plan.sql`
- Create: `server/pkg/db/queries/workflow.sql`
- Modify: `server/pkg/db/queries/queries.sql` — 添加 import（如果存在）

> 注意：先确认 queries 目录下是否有汇总的 `queries.sql` 文件

**Step 1: 确认 queries 结构**

Run: `ls server/pkg/db/queries/`
Expected: 每个实体一个 `.sql` 文件（如 agent.sql, issue.sql），sqlc 会扫描整个目录

**Step 2: 写 plan.sql**

`server/pkg/db/queries/plan.sql`:
```sql
-- name: CreatePlan :one
INSERT INTO plan (workspace_id, creator_id, title, content, status, workflow_id)
VALUES ($1, $2, $3, $4, 'draft', $5)
RETURNING *;

-- name: GetPlan :one
SELECT * FROM plan WHERE id = $1;

-- name: GetPlanByWorkspace :many
SELECT * FROM plan WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: UpdatePlan :one
UPDATE plan SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    status = COALESCE(sqlc.narg('status'), status),
    workflow_id = COALESCE(sqlc.narg('workflow_id'), workflow_id),
    updated_at = now()
WHERE id = $1
RETURNING *;
```

**Step 3: 写 workflow.sql**

`server/pkg/db/queries/workflow.sql`:
```sql
-- name: CreateWorkflow :one
INSERT INTO workflow (plan_id, title, status)
VALUES ($1, $2, 'draft')
RETURNING *;

-- name: GetWorkflow :one
SELECT * FROM workflow WHERE id = $1;

-- name: GetWorkflowByPlan :one
SELECT * FROM workflow WHERE plan_id = $1;

-- name: UpdateWorkflow :one
UPDATE workflow SET
    title = COALESCE(sqlc.narg('title'), title),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateWorkflowNode :one
INSERT INTO workflow_node (workflow_id, agent_id, title, prompt, position_x, position_y, status)
VALUES ($1, $2, $3, $4, $5, $6, 'pending')
RETURNING *;

-- name: ListWorkflowNodes :many
SELECT * FROM workflow_node WHERE workflow_id = $1 ORDER BY created_at ASC;

-- name: UpdateWorkflowNode :one
UPDATE workflow_node SET
    title = COALESCE(sqlc.narg('title'), title),
    prompt = COALESCE(sqlc.narg('prompt'), prompt),
    agent_id = COALESCE(sqlc.narg('agent_id'), agent_id),
    position_x = COALESCE(sqlc.narg('position_x'), position_x),
    position_y = COALESCE(sqlc.narg('position_y'), position_y),
    status = COALESCE(sqlc.narg('status'), status),
    task_id = COALESCE(sqlc.narg('task_id'), task_id),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowNode :exec
DELETE FROM workflow_node WHERE id = $1;

-- name: CreateWorkflowEdge :one
INSERT INTO workflow_edge (workflow_id, source_node_id, target_node_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListWorkflowEdges :many
SELECT * FROM workflow_edge WHERE workflow_id = $1;

-- name: DeleteWorkflowEdge :exec
DELETE FROM workflow_edge WHERE id = $1;

-- name: GetNodeByTaskID :one
SELECT * FROM workflow_node WHERE task_id = $1;
```

**Step 4: 生成 Go 代码**

Run: `make sqlc`
Expected: 无错误，`server/pkg/db/generated/` 下新增 plan.go, workflow.go

**Step 5: 提交**

```bash
git add server/migrations/ server/pkg/db/queries/plan.sql server/pkg/db/queries/workflow.sql
git commit -m "feat(db): add plan, workflow, workflow_node, workflow_edge tables

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Go Handler — Plan CRUD

**Files:**
- Create: `server/internal/handler/plan.go`

**Step 1: 写 handler**

`server/internal/handler/plan.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

type PlanResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	CreatorID   string  `json:"creator_id"`
	Title       string  `json:"title"`
	Content     *string `json:"content"`
	Status      string  `json:"status"`
	WorkflowID  *string `json:"workflow_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func planToResponse(p service.PlanOutput) PlanResponse {
	return PlanResponse{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		CreatorID:   p.CreatorID,
		Title:       p.Title,
		Content:     p.Content,
		Status:      p.Status,
		WorkflowID:  p.WorkflowID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspaceId")
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "missing workspaceId")
		return
	}

	var body struct {
		Title   string  `json:"title"`
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	plan, err := h.PlanSvc.Create(r.Context(), wsID, body.Title, body.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, planToResponse(plan))
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planId")
	plan, err := h.PlanSvc.Get(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, planToResponse(plan))
}

func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "workspaceId")
	plans, err := h.PlanSvc.List(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planId")
	var body struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
		Status  *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	plan, err := h.PlanSvc.Update(r.Context(), planID, body.Title, body.Content, body.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, planToResponse(plan))
}

// Placeholder – fill in helper types after Task 5 (service layer)
func uuidToString(id pgtype.UUID) string { return id.UUID.String() }
func writeError(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
```

> **注意**：Handler 依赖 Service 层类型，先写 Task 5 再回来补充 import 和类型转换

---

### Task 4: Go Handler — Workflow CRUD

**Files:**
- Create: `server/internal/handler/workflow.go`

**Step 1: 写 handler**

`server/internal/handler/workflow.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateNodeRequest struct {
	AgentID   string  `json:"agent_id"`
	Title     string  `json:"title"`
	Prompt    string  `json:"prompt"`
	PositionX float64 `json:"position_x"`
	PositionY float64 `json:"position_y"`
}

type UpdateNodeRequest struct {
	Title     *string  `json:"title"`
	Prompt    *string  `json:"prompt"`
	AgentID   *string  `json:"agent_id"`
	PositionX *float64 `json:"position_x"`
	PositionY *float64 `json:"position_y"`
	Status    *string  `json:"status"`
	TaskID    *string  `json:"task_id"`
}

type CreateEdgeRequest struct {
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
}

// GET /api/workflows/{workflowId}
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	wf, err := h.WorkflowSvc.Get(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// PATCH /api/workflows/{workflowId}
func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	var body struct {
		Title  *string `json:"title"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	wf, err := h.WorkflowSvc.Update(r.Context(), wid, body.Title, body.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// GET /api/workflows/{workflowId}/nodes
func (h *Handler) ListWorkflowNodes(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	nodes, err := h.WorkflowSvc.ListNodes(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// POST /api/workflows/{workflowId}/nodes
func (h *Handler) CreateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	var body CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	node, err := h.WorkflowSvc.CreateNode(r.Context(), wid, body.AgentID, body.Title, body.Prompt, body.PositionX, body.PositionY)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

// PATCH /api/workflows/{workflowId}/nodes/{nodeId}
func (h *Handler) UpdateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	var body UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	node, err := h.WorkflowSvc.UpdateNode(r.Context(), nodeID, &body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// DELETE /api/workflows/{workflowId}/nodes/{nodeId}
func (h *Handler) DeleteWorkflowNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	if err := h.WorkflowSvc.DeleteNode(r.Context(), nodeID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/workflows/{workflowId}/edges
func (h *Handler) ListWorkflowEdges(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	edges, err := h.WorkflowSvc.ListEdges(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

// POST /api/workflows/{workflowId}/edges
func (h *Handler) CreateWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	var body CreateEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	edge, err := h.WorkflowSvc.CreateEdge(r.Context(), wid, body.SourceNodeID, body.TargetNodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, edge)
}

// DELETE /api/workflows/{workflowId}/edges/{edgeId}
func (h *Handler) DeleteWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	edgeID := chi.URLParam(r, "edgeId")
	if err := h.WorkflowSvc.DeleteEdge(r.Context(), edgeID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/workflows/{workflowId}/confirm
func (h *Handler) ConfirmWorkflow(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "workflowId")
	result, err := h.WorkflowSvc.Confirm(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

---

### Task 5: Go Service — Plan + Workflow Service

**Files:**
- Create: `server/internal/service/plan.go`
- Create: `server/internal/service/workflow.go`

**Step 1: 写 plan.go**

`server/internal/service/plan.go`:
```go
package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/db/generated"
)

type PlanOutput struct {
	ID          string
	WorkspaceID string
	CreatorID   string
	Title       string
	Content     *string
	Status      string
	WorkflowID  *string
	CreatedAt   string
	UpdatedAt   string
}

func (s *Service) Create(ctx context.Context, wsID, creatorID, title string, content *string) (PlanOutput, error) {
	// 1. 创建 Workflow
	wf, err := s.Queries.CreateWorkflow(ctx, wsID, title)
	if err != nil {
		return PlanOutput{}, err
	}

	// 2. 创建 Plan 并关联 workflow_id
	plan, err := s.Queries.CreatePlan(ctx, generated.CreatePlanParams{
		WorkspaceID: mustParseUUID(wsID),
		CreatorID:   mustParseUUID(creatorID),
		Title:       title,
		Content:     toPtrOrNull(content),
		WorkflowID:   pgtype.UUID{UUID: wf.ID.UUID, Valid: true},
	})
	if err != nil {
		return PlanOutput{}, err
	}

	return planToOutput(plan), nil
}

func (s *Service) Get(ctx context.Context, planID string) (PlanOutput, error) {
	plan, err := s.Queries.GetPlan(ctx, mustParseUUID(planID))
	if err != nil {
		return PlanOutput{}, err
	}
	return planToOutput(plan), nil
}

func (s *Service) List(ctx context.Context, wsID string) ([]PlanOutput, error) {
	plans, err := s.Queries.GetPlanByWorkspace(ctx, mustParseUUID(wsID))
	if err != nil {
		return nil, err
	}
	out := make([]PlanOutput, len(plans))
	for i, p := range plans {
		out[i] = planToOutput(p)
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, planID string, title, content, status *string) (PlanOutput, error) {
	plan, err := s.Queries.UpdatePlan(ctx, generated.UpdatePlanParams{
		ID:      mustParseUUID(planID),
		Title:   toPGXStringPtr(title),
		Content: toPGXStringPtr(content),
		Status:  toPGXStringPtr(status),
	})
	if err != nil {
		return PlanOutput{}, err
	}
	return planToOutput(plan), nil
}

func planToOutput(p generated.Plan) PlanOutput {
	return PlanOutput{
		ID:          p.ID.UUID.String(),
		WorkspaceID: p.WorkspaceID.UUID.String(),
		CreatorID:   p.CreatorID.UUID.String(),
		Title:       p.Title,
		Content:     pgxDecodeText(p.Content),
		Status:      p.Status,
		WorkflowID:  uuidToStrPtr(p.WorkflowID),
		CreatedAt:   p.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   p.UpdatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

// helpers
func mustParseUUID(s string) pgtype.UUID {
	u, err := pgtype.ParseUUID(s)
	if err != nil {
		panic("invalid uuid: " + s)
	}
	return u
}

func toPGXStringPtr(s *string) *string {
	return s
}

func pgxDecodeText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidToStrPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.UUID.String()
	return &s
}

func toPtrOrNull(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}
```

**Step 2: 写 workflow.go（核心分发逻辑）**

`server/internal/service/workflow.go`:
```go
package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/db/generated"
)

type WorkflowOutput struct {
	ID        string `json:"id"`
	PlanID    string `json:"plan_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type WorkflowNodeOutput struct {
	ID          string  `json:"id"`
	WorkflowID  string  `json:"workflow_id"`
	AgentID     string  `json:"agent_id"`
	Title       string  `json:"title"`
	Prompt      string  `json:"prompt"`
	PositionX   float64 `json:"position_x"`
	PositionY   float64 `json:"position_y"`
	Status      string  `json:"status"`
	TaskID      *string `json:"task_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type WorkflowEdgeOutput struct {
	ID           string `json:"id"`
	WorkflowID   string `json:"workflow_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
}

type ConfirmResult struct {
	Workflow WorkflowOutput   `json:"workflow"`
	Nodes    []WorkflowNodeOutput `json:"nodes"`
}

// Get /api/workflows/{id}
func (s *Service) Get(ctx context.Context, workflowID string) (WorkflowOutput, error) {
	wf, err := s.Queries.GetWorkflow(ctx, mustParseUUID(workflowID))
	if err != nil {
		return WorkflowOutput{}, err
	}
	return workflowToOutput(wf), nil
}

// PATCH /api/workflows/{id}
func (s *Service) Update(ctx context.Context, workflowID string, title, status *string) (WorkflowOutput, error) {
	wf, err := s.Queries.UpdateWorkflow(ctx, generated.UpdateWorkflowParams{
		ID:     mustParseUUID(workflowID),
		Title:  toPGXStringPtr(title),
		Status: toPGXStringPtr(status),
	})
	if err != nil {
		return WorkflowOutput{}, err
	}
	return workflowToOutput(wf), nil
}

// Node CRUD
func (s *Service) ListNodes(ctx context.Context, workflowID string) ([]WorkflowNodeOutput, error) {
	nodes, err := s.Queries.ListWorkflowNodes(ctx, mustParseUUID(workflowID))
	if err != nil {
		return nil, err
	}
	return nodesToOutput(nodes), nil
}

func (s *Service) CreateNode(ctx context.Context, workflowID, agentID, title, prompt string, x, y float64) (WorkflowNodeOutput, error) {
	node, err := s.Queries.CreateWorkflowNode(ctx, generated.CreateWorkflowNodeParams{
		WorkflowID: mustParseUUID(workflowID),
		AgentID:   mustParseUUID(agentID),
		Title:     title,
		Prompt:    prompt,
		PositionX: x,
		PositionY: y,
	})
	if err != nil {
		return WorkflowNodeOutput{}, err
	}
	return nodeToOutput(node), nil
}

type UpdateNodeInput struct {
	Title     *string  `json:"title"`
	Prompt    *string  `json:"prompt"`
	AgentID   *string  `json:"agent_id"`
	PositionX *float64 `json:"position_x"`
	PositionY *float64 `json:"position_y"`
	Status    *string  `json:"status"`
	TaskID    *string  `json:"task_id"`
}

func (s *Service) UpdateNode(ctx context.Context, nodeID string, input *UpdateNodeInput) (WorkflowNodeOutput, error) {
	var taskID *pgtype.UUID
	if input.TaskID != nil {
		t := mustParseUUID(*input.TaskID)
		taskID = &t
	}
	var agentID *pgtype.UUID
	if input.AgentID != nil {
		a := mustParseUUID(*input.AgentID)
		agentID = &a
	}

	node, err := s.Queries.UpdateWorkflowNode(ctx, generated.UpdateWorkflowNodeParams{
		ID:          mustParseUUID(nodeID),
		Title:       toPGXStringPtr(input.Title),
		Prompt:      toPGXStringPtr(input.Prompt),
		AgentID:     agentID,
		PositionX:   input.PositionX,
		PositionY:   input.PositionY,
		Status:      toPGXStringPtr(input.Status),
		TaskID:      taskID,
	})
	if err != nil {
		return WorkflowNodeOutput{}, err
	}
	return nodeToOutput(node), nil
}

func (s *Service) DeleteNode(ctx context.Context, nodeID string) error {
	// 先删所有关联边
	return s.Queries.DeleteWorkflowNode(ctx, mustParseUUID(nodeID))
}

// Edge CRUD
func (s *Service) ListEdges(ctx context.Context, workflowID string) ([]WorkflowEdgeOutput, error) {
	edges, err := s.Queries.ListWorkflowEdges(ctx, mustParseUUID(workflowID))
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowEdgeOutput, len(edges))
	for i, e := range edges {
		out[i] = WorkflowEdgeOutput{
			ID:           e.ID.UUID.String(),
			WorkflowID:   e.WorkflowID.UUID.String(),
			SourceNodeID: e.SourceNodeID.UUID.String(),
			TargetNodeID: e.TargetNodeID.UUID.String(),
		}
	}
	return out, nil
}

func (s *Service) CreateEdge(ctx context.Context, workflowID, sourceID, targetID string) (WorkflowEdgeOutput, error) {
	// 循环检测
	if hasCycle(workflowID, sourceID, targetID) {
		return WorkflowEdgeOutput{}, errors.New("cycle detected")
	}
	edge, err := s.Queries.CreateWorkflowEdge(ctx, generated.CreateWorkflowEdgeParams{
		WorkflowID:   mustParseUUID(workflowID),
		SourceNodeID: mustParseUUID(sourceID),
		TargetNodeID: mustParseUUID(targetID),
	})
	if err != nil {
		return WorkflowEdgeOutput{}, err
	}
	return WorkflowEdgeOutput{
		ID:           edge.ID.UUID.String(),
		WorkflowID:   edge.WorkflowID.UUID.String(),
		SourceNodeID: edge.SourceNodeID.UUID.String(),
		TargetNodeID: edge.TargetNodeID.UUID.String(),
	}, nil
}

func (s *Service) DeleteEdge(ctx context.Context, edgeID string) error {
	return s.Queries.DeleteWorkflowEdge(ctx, mustParseUUID(edgeID))
}

// Confirm — 确认 DAG，开始分发无入边的节点
func (s *Service) Confirm(ctx context.Context, workflowID string) (ConfirmResult, error) {
	wf, err := s.Queries.GetWorkflow(ctx, mustParseUUID(workflowID))
	if err != nil {
		return ConfirmResult{}, err
	}

	// 更新 workflow 状态
	wf, err = s.Queries.UpdateWorkflow(ctx, generated.UpdateWorkflowParams{
		ID:     wf.ID,
		Status: strPtr("running"),
	})
	if err != nil {
		return ConfirmResult{}, err
	}

	// 分发无入边的 pending 节点
	dispatched, err := s.dispatchReadyNodes(ctx, workflowID)
	if err != nil {
		return ConfirmResult{}, err
	}

	return ConfirmResult{
		Workflow: workflowToOutput(wf),
		Nodes:    nodesToOutput(dispatched),
	}, nil
}

// dispatchReadyNodes — 找出所有无入边的 pending 节点并分发 AgentTask
func (s *Service) dispatchReadyNodes(ctx context.Context, workflowID string) ([]generated.WorkflowNode, error) {
	allNodes, err := s.Queries.ListWorkflowNodes(ctx, mustParseUUID(workflowID))
	if err != nil {
		return nil, err
	}
	edges, err := s.Queries.ListWorkflowEdges(ctx, mustParseUUID(workflowID))
	if err != nil {
		return nil, err
	}

	// 构建入度 map
	inDegree := make(map[string]int)
	for _, n := range allNodes {
		inDegree[n.ID.UUID.String()] = 0
	}
	for _, e := range edges {
		inDegree[e.TargetNodeID.UUID.String()]++
	}

	var dispatched []generated.WorkflowNode
	for _, node := range allNodes {
		if node.Status != "pending" {
			continue
		}
		// 无入边 → 可以立即分发
		if inDegree[node.ID.UUID.String()] == 0 {
			taskID, err := s.createTaskForNode(ctx, node)
			if err != nil {
				slog.Error("failed to create task for node", "node_id", node.ID.UUID.String(), "err", err)
				continue
			}
			t := mustParseUUID(taskID)
			updated, err := s.Queries.UpdateWorkflowNode(ctx, generated.UpdateWorkflowNodeParams{
				ID:      node.ID,
				Status:  strPtr("queued"),
				TaskID:  &t,
			})
			if err != nil {
				return nil, err
			}
			dispatched = append(dispatched, updated)
		}
	}
	return dispatched, nil
}

// createTaskForNode — 复用 TaskService 创建 AgentTask
// task_type = "workflow_node"，issue_id = NULL
func (s *Service) createTaskForNode(ctx context.Context, node generated.WorkflowNode) (string, error) {
	// 这里需要调用 TaskService.CreateTaskForNode
	// 等 Task 6 完成后再实现
	return "", nil
}

// hasCycle — 简单 DFS 检测加边后是否有环
func hasCycle(workflowID, sourceID, targetID string) bool {
	// TODO: 完整实现见 Phase 2
	// Phase 1 简化：不检测（最简 MVP）
	return false
}

// output 转换 helpers
func workflowToOutput(w generated.Workflow) WorkflowOutput {
	return WorkflowOutput{
		ID:        w.ID.UUID.String(),
		PlanID:    w.PlanID.UUID.String(),
		Title:     w.Title,
		Status:    w.Status,
		CreatedAt: w.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: w.UpdatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

func nodeToOutput(n generated.WorkflowNode) WorkflowNodeOutput {
	return WorkflowNodeOutput{
		ID:          n.ID.UUID.String(),
		WorkflowID:  n.WorkflowID.UUID.String(),
		AgentID:     n.AgentID.UUID.String(),
		Title:       n.Title,
		Prompt:      n.Prompt.String,
		PositionX:   n.PositionX,
		PositionY:   n.PositionY,
		Status:      n.Status,
		TaskID:      uuidToStrPtr(n.TaskID),
		CreatedAt:   n.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   n.UpdatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

func nodesToOutput(nodes []generated.WorkflowNode) []WorkflowNodeOutput {
	out := make([]WorkflowNodeOutput, len(nodes))
	for i, n := range nodes {
		out[i] = nodeToOutput(n)
	}
	return out
}

func strPtr(s string) *string { return &s }
```

---

### Task 6: 注册路由

**Files:**
- Modify: `server/cmd/server/router.go` — 注册 Plan 和 Workflow 路由

**Step 1: 找到路由注册位置**

Run: `grep -n "r.Route\|/api/agents" server/cmd/server/router.go | head -20`
Expected: 显示路由分组模式

**Step 2: 添加 Plan 路由**

在 workspace-scoped 路由块中添加（参考 `/api/issues` 的位置）：

```go
// Plan
r.Route("/api/plans", func(r chi.Router) {
    r.Get("/", h.ListPlans)
    r.Post("/", h.CreatePlan)
    r.Get("/{planId}", h.GetPlan)
    r.Patch("/{planId}", h.UpdatePlan)
})

// Workflow
r.Route("/api/workflows", func(r chi.Router) {
    r.Get("/{workflowId}", h.GetWorkflow)
    r.Patch("/{workflowId}", h.UpdateWorkflow)
    r.Get("/{workflowId}/nodes", h.ListWorkflowNodes)
    r.Post("/{workflowId}/nodes", h.CreateWorkflowNode)
    r.Patch("/{workflowId}/nodes/{nodeId}", h.UpdateWorkflowNode)
    r.Delete("/{workflowId}/nodes/{nodeId}", h.DeleteWorkflowNode)
    r.Get("/{workflowId}/edges", h.ListWorkflowEdges)
    r.Post("/{workflowId}/edges", h.CreateWorkflowEdge)
    r.Delete("/{workflowId}/edges/{edgeId}", h.DeleteWorkflowEdge)
    r.Post("/{workflowId}/confirm", h.ConfirmWorkflow)
})
```

**Step 3: 添加 Handler 注入**

在 router.go 中找到 Handler struct 初始化处，添加：
```go
PlanSvc     *service.PlanService  // 等 Task 5 完成后添加
WorkflowSvc *service.WorkflowService
```

**Step 4: 编译验证**

Run: `cd server && go build ./cmd/server/`
Expected: 无编译错误（可能有未定义类型错误，这是预期的，等 Task 5 完成后消失）

---

### Task 7: TypeScript 类型

**Files:**
- Create: `packages/core/types/workflow.ts`

**Step 1: 写类型定义**

```typescript
// packages/core/types/workflow.ts

export type PlanStatus = "draft" | "confirmed" | "running" | "done" | "cancelled";
export type WorkflowStatus = "draft" | "running" | "paused" | "done";
export type NodeStatus = "pending" | "queued" | "running" | "completed" | "failed" | "skipped";

export interface Plan {
  id: string;
  workspace_id: string;
  creator_id: string;
  title: string;
  content: string | null;
  status: PlanStatus;
  workflow_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface Workflow {
  id: string;
  plan_id: string;
  title: string;
  status: WorkflowStatus;
  created_at: string;
  updated_at: string;
}

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  agent_id: string;
  title: string;
  prompt: string;
  position_x: number;
  position_y: number;
  status: NodeStatus;
  task_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdge {
  id: string;
  workflow_id: string;
  source_node_id: string;
  target_node_id: string;
}

export interface PlanWithWorkflow extends Plan {
  workflow: (Workflow & { nodes: WorkflowNode[]; edges: WorkflowEdge[] }) | null;
}
```

**Step 2: 导出类型**

找到 `packages/core/types/index.ts`，添加：
```typescript
export * from "./workflow";
```

**Step 3: 提交**

```bash
git add packages/core/types/workflow.ts
git commit -m "feat(types): add workflow types

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: API Client

**Files:**
- Create: `packages/core/api/workflows.ts`

**Step 1: 写 API 客户端**

```typescript
// packages/core/api/workflows.ts
import type { Plan, Workflow, WorkflowNode, WorkflowEdge } from "../types/workflow";

const BASE = "/api";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

// Plan
export const planApi = {
  list: (workspaceId: string) =>
    request<Plan[]>(`/workspaces/${workspaceId}/plans`),

  create: (workspaceId: string, body: { title: string; content?: string }) =>
    request<Plan>(`/workspaces/${workspaceId}/plans`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  get: (planId: string) =>
    request<Plan>(`/plans/${planId}`),

  update: (planId: string, body: Partial<Pick<Plan, "title" | "content" | "status">>) =>
    request<Plan>(`/plans/${planId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
};

// Workflow
export const workflowApi = {
  get: (workflowId: string) =>
    request<Workflow>(`/workflows/${workflowId}`),

  update: (workflowId: string, body: Partial<Pick<Workflow, "title" | "status">>) =>
    request<Workflow>(`/workflows/${workflowId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  // Nodes
  listNodes: (workflowId: string) =>
    request<WorkflowNode[]>(`/workflows/${workflowId}/nodes`),

  createNode: (workflowId: string, body: {
    agent_id: string;
    title: string;
    prompt: string;
    position_x: number;
    position_y: number;
  }) =>
    request<WorkflowNode>(`/workflows/${workflowId}/nodes`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateNode: (workflowId: string, nodeId: string, body: Partial<{
    title: string;
    prompt: string;
    agent_id: string;
    position_x: number;
    position_y: number;
    status: string;
    task_id: string;
  }>) =>
    request<WorkflowNode>(`/workflows/${workflowId}/nodes/${nodeId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  deleteNode: (workflowId: string, nodeId: string) =>
    request<void>(`/workflows/${workflowId}/nodes/${nodeId}`, {
      method: "DELETE",
    }),

  // Edges
  listEdges: (workflowId: string) =>
    request<WorkflowEdge[]>(`/workflows/${workflowId}/edges`),

  createEdge: (workflowId: string, body: {
    source_node_id: string;
    target_node_id: string;
  }) =>
    request<WorkflowEdge>(`/workflows/${workflowId}/edges`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  deleteEdge: (workflowId: string, edgeId: string) =>
    request<void>(`/workflows/${workflowId}/edges/${edgeId}`, {
      method: "DELETE",
    }),

  confirm: (workflowId: string) =>
    request<{ workflow: Workflow; nodes: WorkflowNode[] }>(
      `/workflows/${workflowId}/confirm`,
      { method: "POST" }
    ),
};
```

---

### Task 9: React Flow Canvas（核心 UI）

**Files:**
- Create: `packages/views/workflows/canvas/workflow-canvas.tsx`
- Create: `packages/views/workflows/canvas/agent-node.tsx`
- Create: `packages/views/workflows/canvas/workflow-edge.tsx`

**Step 1: 安装依赖**

Run: `pnpm add @xyflow/react`
Expected: reactflow 安装成功

**Step 2: AgentNode 组件**

`packages/views/workflows/canvas/agent-node.tsx`:
```tsx
"use client";

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { WorkflowNode } from "@multica/core/types/workflow";
import { cn } from "@multica/ui/lib/utils";

const STATUS_STYLES: Record<string, { border: string; bg: string; label: string }> = {
  pending:   { border: "border-dashed border-muted-foreground", bg: "bg-muted/30", label: "Pending" },
  queued:    { border: "border-blue-500",                     bg: "bg-blue-500/10", label: "Queued" },
  running:   { border: "border-blue-500 animate-pulse",       bg: "bg-blue-500/10", label: "Running" },
  completed: { border: "border-green-500",                    bg: "bg-green-500/10", label: "Completed" },
  failed:    { border: "border-red-500",                       bg: "bg-red-500/10", label: "Failed" },
  skipped:   { border: "border-muted-foreground",            bg: "bg-muted", label: "Skipped" },
};

function AgentNode({ data, selected }: NodeProps<WorkflowNode>) {
  const style = STATUS_STYLES[data.status] ?? STATUS_STYLES.pending;

  return (
    <div
      className={cn(
        "min-w-[200px] max-w-[280px] rounded-lg border-2 p-3 shadow-sm transition-all",
        style.border,
        style.bg,
        selected && "ring-2 ring-primary"
      )}
    >
      <Handle type="target" position={Position.Left} className="!w-2 !h-2" />
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium text-sm truncate">{data.title}</span>
        <span className={cn(
          "text-xs px-1.5 py-0.5 rounded-full",
          data.status === "completed" && "bg-green-500/20 text-green-600",
          data.status === "failed" && "bg-red-500/20 text-red-600",
          data.status === "running" && "bg-blue-500/20 text-blue-600",
          data.status === "pending" && "bg-muted text-muted-foreground",
        )}>
          {style.label}
        </span>
      </div>
      <p className="text-xs text-muted-foreground line-clamp-2">
        {data.prompt || "No prompt set"}
      </p>
      <Handle type="source" position={Position.Right} className="!w-2 !h-2" />
    </div>
  );
}

export const AgentNodeComponent = memo(AgentNode);
```

**Step 3: WorkflowEdge 组件**

`packages/views/workflows/canvas/workflow-edge.tsx`:
```tsx
"use client";

import { memo } from "react";
import { BaseEdge, type EdgeProps, getBezierPath } from "@xyflow/react";

function WorkflowEdge({
  id,
  sourceX, sourceY, targetX, targetY,
  sourcePosition, targetPosition,
  selected,
}: EdgeProps) {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX, sourceY, sourcePosition,
    targetX, targetY, targetPosition,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        className={selected ? "!stroke-primary !stroke-[3px]" : "!stroke-muted-foreground/60"}
      />
      {/* Arrow head */}
      <circle
        cx={targetX}
        cy={targetY}
        r={4}
        fill={selected ? "var(--primary)" : "hsl(var(--muted-foreground))"}
      />
    </>
  );
}

export const WorkflowEdgeComponent = memo(WorkflowEdge);
```

**Step 4: WorkflowCanvas 主组件**

`packages/views/workflows/canvas/workflow-canvas.tsx`:
```tsx
"use client";

import { useCallback, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
  BackgroundVariant,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AgentNodeComponent } from "./agent-node";
import { WorkflowEdgeComponent } from "./workflow-edge";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types/workflow";

const nodeTypes: NodeTypes = { agent: AgentNodeComponent };
const edgeTypes: EdgeTypes = { workflow: WorkflowEdgeComponent };

interface WorkflowCanvasProps {
  workflowId: string;
  initialNodes: WorkflowNode[];
  initialEdges: WorkflowEdge[];
  onNodeUpdate?: (nodeId: string, data: Partial<WorkflowNode>) => void;
  onEdgeCreate?: (sourceId: string, targetId: string) => void;
  onEdgeDelete?: (edgeId: string) => void;
  onNodesChange?: (nodes: WorkflowNode[]) => void;
}

export function WorkflowCanvas({
  initialNodes,
  initialEdges,
  onNodeUpdate,
  onEdgeCreate,
  onEdgeDelete,
  onNodesChange,
}: WorkflowCanvasProps) {
  const nodes: Node<WorkflowNode>[] = useMemo(() =>
    initialNodes.map((n) => ({
      id: n.id,
      type: "agent",
      position: { x: n.position_x, y: n.position_y },
      data: n,
    })),
    [initialNodes]
  );

  const edges: Edge[] = useMemo(() =>
    initialEdges.map((e) => ({
      id: e.id,
      type: "workflow",
      source: e.source_node_id,
      target: e.target_node_id,
    })),
    [initialEdges]
  );

  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState(nodes);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState(edges);

  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return;
      setFlowEdges((eds) => addEdge({ ...connection, type: "workflow" }, eds));
      onEdgeCreate?.(connection.source, connection.target);
    },
    [setFlowEdges, onEdgeCreate]
  );

  const onNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      onNodeUpdate?.(node.id, {
        position_x: node.position.x,
        position_y: node.position.y,
      });
    },
    [onNodeUpdate]
  );

  return (
    <div className="w-full h-full">
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        onNodesChange={(changes) => {
          onEdgesChange(changes);
          // 通知外部 nodes 位置变化
          onNodesChange?.(
            flowNodes.map((n) => ({
              ...n.data,
              position_x: n.position.x,
              position_y: n.position.y,
            }))
          );
        }}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeDragStop={onNodeDragStop}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={{ type: "workflow" }}
        fitView
        deleteKeyCode={["Backspace", "Delete"]}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
        <Controls />
        <MiniMap
          nodeColor={(n) => {
            const s = n.data?.status;
            if (s === "completed") return "#22c55e";
            if (s === "failed") return "#ef4444";
            if (s === "running") return "#3b82f6";
            return "#888";
          }}
          maskColor="rgba(0,0,0,0.1)"
        />
      </ReactFlow>
    </div>
  );
}
```

---

### Task 10: Plan 页面框架

**Files:**
- Create: `packages/views/workflows/index.ts`
- Create: `packages/views/workflows/canvas/index.ts`
- Create: `packages/views/workflows/plan/index.ts`
- Create: `packages/views/workflows/plan/plan-page.tsx`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/plans/page.tsx`

**Step 1: Plan 列表页**

`packages/views/workflows/plan/plan-list-page.tsx`:
```tsx
"use client";

import { useQuery } from "@tanstack/react-query";
import { planApi } from "@multica/core/api/workflows";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Card } from "@multica/ui/components/ui/card";

export function PlanListPage() {
  const wsId = useWorkspaceId()!;

  const { data: plans, isLoading } = useQuery({
    queryKey: ["plans", wsId],
    queryFn: () => planApi.list(wsId),
  });

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-semibold">Plans</h1>
        <Button onClick={() => { /* 跳转 /plans/new */ }}>
          New Plan
        </Button>
      </div>
      {isLoading ? (
        <div>Loading...</div>
      ) : plans?.length === 0 ? (
        <div className="text-muted-foreground">No plans yet.</div>
      ) : (
        <div className="grid gap-4">
          {plans?.map((plan) => (
            <Card key={plan.id} className="p-4">
              <h2 className="font-medium">{plan.title}</h2>
              <p className="text-sm text-muted-foreground mt-1">
                {plan.status} — {new Date(plan.created_at).toLocaleDateString()}
              </p>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
```

**Step 2: 导出**

`packages/views/workflows/index.ts`:
```typescript
export { WorkflowCanvas } from "./canvas/workflow-canvas";
export { AgentNodeComponent } from "./canvas/agent-node";
export { PlanListPage } from "./plan/plan-list-page";
```

**Step 3: Next.js 路由**

`apps/web/app/[workspaceSlug]/(dashboard)/plans/page.tsx`:
```tsx
"use client";
import { PlanListPage } from "@multica/views/workflows";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <PlanListPage />
    </ErrorBoundary>
  );
}
```

---

### Task 11: Plan 详情页（Canvas 集成）

**Files:**
- Create: `packages/views/workflows/plan/plan-detail-page.tsx`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/plans/[id]/page.tsx`

**Step 1: Plan 详情页**

`packages/views/workflows/plan/plan-detail-page.tsx`:
```tsx
"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { planApi, workflowApi } from "@multica/core/api/workflows";
import { WorkflowCanvas } from "../canvas/workflow-canvas";
import { Button } from "@multica/ui/components/ui/button";
import { useState } from "react";

interface PlanDetailPageProps {
  planId: string;
}

export function PlanDetailPage({ planId }: PlanDetailPageProps) {
  const qc = useQueryClient();

  const { data: plan, isLoading } = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => planApi.get(planId),
  });

  const workflowId = plan?.workflow_id;
  const { data: nodes = [] } = useQuery({
    queryKey: ["workflow-nodes", workflowId],
    queryFn: () => workflowApi.listNodes(workflowId!),
    enabled: !!workflowId,
  });
  const { data: edges = [] } = useQuery({
    queryKey: ["workflow-edges", workflowId],
    queryFn: () => workflowApi.listEdges(workflowId!),
    enabled: !!workflowId,
  });

  const confirmMutation = useMutation({
    mutationFn: () => workflowApi.confirm(workflowId!),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] });
    },
  });

  const updateNodeMutation = useMutation({
    mutationFn: ({ nodeId, data }: { nodeId: string; data: any }) =>
      workflowApi.updateNode(workflowId!, nodeId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] });
    },
  });

  const createEdgeMutation = useMutation({
    mutationFn: ({ sourceId, targetId }: { sourceId: string; targetId: string }) =>
      workflowApi.createEdge(workflowId!, { source_node_id: sourceId, target_node_id: targetId }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-edges", workflowId] });
    },
  });

  if (isLoading) return <div className="p-6">Loading...</div>;
  if (!plan || !workflowId) return <div className="p-6">Plan not found</div>;

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b">
        <div>
          <h1 className="text-lg font-semibold">{plan.title}</h1>
          <span className="text-sm text-muted-foreground capitalize">{plan.status}</span>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => {
            workflowApi.createNode(workflowId, {
              agent_id: "", // TODO: 从 Agent 列表选择
              title: "New Node",
              prompt: "",
              position_x: 100 + Math.random() * 200,
              position_y: 100 + Math.random() * 200,
            }).then(() => qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] }));
          }}>
            + Add Node
          </Button>
          {plan.status === "draft" && (
            <Button onClick={() => confirmMutation.mutate()}>
              Confirm & Run
            </Button>
          )}
        </div>
      </div>

      {/* Canvas */}
      <div className="flex-1 min-h-0">
        <WorkflowCanvas
          workflowId={workflowId}
          initialNodes={nodes}
          initialEdges={edges}
          onNodeUpdate={(nodeId, data) =>
            updateNodeMutation.mutate({ nodeId, data })
          }
          onEdgeCreate={(sourceId, targetId) =>
            createEdgeMutation.mutate({ sourceId, targetId })
          }
        />
      </div>
    </div>
  );
}
```

**Step 2: Next.js 路由**

`apps/web/app/[workspaceSlug]/(dashboard)/plans/[id]/page.tsx`:
```tsx
"use client";
import { PlanDetailPage } from "@multica/views/workflows";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page({ params }: { params: { id: string } }) {
  return (
    <ErrorBoundary>
      <PlanDetailPage planId={params.id} />
    </ErrorBoundary>
  );
}
```

---

### Task 12: 侧边栏导航（Plans 入口）

**Files:**
- Modify: `packages/views/layout/app-sidebar.tsx` — 添加 Plans 链接

**Step 1: 找到导航配置位置**

Run: `grep -n "issues\|agents\|navigation" packages/views/layout/app-sidebar.tsx | head -20`
Expected: 显示导航项定义位置

**Step 2: 添加 Plans 导航项**

在 Issues 和 Agents 之间（或合适位置）添加：
```tsx
{
  label: "Plans",
  href: `/${workspaceSlug}/plans`,
  icon: <WorkflowIcon className="h-4 w-4" />,
},
```

---

### Task 13: 运行验证

**Step 1: 数据库确认**

Run: `make db-up && make migrate-up`
Expected: Migration 112 applied

**Step 2: 后端编译**

Run: `cd server && go build ./cmd/server/`
Expected: 无错误

**Step 3: 前端类型检查**

Run: `pnpm typecheck`
Expected: 无 TS 错误

**Step 4: 运行开发服务器**

Run: `pnpm dev:web`
Expected: http://localhost:3000 可访问

**Step 5: E2E 手动验证**

1. 登录 workspace
2. 点击侧边栏 Plans
3. 点击 New Plan → 输入标题 → 创建
4. 进入 Plan 详情页
5. 点击 + Add Node → 选择 Agent → 输入 prompt → 保存
6. 重复步骤 5 创建第二个节点
7. 从节点右侧锚点拖到另一节点 → 创建边
8. 点击 Confirm & Run → 观察节点状态变化

---

## Phase 2 实现任务（待 Phase 1 验收后执行）

### Task 14: AI 生成 DAG
- `POST /api/plans/:id/generate` handler
- 服务端 LLM 调用生成 nodes[] + edges[]
- 自动布局算法（Dagre 或简单层布局）

### Task 15: 拓扑分发算法完善
- `OnTaskCompleted` 回调触发 `dispatchReadyNodes`
- WebSocket 事件：`workflow:node_updated`, `workflow:status_changed`
- 循环检测实现

### Task 16: 节点完成时边的脉冲动画
- React Flow `useNodes` + CSS animation
- 节点 completed → 沿边发送脉冲到下游节点

### Task 17: Context 用量展示
- 从 `AgentTask.usage.context_tokens` 读取
- 实时更新（WebSocket task:progress 事件驱动）

---

## Phase 3 实现任务（待 Phase 2 验收后执行）

### Task 18: 从 Agent 列表拖入画布添加节点
- React Flow `onDragOver` + `onDrop`
- Agent 列表作为侧边栏可拖拽区域

### Task 19: 撤销/重做历史
- 基于操作栈的简单 undo/redo
- Zustand history middleware

### Task 20: MiniMap + 节点搜索
- React Flow MiniMap 已集成（Task 9）
- 添加节点搜索/高亮

---

## 已知待完成项（Phase 1 结束后补充）

这些部分依赖 Phase 1 完成后的中间产物：

1. **Service 层的 createTaskForNode**（Task 5）：需要 TaskService.CreateTask 方法，等 TaskService 上下文清晰后实现
2. **循环检测**（Task 5）：Phase 2 实现 DFS 检测
3. **planApi.list** 路由**（Task 8）**：Plan 列表是 `/workspaces/{wsId}/plans`，需要先在 router.go 中注册对应的 workspace-scoped 路由
4. **Agent 列表查询**：节点创建需要可用的 Agent 列表（复用现有 `/api/agents`）

---

*计划版本：v0.1 | 基于 docs/plans/2026-07-01-dag-canvas-design.md，2026-07-01*
