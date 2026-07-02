package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
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
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	wf, err := h.WorkflowSvc.Get(r.Context(), wid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// PATCH /api/workflows/{workflowId}
func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
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
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// GET /api/workflows/{workflowId}/nodes
func (h *Handler) ListWorkflowNodes(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	nodes, err := h.WorkflowSvc.ListNodes(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// POST /api/workflows/{workflowId}/nodes
func (h *Handler) CreateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	var body CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, body.AgentID, "agent_id")
	if !ok {
		return
	}
	node, err := h.WorkflowSvc.CreateNode(r.Context(), wid, agentID, body.Title, body.Prompt, body.PositionX, body.PositionY)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

// PATCH /api/workflows/{workflowId}/nodes/{nodeId}
func (h *Handler) UpdateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := chi.URLParam(r, "nodeId")
	nodeID, ok := parseUUIDOrBadRequest(w, nodeIDStr, "nodeId")
	if !ok {
		return
	}
	var body UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	input := &service.UpdateNodeInput{
		Title:     body.Title,
		Prompt:    body.Prompt,
		AgentID:   body.AgentID,
		PositionX: body.PositionX,
		PositionY: body.PositionY,
		Status:    body.Status,
		TaskID:    body.TaskID,
	}
	node, err := h.WorkflowSvc.UpdateNode(r.Context(), nodeID, input)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// DELETE /api/workflows/{workflowId}/nodes/{nodeId}
func (h *Handler) DeleteWorkflowNode(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := chi.URLParam(r, "nodeId")
	nodeID, ok := parseUUIDOrBadRequest(w, nodeIDStr, "nodeId")
	if !ok {
		return
	}
	if err := h.WorkflowSvc.DeleteNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/workflows/{workflowId}/edges
func (h *Handler) ListWorkflowEdges(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	edges, err := h.WorkflowSvc.ListEdges(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edges)
}

// POST /api/workflows/{workflowId}/edges
func (h *Handler) CreateWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	var body CreateEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, body.SourceNodeID, "source_node_id")
	if !ok {
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, body.TargetNodeID, "target_node_id")
	if !ok {
		return
	}
	edge, err := h.WorkflowSvc.CreateEdge(r.Context(), wid, sourceID, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, edge)
}

// DELETE /api/workflows/{workflowId}/edges/{edgeId}
func (h *Handler) DeleteWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	edgeIDStr := chi.URLParam(r, "edgeId")
	edgeID, ok := parseUUIDOrBadRequest(w, edgeIDStr, "edgeId")
	if !ok {
		return
	}
	if err := h.WorkflowSvc.DeleteEdge(r.Context(), edgeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "edge not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/workflows/{workflowId}/confirm
func (h *Handler) ConfirmWorkflow(w http.ResponseWriter, r *http.Request) {
	widStr := chi.URLParam(r, "workflowId")
	wid, ok := parseUUIDOrBadRequest(w, widStr, "workflowId")
	if !ok {
		return
	}
	result, err := h.WorkflowSvc.Confirm(r.Context(), wid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workflow not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
