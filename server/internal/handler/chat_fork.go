package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ChatForkResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	ChatID string `json:"chat_id,omitempty"`
}

func (h *Handler) CreateChatFork(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" || r.Header.Get("X-Actor-Source") == "cloud_pat" {
		writeError(w, http.StatusForbidden, "only a member can fork a chat")
		return
	}
	source, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID: source.AgentID, WorkspaceID: source.WorkspaceID,
	})
	if err != nil || !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}
	if !h.requireChatProjectVisible(w, r, workspaceID, source.ProjectID, source.AgentID) {
		return
	}
	_, task, err := h.queueChatFork(r.Context(), source, source.CreatorID)
	if errors.Is(err, errChatForkUnavailable) || errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "chat fork requires an idle chat on its original compatible Codex runtime")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue chat fork")
		return
	}
	h.TaskService.FinalizeChatTaskEnqueue(r.Context(), task)
	writeJSON(w, http.StatusAccepted, ChatForkResponse{TaskID: uuidToString(task.ID), Status: task.Status})
}

func (h *Handler) GetChatFork(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" || r.Header.Get("X-Actor-Source") == "cloud_pat" {
		writeError(w, http.StatusForbidden, "only a member can inspect a chat fork")
		return
	}
	source, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	if !h.requireChatProjectVisible(w, r, workspaceID, source.ProjectID, source.AgentID) {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	task, err := h.Queries.GetChatForkTask(r.Context(), db.GetChatForkTaskParams{
		TaskID: taskID, SourceChatID: source.ID, CreatorID: source.CreatorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "chat fork not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read chat fork")
		return
	}
	response := ChatForkResponse{TaskID: uuidToString(task.ID), Status: task.Status}
	if task.Status == "completed" && task.ChatSessionID.Valid {
		response.ChatID = uuidToString(task.ChatSessionID)
	}
	writeJSON(w, http.StatusOK, response)
}
