package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Chat Sessions
// ---------------------------------------------------------------------------

type CreateChatSessionRequest struct {
	AgentID string `json:"agent_id"`
	Title   string `json:"title"`
}

func (h *Handler) CreateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	// Verify agent exists in workspace.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          parseUUID(req.AgentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}

	session, err := h.Queries.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: parseUUID(workspaceID),
		AgentID:     parseUUID(req.AgentID),
		CreatorID:   parseUUID(userID),
		Title:       req.Title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	writeJSON(w, http.StatusCreated, chatSessionToResponse(session))
}

func (h *Handler) ListChatSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	status := r.URL.Query().Get("status")

	var sessions []db.ChatSession
	var err error
	if status == "all" {
		sessions, err = h.Queries.ListAllChatSessionsByCreator(r.Context(), db.ListAllChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
	} else {
		sessions, err = h.Queries.ListChatSessionsByCreator(r.Context(), db.ListChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
		return
	}

	resp := make([]ChatSessionResponse, len(sessions))
	for i, s := range sessions {
		resp[i] = chatSessionToResponse(s)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return
	}

	writeJSON(w, http.StatusOK, chatSessionToResponse(session))
}

func (h *Handler) ArchiveChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return
	}

	if err := h.Queries.ArchiveChatSession(r.Context(), parseUUID(sessionID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive chat session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Chat Messages
// ---------------------------------------------------------------------------

type SendChatMessageRequest struct {
	Content string `json:"content"`
}

type SendChatMessageResponse struct {
	MessageID string `json:"message_id"`
	TaskID    string `json:"task_id"`
}

func (h *Handler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SendChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Load chat session.
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if session.Status != "active" {
		writeError(w, http.StatusBadRequest, "chat session is archived")
		return
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return
	}

	// Create the user message first so the daemon can always find it.
	msg, err := h.Queries.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       req.Content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat message")
		return
	}

	// Enqueue a chat task after the message exists.
	task, err := h.TaskService.EnqueueChatTask(r.Context(), session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue chat task: "+err.Error())
		return
	}

	// Touch session updated_at.
	if err := h.Queries.TouchChatSession(r.Context(), session.ID); err != nil {
		slog.Warn("failed to touch chat session", "session_id", sessionID, "error", err)
	}

	// Broadcast the user message.
	h.publish(protocol.EventChatMessage, workspaceID, "member", userID, protocol.ChatMessagePayload{
		ChatSessionID: sessionID,
		MessageID:     uuidToString(msg.ID),
		Role:          "user",
		Content:       req.Content,
		TaskID:        uuidToString(task.ID),
		CreatedAt:     timestampToString(msg.CreatedAt),
	})

	writeJSON(w, http.StatusCreated, SendChatMessageResponse{
		MessageID: uuidToString(msg.ID),
		TaskID:    uuidToString(task.ID),
	})
}

func (h *Handler) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return
	}

	messages, err := h.Queries.ListChatMessages(r.Context(), parseUUID(sessionID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}

	resp := make([]ChatMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = chatMessageToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Task cancellation (user-facing, with ownership check)
// ---------------------------------------------------------------------------

// CancelTaskByUser cancels a task after verifying the requesting user owns
// the associated chat session or issue within the current workspace.
func (h *Handler) CancelTaskByUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	taskID := chi.URLParam(r, "taskId")

	task, err := h.Queries.GetAgentTask(r.Context(), parseUUID(taskID))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// Verify ownership: for chat tasks, check workspace + creator;
	// for issue tasks, verify the issue belongs to the current workspace.
	if task.ChatSessionID.Valid {
		cs, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          task.ChatSessionID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if uuidToString(cs.CreatorID) != userID {
			writeError(w, http.StatusForbidden, "not your task")
			return
		}
	} else if task.IssueID.Valid {
		issue, err := h.Queries.GetIssue(r.Context(), task.IssueID)
		if err != nil || uuidToString(issue.WorkspaceID) != workspaceID {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
	} else {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	cancelled, err := h.TaskService.CancelTask(r.Context(), parseUUID(taskID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(*cancelled))
}

// ---------------------------------------------------------------------------
// Response types & helpers
// ---------------------------------------------------------------------------

type ChatSessionResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AgentID     string  `json:"agent_id"`
	CreatorID   string  `json:"creator_id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type ChatMessageResponse struct {
	ID            string  `json:"id"`
	ChatSessionID string  `json:"chat_session_id"`
	Role          string  `json:"role"`
	Content       string  `json:"content"`
	TaskID        *string `json:"task_id"`
	CreatedAt     string  `json:"created_at"`
}

func chatSessionToResponse(s db.ChatSession) ChatSessionResponse {
	return ChatSessionResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		AgentID:     uuidToString(s.AgentID),
		CreatorID:   uuidToString(s.CreatorID),
		Title:       s.Title,
		Status:      s.Status,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func chatMessageToResponse(m db.ChatMessage) ChatMessageResponse {
	return ChatMessageResponse{
		ID:            uuidToString(m.ID),
		ChatSessionID: uuidToString(m.ChatSessionID),
		Role:          m.Role,
		Content:       m.Content,
		TaskID:        uuidToPtr(m.TaskID),
		CreatedAt:     timestampToString(m.CreatedAt),
	}
}
