package scheduler

import (
	"context"
	"time"
)

// JobNameProposalExpiry is the canonical name used in audit rows. Stable across
// releases.
const JobNameProposalExpiry = "proposal_expiry"

// ProposalExpirer expires the proposals whose decision window has passed and
// publishes what it changed. It returns how many it expired.
type ProposalExpirer interface {
	ExpireDueProposals(ctx context.Context) (int, error)
}

// ProposalExpiryJob runs once a day. A pending proposal with no decision within
// its window becomes expired, its issue is cancelled, a system note is posted,
// and its leader is woken once (spec 2.4).
//
// Running it again finds nothing new: each proposal is expired under its row
// lock with the same predicate the list used, so a second run cannot wake the
// leader a second time. The job can therefore retry after a failure freely.
func ProposalExpiryJob(expirer ProposalExpirer) JobSpec {
	return JobSpec{
		Name:              JobNameProposalExpiry,
		Cadence:           24 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			n, err := expirer.ExpireDueProposals(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
