package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// CEREBRO-PATCH(github-pr-card-self-heal-hook): JEH-1919 keeps PR-card
// link healing outside github.go; router wires the cerebro implementation.
type PullRequestLinkHealer interface {
	EnsurePullRequestLinksForIssue(ctx context.Context, issueID, workspaceID pgtype.UUID, identifier string)
}
