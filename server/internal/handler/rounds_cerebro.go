package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
)

// RoundReplyObserver marks a snapshot item handled without changing normal
// comment triggers. The implementation lives in the cerebro rounds package.
type RoundReplyObserver interface {
	ObserveMemberReply(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error
}

func observeRoundReply(ctx context.Context, observer RoundReplyObserver, workspaceID, issueID, ownerID pgtype.UUID) {
	if observer == nil {
		return
	}
	if err := observer.ObserveMemberReply(ctx, workspaceID, issueID, ownerID); err != nil {
		slog.Warn("round reply observation failed", "issue_id", issueID, "error", err)
	}
}
