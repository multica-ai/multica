package scheduler

import (
	"context"
	"time"
)

// IssueContinuationJob reuses the server scheduler and reads durable task
// outcomes. Missing events, a restarted scheduler or an offline Mac do not
// erase the remainder. Tick never starts an agent.
func IssueContinuationJob(monitor interface{ Tick(context.Context) error }) JobSpec {
	return JobSpec{
		Name: "issue_continuation_reconcile", Cadence: 30 * time.Second,
		CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, monitor.Tick(ctx)
		},
	}
}
