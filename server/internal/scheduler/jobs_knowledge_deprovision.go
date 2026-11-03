package scheduler

import (
	"context"
	"time"
)

// JobNameKnowledgeDeprovision is the canonical name used in audit rows. Stable
// across releases.
const JobNameKnowledgeDeprovision = "knowledge_deprovision"

// KnowledgeDeprovisioner removes the WeKnora tenants of deleted workspaces once
// their hold has passed. It returns how many it removed.
type KnowledgeDeprovisioner interface {
	DeprovisionDue(ctx context.Context) (int, error)
}

// KnowledgeDeprovisionJob runs hourly. A tenant is removed only after its
// seven-day hold (spec 3.3), so an hourly sweep adds at most an hour of delay.
// Each row carries its own retry time, so a run that finds nothing due does
// nothing, and a run that fails is repeated by the next one.
func KnowledgeDeprovisionJob(d KnowledgeDeprovisioner) JobSpec {
	return JobSpec{
		Name:              JobNameKnowledgeDeprovision,
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
			n, err := d.DeprovisionDue(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
