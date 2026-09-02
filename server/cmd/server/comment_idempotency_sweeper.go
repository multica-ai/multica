package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// Comment idempotency rows are replay state, not history. Seven days covers
	// delayed client retries while keeping the table bounded in steady state.
	commentIdempotencyRetention = handler.CommentIdempotencyReplayWindow
	// Cleanup is deliberately off the latency-sensitive comment write path.
	commentIdempotencySweepInterval  = time.Hour
	commentIdempotencySweepBudget    = 5 * time.Second
	commentIdempotencySweepBatchSize = 500
)

type deleteExpiredCommentIdempotencyFunc func(context.Context, pgtype.Timestamptz, int32) (int64, error)

func runCommentIdempotencySweeper(ctx context.Context, h *handler.Handler) {
	runPeriodicSweep(ctx, commentIdempotencySweepInterval, func() {
		if recovered, err := h.ReconcilePendingCommentIdempotency(ctx, commentIdempotencySweepBatchSize); err != nil {
			slog.Warn("comment idempotency recovery failed", "recovered", recovered, "error", err)
		} else if recovered > 0 {
			slog.Info("comment idempotency recovery completed", "count", recovered)
		}
		deleted, err := sweepCommentIdempotencyWithBudget(
			ctx,
			func(ctx context.Context, cutoff pgtype.Timestamptz, maxRows int32) (int64, error) {
				return h.Queries.DeleteExpiredCommentIdempotency(ctx, db.DeleteExpiredCommentIdempotencyParams{
					CreatedAt: cutoff,
					Limit:     maxRows,
				})
			},
			time.Now().UTC(),
			commentIdempotencyRetention,
			commentIdempotencySweepBatchSize,
		)
		if err != nil {
			slog.Warn("comment idempotency sweeper failed", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("comment idempotency sweeper removed expired replay keys", "count", deleted)
		}
	})
}

func sweepCommentIdempotencyWithBudget(
	ctx context.Context,
	deleteExpired deleteExpiredCommentIdempotencyFunc,
	now time.Time,
	retention time.Duration,
	maxRows int32,
) (int64, error) {
	sweepCtx, cancel := context.WithTimeout(ctx, commentIdempotencySweepBudget)
	defer cancel()

	cutoff := pgtype.Timestamptz{Time: now.Add(-retention), Valid: true}
	return deleteExpired(sweepCtx, cutoff, maxRows)
}
