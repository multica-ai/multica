package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/internal/util"
)

// ── Request types ────────────────────────────────────────────────────────────

type CreateWorkflowRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TemplateID  string `json:"template_id"` // UUID of template to clone from
}

type UpdateWorkflowRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	MaxRetries  *int32  `json:"max_retries"`
}

type CreateNodeRequest struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	PositionX          float64         `json:"position_x"`
	PositionY          float64         `json:"position_y"`
	FormatSchema       json.RawMessage `json:"format_schema"`
	WorkerType         string          `json:"worker_type"`
	WorkerID           *string         `json:"worker_id"`
	CriticType         string          `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticApiURL       *string         `json:"critic_api_url"`
}

type UpdateNodeRequest struct {
	Title              *string         `json:"title"`
	Description        *string         `json:"description"`
	PositionX          *float64        `json:"position_x"`
	PositionY          *float64        `json:"position_y"`
	FormatSchema       json.RawMessage `json:"format_schema"`
	WorkerType         *string         `json:"worker_type"`
	WorkerID           *string         `json:"worker_id"`
	CriticType         *string         `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticApiURL       *string         `json:"critic_api_url"`
	SortOrder          *int32          `json:"sort_order"`
}

type CreateEdgeRequest struct {
	SourceNodeID string          `json:"source_node_id"`
	TargetNodeID string          `json:"target_node_id"`
	Condition    json.RawMessage `json:"condition"`
}

// ── Response types ───────────────────────────────────────────────────────────

type WorkflowResponse struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Status           string `json:"status"`
	MaxRetries       int32  `json:"max_retries"`
	CreatedByType    string `json:"created_by_type"`
	CreatedByID      string `json:"created_by_id"`
	NodeCount        int64  `json:"node_count"`
	IsTemplate       bool   `json:"is_template"`
	SourceTemplateID string `json:"source_template_id"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type WorkflowNodeResponse struct {
	ID                 string          `json:"id"`
	WorkflowID         string          `json:"workflow_id"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	PositionX          float64         `json:"position_x"`
	PositionY          float64         `json:"position_y"`
	FormatSchema       json.RawMessage `json:"format_schema"`
	WorkerType         string          `json:"worker_type"`
	WorkerID           *string         `json:"worker_id"`
	CriticType         string          `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticApiURL       *string         `json:"critic_api_url"`
	SortOrder          int32           `json:"sort_order"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
}

type WorkflowEdgeResponse struct {
	ID           string          `json:"id"`
	WorkflowID   string          `json:"workflow_id"`
	SourceNodeID string          `json:"source_node_id"`
	TargetNodeID string          `json:"target_node_id"`
	Condition    json.RawMessage `json:"condition"`
	CreatedAt    string          `json:"created_at"`
}

type ToggleTemplateRequest struct {
	IsTemplate bool `json:"is_template"`
}

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

type UpdateWorkflowAdminsRequest struct {
	UserIDs []string `json:"user_ids"`
}

type WorkflowAdminResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Email              string `json:"email"`
	CanManageWorkflows bool   `json:"can_manage_workflows"`
}

// ── Converters ───────────────────────────────────────────────────────────────

func workflowToResponse(wf db.MulticaWorkflow, nodeCount int64) WorkflowResponse {
	return WorkflowResponse{
		ID:               uuidToString(wf.ID),
		WorkspaceID:      uuidToString(wf.WorkspaceID),
		Title:            wf.Title,
		Description:      wf.Description,
		Status:           wf.Status,
		MaxRetries:       wf.MaxRetries,
		CreatedByType:    wf.CreatedByType,
		CreatedByID:      uuidToString(wf.CreatedByID),
		NodeCount:        nodeCount,
		IsTemplate:       wf.IsTemplate,
		SourceTemplateID: uuidToString(wf.SourceTemplateID),
		CreatedAt:        timestampToString(wf.CreatedAt),
		UpdatedAt:        timestampToString(wf.UpdatedAt),
	}
}

func workflowNodeToResponse(node db.MulticaWorkflowNode) WorkflowNodeResponse {
	return WorkflowNodeResponse{
		ID:                 uuidToString(node.ID),
		WorkflowID:         uuidToString(node.WorkflowID),
		Title:              node.Title,
		Description:        node.Description,
		PositionX:          node.PositionX,
		PositionY:          node.PositionY,
		FormatSchema:       node.FormatSchema,
		WorkerType:         node.WorkerType,
		WorkerID:           uuidToPtr(node.WorkerID),
		CriticType:         node.CriticType,
		CriticID:           uuidToPtr(node.CriticID),
		CriticApiURL:       textToPtr(node.CriticApiUrl),
		SortOrder:          node.SortOrder,
		CreatedAt:          timestampToString(node.CreatedAt),
		UpdatedAt:          timestampToString(node.UpdatedAt),
	}
}

func workflowEdgeToResponse(edge db.MulticaWorkflowEdge) WorkflowEdgeResponse {
	return WorkflowEdgeResponse{
		ID:           uuidToString(edge.ID),
		WorkflowID:   uuidToString(edge.WorkflowID),
		SourceNodeID: uuidToString(edge.SourceNodeID),
		TargetNodeID: uuidToString(edge.TargetNodeID),
		Condition:    edge.Condition,
		CreatedAt:    timestampToString(edge.CreatedAt),
	}
}

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

// ── Loader ───────────────────────────────────────────────────────────────────

func (h *Handler) loadWorkflowInWorkspace(w http.ResponseWriter, r *http.Request, id string) (db.MulticaWorkflow, bool) {
	wfID, ok := parseUUIDOrBadRequest(w, id, "workflow ID")
	if !ok {
		return db.MulticaWorkflow{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)

	// Try workspace-scoped lookup first.
	wf, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID:          wfID,
		WorkspaceID: wsUUID,
	})
	if err == nil {
		return wf, true
	}

	// Fallback: global lookup — allow cross-workspace access to templates.
	wf, err = h.Queries.GetWorkflow(r.Context(), wfID)
	if err == nil && wf.IsTemplate {
		return wf, true
	}

	writeError(w, http.StatusNotFound, "workflow not found")
	return db.MulticaWorkflow{}, false
}

// ── Workflow CRUD ────────────────────────────────────────────────────────────

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)

	templateFilter := r.URL.Query().Get("template")

	var workflows []db.MulticaWorkflow
	var err error

	switch templateFilter {
	case "true":
		workflows, err = h.Queries.ListTemplates(r.Context())
	case "false":
		workflows, err = h.Queries.ListWorkflowsExcludingTemplates(r.Context(), db.ListWorkflowsExcludingTemplatesParams{
			WorkspaceID: wsUUID,
			Limit:       100,
			Offset:      0,
		})
	default:
		workflows, err = h.Queries.ListWorkflows(r.Context(), db.ListWorkflowsParams{
			WorkspaceID: wsUUID,
			Limit:       100,
			Offset:      0,
		})
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}

	resp := make([]WorkflowResponse, 0, len(workflows))
	for _, wf := range workflows {
		count, _ := h.Queries.CountWorkflowNodes(r.Context(), wf.ID)
		resp = append(resp, workflowToResponse(wf, count))
	}

	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp})
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	wsUUID := parseUUID(workspaceID)
	creatorUUID := parseUUID(userID)

	// Handle template-based creation via template_id.
	if req.TemplateID != "" {
		tplID, ok := parseUUIDOrBadRequest(w, req.TemplateID, "template_id")
		if !ok {
			return
		}
		cloned, _, _, err := h.WorkflowService.CloneWorkflowFromTemplate(
			r.Context(), tplID, wsUUID, req.Title, req.Description,
			"member", creatorUUID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to clone template: %v", err))
			return
		}
		count, _ := h.Queries.CountWorkflowNodes(r.Context(), cloned.ID)
		resp := workflowToResponse(cloned, count)
		h.publish(protocol.EventWorkflowCreated, workspaceID, "member", userID, map[string]any{"workflow": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	wf, err := h.Queries.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID:   wsUUID,
		Title:         req.Title,
		Description:   nonNullText(req.Description),
		Status:        "draft",
		MaxRetries:    3,
		CreatedByType: "member",
		CreatedByID:   creatorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}

	resp := workflowToResponse(wf, 0)

	h.publish(protocol.EventWorkflowCreated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusCreated, resp)
}

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

func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	var req UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate all nodes have worker and critic assigned when activating.
	if req.Status != nil && *req.Status == "active" {
		nodes, err := h.Queries.ListWorkflowNodes(r.Context(), wf.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list nodes")
			return
		}
		var nodeNames []string
		for _, n := range nodes {
			var missingRoles []string
			if n.WorkerType == "" || (!n.WorkerID.Valid && n.WorkerType == "agent") {
				missingRoles = append(missingRoles, "worker")
			}
			if n.CriticType == "" || (!n.CriticID.Valid && n.CriticType == "agent") {
				missingRoles = append(missingRoles, "critic")
			}
			if len(missingRoles) > 0 {
				nodeNames = append(nodeNames, fmt.Sprintf("%s (%s)", n.Title, strings.Join(missingRoles, ", ")))
			}
		}
		if len(nodeNames) > 0 {
			writeError(w, http.StatusBadRequest, "These nodes need assignees: "+strings.Join(nodeNames, ", "))
			return
		}
	}

	params := db.UpdateWorkflowParams{
		ID:          wf.ID,
		Title:       ptrToText(req.Title),
		Description: ptrToText(req.Description),
		Status:      ptrToText(req.Status),
	}
	if req.MaxRetries != nil {
		params.MaxRetries = pgtype.Int4{Int32: *req.MaxRetries, Valid: true}
	}

	updated, err := h.Queries.UpdateWorkflow(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}

	count, _ := h.Queries.CountWorkflowNodes(r.Context(), updated.ID)
	resp := workflowToResponse(updated, count)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	// If this is a template, check for derived workflows.
	if wf.IsTemplate {
		count, err := h.Queries.CountWorkflowsBySourceTemplate(r.Context(), wf.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check template usage")
			return
		}
		if count > 0 {
			writeError(w, http.StatusConflict, fmt.Sprintf("template has %d derived workflows, cannot delete", count))
			return
		}
	}

	if err := h.Queries.DeleteWorkflow(r.Context(), wf.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}

	h.publish(protocol.EventWorkflowDeleted, workspaceID, "member", userID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ── Node CRUD ────────────────────────────────────────────────────────────────

func (h *Handler) ListWorkflowNodes(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	nodes, err := h.Queries.ListWorkflowNodes(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}

	resp := make([]WorkflowNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		resp = append(resp, workflowNodeToResponse(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": resp})
}

func (h *Handler) CreateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	var req CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.WorkerType == "" {
		req.WorkerType = "agent"
	}
	if req.CriticType == "" {
		req.CriticType = "human"
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	var workerID pgtype.UUID
	if req.WorkerID != nil {
		wID, ok := parseUUIDOrBadRequest(w, *req.WorkerID, "worker_id")
		if !ok {
			return
		}
		workerID = wID
	}
	var criticID pgtype.UUID
	if req.CriticID != nil {
		cID, ok := parseUUIDOrBadRequest(w, *req.CriticID, "critic_id")
		if !ok {
			return
		}
		criticID = cID
	}

	node, err := h.Queries.CreateWorkflowNode(r.Context(), db.CreateWorkflowNodeParams{
		WorkflowID:         wf.ID,
		Title:              req.Title,
		Description:        nonNullText(req.Description),
		PositionX:          req.PositionX,
		PositionY:          req.PositionY,
		FormatSchema:       req.FormatSchema,
		WorkerType:         req.WorkerType,
		WorkerID:           workerID,
		CriticType:         req.CriticType,
		CriticID:           criticID,
		CriticApiUrl:       nonNullText(stringOrEmpty(req.CriticApiURL)),
		SortOrder:          0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create node")
		return
	}

	resp := workflowNodeToResponse(node)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"node": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWorkflowNode(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")

	_, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}
	nID, ok := parseUUIDOrBadRequest(w, nodeID, "node ID")
	if !ok {
		return
	}

	var req UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	params := db.UpdateWorkflowNodeParams{
		ID:                 nID,
		Title:              ptrToText(req.Title),
		Description:        ptrToText(req.Description),
		PositionX:          float64ToFloat8(req.PositionX),
		PositionY:          float64ToFloat8(req.PositionY),
		FormatSchema:       req.FormatSchema,
		WorkerType:         ptrToText(req.WorkerType),
		WorkerID:           ptrStrToUUID(req.WorkerID),
		CriticType:         ptrToText(req.CriticType),
		CriticID:           ptrStrToUUID(req.CriticID),
		CriticApiUrl:       ptrToText(req.CriticApiURL),
		SortOrder:          int32ToInt4(req.SortOrder),
	}

	updated, err := h.Queries.UpdateWorkflowNode(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update node")
		return
	}

	resp := workflowNodeToResponse(updated)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"node": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteWorkflowNode(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")

	_, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}
	nID, ok := parseUUIDOrBadRequest(w, nodeID, "node ID")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	if err := h.Queries.DeleteWorkflowNode(r.Context(), nID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete node")
		return
	}

	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": nodeID})
}

// ── Edge CRUD ────────────────────────────────────────────────────────────────

func (h *Handler) ListWorkflowEdges(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	edges, err := h.Queries.ListWorkflowEdges(r.Context(), wf.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list edges")
		return
	}

	resp := make([]WorkflowEdgeResponse, 0, len(edges))
	for _, e := range edges {
		resp = append(resp, workflowEdgeToResponse(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"edges": resp})
}

func (h *Handler) CreateWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	var req CreateEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	srcID, ok := parseUUIDOrBadRequest(w, req.SourceNodeID, "source_node_id")
	if !ok {
		return
	}
	tgtID, ok := parseUUIDOrBadRequest(w, req.TargetNodeID, "target_node_id")
	if !ok {
		return
	}
	if req.SourceNodeID == req.TargetNodeID {
		writeError(w, http.StatusBadRequest, "source and target nodes must be different")
		return
	}

	// Validate same stage: nodes must belong to the same stage (or both unassigned)
	sourceNode, err := h.Queries.GetWorkflowNode(r.Context(), srcID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source node not found")
		return
	}
	targetNode, err := h.Queries.GetWorkflowNode(r.Context(), tgtID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "target node not found")
		return
	}
	sourceStageID := uuidToString(sourceNode.StageID)
	targetStageID := uuidToString(targetNode.StageID)
	if sourceStageID != targetStageID {
		writeError(w, http.StatusBadRequest, "nodes must belong to the same stage")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	edge, err := h.Queries.CreateWorkflowEdge(r.Context(), db.CreateWorkflowEdgeParams{
		WorkflowID:   wf.ID,
		SourceNodeID: srcID,
		TargetNodeID: tgtID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create edge")
		return
	}

	resp := workflowEdgeToResponse(edge)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"edge": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) DeleteWorkflowEdge(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	edgeID := chi.URLParam(r, "edgeId")

	_, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}
	eID, ok := parseUUIDOrBadRequest(w, edgeID, "edge ID")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)

	if err := h.Queries.DeleteWorkflowEdge(r.Context(), eID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete edge")
		return
	}

	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": edgeID})
}

// ── Stage Handlers ─────────────────────────────────────────────────────────────

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
		Description: nonNullText(req.Description),
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create stage")
		return
	}

	writeJSON(w, http.StatusCreated, workflowStageToResponse(stage, 0))
}

func (h *Handler) UpdateWorkflowStage(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	stageID := chi.URLParam(r, "stageId")
	stage, ok := h.loadWorkflowStage(w, r, stageID, wf.ID)
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
		Name:        ptrToText(req.Name),
		Description: ptrToText(req.Description),
		SortOrder:   int32ToInt4(req.SortOrder),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update stage")
		return
	}

	count, _ := h.Queries.CountWorkflowStageNodes(r.Context(), updated.ID)
	writeJSON(w, http.StatusOK, workflowStageToResponse(updated, count))
}

func (h *Handler) DeleteWorkflowStage(w http.ResponseWriter, r *http.Request) {
	wfID := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, wfID)
	if !ok {
		return
	}

	stageID := chi.URLParam(r, "stageId")
	stage, ok := h.loadWorkflowStage(w, r, stageID, wf.ID)
	if !ok {
		return
	}

	if err := h.Queries.DeleteWorkflowStage(r.Context(), stage.ID); err != nil {
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

	// Pre-validate all item IDs before applying any updates
	type validatedItem struct {
		ID        pgtype.UUID
		SortOrder int32
	}
	validated := make([]validatedItem, 0, len(items))
	for _, item := range items {
		id, ok := parseUUIDOrBadRequest(w, item.ID, "stage ID")
		if !ok {
			return
		}
		validated = append(validated, validatedItem{ID: id, SortOrder: item.SortOrder})
	}

	for _, item := range validated {
		_, err := h.Queries.UpdateWorkflowStage(r.Context(), db.UpdateWorkflowStageParams{
			ID:        item.ID,
			SortOrder: int32ToInt4(&item.SortOrder),
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
	stageID, ok := parseUUIDOrBadRequest(w, *req.StageID, "stage_id")
	if !ok {
		return
	}
	stage, err := h.Queries.GetWorkflowStage(r.Context(), stageID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "stage not found")
		return
	}
	if uuidToString(stage.WorkflowID) != uuidToString(node.WorkflowID) {
		writeError(w, http.StatusBadRequest, "stage does not belong to this workflow")
		return
	}

	updated, err := h.Queries.AssignNodeToStage(r.Context(), db.AssignNodeToStageParams{
		ID:      node.ID,
		StageID: stageID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to assign node to stage")
		return
	}
	writeJSON(w, http.StatusOK, workflowNodeToResponse(updated))
}

// ── Stage loader ──

func (h *Handler) loadWorkflowStage(w http.ResponseWriter, r *http.Request, stageID string, wfID pgtype.UUID) (db.MulticaWorkflowStage, bool) {
	id, ok := parseUUIDOrBadRequest(w, stageID, "stageId")
	if !ok {
		return db.MulticaWorkflowStage{}, false
	}

	stage, err := h.Queries.GetWorkflowStage(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "stage not found")
		return db.MulticaWorkflowStage{}, false
	}

	// Verify the stage belongs to the specified workflow
	if wfID != (pgtype.UUID{}) && stage.WorkflowID != wfID {
		writeError(w, http.StatusNotFound, "stage not found in this workflow")
		return db.MulticaWorkflowStage{}, false
	}

	return stage, true
}

func (h *Handler) loadWorkflowNode(w http.ResponseWriter, r *http.Request, wfID, nodeID string) (db.MulticaWorkflowNode, bool) {
	nID, ok := parseUUIDOrBadRequest(w, nodeID, "node ID")
	if !ok {
		return db.MulticaWorkflowNode{}, false
	}

	wfUUID, ok := parseUUIDOrBadRequest(w, wfID, "workflow ID")
	if !ok {
		return db.MulticaWorkflowNode{}, false
	}

	node, err := h.Queries.GetWorkflowNode(r.Context(), nID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return db.MulticaWorkflowNode{}, false
	}

	if uuidToString(node.WorkflowID) != uuidToString(wfUUID) {
		writeError(w, http.StatusNotFound, "node not found in this workflow")
		return db.MulticaWorkflowNode{}, false
	}

	return node, true
}


// ── Template ───────────────────────────────────────────────────────────────────

// validateNodeAgentIsBuiltin checks that a workflow node's worker or critic
// agent reference is a built-in agent (globally accessible across workspaces).
// Non-builtin agents would show as "Unknown Agent" in cloned templates.
func (h *Handler) validateNodeAgentIsBuiltin(ctx context.Context, agentType string, agentID pgtype.UUID, nodeTitle string, role string) error {
	switch agentType {
	case "agent":
		if !agentID.Valid {
			return fmt.Errorf("node %q %s agent not set", nodeTitle, role)
		}
		agent, err := h.Queries.GetAgent(ctx, agentID)
		if err != nil {
			return fmt.Errorf("node %q %s agent not found", nodeTitle, role)
		}
		if !agent.IsBuiltin {
			return fmt.Errorf("node %q %s agent %q is not a built-in agent — only built-in agents are allowed in templates", nodeTitle, role, agent.Name)
		}
	case "squad":
		if !agentID.Valid {
			return fmt.Errorf("node %q %s squad not set", nodeTitle, role)
		}
		squad, err := h.Queries.GetSquad(ctx, agentID)
		if err != nil {
			return fmt.Errorf("node %q %s squad not found", nodeTitle, role)
		}
		leader, err := h.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return fmt.Errorf("node %q %s squad leader not found", nodeTitle, role)
		}
		if !leader.IsBuiltin {
			return fmt.Errorf("node %q %s squad %q leader %q is not a built-in agent — only built-in agents are allowed in templates", nodeTitle, role, squad.Name, leader.Name)
		}
	}
	return nil
}

// ToggleWorkflowTemplate toggles a workflow's is_template flag.
// Only members with can_manage_workflows can toggle.
func (h *Handler) ToggleWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wf, ok := h.loadWorkflowInWorkspace(w, r, id)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	userID, _ := requireUserID(w, r)
	userUUID := parseUUID(userID)

	// Check can_manage_workflows permission on the user (global, not workspace-scoped).
	currentUser, err := h.Queries.GetUser(r.Context(), userUUID)
	if err != nil || !currentUser.CanManageWorkflows {
		writeError(w, http.StatusForbidden, "only workflow admins can manage templates")
		return
	}

	var req ToggleTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// If setting as template, workflow must be active.
	if req.IsTemplate && wf.Status != "active" {
		writeError(w, http.StatusBadRequest, "workflow must be active to set as template")
		return
	}

	// If setting as template, validate all worker/critic agents are built-in.
	// Non-builtin agents are workspace-scoped and would show as "Unknown Agent"
	// when the template is cloned to another workspace.
	if req.IsTemplate {
		nodes, err := h.Queries.ListWorkflowNodes(r.Context(), wf.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list workflow nodes")
			return
		}
		for _, node := range nodes {
			if err := h.validateNodeAgentIsBuiltin(r.Context(), node.WorkerType, node.WorkerID, node.Title, "worker"); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := h.validateNodeAgentIsBuiltin(r.Context(), node.CriticType, node.CriticID, node.Title, "critic"); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}

	updated, err := h.Queries.SetWorkflowTemplate(r.Context(), db.SetWorkflowTemplateParams{
		ID:         wf.ID,
		IsTemplate: req.IsTemplate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to toggle template")
		return
	}

	count, _ := h.Queries.CountWorkflowNodes(r.Context(), updated.ID)
	resp := workflowToResponse(updated, count)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

// ── Workflow Admins ────────────────────────────────────────────────────────────

func userToAdminResponse(u db.MulticaUser) WorkflowAdminResponse {
	return WorkflowAdminResponse{
		ID:                 uuidToString(u.ID),
		Name:               u.Name,
		Email:              u.Email,
		CanManageWorkflows: u.CanManageWorkflows,
	}
}

// ListWorkflowAdmins returns all users with can_manage_workflows = TRUE.
func (h *Handler) ListWorkflowAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := h.Queries.ListWorkflowAdminUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow admins")
		return
	}

	resp := make([]WorkflowAdminResponse, 0, len(admins))
	for _, a := range admins {
		resp = append(resp, userToAdminResponse(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"admins": resp})
}

// UpdateWorkflowAdmins sets can_manage_workflows for the specified users and
// unsets it for all others. Only existing workflow admins can call this.
func (h *Handler) UpdateWorkflowAdmins(w http.ResponseWriter, r *http.Request) {
	userID, _ := requireUserID(w, r)
	userUUID := parseUUID(userID)

	// Only existing workflow admins can manage workflow admins.
	currentUser, err := h.Queries.GetUser(r.Context(), userUUID)
	if err != nil || !currentUser.CanManageWorkflows {
		writeError(w, http.StatusForbidden, "only workflow admins can manage workflow admins")
		return
	}

	var req UpdateWorkflowAdminsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate all user_ids are valid UUIDs and build set for O(1) lookup.
	chosen := make(map[string]bool, len(req.UserIDs))
	for _, id := range req.UserIDs {
		if _, ok := parseUUIDOrBadRequest(w, id, "user_id"); !ok {
			return
		}
		chosen[id] = true
	}

	// Get all current workflow admin users and update each.
	allAdmins, err := h.Queries.ListWorkflowAdminUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow admins")
		return
	}

	var result []WorkflowAdminResponse
	for _, u := range allAdmins {
		shouldBeAdmin := chosen[uuidToString(u.ID)]
		if u.CanManageWorkflows != shouldBeAdmin {
			updated, err := h.Queries.SetUserWorkflowAdmin(r.Context(), db.SetUserWorkflowAdminParams{
				ID:                 u.ID,
				CanManageWorkflows: shouldBeAdmin,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update user")
				return
			}
			result = append(result, userToAdminResponse(updated))
		} else {
			result = append(result, userToAdminResponse(u))
		}
	}

	h.publish(protocol.EventWorkflowUpdated, "", "member", userID, map[string]any{"admins": result})
	writeJSON(w, http.StatusOK, map[string]any{"admins": result})
}

// InviteWorkflowAdmin looks up a user by email and grants them workflow admin permission.
func (h *Handler) InviteWorkflowAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if user.CanManageWorkflows {
		// Already an admin — return success with current state
		writeJSON(w, http.StatusOK, userToAdminResponse(user))
		return
	}

	updated, err := h.Queries.SetUserWorkflowAdmin(r.Context(), db.SetUserWorkflowAdminParams{
		ID:                 user.ID,
		CanManageWorkflows: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set workflow admin")
		return
	}

	writeJSON(w, http.StatusOK, userToAdminResponse(updated))
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func nonNullText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func float64ToFloat8(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}

func int32ToInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func ptrStrToUUID(s *string) pgtype.UUID {
	if s == nil {
		return pgtype.UUID{}
	}
	u, err := util.ParseUUID(*s)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}

