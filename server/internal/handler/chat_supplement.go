package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ChatSupplementReceipt struct {
	TaskID        string  `json:"task_id"`
	Status        string  `json:"status"`
	FailureReason *string `json:"failure_reason,omitempty"`
}

func (h *Handler) hydrateChatSupplements(ctx context.Context, workspaceID pgtype.UUID, messages []ChatMessageResponse) {
	if len(messages) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, parseUUID(m.ID))
	}
	rows, err := h.Queries.ListChatSupplements(ctx, db.ListChatSupplementsParams{WorkspaceID: workspaceID, MessageIds: ids})
	if err != nil {
		return
	}
	byID := make(map[string]ChatSupplementReceipt, len(rows))
	for _, s := range rows {
		status := s.Status
		// Once claimed, lack of an engine acknowledgement cannot prove delivery.
		// It must never be presented as accepted, or silently retried.
		if status == "delivering" {
			status = "unknown"
		}
		byID[uuidToString(s.MessageID)] = ChatSupplementReceipt{uuidToString(s.TaskID), status, textToPtr(s.FailureReason)}
	}
	for i := range messages {
		if s, ok := byID[messages[i].ID]; ok {
			messages[i].Supplement = &s
		}
	}
}

func (h *Handler) writeChatSupplement(w http.ResponseWriter, r *http.Request, session db.ChatSession, message db.ChatMessage, status int) {
	messages := []ChatMessageResponse{chatMessageToResponse(message, nil)}
	h.hydrateChatSupplements(r.Context(), session.WorkspaceID, messages)
	writeJSON(w, status, messages[0])
}

// CreateChatSupplement persists one input against the exact negotiated turn.
// It never enqueues a task or falls back to a successor when that turn ends.
func (h *Handler) CreateChatSupplement(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	authorID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: session.AgentID, WorkspaceID: session.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid || !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, workspaceID) {
		writeError(w, http.StatusForbidden, "you cannot run this agent")
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || task.ChatSessionID != session.ID || task.IssueID.Valid {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var req struct {
		createTaskSupplementRequest
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*maxCommentContentBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = sanitizeNullBytes(req.Content)
	if len(req.AttachmentIDs) > 0 || strings.TrimSpace(req.Content) == "" || len(req.Content) > maxCommentContentBytes {
		writeError(w, http.StatusBadRequest, "invalid content")
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, req.ClientRequestID, "client_request_id")
	if !ok {
		return
	}
	lookup := db.GetChatSupplementByRequestParams{TaskID: task.ID, ChatSessionID: session.ID, WorkspaceID: session.WorkspaceID, AuthorID: authorID, ClientRequestID: requestID}
	if existing, err := h.Queries.GetChatSupplementByRequest(r.Context(), lookup); err == nil {
		h.notifyTaskSupplementAvailable(task)
		h.writeChatSupplement(w, r, session, existing, http.StatusOK)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check additional message")
		return
	}
	message, err := h.Queries.CreateChatSupplement(r.Context(), db.CreateChatSupplementParams{
		TaskID: task.ID, ChatSessionID: session.ID, WorkspaceID: session.WorkspaceID, AuthorID: authorID, ClientRequestID: requestID, Content: req.Content,
	})
	if isUniqueViolation(err) {
		if existing, loadErr := h.Queries.GetChatSupplementByRequest(r.Context(), lookup); loadErr == nil {
			h.notifyTaskSupplementAvailable(task)
			h.writeChatSupplement(w, r, session, existing, http.StatusOK)
			return
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "chat_supplement_unavailable", "this run cannot accept additional messages")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add message to current run")
		return
	}
	h.notifyTaskSupplementAvailable(task)
	h.publishChat(protocol.EventChatSupplement, workspaceID, "member", userID, uuidToString(session.ID), map[string]string{"chat_session_id": uuidToString(session.ID)})
	h.writeChatSupplement(w, r, session, db.ChatMessage(message), http.StatusCreated)
}

func (h *Handler) claimChatSupplement(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue) {
	row, err := h.Queries.ClaimNextChatSupplement(r.Context(), task.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim additional message")
		return
	}
	// The existing daemon transport calls the opaque receipt ID comment_id.
	writeJSON(w, http.StatusOK, map[string]any{"comment_id": uuidToString(row.MessageID), "content": row.Content, "author_type": "member", "author_name": row.AuthorName})
}

func (h *Handler) ackChatSupplement(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue, messageID pgtype.UUID, req ackTaskSupplementRequest) {
	status := "delivered"
	var reason pgtype.Text
	if !req.Delivered {
		reason = pgtype.Text{String: stableTaskSupplementFailureReason(req.Error), Valid: true}
		status = "failed"
		if req.Error == protocol.TaskSupplementFailureUnknown || reason.String == protocol.TaskSupplementFailureTimeout || reason.String == protocol.TaskSupplementFailureTurnEnded {
			status = "unknown"
		}
	}
	row, err := h.Queries.AckChatSupplement(r.Context(), db.AckChatSupplementParams{TaskID: task.ID, MessageID: messageID, Status: status, FailureReason: reason})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "additional message is no longer deliverable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to acknowledge additional message")
		return
	}
	if session, loadErr := h.Queries.GetChatSession(r.Context(), row.ChatSessionID); loadErr == nil {
		h.publishChat(protocol.EventChatSupplement, uuidToString(session.WorkspaceID), "member", uuidToString(session.CreatorID), uuidToString(session.ID), map[string]string{"chat_session_id": uuidToString(session.ID)})
	}
	writeJSON(w, http.StatusOK, ChatSupplementReceipt{uuidToString(row.TaskID), row.Status, textToPtr(row.FailureReason)})
}
