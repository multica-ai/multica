package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChatChannelHistoryReader reads a chat session's bound IM-channel history. The
// Slack reader (slack.History) satisfies it; a future platform registers its
// own. Two operations back the two agent commands: ChannelOverview is the
// channel table-of-contents (`multica chat history`), Thread reads one thread's
// messages (`multica chat thread [id]`). Both are scoped server-side to the
// session's own channel (MUL-3871).
type ChatChannelHistoryReader interface {
	ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error)
	Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error)
}

// ChatChannelHistoryResponse is the unified payload for both commands — the SAME
// shape no matter which channel backs the session, the agent never sees a
// per-platform API.
type ChatChannelHistoryResponse struct {
	ChannelType string `json:"channel_type"`
	// ThreadID is set on a thread read: which thread the messages belong to.
	ThreadID   string                   `json:"thread_id,omitempty"`
	Messages   []channel.HistoryMessage `json:"messages"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	// Note explains an empty result (e.g. the session is not channel-backed), so
	// the agent gets a clear answer instead of a bare empty list.
	Note string `json:"note,omitempty"`
}

// GetChatChannelHistory serves `multica chat history` — the channel overview:
// recent top-level messages, each thread tagged with its id + reply count (no
// thread contents). The agent drills into a thread with `multica chat thread`.
func (h *Handler) GetChatChannelHistory(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	if h.ChatHistory == nil {
		h.writeNoChannelIntegration(w)
		return
	}
	page, err := h.ChatHistory.ChannelOverview(r.Context(), sessionID, historyOptionsFrom(r))
	h.respondChatHistory(w, r, sessionID, page, err)
}

// GetChatThread serves `multica chat thread [id]` — one thread's messages. With
// ?id it reads that specific thread; without, the thread the session is in. The
// channel stays server-pinned to the session, so the id is only a within-channel
// locator.
func (h *Handler) GetChatThread(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	if h.ChatHistory == nil {
		h.writeNoChannelIntegration(w)
		return
	}
	threadID := r.URL.Query().Get("id")
	page, err := h.ChatHistory.Thread(r.Context(), sessionID, threadID, historyOptionsFrom(r))
	h.respondChatHistory(w, r, sessionID, page, err)
}

// chatHistorySession authorizes the request and returns the caller's own chat
// session. It is authorized by the task-scoped token alone: middleware stamps
// the token's task into X-Actor-Source=task_token + X-Task-ID (a normal JWT /
// mul_ PAT leaves X-Actor-Source empty and does NOT strip a client-forged
// X-Task-ID), so requiring the task-token actor is load-bearing — without it a
// member could forge X-Task-ID and read another session's history.
func (h *Handler) chatHistorySession(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "chat history is only available from within an agent task")
		return pgtype.UUID{}, false
	}
	taskIDHeader := r.Header.Get("X-Task-ID")
	if taskIDHeader == "" {
		writeError(w, http.StatusBadRequest, "missing task context")
		return pgtype.UUID{}, false
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return pgtype.UUID{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return pgtype.UUID{}, false
	}
	if !task.ChatSessionID.Valid {
		writeError(w, http.StatusBadRequest, "this task is not a chat task")
		return pgtype.UUID{}, false
	}
	// Defense in depth: load the session and confirm it lives in the token's
	// stamped workspace. The token→task binding already guarantees the agent can
	// only reach its own task; this makes a future wiring regression fail closed.
	session, err := h.Queries.GetChatSession(r.Context(), task.ChatSessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return pgtype.UUID{}, false
	}
	if ws := ctxWorkspaceID(r.Context()); ws != "" && uuidToString(session.WorkspaceID) != ws {
		writeError(w, http.StatusForbidden, "chat session does not belong to this workspace")
		return pgtype.UUID{}, false
	}
	return task.ChatSessionID, true
}

// respondChatHistory writes the shared response: a note (200) when the session
// is not channel-backed, a 502 on a real read failure, the page otherwise.
func (h *Handler) respondChatHistory(w http.ResponseWriter, r *http.Request, sessionID pgtype.UUID, page channel.HistoryPage, err error) {
	if err != nil {
		// The session is not backed by a channel this server can read — a
		// web-only session (slack.ErrNoSlackSession) or one with no reader for
		// its channel type (channel.ErrNoChannelSession, from the dispatcher).
		// Both answer as an empty read with a note, not a failure.
		if errors.Is(err, slack.ErrNoSlackSession) || errors.Is(err, channel.ErrNoChannelSession) {
			writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
				Messages: []channel.HistoryMessage{},
				Note:     "This conversation is not connected to a chat channel, so there is no channel history to read.",
			})
			return
		}
		// The channel IS bound but its history can't be read for a known,
		// non-transient reason (e.g. the app is missing a required scope). Answer
		// with the actionable reason as a note + 200 — never a retryable 5xx,
		// which the agent would misreport as a transient platform outage and keep
		// retrying against a permanent failure.
		var unavailable *channel.HistoryUnavailableError
		if errors.As(err, &unavailable) {
			writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
				Messages: []channel.HistoryMessage{},
				Note:     unavailable.Reason,
			})
			return
		}
		slog.Error("chat channel history read failed", append(logger.RequestAttrs(r),
			"error", err, "chat_session_id", uuidToString(sessionID))...)
		writeError(w, http.StatusBadGateway, "failed to read channel history")
		return
	}
	messages := page.Messages
	if messages == nil {
		messages = []channel.HistoryMessage{}
	}
	writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
		ChannelType: page.ChannelType,
		ThreadID:    page.ThreadID,
		Messages:    messages,
		NextCursor:  page.NextCursor,
	})
}

func (h *Handler) writeNoChannelIntegration(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
		Messages: []channel.HistoryMessage{},
		Note:     "No chat channel integration is configured on this server.",
	})
}

// historyOptionsFrom reads the shared ?limit / ?before paging params.
func historyOptionsFrom(r *http.Request) channel.HistoryOptions {
	return channel.HistoryOptions{
		Limit:  parseHistoryLimit(r.URL.Query().Get("limit")),
		Before: r.URL.Query().Get("before"),
	}
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

// chatHistoryBindingQuerier is the one query the dispatcher needs: the session's
// channel binding, by session id alone, to learn which channel type it is bound
// to. *db.Queries satisfies it.
type chatHistoryBindingQuerier interface {
	GetChannelChatSessionBindingBySessionAny(ctx context.Context, chatSessionID pgtype.UUID) (db.ChannelChatSessionBinding, error)
}

// chatHistoryRouter dispatches an agent's history read to the reader for the
// session's channel type. It is the one seam that lets the unified `multica chat`
// commands serve every platform: it looks up the session's binding (by session
// alone), then delegates to that channel type's registered reader. A session
// with no binding, or one bound to a channel with no reader wired, resolves to
// channel.ErrNoChannelSession, which the handler answers as an empty read + note.
type chatHistoryRouter struct {
	q       chatHistoryBindingQuerier
	readers map[string]ChatChannelHistoryReader
}

// NewChatHistoryRouter builds the channel-type dispatcher over the per-platform
// readers (keyed by channel_type: "slack", "feishu"). Returns nil when no reader
// is registered, so callers can leave Handler.ChatHistory nil ("no channel
// integration configured") instead of wiring an empty router.
func NewChatHistoryRouter(q chatHistoryBindingQuerier, readers map[string]ChatChannelHistoryReader) ChatChannelHistoryReader {
	if len(readers) == 0 {
		return nil
	}
	return &chatHistoryRouter{q: q, readers: readers}
}

func (rt *chatHistoryRouter) readerFor(ctx context.Context, chatSessionID pgtype.UUID) (ChatChannelHistoryReader, error) {
	binding, err := rt.q.GetChannelChatSessionBindingBySessionAny(ctx, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, channel.ErrNoChannelSession
		}
		return nil, err
	}
	reader, ok := rt.readers[binding.ChannelType]
	if !ok {
		return nil, channel.ErrNoChannelSession
	}
	return reader, nil
}

func (rt *chatHistoryRouter) ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	reader, err := rt.readerFor(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	return reader.ChannelOverview(ctx, chatSessionID, opts)
}

func (rt *chatHistoryRouter) Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	reader, err := rt.readerFor(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	return reader.Thread(ctx, chatSessionID, threadID, opts)
}
