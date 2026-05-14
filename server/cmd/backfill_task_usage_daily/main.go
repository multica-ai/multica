// Backfill_task_usage_daily seeds the `task_usage_daily` rollup table
// from the historical contents of `task_usage`. Intended to be run once
// after migrations 072..078 are applied, before flipping the
// USAGE_DAILY_ROLLUP_ENABLED feature flag and before scheduling the
// pg_cron job. The cron schedule is intentionally NOT created by a
// migration (see 076 header) — operators run this backfill, then
// schedule cron, then enable the read-path flag.
//
// Strategy:
//  1. Walk the time range covered by task_usage in monthly slices.
//  2. For each slice, call rollup_task_usage_daily_window(p_from, p_to)
//     — the same primitive the cron path uses, so semantics are
//     guaranteed identical.
//  3. After all slices succeed, advance task_usage_rollup_state.watermark_at
//     to (now() - 5 minutes) so the cron tick that follows doesn't
//     reprocess the same window we just rolled up.
//
// Re-running IS safe. The window function (introduced in 073, refined in
// 077) recomputes each dirty bucket from raw and REPLACES the daily row,
// so rerunning a slice produces the same result. Use this property to
// recover from partial backfill failures without TRUNCATEing
// task_usage_daily first.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
)

func main() {
	logger.Init()

	var (
		dryRun     = flag.Bool("dry-run", false, "log slices that would be processed without touching task_usage_daily")
		monthsBack = flag.Int("months-back", 0, "limit backfill to the last N months (0 = all available history)")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	// Discover the time range we need to cover. If task_usage is empty
	// we just stamp the watermark to now and exit.
	var minTS, maxTS pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT MIN(created_at), MAX(created_at) FROM task_usage`).Scan(&minTS, &maxTS); err != nil {
		slog.Error("scan task_usage time range", "error", err)
		os.Exit(1)
	}
	if !minTS.Valid {
		slog.Info("task_usage is empty; nothing to backfill")
		stampWatermark(ctx, pool)
		return
	}

	from := monthFloor(minTS.Time.UTC())
	end := monthFloor(maxTS.Time.UTC()).AddDate(0, 1, 0)

	if *monthsBack > 0 {
		cutoff := monthFloor(time.Now().UTC()).AddDate(0, -(*monthsBack), 0)
		if cutoff.After(from) {
			from = cutoff
		}
	}

	slog.Info("backfill range", "from", from.Format(time.RFC3339), "to", end.Format(time.RFC3339), "dry_run", *dryRun)

	cursor := from
	var totalRows int64
	for cursor.Before(end) {
		next := cursor.AddDate(0, 1, 0)
		if *dryRun {
			slog.Info("would roll up slice", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339))
			cursor = next
			continue
		}
		var rows int64
		err := pool.QueryRow(
			ctx,
			`SELECT rollup_task_usage_daily_window($1::timestamptz, $2::timestamptz)`,
			cursor, next,
		).Scan(&rows)
		if err != nil {
			slog.Error("rollup slice failed", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339), "error", err)
			os.Exit(1)
		}
		totalRows += rows
		slog.Info("rolled up slice", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339), "rows_touched", rows)
		cursor = next
	}

	if !*dryRun {
		stampWatermark(ctx, pool)
	}
	slog.Info("backfill complete", "total_rows_touched", totalRows)
}

// stampWatermark moves the rollup state's watermark to (now() - 5 min)
// so the next cron tick picks up only events newer than the backfill's
// upper bound. Mirrors the same lag the cron uses to avoid racing
// not-yet-visible inserts.
func stampWatermark(ctx context.Context, pool *pgxpool.Pool) {
	tag, err := pool.Exec(ctx, `
		UPDATE task_usage_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		slog.Error("stamp watermark failed", "error", err)
		os.Exit(1)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("no rollup state row to stamp; was migration 073 applied?")
		return
	}
	fmt.Println("watermark stamped to now() - 5 minutes")
}

func monthFloor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
