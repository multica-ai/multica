package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

const (
	// sourceContextSweepInterval paces snapshot cleanup (MUL-6555). Nothing
	// here is latency-critical: an orphaned object intent only becomes eligible
	// one hour after its capture failed and carries a five-minute retry
	// backoff, and an abandoned capture waits out a 30-day retention window.
	// A minute keeps the drain rate for a fresh backlog close to what the old
	// placement in the 30s runtime tick gave, at two cheap queries per round.
	sourceContextSweepInterval = time.Minute
	// sourceContextSweepBudget bounds one round below the interval so a slow
	// object store delays cleanup instead of stacking rounds. Per-object
	// deadlines live in the service; this is the whole-round backstop.
	sourceContextSweepBudget = 45 * time.Second
	// sourceContextCleanupBatchSize bounds delete attempts per round.
	sourceContextCleanupBatchSize = 50
)

// sourceContextCleaner is the slice of TaskService this sweeper drives, kept as
// an interface so the round can be tested against a stalled object store.
type sourceContextCleaner interface {
	CleanupSourceContextObjectIntents(ctx context.Context, limit int) (int, error)
	CleanupAbandonedSourceContexts(ctx context.Context, limit int32) (int, error)
}

var _ sourceContextCleaner = (*service.TaskService)(nil)

// runSourceContextSweeper reclaims snapshot objects whose capture never
// committed and captures whose retry window has expired.
//
// It deliberately runs on its own goroutine rather than inside the runtime
// sweep tick. Both stages talk to the object store, and a stalled or throttled
// endpoint there must never delay marking runtimes offline, reclaiming
// orphaned tasks, or replaying delegated failure recoveries — those are the
// stages that keep the board truthful within ~180s of a daemon dying.
func runSourceContextSweeper(ctx context.Context, cleaner sourceContextCleaner) {
	ticker := time.NewTicker(sourceContextSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepSourceContextsWithBudget(ctx, cleaner, sourceContextSweepBudget)
		}
	}
}

func sweepSourceContextsWithBudget(ctx context.Context, cleaner sourceContextCleaner, budget time.Duration) {
	sweepCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	if cleaned, err := cleaner.CleanupSourceContextObjectIntents(sweepCtx, sourceContextCleanupBatchSize); err != nil {
		slog.Warn("source context object intent cleanup failed", "error", err)
	} else if cleaned > 0 {
		slog.Info("source context object intent cleanup completed", "count", cleaned)
	}
	if cleaned, err := cleaner.CleanupAbandonedSourceContexts(sweepCtx, sourceContextCleanupBatchSize); err != nil {
		slog.Warn("source context cleanup sweeper failed", "error", err)
	} else if cleaned > 0 {
		slog.Info("source context cleanup sweeper removed abandoned captures", "count", cleaned)
	}
}
