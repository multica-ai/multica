package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

type FindSkillsWithAIRequest struct {
	Prompt  string `json:"prompt"`
	AgentID string `json:"agent_id"`
}

type AITaskQueuedResponse struct {
	TaskID string `json:"task_id"`
}

func (h *Handler) FindSkillsWithAI(w http.ResponseWriter, r *http.Request) {
	var req FindSkillsWithAIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
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
	_, agentUUID, ok := h.validateHostAgentForAITask(w, r, workspaceID, req.AgentID)
	if !ok {
		return
	}
	task, err := h.TaskService.EnqueueAITask(r.Context(), service.AITaskContextTypeSkillFind, wsUUID, requesterUUID, agentUUID, prompt)
	if err != nil {
		slog.Warn("skill-find enqueue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to enqueue skill finder task")
		return
	}
	writeJSON(w, http.StatusAccepted, AITaskQueuedResponse{TaskID: uuidToString(task.ID)})
}
