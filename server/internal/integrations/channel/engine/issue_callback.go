package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueCallback is the chat thread an issue reports back to: the platform ids
// of the message that created it, plus the member who sent that message.
type IssueCallback struct {
	WorkspaceID     pgtype.UUID
	IssueID         pgtype.UUID
	InstallationID  pgtype.UUID
	ChannelType     channel.Type
	ChatID          string
	ChatType        channel.ChatType
	ThreadID        string
	AnchorMessageID string
	RegisteredBy    pgtype.UUID
}

// IssueCallbackRegistrar records an issue's callback. Shared across adapters;
// an adapter opts in by setting ResolverSet.IssueCallbacks.
type IssueCallbackRegistrar interface {
	RegisterIssueCallback(ctx context.Context, cb IssueCallback) error
}

// IssueCallbackWriter is the single query DBIssueCallbackRegistrar needs.
// *db.Queries satisfies it.
type IssueCallbackWriter interface {
	CreateIssueChannelCallback(ctx context.Context, arg db.CreateIssueChannelCallbackParams) (db.IssueChannelCallback, error)
}

// DBIssueCallbackRegistrar stores callbacks in issue_channel_callback.
type DBIssueCallbackRegistrar struct{ q IssueCallbackWriter }

func NewDBIssueCallbackRegistrar(q IssueCallbackWriter) *DBIssueCallbackRegistrar {
	return &DBIssueCallbackRegistrar{q: q}
}

func (r *DBIssueCallbackRegistrar) RegisterIssueCallback(ctx context.Context, cb IssueCallback) error {
	if _, err := r.q.CreateIssueChannelCallback(ctx, db.CreateIssueChannelCallbackParams{
		WorkspaceID:     cb.WorkspaceID,
		IssueID:         cb.IssueID,
		InstallationID:  cb.InstallationID,
		ChannelType:     string(cb.ChannelType),
		ChatID:          cb.ChatID,
		ChatType:        string(cb.ChatType),
		ThreadID:        cb.ThreadID,
		AnchorMessageID: cb.AnchorMessageID,
		RegisteredBy:    cb.RegisteredBy,
	}); err != nil {
		return fmt.Errorf("create issue channel callback: %w", err)
	}
	return nil
}

// issueCallbackFor returns the callback an /issue message registers, or false
// when the message lacks the ids a later reply would be addressed by.
func issueCallbackFor(inst ResolvedInstallation, msg channel.InboundMessage, issueID, senderID pgtype.UUID) (IssueCallback, bool) {
	if msg.MessageID == "" || msg.Source.ChatID == "" || !issueID.Valid || !senderID.Valid {
		return IssueCallback{}, false
	}
	return IssueCallback{
		WorkspaceID:     inst.WorkspaceID,
		IssueID:         issueID,
		InstallationID:  inst.ID,
		ChannelType:     msg.Source.ChannelType,
		ChatID:          msg.Source.ChatID,
		ChatType:        msg.Source.ChatType,
		ThreadID:        msg.Source.ThreadID,
		AnchorMessageID: msg.MessageID,
		RegisteredBy:    senderID,
	}, true
}
