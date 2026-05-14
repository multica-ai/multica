package outbound

import (
	"context"
	"log/slog"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// CleanupTickInterval is how often the cleanup worker runs.
	// All three cleanup tasks run on the same hourly tick.
	CleanupTickInterval = 1 * time.Hour
)

// CleanupWorker periodically removes stale rows from channel-related tables:
//   - channel_inbound_event_dedup: entries older than 7 days
//   - channel_bind_token: consumed or expired tokens older than 1 day
//
// TOCTOU note (T4 Rec-3): The DELETE statements in this worker are
// idempotent and operate on non-overlapping time windows. Between the
// SELECT (if any) and DELETE, new rows may arrive or old rows may be
// deleted by a concurrent cleanup on another replica. Both cases are
// safe: the DELETE simply affects zero rows, and duplicate deletes are
// no-ops. No advisory lock is needed.
type CleanupWorker struct {
	queries *db.Queries
}

// NewCleanupWorker creates a CleanupWorker.
func NewCleanupWorker(queries *db.Queries) *CleanupWorker {
	return &CleanupWorker{queries: queries}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
func (w *CleanupWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(CleanupTickInterval)
	defer ticker.Stop()

	// Run once immediately on startup so stale data from downtime is
	// cleaned up without waiting for the first tick.
	w.runAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runAll(ctx)
		}
	}
}

func (w *CleanupWorker) runAll(ctx context.Context) {
	w.cleanupDedup(ctx)
	w.cleanupBindTokens(ctx)
}

func (w *CleanupWorker) cleanupDedup(ctx context.Context) {
	if err := w.queries.CleanupOldInboundEventDedup(ctx); err != nil {
		slog.Error("cleanup worker: dedup cleanup failed", "error", err)
		return
	}
	slog.Debug("cleanup worker: dedup cleanup done")
}

func (w *CleanupWorker) cleanupBindTokens(ctx context.Context) {
	if err := w.queries.CleanupExpiredBindTokens(ctx); err != nil {
		slog.Error("cleanup worker: bind token cleanup failed", "error", err)
		return
	}
	slog.Debug("cleanup worker: bind token cleanup done")
}
