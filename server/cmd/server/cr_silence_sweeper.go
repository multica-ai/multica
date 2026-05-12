package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	githubintegration "github.com/multica-ai/multica/server/internal/integrations/github"
	"github.com/multica-ai/multica/server/internal/util"
	dbtx "github.com/multica-ai/multica/server/pkg/db"
	audit "github.com/multica-ai/multica/server/pkg/db/audit"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var crNoReviewSecs = envSeconds("MULTICA_CR_NO_REVIEW_SECS", 1800)

func runCRSilenceSweeper(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus) {
	if crSweepInterval <= 0 {
		slog.Info("cr-silence: disabled (MULTICA_CR_SWEEP_INTERVAL_SECS=0)")
		return
	}
	slog.Info("cr-silence: starting", "interval_secs", crSweepInterval, "no_review_secs", crNoReviewSecs)
	ticker := time.NewTicker(time.Duration(crSweepInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepSilent(ctx, queries, txStarter, bus)
		}
	}
}

func sweepSilent(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus) {
	rows, err := queries.ListSilentCoderabbitIssues(ctx, int32(crNoReviewSecs))
	if err != nil {
		slog.Warn("cr-silence: query failed", "error", err)
		return
	}
	for _, row := range rows {
		handleSilent(ctx, queries, txStarter, bus, row)
	}
}

func handleSilent(ctx context.Context, queries *db.Queries, txStarter dbtx.TxStarter, bus *events.Bus, row db.ListSilentCoderabbitIssuesRow) {
	newStatus := githubintegration.StatusStaged
	outcome := "completed_clean"
	reason := "cr_not_required_silent"
	sidecarBody := ""
	if row.CrRequired {
		newStatus = githubintegration.StatusBlocked
		outcome = "silent_total"
		reason = "no_cr_signal_within_no_review_secs"
		sidecarBody = "<!-- sidecar-block -->\n\nreason: cr_silent_total\n"
	}

	tx, err := txStarter.Begin(ctx)
	if err != nil {
		slog.Warn("cr-silence: tx begin failed", "error", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	updated, err := qtx.UpdateIssueStatusIfCurrent(ctx, db.UpdateIssueStatusIfCurrentParams{
		ID:             row.IssueID,
		ExpectedStatus: githubintegration.StatusCoderabbit,
		NewStatus:      newStatus,
	})
	if err != nil {
		slog.Warn("cr-silence: status update failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}

	prURL := ""
	if iss, err := qtx.GetIssue(ctx, row.IssueID); err == nil && iss.PrUrl.Valid {
		prURL = iss.PrUrl.String
	}
	if _, err := qtx.UpsertCRReviewAttempt(ctx, db.UpsertCRReviewAttemptParams{
		IssueID:     row.IssueID,
		WorkspaceID: row.WorkspaceID,
		CrRound:     row.CrRound,
		PrUrl:       prURL,
		HeadSha:     "",
	}); err != nil {
		slog.Warn("cr-silence: upsert attempt failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	closed, err := qtx.CloseCRReviewAttempt(ctx, db.CloseCRReviewAttemptParams{
		IssueID:       row.IssueID,
		CrRound:       row.CrRound,
		Outcome:       pgtype.Text{String: outcome, Valid: true},
		OutcomeReason: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		slog.Warn("cr-silence: close attempt failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	if sidecarBody != "" {
		if _, err := qtx.CreateComment(ctx, db.CreateCommentParams{
			IssueID:     row.IssueID,
			WorkspaceID: row.WorkspaceID,
			AuthorType:  "system",
			AuthorID:    pgtype.UUID{Valid: false},
			Content:     sidecarBody,
			Type:        "system",
			ParentID:    pgtype.UUID{Valid: false},
		}); err != nil {
			slog.Warn("cr-silence: sidecar comment failed", "issue", util.UUIDToString(row.IssueID), "error", err)
			return
		}
	} else if err := audit.WriteCRAttemptAuditCommentByID(ctx, qtx, row.IssueID, row.WorkspaceID, closed); err != nil {
		slog.Warn("cr-silence: audit comment failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}

	details, _ := json.Marshal(map[string]any{
		"from":        githubintegration.StatusCoderabbit,
		"to":          newStatus,
		"reason":      reason,
		"cr_required": row.CrRequired,
		"cr_round":    row.CrRound,
	})
	if _, err := qtx.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: row.WorkspaceID,
		IssueID:     pgtype.UUID{Bytes: row.IssueID.Bytes, Valid: true},
		ActorType:   pgtype.Text{String: "system", Valid: true},
		Action:      "cr_silence_handled",
		Details:     details,
	}); err != nil {
		slog.Warn("cr-silence: activity insert failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("cr-silence: tx commit failed", "issue", util.UUIDToString(row.IssueID), "error", err)
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"id":             util.UUIDToString(updated.ID),
			"status":         updated.Status,
			"prev":           githubintegration.StatusCoderabbit,
			"prev_status":    githubintegration.StatusCoderabbit,
			"status_changed": true,
			"source":         "cr_silence_sweeper",
			"src_event":      reason,
		},
	})
}
