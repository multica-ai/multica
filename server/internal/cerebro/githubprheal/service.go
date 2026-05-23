package githubprheal

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Service struct {
	cerebro *cerebrodb.Queries
	queries *db.Queries
}

func New(cerebro *cerebrodb.Queries, queries *db.Queries) *Service {
	return &Service{cerebro: cerebro, queries: queries}
}

func (s *Service) EnsurePullRequestLinksForIssue(ctx context.Context, issueID, workspaceID pgtype.UUID, identifier string) {
	if s == nil || s.cerebro == nil || s.queries == nil || identifier == "" {
		return
	}
	matching, err := s.cerebro.ListUnlinkedPullRequestsMatchingIssueIdentifier(ctx, cerebrodb.ListUnlinkedPullRequestsMatchingIssueIdentifierParams{
		IssueID:         issueID,
		WorkspaceID:     workspaceID,
		IdentifierRegex: IdentifierBoundaryRegex(identifier),
	})
	if err != nil {
		slog.Warn("github: self-heal pr links failed", "err", err, "issue_id", util.UUIDToString(issueID))
		return
	}
	for _, pr := range matching {
		if err := s.queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
			IssueID:       issueID,
			PullRequestID: pr.ID,
			LinkedByType:  pgtype.Text{String: "system", Valid: true},
			LinkedByID:    pgtype.UUID{},
		}); err != nil {
			slog.Warn("github: self-heal pr link failed", "err", err, "issue_id", util.UUIDToString(issueID), "pr_id", util.UUIDToString(pr.ID))
		}
	}
}

func IdentifierBoundaryRegex(identifier string) string {
	return `(^|[^[:alnum:]])` + regexp.QuoteMeta(identifier) + `([^[:alnum:]]|$)`
}
