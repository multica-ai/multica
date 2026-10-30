package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// JobNamePruneSearchIndexChanges is the canonical name used in audit rows.
// Stable across releases — do not rename without a migration.
const JobNamePruneSearchIndexChanges = "prune_search_index_changes"

const (
	// SearchIndexChangeRetention bounds how long a local search index can stay
	// offline and still catch up; an older client rebuilds its copy (the changes
	// endpoint answers 410 once pruning passes its snapshot).
	SearchIndexChangeRetention = 30 * 24 * time.Hour

	searchIndexPruneBatchSize = 5000
	// Caps one run at 1M rows so a backlog drains over several ticks instead of
	// one unbounded run.
	searchIndexPruneMaxBatches = 200
)

// SearchIndexChangePruneJob drops search_index_change rows older than the
// retention window (MUL-7754). Rows are one per entity, so the table would
// otherwise grow with every issue, comment, and project ever written.
func SearchIndexChangePruneJob(queries *db.Queries) JobSpec {
	return JobSpec{
		Name:              JobNamePruneSearchIndexChanges,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler:           makeSearchIndexChangePruneHandler(queries),
	}
}

func makeSearchIndexChangePruneHandler(queries *db.Queries) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		cutoff := in.PlanTime.Add(-SearchIndexChangeRetention)
		var total int64
		for batch := 0; batch < searchIndexPruneMaxBatches; batch++ {
			deleted, err := queries.PruneSearchIndexChanges(ctx, db.PruneSearchIndexChangesParams{
				Cutoff:    pgtype.Timestamptz{Time: cutoff, Valid: true},
				BatchSize: searchIndexPruneBatchSize,
			})
			if err != nil {
				return HandlerResult{RowsAffected: total}, fmt.Errorf("prune search index changes: %w", err)
			}
			total += deleted
			if deleted < searchIndexPruneBatchSize {
				break
			}
			if in.Heartbeat != nil {
				if err := in.Heartbeat(ctx); err != nil {
					return HandlerResult{RowsAffected: total}, err
				}
			}
		}
		return HandlerResult{
			RowsAffected: total,
			Result:       map[string]any{"cutoff": cutoff.UTC().Format(time.RFC3339)},
		}, nil
	}
}
