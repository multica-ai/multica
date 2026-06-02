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
	Template    string `json:"template"`    // legacy "ai-coding" compat
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
	WorkerInstructions string          `json:"worker_instructions"`
	CriticType         string          `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticInstructions string          `json:"critic_instructions"`
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
	WorkerInstructions *string         `json:"worker_instructions"`
	CriticType         *string         `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticInstructions *string         `json:"critic_instructions"`
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
	WorkerInstructions string          `json:"worker_instructions"`
	CriticType         string          `json:"critic_type"`
	CriticID           *string         `json:"critic_id"`
	CriticInstructions string          `json:"critic_instructions"`
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
		WorkerInstructions: node.WorkerInstructions,
		CriticType:         node.CriticType,
		CriticID:           uuidToPtr(node.CriticID),
		CriticInstructions: node.CriticInstructions,
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

	// If a template is requested, create pre-configured nodes + edges.
	if req.Template == "ai-coding" {
		h.createAICodingTemplate(r.Context(), wf.ID)
	}
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

	nodeResps := make([]WorkflowNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		nodeResps = append(nodeResps, workflowNodeToResponse(n))
	}
	edgeResps := make([]WorkflowEdgeResponse, 0, len(edges))
	for _, e := range edges {
		edgeResps = append(edgeResps, workflowEdgeToResponse(e))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"workflow": workflowToResponse(wf, int64(len(nodes))),
		"nodes":    nodeResps,
		"edges":    edgeResps,
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
		WorkerInstructions: nonNullText(req.WorkerInstructions),
		CriticType:         req.CriticType,
		CriticID:           criticID,
		CriticInstructions: nonNullText(req.CriticInstructions),
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
		WorkerInstructions: ptrToText(req.WorkerInstructions),
		CriticType:         ptrToText(req.CriticType),
		CriticID:           ptrStrToUUID(req.CriticID),
		CriticInstructions: ptrToText(req.CriticInstructions),
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

// ── Template ───────────────────────────────────────────────────────────────────

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

// ── Template: AI Coding workflow ─────────────────────────────────────────────

func (h *Handler) createAICodingTemplate(ctx context.Context, workflowID pgtype.UUID) {
	type nodeDef struct {
		title              string
		description        string
		inSchema           string
		outSchema          string
		workerInstructions string
		criticInstructions string
		criticType         string
		x, y               float64
	}

	nodes := []nodeDef{
		{
			title:       "需求分析",
			description: "分析需求并产出需求文档",
			inSchema:    `{"type":"object","properties":{"idea":{"type":"string","description":"产品构思或需求描述"}},"required":["idea"]}`,
			outSchema:   `{"type":"object","properties":{"requirement_doc":{"type":"string","description":"需求文档"}},"required":["requirement_doc"]}`,
			workerInstructions: "你是一位资深产品需求分析师。请根据输入的产品构思，撰写一份完整的需求分析文档，包括：功能需求、非功能需求、用户故事、验收标准。",
			criticInstructions: "作为评审者，请评估需求文档的完整性、清晰度和可行性。检查是否遗漏了关键场景。如果不通过，请明确指出需要改进的部分。",
			criticType:         "human",
			x:                  100, y: 50,
		},
		{
			title:       "架构设计",
			description: "基于需求文档设计技术架构",
			inSchema:    `{"type":"object","properties":{"requirement_doc":{"type":"string"}},"required":["requirement_doc"]}`,
			outSchema:   `{"type":"object","properties":{"architecture_doc":{"type":"string","description":"架构设计文档"}},"required":["architecture_doc"]}`,
			workerInstructions: "你是一位资深技术架构师。请根据需求文档，撰写技术架构设计方案，包括：技术选型、系统架构图描述、模块划分、数据流设计、接口设计原则。",
			criticInstructions: "作为技术负责人，请评审架构方案的合理性、可扩展性和技术风险。如果不通过，请指出具体问题和改进方向。",
			criticType:         "human",
			x:                  350, y: 50,
		},
		{
			title:       "任务拆分",
			description: "将架构设计拆分为具体开发任务",
			inSchema:    `{"type":"object","properties":{"architecture_doc":{"type":"string"}},"required":["architecture_doc"]}`,
			outSchema:   `{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"estimated_hours":{"type":"number"}}}}}},"required":["tasks"]}`,
			workerInstructions: "你是一位资深项目经理。请根据架构设计文档，将工作拆分为可执行的开发任务，每个任务应包含标题、描述、预估工时和优先级。",
			criticInstructions: "请评审任务拆分的合理性：粒度是否合适？是否有遗漏？依赖关系是否清晰？",
			criticType:         "human",
			x:                  600, y: 50,
		},
		{
			title:       "编码",
			description: "根据任务拆分进行编码实现",
			inSchema:    `{"type":"object","properties":{"tasks":{"type":"array"}},"required":["tasks"]}`,
			outSchema:   `{"type":"object","properties":{"code_changes":{"type":"array","description":"代码变更列表"}},"required":["code_changes"]}`,
			workerInstructions: "你是一位资深软件工程师。请根据分配的任务进行编码实现，确保代码质量、测试覆盖和文档完整。",
			criticInstructions: "作为代码审查Agent，请检查代码的正确性、安全性、性能、代码风格和测试覆盖。如不通过请指出具体问题。",
			criticType:         "agent",
			x:                  850, y: 50,
		},
		{
			title:       "测试",
			description: "对编码结果进行全面测试",
			inSchema:    `{"type":"object","properties":{"code_changes":{"type":"array"}},"required":["code_changes"]}`,
			outSchema:   `{"type":"object","properties":{"test_report":{"type":"string","description":"测试报告"}},"required":["test_report"]}`,
			workerInstructions: "你是一位资深测试工程师。请对代码变更进行全面的测试验证，包括单元测试、集成测试、端到端测试，并产出测试报告。",
			criticInstructions: "请评审测试报告：测试覆盖率是否充分？是否有遗漏的测试场景？测试结果是否可靠？",
			criticType:         "human",
			x:                  1100, y: 50,
		},
	}

	var nodeIDs []pgtype.UUID
	for i, nd := range nodes {
		node, err := h.Queries.CreateWorkflowNode(ctx, db.CreateWorkflowNodeParams{
			WorkflowID:         workflowID,
			Title:              nd.title,
			Description:        nonNullText(nd.description),
			PositionX:          nd.x,
			PositionY:          nd.y,
			FormatSchema:       []byte(nd.inSchema),
			WorkerType:         "agent",
			WorkerInstructions: nonNullText(nd.workerInstructions),
			CriticType:         nd.criticType,
			CriticInstructions: nonNullText(nd.criticInstructions),
			SortOrder:          int32(i),
		})
		if err != nil {
			continue
		}
		nodeIDs = append(nodeIDs, node.ID)

		// Create edge from previous node
		if i > 0 {
			h.Queries.CreateWorkflowEdge(ctx, db.CreateWorkflowEdgeParams{
				WorkflowID:   workflowID,
				SourceNodeID: nodeIDs[i-1],
				TargetNodeID: node.ID,
			})
		}
	}
}
