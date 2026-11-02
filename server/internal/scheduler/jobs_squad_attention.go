package scheduler

import (
	"context"
	"time"
)

// JobNameSquadAttention is the canonical name used in audit rows. Stable across
// releases.
const JobNameSquadAttention = "squad_attention"

// SquadAttentionRecomputer recomputes the adaptive attention caps of the squads
// that have decided proposals. It returns how many squads it recomputed.
type SquadAttentionRecomputer interface {
	RecomputeSquadAttention(ctx context.Context) (int, error)
}

// SquadAttentionJob runs once a day (spec 2.6). Each squad with decided
// proposals in the last 60 days gets its computed caps from its acceptance rate;
// the rest keep the defaults. Pins are not touched.
//
// The recompute is idempotent: running it again writes the same caps from the
// same decisions, so a retry after a failure is safe.
func SquadAttentionJob(recomputer SquadAttentionRecomputer) JobSpec {
	return JobSpec{
		Name:              JobNameSquadAttention,
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
			n, err := recomputer.RecomputeSquadAttention(ctx)
			return HandlerResult{RowsAffected: int64(n)}, err
		},
	}
}
