// Backfill_task_usage_hourly seeds the unified hourly rollup table
// (`task_usage_hourly`) from historical `task_usage`
// rows. Run once after the hourly-pipeline migrations ship, BEFORE registering
// the pg_cron job for rollup_task_usage_hourly().
//
// SELF-HOST UPGRADE ORDER — migrations 100–104 are one group, but they
// must NOT be applied in a single `make migrate-up`:
//
//  1. Apply 101+102 (creates task_usage_hourly + installs the triggers).
//  2. Run THIS backfill to seed historical buckets.
//  3. Apply 103+104 (drops the legacy daily rollups + runtime.timezone)
//     and register the pg_cron job.
//
// If you run `migrate-up` straight through to 103/104 before this
// backfill, the legacy daily pipelines are gone while task_usage_hourly
// only holds buckets the triggers wrote since 102 — dashboards will show
// empty history until backfill + cron catch up (tens to hundreds of
// ticks on a DB with years of data, given the per-tick 1-day cap).
//
// Mirrors backfill_task_usage_dashboard_daily: walk task_usage's time
// range in monthly slices and call the same idempotent window
// primitive the cron path uses. Then stamp the rollup-state watermark
// so the first cron tick after backfill does not reprocess history.
//
// Re-running is safe — rollup_task_usage_hourly_window is idempotent
// (recomputes each dirty key from raw and REPLACES the bucket), so a
// partially completed backfill can be resumed without TRUNCATEing
// task_usage_hourly first.
//
// Read pressure: each slice scans task_usage / agent_task_queue / agent
// / issue. On a database with years of history that is sustained heavy
// load. Use --sleep-between-slices to throttle on a busy production DB,
// and coordinate a maintenance window with the DB team before a
// full-history run (see docs/timezone-architecture-rfc.md §7.1).
//
// Operator note: this command does NOT call prune_task_usage_hourly_dirty.
// The dirty queue starts empty during backfill (triggers fire only on
// future writes), so there is nothing to prune until the rollup worker
// has been running for a while.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
)

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dryRun       = flag.Bool("dry-run", false, "log slices that would be processed without touching task_usage_hourly")
		monthsBack   = flag.Int("months-back", 0, "limit backfill to the last N months (0 = all available history)")
		forcePartial = flag.Bool("force-partial", false, "acknowledge that --months-back permanently abandons buckets older than the cutoff (the watermark still advances past them)")
		sleep        = flag.Duration("sleep-between-slices", 0, "pause this long between monthly slices to throttle source-table read pressure on a busy DB (e.g. 2s)")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	// SIGINT/SIGTERM cancels ctx so an in-flight slice stops cleanly —
	// each slice runs in its own transaction (the window function), so
	// Postgres rolls back the interrupted one and the idempotent design
	// lets a later run resume from where this one stopped.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	// Serialise against the cron rollup and any other backfill run via
	// advisory lock 4246 — the same id the cron entry checks with
	// pg_try_advisory_lock. While this backfill holds it, the cron tick
	// no-ops instead of racing on task_usage_hourly row locks; a second
	// concurrent backfill blocks here until this one finishes. The lock
	// is held on a dedicated session connection for the whole run.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(4246)`); err != nil {
		return fmt.Errorf("acquire advisory lock 4246: %w", err)
	}
	defer func() {
		// Unlock on a fresh context so a cancelled ctx (SIGINT) does not
		// skip the release. Releasing the connection afterwards would end
		// the session anyway, but an explicit unlock frees it immediately.
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(4246)`)
	}()

	var minTS, maxTS pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT MIN(created_at), MAX(created_at) FROM task_usage`).Scan(&minTS, &maxTS); err != nil {
		return fmt.Errorf("scan task_usage time range: %w", err)
	}
	if !minTS.Valid {
		slog.Info("task_usage is empty; nothing to backfill")
		if *dryRun {
			return nil
		}
		return stampWatermark(ctx, pool)
	}

	from := monthFloor(minTS.Time.UTC())
	end := monthFloor(maxTS.Time.UTC()).AddDate(0, 1, 0)

	if *monthsBack > 0 {
		cutoff := monthFloor(time.Now().UTC()).AddDate(0, -(*monthsBack), 0)
		// A partial backfill still stamps the watermark at now()-5min, so
		// buckets older than the cutoff are abandoned permanently: the cron
		// worker will never look back that far. That data loss must be an
		// explicit operator decision — require --force-partial to proceed.
		if cutoff.After(from) {
			if !*forcePartial {
				return fmt.Errorf("--months-back=%d would skip buckets before %s (oldest available %s) and the watermark would still advance past them; re-run with --force-partial to accept this, or omit --months-back for a full backfill",
					*monthsBack, cutoff.Format(time.RFC3339), minTS.Time.UTC().Format(time.RFC3339))
			}
			from = cutoff
			slog.Warn("partial backfill: --months-back limits coverage; older buckets will be left empty and the watermark will still advance past them",
				"months_back", *monthsBack, "effective_from", from.Format(time.RFC3339),
				"oldest_available", minTS.Time.UTC().Format(time.RFC3339))
		}
	}

	slog.Info("backfill range", "from", from.Format(time.RFC3339), "to", end.Format(time.RFC3339), "dry_run", *dryRun, "sleep_between_slices", sleep.String())

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
			`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
			cursor, next,
		).Scan(&rows)
		if err != nil {
			return fmt.Errorf("rollup slice %s..%s: %w", cursor.Format(time.RFC3339), next.Format(time.RFC3339), err)
		}
		totalRows += rows
		slog.Info("rolled up slice", "from", cursor.Format(time.RFC3339), "to", next.Format(time.RFC3339), "rows_touched", rows)
		cursor = next
		if *sleep > 0 && cursor.Before(end) {
			select {
			case <-time.After(*sleep):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if *dryRun {
		slog.Info("dry-run complete; watermark left untouched")
		return nil
	}
	// Stamp on a fresh context so a SIGINT arriving after the slices
	// complete cannot skip the watermark UPDATE — losing it would force the
	// next run to restart from epoch. Mirrors the deferred advisory-unlock.
	if err := stampWatermark(context.Background(), pool); err != nil {
		return err
	}
	slog.Info("backfill complete", "total_rows_touched", totalRows)
	return nil
}

// stampWatermark moves the hourly rollup state's watermark to
// `now() - 5 min`, mirroring the cron entry's upper bound. The next
// scheduled tick therefore picks up only events newer than the
// backfill horizon and does not redo work the backfill already did.
func stampWatermark(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("no rollup state row to stamp; was the task_usage_hourly schema migration applied?")
		return nil
	}
	fmt.Println("watermark stamped to now() - 5 minutes")
	return nil
}

func monthFloor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
