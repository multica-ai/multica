package audit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func WriteCRAttemptAuditComment(ctx context.Context, qtx *db.Queries, issue db.Issue, attempt db.CrReviewAttempt) error {
	return WriteCRAttemptAuditCommentByID(ctx, qtx, issue.ID, issue.WorkspaceID, attempt)
}

func WriteCRAttemptAuditCommentByID(ctx context.Context, qtx *db.Queries, issueID, workspaceID pgtype.UUID, attempt db.CrReviewAttempt) error {
	findings := int(attempt.FindingsCount)
	durationSecs := 0
	if attempt.ClosedAt.Valid && !attempt.StartedAt.Time.IsZero() {
		durationSecs = int(attempt.ClosedAt.Time.Sub(attempt.StartedAt.Time).Seconds())
	}
	outcome := ""
	if attempt.Outcome.Valid {
		outcome = attempt.Outcome.String
	}
	body := fmt.Sprintf(
		"<!-- sidecar-cr-attempt -->\n\nRound %d closed: **%s**\nFindings: %d\nDuration: %ds\n",
		attempt.CrRound, outcome, findings, durationSecs,
	)
	_, err := qtx.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: false},
		Content:     body,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	return err
}
