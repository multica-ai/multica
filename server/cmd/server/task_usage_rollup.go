package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// taskUsageRollupInterval matches the cadence documented for the pg_cron entry
// in migration 102 (`*/5 * * * *`). In steady state each tick rolls a tiny
// `now()-5min` window; the function caps catch-up at one day per tick.
const taskUsageRollupInterval = 5 * time.Minute

// runTaskUsageRollup periodically drives rollup_task_usage_hourly(), which
// aggregates raw task_usage rows into task_usage_hourly (the table the
// dashboard and runtime-trend reads consume).
//
// On cloud this is scheduled via pg_cron per the operator playbook, but the
// default self-host Postgres image (pgvector/pgvector:pg17) ships without
// pg_cron, so nothing ever ran the rollup and task_usage_hourly stayed empty.
// This in-process ticker removes that dependency: it works on any Postgres and
// the function's own advisory lock 4246 makes a tick a no-op while a backfill
// or another instance holds it, so concurrent runs are safe.
func runTaskUsageRollup(ctx context.Context, pool *pgxpool.Pool) {
	// Run once at startup so a freshly-started self-host instance populates
	// task_usage_hourly without waiting a full interval.
	tickTaskUsageRollup(ctx, pool)

	ticker := time.NewTicker(taskUsageRollupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickTaskUsageRollup(ctx, pool)
		}
	}
}

// tickTaskUsageRollup runs a single rollup pass and logs the outcome.
func tickTaskUsageRollup(ctx context.Context, pool *pgxpool.Pool) {
	var rows int64
	if err := pool.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&rows); err != nil {
		slog.Warn("task usage rollup: tick failed", "error", err)
		return
	}
	if rows > 0 {
		slog.Info("task usage rollup: rolled up hourly buckets", "rows_touched", rows)
	}
}
