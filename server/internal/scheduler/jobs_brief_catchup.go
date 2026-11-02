package scheduler

import (
	"context"
	"time"
)

// JobNameBriefCatchup is the canonical name used in audit rows. Stable across
// releases.
const JobNameBriefCatchup = "brief_catchup"

// BriefCatchupRunner fires the missed daily briefs and reports how many it fired.
type BriefCatchupRunner interface {
	CatchUpBriefs(ctx context.Context) (int, error)
}

// BriefCatchupJob runs every hour (spec 2.9, plan 9.13). A daily brief that the
// schedule missed is fired once the workspace's catch-up hour has passed and the
// Secretary can run.
//
// The run is idempotent, so a retry or a second pass in the same hour fires
// nothing new: the fired run is either in flight or counted for the day. The
// handler is idempotent, so a failed pass is retried by the scheduler.
func BriefCatchupJob(runner BriefCatchupRunner) JobSpec {
	return JobSpec{
		Name:              JobNameBriefCatchup,
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
			n, err := runner.CatchUpBriefs(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
