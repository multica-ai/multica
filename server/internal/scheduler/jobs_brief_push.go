package scheduler

import (
	"context"
	"time"
)

// JobNameBriefPushDispatch is the canonical name used in audit rows. Stable
// across releases.
const JobNameBriefPushDispatch = "brief_push_dispatch"

// BriefPushDispatcher sends the brief deliveries that are due and reports how
// many rows it took.
type BriefPushDispatcher interface {
	DeliverDue(ctx context.Context) (int, error)
}

// BriefPushDispatchJob sends briefs to the chat apps their members chose (spec
// 2.7, plan 9.10). It runs every 30 seconds, so a brief published at 08:00
// reaches a chat within a minute of publish, and a delivery that is backing off
// or deferred for quiet hours is picked up when it is due.
//
// Retries are the rows' own schedule, not the scheduler's: a row that fails is
// rescheduled in the table, so MaxAttempts stays at one. A run that is cut short
// leaves its unsent rows on their lease, and the next run takes them.
func BriefPushDispatchJob(dispatcher BriefPushDispatcher) JobSpec {
	return JobSpec{
		Name:              JobNameBriefPushDispatch,
		Cadence:           30 * time.Second,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      6 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       1,
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := dispatcher.DeliverDue(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
