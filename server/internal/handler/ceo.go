package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// CEO Command — simple entry point to trigger orchestrated workflows
// ---------------------------------------------------------------------------

type CEOCommandRequest struct {
	Message string `json:"message"`
}

type CEOCommandResponse struct {
	WorkflowID    string `json:"workflow_id"`
	WorkflowRunID string `json:"workflow_run_id"`
	IssueID       string `json:"issue_id,omitempty"`
	PlannerTaskID string `json:"planner_task_id,omitempty"`
}

// SendCEOCommand handles POST /api/command.
// It reads the CEO agent ID from workspace settings, creates an ad-hoc
// orchestrated workflow, and triggers it — all in one call.
func (h *Handler) SendCEOCommand(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	if workspaceID == "" {
		workspaceID = resolveWorkspaceID(r)
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required (header X-Workspace-ID or query param)")
		return
	}

	var req CEOCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Load workspace to read settings.
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	// Extract ceo_agent_id from workspace settings.
	ceoAgentID := extractCEOAgentID(ws)
	if ceoAgentID == "" {
		writeError(w, http.StatusBadRequest, "workspace has no ceo_agent_id configured in settings")
		return
	}

	// Verify the CEO agent exists, is not archived, and has a runtime.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          parseUUID(ceoAgentID),
		WorkspaceID: ws.ID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "CEO agent not found in workspace")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "CEO agent is archived")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "CEO agent has no runtime configured")
		return
	}

	// Delegate to workflow service.
	result, err := h.WorkflowService.StartCEOCommand(
		r.Context(),
		ws.ID,
		parseUUID(userID),
		parseUUID(ceoAgentID),
		req.Message,
	)
	if err != nil {
		slog.Error("CEO command failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to execute CEO command: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// extractCEOAgentID reads settings.ceo_agent_id from workspace settings JSON.
func extractCEOAgentID(ws db.Workspace) string {
	if ws.Settings == nil {
		return ""
	}
	var settings map[string]any
	if err := json.Unmarshal(ws.Settings, &settings); err != nil {
		return ""
	}
	id, _ := settings["ceo_agent_id"].(string)
	return id
}
