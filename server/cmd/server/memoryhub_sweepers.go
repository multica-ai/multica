// MemoryHub background workers (ALL-16). Bound to sweepCtx in main.go so they
// wind down alongside the other long-running workers.
//
// CompensationSweeper drives the durable compensation rows (§11); the review
// scheduler is the UNIQUE independent-review producer (V5-7). Both reuse the
// router's MemoryHub remote boundary so a single config knob controls every
// memoryhub background path.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// runMemoryHubSweepers starts the compensation sweeper and the review
// scheduler on ctx. Each loop iteration is one pass; transient errors are
// logged and the loop retries after the configured interval.
func runMemoryHubSweepers(ctx context.Context, queries *db.Queries, tx service.TxStarter, remote service.RemoteClient, interval time.Duration) {
	compSweeper := service.NewCompensationSweeper(
		queries,
		service.NewCompensationOpExecutor(remote),
		"memoryhub-compensation-sweeper",
		45*time.Second,
		50,
	)
	reviewScheduler := service.NewEvidenceReviewScheduler(
		queries,
		service.NewDefaultReviewTaskEnqueuer(queries, tx),
		service.ReviewSchedulerConfig{
			LeaseOwner:    "memoryhub-review-scheduler",
			LeaseDuration: 45 * time.Second,
			BatchSize:     50,
		},
	)

	go func() {
		slog.Info("memoryhub compensation sweeper starting")
		runSweeperLoop(ctx, "memoryhub-compensation", interval, func(ctx context.Context) error {
			return compSweeper.Sweep(ctx)
		})
	}()
	go func() {
		slog.Info("memoryhub review scheduler starting")
		runSweeperLoop(ctx, "memoryhub-review", interval, func(ctx context.Context) error {
			return reviewScheduler.Sweep(ctx)
		})
	}()
}

// runSweeperLoop invokes fn on a fixed interval until ctx is cancelled,
// logging per-pass errors without crashing the server.
func runSweeperLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := fn(ctx); err != nil {
			slog.Warn("memoryhub sweeper pass failed", "sweeper", name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
