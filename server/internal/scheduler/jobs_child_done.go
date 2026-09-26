package scheduler

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type ChildEventSweeper interface {
	SweepChildEvents(context.Context) error
	BackfillChildDoneRules(context.Context, int32) (int, error)
}

// ChildEventSweepJob processes sub-issue changes that a write recorded but did
// not finish processing (a crash, a deploy, a transient database error); the
// rows themselves are the durable queue. Until every open parent has its
// child_done rule, it also creates rules for parents that predate it.
func ChildEventSweepJob(sweeper ChildEventSweeper) JobSpec {
	var backfilled atomic.Bool
	return JobSpec{
		Name: "issue_child_event_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			if !backfilled.Load() {
				created, err := sweeper.BackfillChildDoneRules(ctx, 200)
				if err != nil {
					slog.Warn("child_done backfill failed; will retry", "error", err)
				} else if created == 0 {
					backfilled.Store(true)
				}
			}
			return HandlerResult{}, sweeper.SweepChildEvents(ctx)
		},
	}
}
