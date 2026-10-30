// backfill_delegated_failure_settled retires delegated-failure recovery
// comments that were already closed by a terminal delivery receipt before
// comment.recovery_settled_at existed.
//
// Run it after every backend in a rolling deployment carries the code that
// writes the marker. Until it finishes, the outbox scan behaves exactly as it
// did before — unmarked history is still scanned and no recovery is lost — so
// this is a performance recovery step, not a correctness step, and it is safe
// to run off-peak or in several sittings.
//
// # Resuming
//
// The walk is keyed on (created_at, id), matching
// idx_comment_delegated_failure_unsettled, and the watermark advances over
// every candidate EXAMINED rather than only the ones marked. That distinction
// is what makes a bounded run resumable: the rows this walk deliberately leaves
// pending — a recovery still genuinely open — sit permanently at the front of
// the index, so a run that only remembered marked rows would restart inside
// that same prefix forever and never reach the settleable history behind it.
//
// Pass --checkpoint-file to persist the watermark after every committed batch
// and resume from it automatically, or read the resume_after_* fields this
// command logs on every exit path and pass them back with --after-created-at /
// --after-id. Without either, a rerun starts from the beginning; that is
// correct but repeats work.
//
// A session advisory lock keeps two operators from walking at once. It is
// operator mutual exclusion for this manual command only — the periodic
// sweeper's single-instance gate is a separate concern and is not this lock.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/delegatedrecoverybackfill"
	"github.com/multica-ai/multica/server/internal/logger"
)

const advisoryLockName = "delegated_failure_recovery_settled_backfill"

// checkpoint is the on-disk form of a keyset watermark.
type checkpoint struct {
	AfterCreatedAt time.Time `json:"after_created_at"`
	AfterID        string    `json:"after_id"`
}

func readCheckpoint(path string) (*delegatedrecoverybackfill.Cursor, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read checkpoint %s: %w", path, err)
	}
	var cp checkpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, fmt.Errorf("parse checkpoint %s: %w", path, err)
	}
	if cp.AfterID == "" {
		return nil, nil
	}
	return &delegatedrecoverybackfill.Cursor{CreatedAt: cp.AfterCreatedAt, ID: cp.AfterID}, nil
}

// writeCheckpoint replaces the file atomically so an interrupted write cannot
// leave behind a half-written watermark that resumes in the wrong place.
func writeCheckpoint(path string, cursor *delegatedrecoverybackfill.Cursor) error {
	raw, err := json.Marshal(checkpoint{AfterCreatedAt: cursor.CreatedAt, AfterID: cursor.ID})
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if err != nil {
		return fmt.Errorf("create checkpoint temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close checkpoint: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("install checkpoint %s: %w", path, err)
	}
	return nil
}

func resolveStart(checkpointFile, afterCreatedAt, afterID string) (*delegatedrecoverybackfill.Cursor, error) {
	if (afterCreatedAt == "") != (afterID == "") {
		return nil, fmt.Errorf("--after-created-at and --after-id must be given together")
	}
	if afterID != "" {
		createdAt, err := time.Parse(time.RFC3339Nano, afterCreatedAt)
		if err != nil {
			return nil, fmt.Errorf("--after-created-at must be RFC3339: %w", err)
		}
		// An explicit flag wins over the file: it is how an operator overrides
		// a checkpoint they no longer trust.
		return &delegatedrecoverybackfill.Cursor{CreatedAt: createdAt, ID: afterID}, nil
	}
	if checkpointFile == "" {
		return nil, nil
	}
	return readCheckpoint(checkpointFile)
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("delegated failure recovery settled backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	batchSize := flag.Int("batch-size", delegatedrecoverybackfill.DefaultBatchSize, "maximum candidate recovery comments examined per statement")
	delay := flag.Duration("sleep-between-batches", 100*time.Millisecond, "delay between committed batches")
	maxBatches := flag.Int("max-batches", 0, "stop after N batches (0 = walk the whole history)")
	checkpointFile := flag.String("checkpoint-file", "", "persist the keyset watermark here after every batch and resume from it")
	afterCreatedAt := flag.String("after-created-at", "", "resume after this comment created_at (RFC3339); requires --after-id")
	afterID := flag.String("after-id", "", "resume after this comment id; requires --after-created-at")
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
	cursor, err := resolveStart(*checkpointFile, *afterCreatedAt, *afterID)
	if err != nil {
		return err
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

	remaining, err := delegatedrecoverybackfill.CountRemaining(ctx, pool, "")
	if err != nil {
		return err
	}
	slog.Info("delegated failure recovery settled backfill started",
		"unsettled", remaining, "batch_size", *batchSize, "delay", delay.String(),
		"resumed_from_id", cursorID(cursor))

	var scanned, settled int64
	// Report the watermark on EVERY exit, including SIGTERM and --max-batches,
	// so a run without --checkpoint-file is still resumable from the logs.
	defer func() {
		slog.Info("delegated failure recovery settled backfill stopped",
			"scanned", scanned, "settled", settled,
			"resume_after_created_at", cursorCreatedAt(cursor),
			"resume_after_id", cursorID(cursor))
	}()

	for batch := 1; *maxBatches == 0 || batch <= *maxBatches; batch++ {
		result, err := delegatedrecoverybackfill.Batch(ctx, pool, delegatedrecoverybackfill.Options{
			BatchSize: *batchSize,
			After:     cursor,
		})
		if err != nil {
			return err
		}
		if result.Scanned == 0 {
			break
		}
		if result.Last == nil {
			return fmt.Errorf("backfill batch examined %d candidates without a keyset watermark", result.Scanned)
		}
		scanned += result.Scanned
		settled += result.Settled
		cursor = result.Last
		if *checkpointFile != "" {
			if err := writeCheckpoint(*checkpointFile, cursor); err != nil {
				return err
			}
		}
		slog.Info("delegated failure recovery settled batch committed",
			"batch", batch, "scanned", result.Scanned, "settled", result.Settled,
			"scanned_total", scanned, "settled_total", settled, "last_id", cursor.ID)
		if *delay > 0 {
			select {
			case <-time.After(*delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	remaining, err = delegatedrecoverybackfill.CountRemaining(ctx, pool, "")
	if err != nil {
		return err
	}
	// The remainder is the genuinely open backlog plus anything a reversible
	// exclusion is holding, which is what the index is supposed to contain.
	slog.Info("delegated failure recovery settled backfill finished",
		"scanned", scanned, "settled", settled, "unsettled_remaining", remaining)
	return nil
}

func cursorID(c *delegatedrecoverybackfill.Cursor) string {
	if c == nil {
		return ""
	}
	return c.ID
}

func cursorCreatedAt(c *delegatedrecoverybackfill.Cursor) string {
	if c == nil {
		return ""
	}
	return c.CreatedAt.Format(time.RFC3339Nano)
}
