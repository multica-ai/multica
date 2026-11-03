package scheduler

import (
	"context"
	"time"
)

// JobNameInitiativeSweep is the canonical name used in audit rows.
const JobNameInitiativeSweep = "initiative_sweep"

// InitiativeSweeper moves initiatives whose stage is ready, files their stage
// gates, and files the operate-stage review gates whose day has come.
type InitiativeSweeper interface {
	RunInitiativeSweep(ctx context.Context) (int, error)
}

// InitiativeSweepJob runs hourly. Each step is idempotent: an advance happens
// once because the stage is re-read under its row lock, and a gate is filed once
// because a day or a pending gate already counts as filed. A failed run is
// retried by the next one.
func InitiativeSweepJob(sweeper InitiativeSweeper) JobSpec {
	return JobSpec{
		Name:              JobNameInitiativeSweep,
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
			n, err := sweeper.RunInitiativeSweep(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
