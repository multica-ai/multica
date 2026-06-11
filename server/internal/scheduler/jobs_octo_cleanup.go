package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// JobNameOctoCleanup is the canonical name used in audit rows. Stable across
// releases — do not rename without a migration.
const JobNameOctoCleanup = "octo_cleanup"

// octoDedupRetention is how long processed inbound-dedup rows are kept before
// purge. The dedup gate only needs to remember a message id long enough to
// reject a WS replay; 24h is comfortably beyond any reconnect window. Matches
// the cutoff documented on the PurgeOctoInboundDedup query.
const octoDedupRetention = 24 * time.Hour

// OctoCleanupJob returns the JobSpec that purges expired Octo binding tokens
// and stale inbound-dedup rows. Both tables would otherwise grow unbounded:
// binding tokens are single-use with a 15m TTL but the consumed/expired rows
// linger, and dedup rows accumulate one per inbound message forever. The purge
// queries always existed (PurgeExpiredOctoBindingTokens / PurgeOctoInboundDedup)
// but were never scheduled.
func OctoCleanupJob(pool *pgxpool.Pool) JobSpec {
	return JobSpec{
		Name:              JobNameOctoCleanup,
		Cadence:           1 * time.Hour,
		ScheduleDelay:     1 * time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
		},
		Scopes:  StaticScopes(ScopeGlobal),
		Handler: makeOctoCleanupHandler(pool),
	}
}

// makeOctoCleanupHandler deletes expired binding tokens (expires_at < now) and
// dedup rows older than the retention window. Both deletes are idempotent and
// safe to re-run, so a retry after a partial failure simply removes whatever
// remains.
func makeOctoCleanupHandler(pool *pgxpool.Pool) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		q := db.New(pool)
		now := pgtype.Timestamptz{Time: in.PlanTime, Valid: true}
		if err := q.PurgeExpiredOctoBindingTokens(ctx, now); err != nil {
			return HandlerResult{}, fmt.Errorf("purge expired octo binding tokens: %w", err)
		}
		dedupCutoff := pgtype.Timestamptz{Time: in.PlanTime.Add(-octoDedupRetention), Valid: true}
		if err := q.PurgeOctoInboundDedup(ctx, dedupCutoff); err != nil {
			return HandlerResult{}, fmt.Errorf("purge octo inbound dedup: %w", err)
		}
		if in.Heartbeat != nil {
			_ = in.Heartbeat(ctx)
		}
		return HandlerResult{
			Result: map[string]any{
				"dedup_retention": octoDedupRetention.String(),
			},
		}, nil
	}
}
