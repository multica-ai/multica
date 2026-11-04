package scheduler

import (
	"context"
	"time"
)

// JobNameMetricNightly is the canonical name used in audit rows.
const JobNameMetricNightly = "metric_nightly"

// MetricNightlyRunner computes the built-in metric readings, refreshes the
// derived confidence of active goals and applies the retention pass.
type MetricNightlyRunner interface {
	RunMetricNightly(ctx context.Context) (int, error)
}

// MetricNightlyJob runs once a day (spec 4.1). Readings for a day are replaced
// rather than added, so a run that fails and is retried leaves one reading per
// series per day.
func MetricNightlyJob(runner MetricNightlyRunner) JobSpec {
	return JobSpec{
		Name:              JobNameMetricNightly,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        30 * time.Minute,
		StaleTimeout:      40 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{5 * time.Minute, 15 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := runner.RunMetricNightly(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
