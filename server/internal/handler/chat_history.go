package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
//
// A chat session that is NOT backed by an IM channel has no channel overview to
// read — its history is the chat_message table itself (web chat, Feishu). For
// those, this endpoint falls back to returning that transcript, so the agent can
// reconstruct the conversation after a lost resume instead of being told nothing
// is readable (see the continuity-notice split in execenv).
func (h *Handler) GetChatChannelHistory(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.chatHistorySession(w, r)
	if !ok {
		return
	}
	var page channel.HistoryPage
	var err error
	if h.SlackHistory == nil {
		// No Slack integration configured, so the session cannot be Slack-backed:
		// serve the stored transcript directly. Without this a no-Slack deployment
		// — the exact one this feature targets — would dead-end on a "no channel
		// integration" note and never reach the transcript.
		page, err = h.chatMessageHistory(r, sessionID)
	} else {
		page, err = h.SlackHistory.ChannelOverview(r.Context(), sessionID, historyOptionsFrom(r))
		if errors.Is(err, slack.ErrNoSlackSession) {
			// Not Slack-backed: read the session's own stored transcript instead.
			page, err = h.chatMessageHistory(r, sessionID)
		}
	}
	h.respondChatHistory(w, r, sessionID, page, err)
}

// chatMessageHistory reads a chat session's own stored transcript (chat_message)
// as a channel.HistoryPage, oldest-first, honoring the shared ?limit / ?before
// paging contract. It backs `multica chat history` for sessions with no IM
// channel (web chat, Feishu, WeCom, DingTalk), whose history lives only in
// Multica — there is no platform to read back. It pages through the same
// (created_at, id) cursor the frontend's message list uses, so an agent can walk
// a long session back without re-reading the recent window each time.
func (h *Handler) chatMessageHistory(r *http.Request, sessionID pgtype.UUID) (channel.HistoryPage, error) {
	limit := clampTranscriptLimit(parseHistoryLimit(r.URL.Query().Get("limit")))
	beforeCreatedAt, beforeID := parseTranscriptCursor(r.URL.Query().Get("before"))
	messages, err := h.Queries.ListChatMessagesPage(r.Context(), db.ListChatMessagesPageParams{
		ChatSessionID:   sessionID,
		Limit:           int32(limit),
		BeforeCreatedAt: beforeCreatedAt,
		BeforeID:        beforeID,
	})
	if err != nil {
		return channel.HistoryPage{}, err
	}
	// ListChatMessagesPage returns newest-first; the channel contract is
	// oldest-first, so emit the rows in reverse.
	out := make([]channel.HistoryMessage, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		role := channel.HistoryRoleUser
		if m.Role == "assistant" {
			role = channel.HistoryRoleAssistant
		}
		out = append(out, channel.HistoryMessage{
			ID:     uuidToString(m.ID),
			Role:   role,
			Text:   m.Content,
			Author: transcriptAuthor(role),
			TS:     m.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		})
	}
	// Name the platform the transcript came from. HistoryPage.ChannelType is
	// documented as empty ONLY for a session bound to no channel, so leaving it
	// unset here would tell a Feishu/WeCom/DingTalk agent it is in a web-only
	// chat — and would disagree with the empty-read path below, which already
	// reports the bound platform for the very same session.
	channelType, err := h.sessionChannelType(r.Context(), sessionID)
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("%w: %w", errChannelBindingRead, err)
	}
	page := channel.HistoryPage{ChannelType: channelType, Messages: out}
	// Advertise a cursor when a full page came back, so the agent can page to
	// older messages (mirrors the Slack reader's "more may exist" signal).
	if len(messages) == limit && len(out) > 0 {
		oldest := messages[len(messages)-1]
		page.NextCursor = transcriptCursor(oldest.CreatedAt.Time, oldest.ID)
	}
	return page, nil
}

// Transcript paging bounds, mirroring the Slack reader's clamp so an agent
// cannot dump a long session's whole transcript into its context.
const (
	defaultTranscriptLimit = 30
	maxTranscriptLimit     = 50
)

func clampTranscriptLimit(n int) int {
	if n <= 0 {
		return defaultTranscriptLimit
	}
	if n > maxTranscriptLimit {
		return maxTranscriptLimit
	}
	return n
}

// transcriptCursor encodes a (created_at, id) pair into the opaque ?before
// cursor the channel contract uses. The transcript pages by the same
// (created_at, id) tuple as the frontend's message list, so the cursor must
// carry both halves; RFC3339Nano keeps the timestamp lossless and readable to
// an agent that inspects it.
func transcriptCursor(createdAt time.Time, id pgtype.UUID) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "|" + uuidToString(id)
}

// parseTranscriptCursor splits a ?before cursor back into the (created_at, id)
// tuple ListChatMessagesPage pages by. A missing or malformed cursor returns
// zero values so the read starts at the most recent messages.
func parseTranscriptCursor(before string) (pgtype.Timestamptz, pgtype.UUID) {
	ts, id, ok := strings.Cut(before, "|")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	uid, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, uid
}

// transcriptAuthor labels a chat_message row with the channel vocabulary
// ("Bot" / "User") rather than the raw role string, so an agent reading the
// transcript sees the same author kinds as the Slack path instead of literal
// "user" / "assistant".
func transcriptAuthor(role channel.HistoryRole) string {
	if role == channel.HistoryRoleAssistant {
		return "Bot"
	}
	return "User"
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
	if h.SlackHistory == nil {
		h.writeNoChannelIntegration(w)
		return
	}
	threadID := r.URL.Query().Get("id")
	page, err := h.SlackHistory.Thread(r.Context(), sessionID, threadID, historyOptionsFrom(r))
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
		if errors.Is(err, slack.ErrNoSlackSession) {
			// One read of the binding, two derived fields. Reading it twice
			// lets an archive land between them and produce a response whose
			// channel_type names a platform while its note says there is no
			// channel.
			channelType, bindingErr := h.sessionChannelType(r.Context(), sessionID)
			if bindingErr != nil {
				slog.Error("chat session channel binding read failed", append(logger.RequestAttrs(r),
					"error", bindingErr, "chat_session_id", uuidToString(sessionID))...)
				writeError(w, http.StatusInternalServerError, "failed to read chat session channel binding")
				return
			}
			writeJSON(w, http.StatusOK, ChatChannelHistoryResponse{
				ChannelType: channelType,
				Messages:    []channel.HistoryMessage{},
				Note:        noHistoryNote(channelType),
			})
			return
		}
		if errors.Is(err, errChannelBindingRead) {
			slog.Error("chat session channel binding read failed", append(logger.RequestAttrs(r),
				"error", err, "chat_session_id", uuidToString(sessionID))...)
			writeError(w, http.StatusInternalServerError, "failed to read chat session channel binding")
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

// noHistoryNote explains an empty read in terms the agent can act on. It is a
// pure function of the channel type its caller already resolved, so the note
// and the response's channel_type cannot disagree.
//
// The reader is Slack-only, so every other platform lands here — and the note
// said "this conversation is not connected to a chat channel", which for a
// WeCom, Lark or DingTalk session is simply false. An agent told it is in a
// web-only conversation reasons differently about who can see its answer than
// one told it is in a group whose backlog it cannot read, and that is a
// difference worth not lying about.
func noHistoryNote(channelType string) string {
	if channelType == "" {
		return "This conversation is not connected to a chat channel, so there is no channel history to read."
	}
	return "This conversation is on " + channelType + ", whose backlog this server cannot read. You can see the messages addressed to you in this session, but not the rest of the room."
}

// errChannelBindingRead marks a transcript read whose MESSAGES were fetched but
// whose channel binding could not be. It is not an upstream channel failure, so
// respondChatHistory answers it like the empty-read path answers the same
// failure — one status code for one cause, whether or not the session happened
// to have messages.
var errChannelBindingRead = errors.New("chat session channel binding read failed")

// sessionChannelType names the platform behind a session, or "" when there is
// none. Channel-agnostic on purpose: a per-platform lookup here would go blind
// the next time a channel is added, which is exactly how the note above came
// to be wrong.
//
// Only "no such row" means "no channel". Any other failure is us being unable
// to tell, and answering "" there hands the agent the very note this change
// removes — a WeCom or Lark session told it is web-only, on a 200, because a
// connection blipped. The caller reports that rather than guessing, the same
// way the archive path refuses to guess about the same read.
func (h *Handler) sessionChannelType(ctx context.Context, sessionID pgtype.UUID) (string, error) {
	if h.Queries == nil || !sessionID.Valid {
		return "", nil
	}
	binding, err := h.Queries.GetChannelChatSessionBindingBySessionAny(ctx, sessionID)
	switch {
	case err == nil:
		return binding.ChannelType, nil
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	default:
		return "", err
	}
}
