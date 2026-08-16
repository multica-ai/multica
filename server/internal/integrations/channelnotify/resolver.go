package channelnotify

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type targetQueries interface {
	FindIssueChannelNotificationTarget(context.Context, db.FindIssueChannelNotificationTargetParams) (db.FindIssueChannelNotificationTargetRow, error)
}

// Resolver chooses an issue-participating Bot whose exact installation also
// contains the recipient's external identity binding.
type Resolver struct {
	queries targetQueries
}

func NewResolver(queries targetQueries) *Resolver {
	return &Resolver{queries: queries}
}

func (r *Resolver) Resolve(ctx context.Context, notification Notification, channelType channel.Type) (Target, bool, error) {
	row, err := r.queries.FindIssueChannelNotificationTarget(ctx, db.FindIssueChannelNotificationTargetParams{
		WorkspaceID: notification.WorkspaceID,
		RecipientID: notification.RecipientID,
		IssueID:     notification.IssueID,
		ChannelType: string(channelType),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, false, err
	}

	return Target{
		InstallationID: row.InstallationID,
		AgentID:        row.AgentID,
		ChannelType:    channel.Type(row.ChannelType),
		ChannelUserID:  row.ChannelUserID,
		WorkspaceSlug:  row.WorkspaceSlug,
	}, true, nil
}
