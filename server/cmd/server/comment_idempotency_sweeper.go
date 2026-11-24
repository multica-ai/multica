package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const commentIdempotencySweepInterval = time.Hour

// runCommentIdempotencySweeper deletes comment idempotency receipts once they
// are past the retry window. Every replica may run it; the delete is safe to
// repeat.
func runCommentIdempotencySweeper(ctx context.Context, queries *db.Queries) {
	ticker := time.NewTicker(commentIdempotencySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, time.Minute)
			if n, err := handler.SweepCommentIdempotencyReceipts(sweepCtx, queries, time.Now()); err != nil {
				slog.Warn("comment idempotency sweep failed", "error", err)
			} else if n > 0 {
				slog.Debug("comment idempotency sweep removed receipts", "count", n)
			}
			cancel()
		}
	}
}
