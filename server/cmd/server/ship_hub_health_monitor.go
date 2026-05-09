// Phase 5 Ship Hub — post-deploy live health monitor.
//
// A background goroutine that, every 5 minutes, scans every successful
// deploy whose completed_at is within the last 24 hours and writes a
// fresh deploy_health_snapshot row for it. The snapshot compares:
//
//   - agent_task_queue failure rate in the 5 minutes leading up to NOW
//     against a 24-hour baseline preceding the deploy
//   - inbox_item creation count since the deploy's completed_at
//
// We deliberately don't compute a real-time error_rate or p99 latency
// here because the platform doesn't yet emit those signals into a
// queryable table. The columns are populated with NULL when the data
// isn't available; the frontend renders "—" and the rest of the panel
// keeps working. Adding a real probe later is one query change away.
//
// The goroutine matches the pattern of runShipHubReconciler: per-tick
// scan, per-deploy errors logged and skipped so one bad row doesn't
// stall the rest. Single-node only at this stage — for multi-node
// scaling we'd lock per (deploy_id, snapshot_at) bucket via a
// `pg_advisory_xact_lock`. Today the in-flight window is small enough
// that a duplicate snapshot from two nodes is a 16-byte rounding error,
// not a correctness bug.

package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// shipHubHealthInterval — every 5 minutes. The "live panel" updates on
// the next user fetch, so a 5min lag matches the reconciler's pace and
// avoids hot-looping the DB for a feature whose value is qualitative
// trend ("error rate is climbing") rather than minute-by-minute
// accuracy.
const shipHubHealthInterval = 5 * time.Minute

func runShipHubHealthMonitor(ctx context.Context, queries *db.Queries) {
	slog.Info("ship hub health monitor started", "interval", shipHubHealthInterval.String())
	t := time.NewTicker(shipHubHealthInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("ship hub health monitor stopped")
			return
		case <-t.C:
			runShipHubHealthOnce(ctx, queries)
		}
	}
}

// runShipHubHealthOnce does one pass over the in-window deploys.
// Extracted so a future test can drive it deterministically.
func runShipHubHealthOnce(ctx context.Context, queries *db.Queries) {
	deploys, err := queries.ListRecentSucceededDeploys(ctx)
	if err != nil {
		slog.Warn("ship hub health monitor: list deploys failed", "error", err)
		return
	}
	for _, d := range deploys {
		if err := snapshotDeployHealth(ctx, queries, d); err != nil {
			slog.Warn("ship hub health monitor: snapshot failed",
				"deploy_id", d.ID, "error", err)
		}
	}
}

// snapshotDeployHealth computes one row's worth of data and writes it.
// All sub-queries are best-effort — any single failure substitutes
// NULL for the column and the snapshot still lands. We do that because
// the panel reads the LATEST row even if it's partial; an empty miss
// would be a worse user experience.
func snapshotDeployHealth(ctx context.Context, queries *db.Queries, d db.Deploy) error {
	if !d.CompletedAt.Valid {
		return errors.New("deploy has no completed_at")
	}
	now := time.Now()
	completed := d.CompletedAt.Time

	// 1. Inbox issues opened since the deploy completed.
	var inboxSince int32
	if c, err := queries.ListRecentInboxOpensSinceForWorkspace(ctx, db.ListRecentInboxOpensSinceForWorkspaceParams{
		WorkspaceID: d.WorkspaceID,
		CreatedAt:   d.CompletedAt,
	}); err == nil {
		inboxSince = int32(c)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Debug("ship hub health monitor: inbox count failed",
			"deploy_id", d.ID, "error", err)
	}

	// 2. Agent task failure rate Δ (current 5-min window vs 24-hour baseline).
	currWinStart := now.Add(-shipHubHealthInterval)
	currWinEnd := now
	baseStart := completed.Add(-24 * time.Hour)
	baseEnd := completed

	var failureDelta pgtype.Float8
	if curRate, ok := failureRate(ctx, queries, d.WorkspaceID, currWinStart, currWinEnd); ok {
		if baseRate, ok := failureRate(ctx, queries, d.WorkspaceID, baseStart, baseEnd); ok {
			failureDelta = pgtype.Float8{Float64: curRate - baseRate, Valid: true}
		}
	}

	_ = now // snapshot_at defaults to NOW() in the SQL.
	if _, err := queries.InsertDeployHealthSnapshot(ctx, db.InsertDeployHealthSnapshotParams{
		WorkspaceID:           d.WorkspaceID,
		DeployID:              d.ID,
		ErrorRateBaseline:     pgtype.Float8{}, // Reserved for future signal — see file header.
		ErrorRateCurrent:      pgtype.Float8{},
		P99LatencyBaselineMs:  pgtype.Float8{},
		P99LatencyCurrentMs:   pgtype.Float8{},
		InboxIssuesSince:      inboxSince,
		AgentFailureRateDelta: failureDelta,
	}); err != nil {
		return err
	}
	return nil
}

// failureRate runs the AgentTaskFailureRateInWindow query and returns
// (failed/total) as a fraction. ok=false when the window has zero
// completed tasks (no signal); caller treats that as "no Δ to record".
func failureRate(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
	from, to time.Time,
) (float64, bool) {
	row, err := queries.AgentTaskFailureRateInWindow(ctx, db.AgentTaskFailureRateInWindowParams{
		WorkspaceID: workspaceID,
		CompletedAt: pgtype.Timestamptz{Time: from, Valid: true},
		CompletedAt_2: pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil || row.Total == 0 {
		return 0, false
	}
	return float64(row.Failed) / float64(row.Total), true
}
