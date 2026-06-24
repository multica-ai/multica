package wecom

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type ChatSessionService interface {
	EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (pgtype.UUID, error)
	AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error)
}

type EnsureChatSessionParams struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	AgentID        pgtype.UUID
	ChatID         ChatID
	ChatType       ChatType
	Sender         pgtype.UUID
}

type AppendUserMessageParams struct {
	ChatSessionID  pgtype.UUID
	Sender         pgtype.UUID
	Body           string
	CommandBody    string
	InstallationID pgtype.UUID
	WecomMessageID string
	ClaimToken     pgtype.UUID
}

type AppendResult struct {
	IssueCommand *IssueCommand
	DedupMarked  bool
}

type IssueCommand struct {
	Title       string
	Description string
}

type AuditLogger interface {
	RecordDrop(ctx context.Context, p AuditDropParams) error
}

type AuditDropParams struct {
	InstallationID pgtype.UUID
	ChatID         ChatID
	EventType      string
	WecomMessageID string
	Reason         DropReason
}
