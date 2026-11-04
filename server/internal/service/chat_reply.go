package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Chat replies: the Secretary writes an assistant message into a chat session
// from a run that is not a chat turn (`multica chat reply`). Only the workspace's
// Secretary, only from a live run linked to the session, only when the run's
// originator owns the session.

const (
	// ChatReplyMaxRunes caps one reply. A reply is a chat message, not a document;
	// longer material belongs in a brief or an issue document.
	ChatReplyMaxRunes = 20000
	// ChatReplyPerMinuteLimit caps assistant messages per session per minute.
	ChatReplyPerMinuteLimit = 10
)

var (
	ErrChatReplyNotSecretary    = errors.New("only the workspace's secretary agent may reply in chat")
	ErrChatReplySessionNotFound = errors.New("chat session not found in this workspace")
	ErrChatReplyWrongSession    = errors.New("the secretary can reply only in a chat session it is the agent of")
	ErrChatReplyArchived        = errors.New("chat session is archived; the boss can no longer see replies here, so report through the inbox instead")
	ErrChatReplyRunNotLive      = errors.New("the run has finished; a reply must come from a live run")
	ErrChatReplyNotLinked       = errors.New("the run is not linked to this chat session: its chat_session_id must name this session")
	ErrChatReplyNotOwner        = errors.New("the run's originator is not the owner of this chat session")
	ErrChatReplyEmpty           = errors.New("content is required")
	ErrChatReplyTooLong         = fmt.Errorf("content must be at most %d characters", ChatReplyMaxRunes)
	ErrChatReplyRateLimited     = fmt.Errorf("at most %d chat replies per session per minute; wait a minute and try again", ChatReplyPerMinuteLimit)
)

// ChatReplyFacts is everything a reply is judged on, as canonical UUID text
// ("" when absent), so the rule can be tested without a database.
type ChatReplyFacts struct {
	IsSecretary      bool
	SessionAgentID   string
	SessionOwnerID   string
	SessionArchived  bool
	RunAgentID       string
	RunStatus        string
	RunChatSessionID string
	RunOriginatorID  string
	SessionID        string
}

// CheckChatReply returns nil when the facts allow the run to reply in the session.
// It checks authority first, then state, then linkage, so the error names the
// first reason a reply is refused.
func CheckChatReply(f ChatReplyFacts) error {
	switch {
	case !f.IsSecretary:
		return ErrChatReplyNotSecretary
	case f.SessionAgentID != f.RunAgentID:
		return ErrChatReplyWrongSession
	case f.SessionArchived:
		return ErrChatReplyArchived
	case isTerminalRunStatus(f.RunStatus):
		return ErrChatReplyRunNotLive
	case f.RunChatSessionID != f.SessionID:
		return ErrChatReplyNotLinked
	case f.RunOriginatorID == "" || f.RunOriginatorID != f.SessionOwnerID:
		return ErrChatReplyNotOwner
	}
	return nil
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// ValidateChatReplyContent trims the content and enforces its size. The returned
// text is what gets stored.
func ValidateChatReplyContent(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", ErrChatReplyEmpty
	}
	if utf8.RuneCountInString(trimmed) > ChatReplyMaxRunes {
		return "", ErrChatReplyTooLong
	}
	return trimmed, nil
}

// ChatReplyWindow is the sliding-window cap. It is a pure decision on the count
// the caller read, so the rule is testable without a clock or a database.
func ChatReplyWindow(recent int64) error {
	if recent >= ChatReplyPerMinuteLimit {
		return ErrChatReplyRateLimited
	}
	return nil
}

// ChatReplyParams is one reply attempt. AgentID and RunID are the authenticated
// actor and the X-Task-ID run; the handler resolves both.
type ChatReplyParams struct {
	WorkspaceID pgtype.UUID
	SessionID   pgtype.UUID
	AgentID     pgtype.UUID
	RunID       pgtype.UUID
	Content     string
	Now         time.Time
}

// ChatReplyResult is the stored message and the session owner, who is the only
// member the chat event may reach.
type ChatReplyResult struct {
	Message db.ChatMessage
	OwnerID pgtype.UUID
}

// CreateChatReply writes the Secretary's reply and returns it. The message has no
// task_id: it is not the run's turn outcome, so it must not be mistaken for the
// chat turn's own assistant row. Unread state follows from the new created_at
// being after the session's read cursor.
func CreateChatReply(ctx context.Context, tx TxStarter, q *db.Queries, p ChatReplyParams) (ChatReplyResult, error) {
	content, err := ValidateChatReplyContent(p.Content)
	if err != nil {
		return ChatReplyResult{}, err
	}

	session, err := q.GetChatSessionInWorkspace(ctx, db.GetChatSessionInWorkspaceParams{ID: p.SessionID, WorkspaceID: p.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatReplyResult{}, ErrChatReplySessionNotFound
	}
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("load chat session: %w", err)
	}
	run, err := q.GetAgentTask(ctx, p.RunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatReplyResult{}, ErrChatReplyRunNotLive
	}
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("load run: %w", err)
	}
	ws, err := q.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("load workspace: %w", err)
	}

	facts := ChatReplyFacts{
		IsSecretary:      ws.SecretaryAgentID.Valid && ws.SecretaryAgentID == p.AgentID,
		SessionAgentID:   util.UUIDToString(session.AgentID),
		SessionOwnerID:   util.UUIDToString(session.CreatorID),
		SessionArchived:  session.Status == "archived",
		RunAgentID:       util.UUIDToString(p.AgentID),
		RunStatus:        run.Status,
		RunChatSessionID: util.UUIDToString(run.ChatSessionID),
		RunOriginatorID:  util.UUIDToString(run.OriginatorUserID),
		SessionID:        util.UUIDToString(session.ID),
	}
	if err := CheckChatReply(facts); err != nil {
		return ChatReplyResult{}, err
	}

	recent, err := q.CountRecentChatAssistantMessages(ctx, db.CountRecentChatAssistantMessagesParams{
		ChatSessionID: session.ID,
		Since:         pgtype.Timestamptz{Time: p.Now.Add(-time.Minute), Valid: true},
	})
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("count recent replies: %w", err)
	}
	if err := ChatReplyWindow(recent); err != nil {
		return ChatReplyResult{}, err
	}

	dbTx, err := tx.Begin(ctx)
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("begin chat reply: %w", err)
	}
	defer dbTx.Rollback(ctx)
	qtx := q.WithTx(dbTx)
	msg, err := createAssistantChatMessage(ctx, qtx, db.CreateChatMessageParams{
		ID:            dbid.NewV7(),
		ChatSessionID: session.ID,
		Role:          "assistant",
		Content:       content,
	})
	if err != nil {
		return ChatReplyResult{}, fmt.Errorf("create chat reply: %w", err)
	}
	if err := dbTx.Commit(ctx); err != nil {
		return ChatReplyResult{}, fmt.Errorf("commit chat reply: %w", err)
	}
	return ChatReplyResult{Message: msg, OwnerID: session.CreatorID}, nil
}
