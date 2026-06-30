package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

// ChatChannelHistoryReader reads a chat session's bound IM-channel history. The
// Slack reader (slack.History) satisfies it; a future platform registers its
// own. Defined here as a narrow interface so the handler stays testable and so
// the channel-agnostic contract — one shape regardless of platform — is enforced
// at the boundary (MUL-3871).
type ChatChannelHistoryReader interface {
	Fetch(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error)
}

// ChatChannelHistoryResponse is the unified `multica chat history` payload. It
// is the SAME shape no matter which channel backs the session — the agent never
// sees a per-platform API.
type ChatChannelHistoryResponse struct {
	ChannelType string                   `json:"channel_type"`
	Scope       channel.HistoryScope     `json:"scope,omitempty"`
	Messages    []channel.HistoryMessage `json:"messages"`
	NextCursor  string                   `json:"next_cursor,omitempty"`
	// Note carries a human-readable explanation when there is no history to
	// read (e.g. the session is not connected to a chat channel), so the agent
	// gets a clear answer instead of a bare empty list.
	Note string `json:"note,omitempty"`
}

// GetChatChannelHistory serves the agent-facing `multica chat history` command.
// It is authorized by the task-scoped token alone: middleware stamps the token's
// task into X-Task-ID (the client cannot forge it), and the endpoint reads the
// history of THAT task's chat session — so an agent can only ever read the
// conversation it is currently running for, never an arbitrary session/channel.
func (h *Handler) GetChatChannelHistory(w http.ResponseWriter, r *http.Request) {
	// X-Actor-Source is server-set only: the Auth middleware deletes any
	// client-supplied value and re-stamps "task_token" ONLY on the mat_ task
	// token branch (along with the authoritative X-Task-ID). A normal JWT / mul_
	// PAT request leaves it empty and does NOT strip a client-forged X-Task-ID,
	// so this gate is load-bearing: without it a member could forge X-Task-ID and
	// read another session's channel history. Require the task-token actor here,
	// THEN trust X-Task-ID.
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "chat history is only available from within an agent task")
		return
	}
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		writeError(w, http.StatusBadRequest, "missing task context")
		return
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if !task.ChatSessionID.Valid {
		writeError(w, http.StatusBadRequest, "this task is not a chat task")
		return
	}
	// Defense in depth: load the session and confirm it lives in the token's
	// stamped workspace. The token→task binding already guarantees the agent can
	// only reach its own task here; this makes a future wiring regression fail
	// closed instead of leaking another workspace's conversation.
	session, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return
	}
	if ws := ctxWorkspaceID(r.Context()); ws != "" && uuidToString(session.WorkspaceID) != ws {
		writeError(w, http.StatusForbidden, "chat session does not belong to this workspace")
		return
	}

	scope := parseHistoryScope(r.URL.Query().Get("scope"))
	if scope == channel.HistoryScopeAuto {
		// First turn — the bot has not replied yet, so no thread exists — reads
		// the surrounding channel (where the prior context lives). A follow-up
		// reads the agent's own thread. The agent can override with
		// ?scope=channel|thread.
		if h.chatSessionHasBotReply(r.Context(), task.ChatSessionID) {
			scope = channel.HistoryScopeThread
		} else {
			scope = channel.HistoryScopeChannel
		}
	}
	opts := channel.HistoryOptions{
		Scope:  scope,
		Limit:  parseHistoryLimit(r.URL.Query().Get("limit")),
		Before: r.URL.Query().Get("before"),
	}

	empty := ChatChannelHistoryResponse{Messages: []channel.HistoryMessage{}}
	if h.SlackHistory == nil {
		empty.Note = "No chat channel integration is configured on this server."
		writeJSON(w, http.StatusOK, empty)
		return
	}

	page, err := h.SlackHistory.Fetch(r.Context(), task.ChatSessionID, opts)
	if err != nil {
		if errors.Is(err, slack.ErrNoSlackSession) {
			empty.Note = "This conversation is not connected to a chat channel, so there is no prior channel history to read."
			writeJSON(w, http.StatusOK, empty)
			return
		}
		slog.Error("chat channel history fetch failed", append(logger.RequestAttrs(r),
			"error", err, "chat_session_id", uuidToString(task.ChatSessionID))...)
		writeError(w, http.StatusBadGateway, "failed to read channel history")
		return
	}

	messages := page.Messages
	if messages == nil {
		messages = []channel.HistoryMessage{}
	}
	writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
		ChannelType: page.ChannelType,
		Scope:       page.Scope,
		Messages:    messages,
		NextCursor:  page.NextCursor,
	})
}

// parseHistoryScope maps the ?scope query value to a HistoryScope, defaulting to
// auto for empty / unknown values.
func parseHistoryScope(raw string) channel.HistoryScope {
	switch channel.HistoryScope(raw) {
	case channel.HistoryScopeThread:
		return channel.HistoryScopeThread
	case channel.HistoryScopeChannel:
		return channel.HistoryScopeChannel
	default:
		return channel.HistoryScopeAuto
	}
}

// chatSessionHasBotReply reports whether the bot has already replied in this
// session — i.e. this is a follow-up, not the first turn. On Slack the bot's
// first reply opens the thread, so an existing assistant message is the signal
// that a thread worth reading exists. Best-effort: a query error defaults to
// false (treat as first turn → channel), the safe, context-rich choice.
func (h *Handler) chatSessionHasBotReply(ctx context.Context, sessionID pgtype.UUID) bool {
	msgs, err := h.Queries.ListChatMessages(ctx, sessionID)
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			return true
		}
	}
	return false
}

// parseHistoryLimit reads the ?limit query param, ignoring junk (the reader
// clamps the range). 0 means "use the reader's default".
func parseHistoryLimit(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
