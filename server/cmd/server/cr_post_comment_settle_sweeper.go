package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	githubintegration "github.com/multica-ai/multica/server/internal/integrations/github"
	"github.com/multica-ai/multica/server/internal/util"
	dbtx "github.com/multica-ai/multica/server/pkg/db"
	audit "github.com/multica-ai/multica/server/pkg/db/audit"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var crPostCommentSettleSecs = envSeconds("MULTICA_CR_POST_COMMENT_SETTLE_SECS", 120)
var evaluateCRPredicate = githubintegration.EvaluatePredicate
var newCRPredicateClient = func(auth *githubintegration.AppAuth, installationID int64) githubintegration.PRReviewClient {
	return githubintegration.NewGitHubAPIClient(auth, installationID)
}

func runCRPostCommentSettleSweeper(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus, auth *githubintegration.AppAuth) {
	if crSweepInterval <= 0 {
		slog.Info("cr-post-comment-settle: disabled (MULTICA_CR_SWEEP_INTERVAL_SECS=0)")
		return
	}
	if auth == nil {
		slog.Warn("cr-post-comment-settle: AppAuth nil; disabled")
		return
	}
	slog.Info("cr-post-comment-settle: starting",
		"interval_secs", crSweepInterval,
		"settle_secs", crPostCommentSettleSecs,
	)
	ticker := time.NewTicker(time.Duration(crSweepInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepPendingCommentedApprovals(ctx, queries, txStarter, bus, auth)
		}
	}
}

func sweepPendingCommentedApprovals(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus, auth *githubintegration.AppAuth) {
	rows, err := queries.ListPendingCommentedApprovals(ctx, int32(crPostCommentSettleSecs))
	if err != nil {
		slog.Warn("cr-post-comment-settle: query failed", "error", err)
		return
	}
	for _, row := range rows {
		promoteOrBounceCommentedClean(ctx, queries, txStarter, bus, auth, row)
	}
}

func promoteOrBounceCommentedClean(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus, auth *githubintegration.AppAuth, row db.ListPendingCommentedApprovalsRow) {
	binding, err := queries.GetRepoBindingByRepo(ctx, row.PrRepo)
	if err != nil {
		slog.Warn("cr-post-comment-settle: binding lookup failed", "repo", row.PrRepo, "error", err)
		return
	}
	owner, repo, ok := splitOwnerRepo(row.PrRepo)
	if !ok {
		slog.Warn("cr-post-comment-settle: invalid repo full name", "repo", row.PrRepo)
		return
	}
	apiClient := newCRPredicateClient(auth, binding.InstallationID)
	noOpenChanges, noUnresolved, err := evaluateCRPredicate(ctx, apiClient, owner, repo, int(row.PrNumber), binding.CrBotUsername)
	if err != nil {
		slog.Warn("cr-post-comment-settle: predicate failed; deferring", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}

	newStatus := githubintegration.StatusResolving
	outcome := "completed_with_findings"
	reason := "commented_clean_then_dirty_at_settle"
	action := "review_comments_unresolved"
	if noOpenChanges && noUnresolved {
		newStatus = githubintegration.StatusStaged
		outcome = "completed_clean"
		reason = "commented_clean_settled"
		action = "review_passed"
	}

	tx, err := txStarter.Begin(ctx)
	if err != nil {
		slog.Warn("cr-post-comment-settle: tx begin failed", "error", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)
	if _, err := qtx.UpdateIssueStatusIfCurrent(ctx, db.UpdateIssueStatusIfCurrentParams{
		ID:             row.IssueID,
		ExpectedStatus: githubintegration.StatusCoderabbit,
		NewStatus:      newStatus,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Info("cr-post-comment-settle: issue no longer in coderabbit; skipping",
				"issue", util.UUIDToString(row.IssueID),
				"target_status", newStatus,
			)
			return
		}
		slog.Warn("cr-post-comment-settle: conditional status update failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	closed, err := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
		IssueID:       row.IssueID,
		CrRound:       row.CrRound,
		Outcome:       pgtype.Text{String: outcome, Valid: true},
		OutcomeReason: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		slog.Warn("cr-post-comment-settle: close attempt failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	if err := audit.WriteCRAttemptAuditCommentByID(ctx, qtx, row.IssueID, row.WorkspaceID, closed); err != nil {
		slog.Warn("cr-post-comment-settle: audit comment failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{
		"from":      githubintegration.StatusCoderabbit,
		"to":        newStatus,
		"reason":    reason,
		"pr_url":    row.PrUrl,
		"pr_number": row.PrNumber,
		"pr_repo":   row.PrRepo,
		"cr_round":  row.CrRound,
	})
	if _, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: row.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: row.IssueID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		Action:      action,
		Details:     details,
	}); err != nil {
		slog.Warn("cr-post-comment-settle: activity insert failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("cr-post-comment-settle: tx commit failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"id":             util.UUIDToString(row.IssueID),
			"status":         newStatus,
			"prev":           githubintegration.StatusCoderabbit,
			"prev_status":    githubintegration.StatusCoderabbit,
			"status_changed": true,
			"pr_url":         row.PrUrl,
			"pr_number":      row.PrNumber,
			"pr_repo":        row.PrRepo,
			"source":         "cr_post_comment_settle_sweeper",
			"src_event":      reason,
		},
	})
}

func splitOwnerRepo(full string) (owner, repo string, ok bool) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
