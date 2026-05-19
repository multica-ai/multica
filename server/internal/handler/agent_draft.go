package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type DraftAgentWithAIRequest struct {
	Prompt      string `json:"prompt"`
	HostAgentID string `json:"host_agent_id"`
}

func (h *Handler) DraftAgentWithAI(w http.ResponseWriter, r *http.Request) {
	var req DraftAgentWithAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if strings.TrimSpace(req.HostAgentID) == "" {
		writeError(w, http.StatusBadRequest, "host_agent_id is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	requesterID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	requesterUUID, ok := parseUUIDOrBadRequest(w, requesterID, "requester_id")
	if !ok {
		return
	}
	hostAgent, hostAgentUUID, ok := h.validateHostAgentForAITask(w, r, workspaceID, req.HostAgentID)
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          hostAgent.RuntimeID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeAgentUnavailable(w, "agent runtime is unavailable")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can draft agents on it")
		return
	}
	task, err := h.TaskService.EnqueueAITask(r.Context(), service.AITaskContextTypeAgentCreate, wsUUID, requesterUUID, hostAgentUUID, prompt)
	if err != nil {
		slog.Warn("agent-draft enqueue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to enqueue agent draft task")
		return
	}
	writeJSON(w, http.StatusAccepted, AITaskQueuedResponse{TaskID: uuidToString(task.ID)})
}
