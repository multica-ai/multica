package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Request types ────────────────────────────────────────────────────────────

type StartRunRequest struct {
	Input json.RawMessage `json:"input"`
}

type SubmitNodeRunRequest struct {
	Output json.RawMessage `json:"output"`
}

type ReviewNodeRunRequest struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

type FinalizeNodeRunRequest struct {
	Approved *bool `json:"approved,omitempty"`
}

// ── Response types ───────────────────────────────────────────────────────────

type WorkflowRunResponse struct {
	ID              string          `json:"id"`
	WorkflowID      string          `json:"workflow_id"`
	WorkspaceID     string          `json:"workspace_id"`
	WorkflowTitle   string          `json:"workflow_title"`
	Status          string          `json:"status"`
	TriggeredByType string          `json:"triggered_by_type"`
	TriggeredByID   *string         `json:"triggered_by_id"`
	Input           json.RawMessage `json:"input"`
	Output          json.RawMessage `json:"output"`
	StartedAt       string          `json:"started_at"`
	CompletedAt     *string         `json:"completed_at"`
	CreatedAt       string          `json:"created_at"`
}

type WorkflowNodeRunResponse struct {
	ID              string          `json:"id"`
	WorkflowRunID   string          `json:"workflow_run_id"`
	WorkflowNodeID  string          `json:"workflow_node_id"`
	NodeTitle       string          `json:"node_title"`
	Status          string          `json:"status"`
	RetryCount      int32           `json:"retry_count"`
	WorkerType      string          `json:"worker_type"`
	WorkerID        *string         `json:"worker_id"`
	WorkerOutput    json.RawMessage `json:"worker_output"`
	CriticType      string          `json:"critic_type"`
	CriticID        *string         `json:"critic_id"`
	CriticOutput    json.RawMessage `json:"critic_output"`
	CriticComment   string          `json:"critic_comment"`
	AgentTaskID     *string         `json:"agent_task_id"`
	StartedAt       *string         `json:"started_at"`
	CompletedAt     *string         `json:"completed_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// ── Converters ───────────────────────────────────────────────────────────────

func workflowRunToResponse(r db.MulticaWorkflowRun) WorkflowRunResponse {
	return WorkflowRunResponse{
		ID:              uuidToString(r.ID),
		WorkflowID:      uuidToString(r.WorkflowID),
		WorkspaceID:     uuidToString(r.WorkspaceID),
		WorkflowTitle:   r.WorkflowTitle,
		Status:          r.Status,
		TriggeredByType: r.TriggeredByType,
		TriggeredByID:   uuidToPtr(r.TriggeredByID),
		Input:           json.RawMessage(r.Input),
		Output:          json.RawMessage(r.Output),
		StartedAt:       timestampToString(r.StartedAt),
		CompletedAt:     timestampToPtr(r.CompletedAt),
		CreatedAt:       timestampToString(r.CreatedAt),
	}
}

func workflowNodeRunToResponse(nr db.MulticaWorkflowNodeRun) WorkflowNodeRunResponse {
	return WorkflowNodeRunResponse{
		ID:             uuidToString(nr.ID),
		WorkflowRunID:  uuidToString(nr.WorkflowRunID),
		WorkflowNodeID: uuidToString(nr.WorkflowNodeID),
		NodeTitle:      nr.NodeTitle,
		Status:         nr.Status,
		RetryCount:     nr.RetryCount,
		WorkerType:     nr.WorkerType,
		WorkerID:       uuidToPtr(nr.WorkerID),
		WorkerOutput:   json.RawMessage(nr.WorkerOutput),
		CriticType:     nr.CriticType,
		CriticID:       uuidToPtr(nr.CriticID),
		CriticOutput:   json.RawMessage(nr.CriticOutput),
		CriticComment:  nr.CriticComment.String,
		AgentTaskID:    uuidToPtr(nr.AgentTaskID),
		StartedAt:      timestampToPtr(nr.StartedAt),
		CompletedAt:    timestampToPtr(nr.CompletedAt),
		CreatedAt:      timestampToString(nr.CreatedAt),
		UpdatedAt:      timestampToString(nr.UpdatedAt),
	}
}

// ── Run handlers ─────────────────────────────────────────────────────────────

func (h *Handler) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	limit, offset := paginationFromQuery(r)

	runs, err := h.Queries.ListWorkflowRuns(r.Context(), db.ListWorkflowRunsParams{
		WorkflowID: wf.ID,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	resp := make([]WorkflowRunResponse, len(runs))
	for i, run := range runs {
		resp[i] = workflowRunToResponse(run)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": resp, "total": len(resp)})
}

func (h *Handler) StartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	if wf.Status != "active" {
		writeError(w, http.StatusBadRequest, "workflow is not active")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req StartRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Empty body OK — use default input.
		req.Input = json.RawMessage("{}")
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage("{}")
	}

	// Validate DAG before starting.
	if err := h.WorkflowService.ValidateDAG(r.Context(), wf.ID); err != nil {
		writeError(w, http.StatusBadRequest, "workflow has cycles: "+err.Error())
		return
	}

	run, err := h.WorkflowService.StartRun(r.Context(), wf, "member", userID, req.Input, pgtype.UUID{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start run: "+err.Error())
		return
	}

	resp := workflowRunToResponse(*run)
	h.publish(protocol.EventWorkflowRunStarted, workspaceID, "member", userID, map[string]any{
		"run":      resp,
		"workflow": map[string]string{"id": uuidToString(wf.ID), "title": wf.Title},
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Load workflow to verify workspace access.
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run id")
	if !ok {
		return
	}

	run, err := h.Queries.GetWorkflowRun(r.Context(), runUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if uuidToString(run.WorkflowID) != uuidToString(wf.ID) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	resp := workflowRunToResponse(run)

	// Include node runs.
	nodeRuns, err := h.Queries.ListWorkflowNodeRunsByRun(r.Context(), run.ID)
	if err != nil {
		nodeRuns = nil
	}
	nodeRunResp := make([]WorkflowNodeRunResponse, len(nodeRuns))
	for i, nr := range nodeRuns {
		nodeRunResp[i] = workflowNodeRunToResponse(db.MulticaWorkflowNodeRun{
		ID:             nr.ID,
		WorkflowRunID:  nr.WorkflowRunID,
		WorkflowNodeID: nr.WorkflowNodeID,
		NodeTitle:      nr.NodeTitle,
		Status:         nr.Status,
		RetryCount:     nr.RetryCount,
		WorkerType:     nr.WorkerType,
		WorkerID:       nr.WorkerID,
		WorkerOutput:   nr.WorkerOutput,
		CriticType:     nr.CriticType,
		CriticID:       nr.CriticID,
		CriticOutput:   nr.CriticOutput,
		CriticComment:  nr.CriticComment,
		AgentTaskID:    nr.AgentTaskID,
		StartedAt:      nr.StartedAt,
		CompletedAt:    nr.CompletedAt,
		CreatedAt:      nr.CreatedAt,
		UpdatedAt:      nr.UpdatedAt,
	})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"run":       resp,
		"node_runs": nodeRunResp,
	})
}

func (h *Handler) CancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run id")
	if !ok {
		return
	}

	run, err := h.Queries.GetWorkflowRun(r.Context(), runUUID)
	if err != nil || uuidToString(run.WorkflowID) != uuidToString(wf.ID) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	if err := h.WorkflowService.CancelRun(r.Context(), runUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel run")
		return
	}

	h.publish(protocol.EventWorkflowRunCancelled, workspaceID, "member", userID, map[string]any{
		"run_id":      uuidToString(runUUID),
		"workflow_id": uuidToString(wf.ID),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ── Node run actions ─────────────────────────────────────────────────────────

func (h *Handler) SubmitNodeRun(w http.ResponseWriter, r *http.Request) {
	nodeRunID := chi.URLParam(r, "nodeRunId")
	nodeRunUUID, ok := parseUUIDOrBadRequest(w, nodeRunID, "node run id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req SubmitNodeRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Output) == 0 {
		writeError(w, http.StatusBadRequest, "output is required")
		return
	}

	// Verify workspace access: fetch node run, resolve to workflow, check workspace.
	nodeRun, err := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}
	run, err := h.Queries.GetWorkflowRun(r.Context(), nodeRun.WorkflowRunID)
	if err != nil || uuidToString(run.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}

	if err := h.WorkflowService.SubmitWorkerOutput(r.Context(), nodeRunUUID, req.Output); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, _ := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	resp := workflowNodeRunToResponse(updated)
	h.publish(protocol.EventWorkflowNodeRunCompleted, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ReviewNodeRun(w http.ResponseWriter, r *http.Request) {
	nodeRunID := chi.URLParam(r, "nodeRunId")
	nodeRunUUID, ok := parseUUIDOrBadRequest(w, nodeRunID, "node run id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req ReviewNodeRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify workspace access.
	nodeRun, err := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}
	run, err := h.Queries.GetWorkflowRun(r.Context(), nodeRun.WorkflowRunID)
	if err != nil || uuidToString(run.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}

	if err := h.WorkflowService.ReviewNodeRun(r.Context(), nodeRunUUID, req.Approved, req.Comment, nil); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, _ := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	resp := workflowNodeRunToResponse(updated)

	eventType := protocol.EventWorkflowNodeRunReviewed
	if updated.Status == service.NodeRunStatusBlocked {
		eventType = protocol.EventWorkflowNodeRunBlocked
	}
	h.publish(eventType, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SkipNodeRun(w http.ResponseWriter, r *http.Request) {
	nodeRunID := chi.URLParam(r, "nodeRunId")
	nodeRunUUID, ok := parseUUIDOrBadRequest(w, nodeRunID, "node run id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify workspace access.
	nodeRun, err := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}
	run, err := h.Queries.GetWorkflowRun(r.Context(), nodeRun.WorkflowRunID)
	if err != nil || uuidToString(run.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "node run not found")
		return
	}

	skipped, err := h.WorkflowService.TransitionNodeRun(r.Context(), nodeRun, service.NodeRunStatusSkipped)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := workflowNodeRunToResponse(*skipped)
	h.publish(protocol.EventWorkflowNodeRunCompleted, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})

	// Trigger downstream propagation.
	if err := h.WorkflowService.OnNodeRunCompleted(r.Context(), nodeRunUUID); err != nil {
		// Non-fatal: the skip already persisted.
	}

	writeJSON(w, http.StatusOK, resp)
}

// loadNodeRunForWorkspace resolves a node-run URL param and verifies the caller
// can access its workspace, returning the node run and its parent run. On any
// failure it writes the response and returns ok=false.
func (h *Handler) loadNodeRunForWorkspace(w http.ResponseWriter, r *http.Request) (db.MulticaWorkflowNodeRun, db.MulticaWorkflowRun, string, bool) {
	nodeRunID := chi.URLParam(r, "nodeRunId")
	nodeRunUUID, ok := parseUUIDOrBadRequest(w, nodeRunID, "node run id")
	if !ok {
		return db.MulticaWorkflowNodeRun{}, db.MulticaWorkflowRun{}, "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	nodeRun, err := h.Queries.GetWorkflowNodeRun(r.Context(), nodeRunUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node run not found")
		return db.MulticaWorkflowNodeRun{}, db.MulticaWorkflowRun{}, "", false
	}
	run, err := h.Queries.GetWorkflowRun(r.Context(), nodeRun.WorkflowRunID)
	if err != nil || uuidToString(run.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "node run not found")
		return db.MulticaWorkflowNodeRun{}, db.MulticaWorkflowRun{}, "", false
	}
	return nodeRun, run, workspaceID, true
}

// TakeoverNodeRun pauses a running node so a human can intervene in its CSC
// session (working → blocked). Node-level control only — the CSC session
// actions (message/interrupt/permission) flow through Cloud Web, not here.
//
// NOTE: runtime-level takeover permission (beyond workspace membership) is
// deferred (task L1.4 / plan module A5); for now workspace access gates this.
func (h *Handler) TakeoverNodeRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	nodeRun, run, workspaceID, ok := h.loadNodeRunForWorkspace(w, r)
	if !ok {
		return
	}

	updated, err := h.WorkflowService.TakeoverNodeRun(r.Context(), nodeRun)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := workflowNodeRunToResponse(*updated)
	h.publish(protocol.EventWorkflowNodeRunBlocked, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

// HandbackNodeRun returns control to the agent (blocked → working) so the
// daemon resumes the same CSC session.
func (h *Handler) HandbackNodeRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	nodeRun, run, workspaceID, ok := h.loadNodeRunForWorkspace(w, r)
	if !ok {
		return
	}

	updated, err := h.WorkflowService.HandbackNodeRun(r.Context(), nodeRun)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := workflowNodeRunToResponse(*updated)
	h.publish(protocol.EventWorkflowNodeRunResumed, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

// FinalizeNodeRun lets a human conclude a taken-over node directly
// (blocked → completed / failed) instead of handing it back.
func (h *Handler) FinalizeNodeRun(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req FinalizeNodeRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outcome := service.NodeRunStatusCompleted
	eventType := protocol.EventWorkflowNodeRunCompleted
	if req.Approved != nil && !*req.Approved {
		outcome = service.NodeRunStatusFailed
		eventType = protocol.EventWorkflowNodeRunFailed
	}

	nodeRun, run, workspaceID, ok := h.loadNodeRunForWorkspace(w, r)
	if !ok {
		return
	}

	updated, err := h.WorkflowService.FinalizeNodeRun(r.Context(), nodeRun, outcome)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := workflowNodeRunToResponse(*updated)
	h.publish(eventType, workspaceID, "member", userID, map[string]any{
		"node_run": resp,
		"run_id":   uuidToString(run.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

// ── My tasks ─────────────────────────────────────────────────────────────────

func (h *Handler) ListMyWorkflowTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	userUUID := parseUUID(userID)
	wsUUID := parseUUID(workspaceID)

	// Query node runs where the current user is the human worker or critic.
	// This lists node_runs in awaiting_critic or worker_assigned status
	// where the worker_type/critic_type is "human" and the worker_id is
	// either NULL (any member) or matches this user.
	nodeRuns, err := h.Queries.ListMyWorkflowTasks(r.Context(), db.ListMyWorkflowTasksParams{
		WorkspaceID: wsUUID,
		MemberID:    userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	resp := make([]WorkflowNodeRunResponse, len(nodeRuns))
	for i, nr := range nodeRuns {
		resp[i] = workflowNodeRunToResponse(db.MulticaWorkflowNodeRun{
		ID:             nr.ID,
		WorkflowRunID:  nr.WorkflowRunID,
		WorkflowNodeID: nr.WorkflowNodeID,
		NodeTitle:      nr.NodeTitle,
		Status:         nr.Status,
		RetryCount:     nr.RetryCount,
		WorkerType:     nr.WorkerType,
		WorkerID:       nr.WorkerID,
		WorkerOutput:   nr.WorkerOutput,
		CriticType:     nr.CriticType,
		CriticID:       nr.CriticID,
		CriticOutput:   nr.CriticOutput,
		CriticComment:  nr.CriticComment,
		AgentTaskID:    nr.AgentTaskID,
		StartedAt:      nr.StartedAt,
		CompletedAt:    nr.CompletedAt,
		CreatedAt:      nr.CreatedAt,
		UpdatedAt:      nr.UpdatedAt,
	})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp, "total": len(resp)})
}

func paginationFromQuery(r *http.Request) (int32, int32) { return 50, 0 }

func (h *Handler) ListWorkflowNodeRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok { return }
	runID := chi.URLParam(r, "runId")
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "run id")
	if !ok { return }
	run, err := h.Queries.GetWorkflowRun(r.Context(), runUUID)
	if err != nil { writeError(w, http.StatusNotFound, "run not found"); return }
	if uuidToString(run.WorkflowID) != uuidToString(wf.ID) { writeError(w, http.StatusNotFound, "run not found"); return }
	nodeRuns, err := h.Queries.ListWorkflowNodeRunsByRun(r.Context(), run.ID)
	if err != nil { nodeRuns = nil }
	resp := make([]WorkflowNodeRunResponse, 0, len(nodeRuns))
	for _, nr := range nodeRuns {
		resp = append(resp, workflowNodeRunToResponse(nr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_runs": resp})
}
