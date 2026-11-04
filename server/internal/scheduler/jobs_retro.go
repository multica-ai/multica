package scheduler

import (
	"context"
	"time"
)

// JobNameRetroTrigger is the canonical name used in audit rows. Stable across
// releases.
const JobNameRetroTrigger = "retro_trigger"

// RetroEvaluator runs the retrospective triggers and reports how many fired.
type RetroEvaluator interface {
	EvaluateRetros(ctx context.Context) (int, error)
}

// RetroTriggerJob runs every hour (spec 4.7). It evaluates the five triggers for
// every workspace; each subject fires at most once per window, so a run that
// repeats finds nothing new to open.
//
// The job is idempotent within a window. A retry after a failure may open an
// issue for a subject whose trigger row was not yet written; the issue is then
// the only record, which the leader can close.
func RetroTriggerJob(evaluator RetroEvaluator) JobSpec {
	return JobSpec{
		Name:              JobNameRetroTrigger,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := evaluator.EvaluateRetros(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
