// This file defines channel turn request and client contracts.
package turn

import (
	"context"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
)

// Request is the stable input for channel agent turns.
type Request struct {
	WorkspaceID      string
	IssuePrefix      string
	DefaultProjectID string
	// AgentID, when non-empty, forces channel turns to use that agent only.
	AgentID         string
	Text            string
	Channel         string
	ConnectionID    string
	ChatID          string
	ChatType        string
	SenderID        string
	SenderName      string
	InboundEventID  string
	SourceHint      chaction.Source
	ContextIssueKey string
	ContextMode     string

	ThreadID         string
	QuotedMessageID  string
	QuotedText       string
	ReplyToMessageID string

	// ContextEntities carries recent entity references from channel messages
	// in this conversation and sender scope.
	ContextEntities []channelconversation.EntityRef
	// ExplicitEntities carries entities derived from explicit platform signals.
	ExplicitEntities []channelconversation.EntityRef

	// PendingAction carries the last active clarification state from this
	// sender's channel turn history.
	PendingAction *PendingAction
}

// AgentClient starts channel agent turns and reads their task result.
type AgentClient interface {
	StartAgentTurn(ctx context.Context, req Request) (string, error)
	ParseAgentTurnResult(ctx context.Context, taskID string) (string, bool, error)
}
