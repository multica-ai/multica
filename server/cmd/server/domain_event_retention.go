package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Domain event retention (MUL-4332 §4.1 / §9). The transactional-outbox
// domain_event table would grow without bound, so a periodic sweep reclaims
// events that are BOTH already dispatched AND older than the TTL. In PR1 there
// is no matcher, so nothing is ever marked 'dispatched' — this sweep is a
// deliberate no-op until PR3, matching the "zero behavior change" contract
// while ensuring retention lands with the table it governs.
const (
	// domainEventRetentionInterval is how often the sweep runs. Retention is
	// coarse-grained, so an hourly tick is plenty.
	domainEventRetentionInterval = 1 * time.Hour
	// domainEventTTL is the retention window: dispatched events older than this
	// are reclaimed (MUL-4332 §9 fixes this at 90 days).
	domainEventTTL = 90 * 24 * time.Hour
	// domainEventRetentionBatch bounds a single DELETE so a large backlog can
	// never monopolize the DB; the sweep drains in batches until a short one.
	domainEventRetentionBatch = 1000
)

// runDomainEventRetention runs the retention sweep on a ticker until ctx is
// cancelled. Registered alongside the other sweepCtx-bound workers in main so
// it stops cleanly on shutdown.
func runDomainEventRetention(ctx context.Context, queries *db.Queries) {
	ticker := time.NewTicker(domainEventRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepDomainEvents(ctx, queries)
		}
	}
}

// sweepDomainEvents deletes dispatched-and-expired events in bounded batches.
func sweepDomainEvents(ctx context.Context, queries *db.Queries) {
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-domainEventTTL), Valid: true}
	var total int64
	for {
		deleted, err := queries.DeleteDispatchedDomainEventsBefore(ctx, db.DeleteDispatchedDomainEventsBeforeParams{
			CreatedAt: cutoff,
			Limit:     domainEventRetentionBatch,
		})
		if err != nil {
			slog.Warn("domain event retention: delete failed", "error", err)
			return
		}
		total += deleted
		// A short batch means the backlog is drained.
		if deleted < domainEventRetentionBatch {
			break
		}
		// Bail out promptly on shutdown between batches.
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	if total > 0 {
		slog.Info("domain event retention: reclaimed dispatched events", "count", total)
	}
}
