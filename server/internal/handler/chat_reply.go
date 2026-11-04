package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ChatReplyRequest is the body of POST /api/chat/sessions/{sessionId}/reply.
type ChatReplyRequest struct {
	Content string `json:"content"`
}

// ReplyChatSession lets the workspace's Secretary write an assistant message into
// a chat session from a run that is not a chat turn. The run is named by the
// server-trusted X-Task-ID header. See service.CheckChatReply for the rules.
//
//	POST /api/chat/sessions/{sessionId}/reply {content}
func (h *Handler) ReplyChatSession(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusForbidden, "only the workspace's secretary agent may reply in chat")
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "session id")
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	if runID == "" {
		writeError(w, http.StatusForbidden, "a chat reply must come from a live run")
		return
	}
	runUUID, ok := parseUUIDOrBadRequest(w, runID, "task id")
	if !ok {
		return
	}

	var req ChatReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := service.CreateChatReply(r.Context(), h.TxStarter, h.Queries, service.ChatReplyParams{
		WorkspaceID: wsUUID,
		SessionID:   sessionUUID,
		AgentID:     actorUUID,
		RunID:       runUUID,
		Content:     req.Content,
		Now:         time.Now(),
	})
	switch {
	case err == nil:
	case errors.Is(err, service.ErrChatReplySessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, service.ErrChatReplyArchived):
		writeErrorCode(w, http.StatusConflict, "session_archived", err.Error())
		return
	case errors.Is(err, service.ErrChatReplyRateLimited):
		writeErrorCode(w, http.StatusTooManyRequests, "reply_limit", err.Error())
		return
	case errors.Is(err, service.ErrChatReplyEmpty), errors.Is(err, service.ErrChatReplyTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, service.ErrChatReplyNotSecretary),
		errors.Is(err, service.ErrChatReplyWrongSession),
		errors.Is(err, service.ErrChatReplyRunNotLive),
		errors.Is(err, service.ErrChatReplyNotLinked),
		errors.Is(err, service.ErrChatReplyNotOwner):
		writeError(w, http.StatusForbidden, err.Error())
		return
	default:
		slog.Warn("chat reply failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to write the reply")
		return
	}

	// The message is committed: tell the session owner's clients. ChatSessionOwnerID
	// is the session creator, so the agent's actor id does not decide who receives it.
	sessionID := util.UUIDToString(sessionUUID)
	h.Bus.Publish(events.Event{
		Type:               protocol.EventChatMessage,
		WorkspaceID:        workspaceID,
		ActorType:          "agent",
		ActorID:            actorID,
		ChatSessionID:      sessionID,
		ChatSessionOwnerID: util.UUIDToString(res.OwnerID),
		Payload: protocol.ChatMessagePayload{
			ChatSessionID: sessionID,
			MessageID:     util.UUIDToString(res.Message.ID),
			Role:          res.Message.Role,
			Content:       res.Message.Content,
			CreatedAt:     res.Message.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		},
	})
	// A session bound to an external channel hears the reply through the chat-done
	// path, on the run that wrote it. The reply is already stored, so a failure to
	// set the route is logged, not returned.
	routed, err := service.ChatReplyRoute(r.Context(), h.Queries, sessionUUID, runUUID)
	if err != nil {
		slog.Warn("chat reply route failed", append(logger.RequestAttrs(r), "error", err, "session_id", sessionID)...)
	} else if routed {
		h.TaskService.PublishChatReplyDone(r.Context(), res.Run, res.Message)
	}
	writeJSON(w, http.StatusCreated, chatMessageToResponse(res.Message, nil))
}
