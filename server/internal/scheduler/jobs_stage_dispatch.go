package scheduler

import (
	"context"
	"time"
)

// StageDispatcher assigns the open stages of parents whose stages the server
// dispatches.
type StageDispatcher interface {
	Sweep(context.Context) error
}

// StageDispatchJob catches what the pass right after a sub-issue change did
// not: a failed pass, and changes that record no sub-issue event.
func StageDispatchJob(dispatcher StageDispatcher) JobSpec {
	return JobSpec{
		Name: "issue_stage_dispatch", Cadence: time.Minute, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 50 * time.Second, StaleTimeout: 2 * time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, dispatcher.Sweep(ctx)
		},
	}
}
