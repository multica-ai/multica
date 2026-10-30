// backfill_issue_last_activity reconstructs issue.last_activity_at from the
// existing updated_at value. Run it after every issue-writing backend in a
// rolling deployment has been upgraded; older writers do not maintain the new
// column. The command is deliberately separate from migrations and startup so
// a large issue table never blocks a deploy.
//
// Each batch commits independently. SIGINT/SIGTERM, a database error, or
// --max-batches can stop the walk, and a later invocation resumes from rows
// whose last_activity_at is still NULL. Each pass advances by an id keyset
// watermark instead of rescanning its completed prefix. A session advisory lock
// serializes operators while SKIP LOCKED avoids waiting behind unrelated hot
// rows; later passes wrap around to recover rows skipped while locked.
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/issueactivitybackfill"
	"github.com/multica-ai/multica/server/internal/logger"
)

const (
	advisoryLockName        = "issue_last_activity_backfill"
	defaultMaxStalledPasses = 10
)

type stalledPassGuard struct {
	max         int
	consecutive int
}

func (g *stalledPassGuard) observe(passRows, remaining int64) error {
	if passRows > 0 {
		g.consecutive = 0
		return nil
	}
	g.consecutive++
	if g.max > 0 && g.consecutive >= g.max {
		return fmt.Errorf(
			"issue last-activity backfill stalled: %d consecutive passes made no progress with %d rows remaining; release long-held row locks and rerun, or increase --max-stalled-passes",
			g.consecutive,
			remaining,
		)
	}
	return nil
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("issue last-activity backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	batchSize := flag.Int("batch-size", issueactivitybackfill.DefaultBatchSize, "maximum issue rows updated per transaction")
	delay := flag.Duration("sleep-between-batches", 100*time.Millisecond, "delay between committed batches")
	maxBatches := flag.Int("max-batches", 0, "stop after N batches (0 = finish all remaining rows)")
	maxStalledPasses := flag.Int("max-stalled-passes", defaultMaxStalledPasses, "fail after N consecutive no-progress passes (0 = disable the guard)")
	flag.Parse()
	if *batchSize < 1 {
		return fmt.Errorf("--batch-size must be at least 1")
	}
	if *delay < 0 {
		return fmt.Errorf("--sleep-between-batches must not be negative")
	}
	if *maxBatches < 0 {
		return fmt.Errorf("--max-batches must not be negative")
	}
	if *maxStalledPasses < 0 {
		return fmt.Errorf("--max-stalled-passes must not be negative")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, advisoryLockName); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, advisoryLockName)
	}()

	remaining, err := issueactivitybackfill.CountRemaining(ctx, pool, "")
	if err != nil {
		return err
	}
	slog.Info("issue last-activity backfill started", "remaining", remaining, "batch_size", *batchSize, "delay", delay.String())

	var total int64
	var passRows int64
	var afterID *string
	pass := 1
	stallGuard := stalledPassGuard{max: *maxStalledPasses}
	for batch := 1; *maxBatches == 0 || batch <= *maxBatches; batch++ {
		result, err := issueactivitybackfill.Batch(ctx, pool, issueactivitybackfill.Options{
			BatchSize: *batchSize,
			AfterID:   afterID,
		})
		if err != nil {
			return err
		}
		if result.Rows > 0 && result.LastID == "" {
			return fmt.Errorf("issue last-activity backfill batch returned %d rows without a keyset watermark", result.Rows)
		}
		total += result.Rows
		passRows += result.Rows
		if result.Rows > 0 {
			slog.Info("issue last-activity batch committed", "batch", batch, "pass", pass, "rows", result.Rows, "total", total, "last_id", result.LastID)
			next := result.LastID
			afterID = &next
		}
		// A short/empty keyset batch marks the end of this pass, not necessarily
		// completion: SKIP LOCKED may have left hot rows below the watermark.
		// Count once per pass, then wrap to the start until those rows unlock.
		if result.Rows < int64(*batchSize) {
			remaining, err = issueactivitybackfill.CountRemaining(ctx, pool, "")
			if err != nil {
				return err
			}
			if remaining == 0 {
				slog.Info("issue last-activity backfill complete", "rows_backfilled", total, "remaining", 0)
				return nil
			}
			if err := stallGuard.observe(passRows, remaining); err != nil {
				return err
			}
			pass++
			afterID = nil
			slog.Info("issue last-activity pass complete; rows remain locked or pending", "pass", pass-1, "rows", passRows, "remaining", remaining, "stalled_passes", stallGuard.consecutive)
			passRows = 0
		}
		if *delay > 0 {
			select {
			case <-time.After(*delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	remaining, err = issueactivitybackfill.CountRemaining(ctx, pool, "")
	if err != nil {
		return err
	}
	slog.Info("issue last-activity backfill stopped at max batches", "rows_backfilled", total, "remaining", remaining)
	return nil
}
