package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LimitExceededAttempt is one operation that went over a gate's limit. Under
// observe it was admitted anyway; under enforce it was rejected.
type LimitExceededAttempt struct {
	WorkspaceID pgtype.UUID
	Gate        entitlement.GateName
	Action      entitlement.Action
	PeriodStart time.Time
	PeriodEnd   time.Time
	Limit       int64
	// AttemptedUsage is the usage the operation would reach if admitted,
	// counting itself.
	AttemptedUsage int64
}

// LimitOverage is how far one workspace went over one gate's limit in one
// period. Observe-mode rollouts, over-limit notices and grace periods read it
// per workspace; metrics deliberately carry no workspace label.
type LimitOverage struct {
	WorkspaceID uuid.UUID
	Gate        entitlement.GateName
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Limit is the latest limit seen in the period.
	Limit     int64
	PeakUsage int64
	// Over is PeakUsage beyond Limit. It is zero when a later, higher limit
	// (an upgrade mid-period) covers the peak.
	Over            int64
	ObservedCount   int64
	EnforcedCount   int64
	FirstExceededAt time.Time
	LastExceededAt  time.Time
}

// EntitlementOverages records and reads over-limit attempts for every
// entitlement gate. Only gates that are observing or enforcing reach it, so a
// self-hosted deployment without Cloud never touches the table.
type EntitlementOverages struct {
	Queries *db.Queries
}

// Record stores one over-limit attempt. It is a best-effort side effect that
// runs after the caller's admission decision has committed: a failure is
// logged and never changes whether the operation was admitted.
func (o EntitlementOverages) Record(ctx context.Context, attempt LimitExceededAttempt) {
	if attempt.Action != entitlement.ActionObserve && attempt.Action != entitlement.ActionEnforce {
		return
	}
	_, err := o.Queries.RecordEntitlementLimitExceeded(ctx, db.RecordEntitlementLimitExceededParams{
		WorkspaceID:    attempt.WorkspaceID,
		Gate:           string(attempt.Gate),
		PeriodStart:    pgtype.Timestamptz{Time: attempt.PeriodStart.UTC(), Valid: true},
		PeriodEnd:      pgtype.Timestamptz{Time: attempt.PeriodEnd.UTC(), Valid: true},
		LimitValue:     attempt.Limit,
		AttemptedUsage: attempt.AttemptedUsage,
		Observed:       attempt.Action == entitlement.ActionObserve,
	})
	if err != nil {
		slog.Warn("entitlement over-limit attempt was not recorded; admission decision is unchanged",
			"workspace_id", util.UUIDToString(attempt.WorkspaceID),
			"gate", string(attempt.Gate),
			"action", string(attempt.Action),
			"error", err,
		)
	}
}

// SyncLimit updates the limit on an existing over-limit record when an
// admission inside the limit sees a different one, e.g. after an upgrade
// mid-period. Without it Over would keep being measured against the old
// limit. Best-effort like Record; a workspace with no record is a no-op.
func (o EntitlementOverages) SyncLimit(
	ctx context.Context,
	workspaceID pgtype.UUID,
	gate entitlement.GateName,
	periodStart, periodEnd time.Time,
	limit int64,
) {
	err := o.Queries.SyncEntitlementLimitOverageLimit(ctx, db.SyncEntitlementLimitOverageLimitParams{
		WorkspaceID: workspaceID,
		Gate:        string(gate),
		PeriodStart: pgtype.Timestamptz{Time: periodStart.UTC(), Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: periodEnd.UTC(), Valid: true},
		LimitValue:  limit,
	})
	if err != nil {
		slog.Warn("entitlement over-limit record limit was not synced; admission decision is unchanged",
			"workspace_id", util.UUIDToString(workspaceID),
			"gate", string(gate),
			"error", err,
		)
	}
}

// ForWorkspace lists every period in which a workspace went over a limit,
// newest period first.
func (o EntitlementOverages) ForWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]LimitOverage, error) {
	rows, err := o.Queries.ListWorkspaceEntitlementLimitOverages(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace limit overages: %w", err)
	}
	return limitOveragesFromRows(rows), nil
}

// ForPeriod lists the workspaces that went over one gate's limit in a
// period, furthest over first.
func (o EntitlementOverages) ForPeriod(
	ctx context.Context,
	gate entitlement.GateName,
	periodStart, periodEnd time.Time,
) ([]LimitOverage, error) {
	rows, err := o.Queries.ListEntitlementLimitOveragesForPeriod(ctx, db.ListEntitlementLimitOveragesForPeriodParams{
		Gate:        string(gate),
		PeriodStart: pgtype.Timestamptz{Time: periodStart.UTC(), Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: periodEnd.UTC(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list limit overages for period: %w", err)
	}
	return limitOveragesFromRows(rows), nil
}

func limitOveragesFromRows(rows []db.EntitlementLimitOverage) []LimitOverage {
	out := make([]LimitOverage, 0, len(rows))
	for _, row := range rows {
		over := row.PeakUsage - row.LimitValue
		if over < 0 {
			over = 0
		}
		out = append(out, LimitOverage{
			WorkspaceID:     uuid.UUID(row.WorkspaceID.Bytes),
			Gate:            entitlement.GateName(row.Gate),
			PeriodStart:     row.PeriodStart.Time.UTC(),
			PeriodEnd:       row.PeriodEnd.Time.UTC(),
			Limit:           row.LimitValue,
			PeakUsage:       row.PeakUsage,
			Over:            over,
			ObservedCount:   row.ObservedCount,
			EnforcedCount:   row.EnforcedCount,
			FirstExceededAt: row.FirstExceededAt.Time.UTC(),
			LastExceededAt:  row.LastExceededAt.Time.UTC(),
		})
	}
	return out
}
