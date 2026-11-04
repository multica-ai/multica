package scheduler

import (
	"context"
	"time"
)

// JobNamePlanningCycle is the canonical name used in audit rows.
const JobNamePlanningCycle = "planning_cycle"

// PlanningCycleRunner opens the planning issues of the current period.
type PlanningCycleRunner interface {
	RunPlanningCycle(ctx context.Context) (int, error)
}

// PlanningCycleJob runs daily (spec 4.1). The period is a quarter, and each
// squad with a charter gets one planning issue per period, recorded in the same
// transaction, so the first daily run of a quarter opens them and the rest find
// nothing to do.
func PlanningCycleJob(runner PlanningCycleRunner) JobSpec {
	return JobSpec{
		Name:              JobNamePlanningCycle,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        15 * time.Minute,
		StaleTimeout:      20 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := runner.RunPlanningCycle(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
