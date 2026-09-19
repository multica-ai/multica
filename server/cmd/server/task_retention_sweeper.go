package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Bounded agent task retention sweeper (SE-37668). Deletes aged terminal rows
// from agent_task_queue — failed tasks past MULTICA_TASK_RETENTION_FAILED_DAYS
// and completed tasks past MULTICA_TASK_RETENTION_COMPLETED_DAYS — in small
// deterministic batches, under a cross-process singleton advisory lock, with
// autopilot/retry-lineage safety guards enforced entirely in SQL (see
// pkg/db/queries/task_retention.sql).
//
// It ships dry-run by default. Enabling deletion is a deliberate operator
// action (MULTICA_TASK_RETENTION_DRY_RUN=false); there is no time-based
// auto-flip from dry-run to delete.
const (
	taskRetentionDefaultFailedDays    int32 = 30
	taskRetentionDefaultCompletedDays int32 = 90
	taskRetentionDefaultCadence             = 24 * time.Hour
	taskRetentionDefaultBatchSize     int32 = 500
	// taskRetentionMinCadence is the smallest accepted sweep cadence. A
	// sub-minute cadence would churn the advisory lock and round loop far
	// faster than a retention sweep can ever need; reject it fail-closed
	// rather than honor a value like "500ms".
	taskRetentionMinCadence = 1 * time.Minute
	// taskRetentionMaxBatchSize is a hard cap: a larger configured batch is a
	// destructive-config error, not a value we silently clamp. A single batch
	// runs inside one short transaction, so the cap bounds both lock hold time
	// and the size of any one delete.
	taskRetentionMaxBatchSize int32 = 500

	// taskRetentionAdvisoryLockKey is a session-scoped advisory lock ("MULTAR")
	// held for a whole sweep round so only one process runs the sweeper at a
	// time. It is distinct from the telemetry and usage-backfill lock keys.
	taskRetentionAdvisoryLockKey int64 = 0x4d554c544152

	// taskRetentionRoundTimeout is the hard bound on one full-drain round. A
	// backlog too large to drain in one round is finished by the next daily run.
	taskRetentionRoundTimeout = 10 * time.Minute
	// taskRetentionBatchTimeout bounds one batch's transaction end to end,
	// independent of the per-statement timeout applied inside it.
	taskRetentionBatchTimeout = 60 * time.Second
	// taskRetentionCleanupTimeout bounds rollback/unlock issued after the round
	// context may already be cancelled.
	taskRetentionCleanupTimeout = 5 * time.Second

	// SET LOCAL guards (milliseconds) keep a single batch's transaction short:
	// lock_timeout so acquiring row/table locks cannot block indefinitely,
	// statement_timeout so the delete itself cannot run away.
	taskRetentionLockTimeoutMillis      = 5000
	taskRetentionStatementTimeoutMillis = 30000

	// Bounded backoff for the case where a batch deletes nothing yet candidates
	// remain (all victims were SKIP LOCKED by concurrent work). The round
	// timeout is the outer bound; the backoff caps how fast the round retries a
	// fully-contended snapshot until it drains or the round context expires.
	taskRetentionInitialBackoff = 100 * time.Millisecond
	taskRetentionMaxBackoff     = 5 * time.Second
)

const (
	envTaskRetentionFailedDays    = "MULTICA_TASK_RETENTION_FAILED_DAYS"
	envTaskRetentionCompletedDays = "MULTICA_TASK_RETENTION_COMPLETED_DAYS"
	envTaskRetentionCadence       = "MULTICA_TASK_RETENTION_CADENCE"
	envTaskRetentionBatchSize     = "MULTICA_TASK_RETENTION_BATCH_SIZE"
	envTaskRetentionDryRun        = "MULTICA_TASK_RETENTION_DRY_RUN"
)

type taskRetentionConfig struct {
	FailedDays    int32
	CompletedDays int32
	Cadence       time.Duration
	BatchSize     int32
	DryRun        bool
}

// parseTaskRetentionConfig reads the retention knobs fail-closed: any invalid
// value for a destructive knob returns an error so the caller stops the boot
// rather than falling back to a default and silently deleting under an operator
// setting they thought they had changed. An unset knob uses its default.
func parseTaskRetentionConfig(getenv func(string) string) (taskRetentionConfig, error) {
	cfg := taskRetentionConfig{
		FailedDays:    taskRetentionDefaultFailedDays,
		CompletedDays: taskRetentionDefaultCompletedDays,
		Cadence:       taskRetentionDefaultCadence,
		BatchSize:     taskRetentionDefaultBatchSize,
		DryRun:        true,
	}

	if raw := getenv(envTaskRetentionFailedDays); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || int64(v) > math.MaxInt32 {
			return cfg, fmt.Errorf("%s must be a positive integer, got %q", envTaskRetentionFailedDays, raw)
		}
		cfg.FailedDays = int32(v)
	}
	if raw := getenv(envTaskRetentionCompletedDays); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || int64(v) > math.MaxInt32 {
			return cfg, fmt.Errorf("%s must be a positive integer, got %q", envTaskRetentionCompletedDays, raw)
		}
		cfg.CompletedDays = int32(v)
	}
	if raw := getenv(envTaskRetentionCadence); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil || v < taskRetentionMinCadence {
			return cfg, fmt.Errorf("%s must be a duration >= %s, got %q", envTaskRetentionCadence, taskRetentionMinCadence, raw)
		}
		cfg.Cadence = v
	}
	if raw := getenv(envTaskRetentionBatchSize); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > int(taskRetentionMaxBatchSize) {
			return cfg, fmt.Errorf("%s must be between 1 and %d, got %q", envTaskRetentionBatchSize, taskRetentionMaxBatchSize, raw)
		}
		cfg.BatchSize = int32(v)
	}
	if raw := getenv(envTaskRetentionDryRun); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return cfg, fmt.Errorf("%s must be a boolean, got %q", envTaskRetentionDryRun, raw)
		}
		cfg.DryRun = v
	}
	return cfg, nil
}

// runTaskRetentionSweeper runs one bounded retention round per configured
// cadence. It reuses runPeriodicSweep so overlapping ticks are dropped rather
// than stacked, matching the runtime GC sweeper.
func runTaskRetentionSweeper(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, cfg taskRetentionConfig) {
	slog.Info("task retention sweeper started",
		"failed_days", cfg.FailedDays,
		"completed_days", cfg.CompletedDays,
		"cadence", cfg.Cadence.String(),
		"batch_size", cfg.BatchSize,
		"dry_run", cfg.DryRun,
	)
	runPeriodicSweep(ctx, cfg.Cadence, func() {
		sweepTaskRetention(ctx, pool, queries, metrics, cfg)
	})
}

type taskRetentionStage struct {
	status      string
	count       func(context.Context, pgtype.Timestamptz) (int64, error)
	deleteBatch func(context.Context, *db.Queries, pgtype.Timestamptz) ([]pgtype.UUID, error)
}

// sweepTaskRetention runs one round: acquire the singleton lock, take one DB
// clock reading, then process the failed and completed stages. It records
// exactly one run metric (mode + result) and stops the round on the first
// error (fail-stop).
func sweepTaskRetention(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, cfg taskRetentionConfig) {
	startedAt := time.Now()
	mode := obsmetrics.TaskRetentionModeDelete
	if cfg.DryRun {
		mode = obsmetrics.TaskRetentionModeDryRun
	}

	roundCtx, cancel := context.WithTimeout(ctx, taskRetentionRoundTimeout)
	defer cancel()

	session, acquired, err := acquireTaskRetentionLock(roundCtx, pool)
	if err != nil {
		slog.Warn("task retention: failed to acquire advisory lock", "error", err)
		metrics.RecordTaskRetentionRun(mode, obsmetrics.TaskRetentionResultFailure, time.Since(startedAt))
		return
	}
	if !acquired {
		// Another process holds the lock and is running this round.
		metrics.RecordTaskRetentionRun(mode, obsmetrics.TaskRetentionResultSkippedLock, time.Since(startedAt))
		return
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("task retention: advisory lock release failed", "error", cerr)
		}
	}()

	asOf, err := queries.GetTaskRetentionAsOf(roundCtx)
	if err != nil {
		slog.Warn("task retention: failed to read DB clock", "error", err)
		metrics.RecordTaskRetentionRun(mode, obsmetrics.TaskRetentionResultFailure, time.Since(startedAt))
		return
	}

	stages := []taskRetentionStage{
		{
			status: obsmetrics.TaskRetentionStatusFailed,
			count: func(ctx context.Context, asOf pgtype.Timestamptz) (int64, error) {
				return queries.CountFailedTaskRetentionCandidates(ctx, db.CountFailedTaskRetentionCandidatesParams{
					AsOf:       asOf,
					FailedDays: cfg.FailedDays,
				})
			},
			deleteBatch: func(ctx context.Context, qtx *db.Queries, asOf pgtype.Timestamptz) ([]pgtype.UUID, error) {
				return qtx.DeleteFailedTaskRetentionBatch(ctx, db.DeleteFailedTaskRetentionBatchParams{
					AsOf:       asOf,
					FailedDays: cfg.FailedDays,
					BatchSize:  cfg.BatchSize,
				})
			},
		},
		{
			status: obsmetrics.TaskRetentionStatusCompleted,
			count: func(ctx context.Context, asOf pgtype.Timestamptz) (int64, error) {
				return queries.CountCompletedTaskRetentionCandidates(ctx, db.CountCompletedTaskRetentionCandidatesParams{
					AsOf:          asOf,
					CompletedDays: cfg.CompletedDays,
				})
			},
			deleteBatch: func(ctx context.Context, qtx *db.Queries, asOf pgtype.Timestamptz) ([]pgtype.UUID, error) {
				return qtx.DeleteCompletedTaskRetentionBatch(ctx, db.DeleteCompletedTaskRetentionBatchParams{
					AsOf:          asOf,
					CompletedDays: cfg.CompletedDays,
					BatchSize:     cfg.BatchSize,
				})
			},
		},
	}

	result := obsmetrics.TaskRetentionResultSuccess
	for _, stage := range stages {
		if err := runTaskRetentionStage(roundCtx, pool, queries, metrics, cfg, asOf, stage); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				slog.Info("task retention: round context ended", "status", stage.status, "error", err)
			} else {
				slog.Warn("task retention: stage failed; stopping round", "status", stage.status, "error", err)
			}
			result = obsmetrics.TaskRetentionResultFailure
			break
		}
	}
	metrics.RecordTaskRetentionRun(mode, result, time.Since(startedAt))
}

// runTaskRetentionStage records the candidate count for one status, then either
// logs candidates (dry-run) or drains eligible rows in bounded batches
// (delete). A batch error aborts the stage without swallowing it, so the round
// stops rather than looping on a broken transaction.
func runTaskRetentionStage(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, cfg taskRetentionConfig, asOf pgtype.Timestamptz, stage taskRetentionStage) error {
	candidates, err := stage.count(ctx, asOf)
	if err != nil {
		return fmt.Errorf("count %s candidates: %w", stage.status, err)
	}
	metrics.SetTaskRetentionCandidateRows(stage.status, candidates)

	if cfg.DryRun {
		slog.Info(fmt.Sprintf("task retention dry-run: %d %s candidate rows (no deletes)", candidates, stage.status),
			"status", stage.status,
			"candidate_rows", candidates,
			"mode", obsmetrics.TaskRetentionModeDryRun,
		)
		return nil
	}

	purged := 0
	backoff := taskRetentionInitialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		deleted, err := deleteTaskRetentionBatch(ctx, pool, queries, cfg, asOf, stage)
		if err != nil {
			return fmt.Errorf("delete %s batch: %w", stage.status, err)
		}
		if deleted > 0 {
			purged += deleted
			metrics.RecordTaskRetentionPurged(stage.status, deleted)
			backoff = taskRetentionInitialBackoff
			continue
		}

		// Nothing deleted this batch: either fully drained, or every candidate
		// was SKIP LOCKED by concurrent work. Re-count to tell them apart.
		remaining, err := stage.count(ctx, asOf)
		if err != nil {
			return fmt.Errorf("recount %s candidates: %w", stage.status, err)
		}
		if remaining == 0 {
			break
		}

		// Candidates remain but every one was locked this pass. Back off and
		// retry within the round context instead of silently deferring aged rows
		// to the next daily run. The round timeout is the only bound: if it
		// expires first, the ctx.Err() check at the top of the loop ends the
		// round as a failure (fail-stop), never a success that leaves eligible
		// rows behind.
		if err := sleepWithContext(ctx, backoff); err != nil {
			return err
		}
		if backoff *= 2; backoff > taskRetentionMaxBackoff {
			backoff = taskRetentionMaxBackoff
		}
	}

	slog.Info(fmt.Sprintf("task retention: purged %d rows", purged),
		"status", stage.status,
		"purged", purged,
		"mode", obsmetrics.TaskRetentionModeDelete,
	)
	return nil
}

// deleteTaskRetentionBatch deletes one bounded batch inside a short transaction
// with SET LOCAL lock/statement timeouts, and returns the number of rows
// deleted. A failure rolls the batch back (its own rollback window) so only the
// current batch is lost, never earlier committed batches.
func deleteTaskRetentionBatch(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, cfg taskRetentionConfig, asOf pgtype.Timestamptz, stage taskRetentionStage) (int, error) {
	batchCtx, cancel := context.WithTimeout(ctx, taskRetentionBatchTimeout)
	defer cancel()

	tx, err := pool.Begin(batchCtx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), taskRetentionCleanupTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if _, err := tx.Exec(batchCtx, fmt.Sprintf("SET LOCAL lock_timeout = %d", taskRetentionLockTimeoutMillis)); err != nil {
		return 0, fmt.Errorf("set lock_timeout: %w", err)
	}
	if _, err := tx.Exec(batchCtx, fmt.Sprintf("SET LOCAL statement_timeout = %d", taskRetentionStatementTimeoutMillis)); err != nil {
		return 0, fmt.Errorf("set statement_timeout: %w", err)
	}

	ids, err := stage.deleteBatch(batchCtx, queries.WithTx(tx), asOf)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(batchCtx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return len(ids), nil
}

// sleepWithContext waits for d or until ctx ends, whichever comes first.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// taskRetentionLockSession holds the singleton advisory lock on a dedicated
// pooled connection for the duration of a round. Close releases the lock and,
// if the explicit unlock fails, hijacks and closes the physical connection so a
// possibly-locked session is never returned to the pool.
type taskRetentionLockSession struct {
	conn *pgxpool.Conn
}

func acquireTaskRetentionLock(ctx context.Context, pool *pgxpool.Pool) (*taskRetentionLockSession, bool, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", taskRetentionAdvisoryLockKey).Scan(&locked); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, false, nil
	}
	return &taskRetentionLockSession{conn: conn}, true, nil
}

func (s *taskRetentionLockSession) Close() error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), taskRetentionCleanupTimeout)
	defer cancel()

	var unlocked bool
	err := s.conn.QueryRow(cleanupCtx, "SELECT pg_advisory_unlock($1)", taskRetentionAdvisoryLockKey).Scan(&unlocked)
	if err == nil && unlocked {
		s.conn.Release()
		return nil
	}

	// Never return a possibly-locked session to the pool. Closing the physical
	// connection releases every PostgreSQL session lock even when the explicit
	// unlock failed during cancellation or a network fault.
	raw := s.conn.Hijack()
	closeErr := raw.Close(cleanupCtx)
	if err == nil && !unlocked {
		err = errors.New("task retention advisory lock was not held during release")
	}
	return errors.Join(err, closeErr)
}
