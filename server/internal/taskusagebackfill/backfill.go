// Package taskusagebackfill seeds task_usage_hourly from historical
// task_usage rows.
//
// It exists in two callers:
//
//   - server/cmd/backfill_task_usage_hourly: explicit operator command,
//     used by the SELF-HOST UPGRADE ORDER documented in that file.
//   - server/cmd/migrate: invoked as a hook BEFORE migration 103 runs,
//     so that operators upgrading directly from v0.3.4 (or any version
//     prior to the hourly pipeline) do not trip migration 103's
//     fail-closed watermark guard while the server is still down. The
//     hook can run a full idempotent backfill in the same `migrate up`
//     invocation and then continue applying 103/104.
//
// The implementation uses the same SQL window primitive
// (`rollup_task_usage_hourly_window`) that the rollup worker uses, so
// re-running is safe — partial progress is recovered on the next call.
//
// All callers MUST hold advisory lock 4246 (AdvisoryLockKey) for the
// duration of the backfill walk. That lock is what makes this safe to
// run alongside the SQL `rollup_task_usage_hourly()` cron entry, the
// in-process scheduler, and any other concurrent backfill — winners
// take the lock, losers no-op until it is released.
package taskusagebackfill

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdvisoryLockKey is the int64 identifier shared by every path that
// drives task_usage_hourly's rollup writes:
//
//   - rollup_task_usage_hourly() in migration 102.
//   - cmd/backfill_task_usage_hourly.
//   - cmd/migrate's pre-103 hook (MUL-2957).
//   - the in-process scheduler's rollup_task_usage_hourly handler
//     (defense in depth — the scheduler's lease already prevents
//     double-runs across instances; the advisory lock additionally
//     prevents racing legacy pg_cron / manual entrypoints).
//
// It is the same id used by every prior version of the rollup pipeline,
// so a mixed-version cluster (rolling deploy) cannot double-write.
const AdvisoryLockKey int64 = 4246

// MaxLagThreshold mirrors the v_lag interval inside migration 103's
// fail-closed guard. The pre-103 hook only triggers a backfill when the
// watermark trails the latest task_usage event by more than this; below
// the threshold the migration would have passed anyway, so we save the
// scan.
const MaxLagThreshold = time.Hour

// The watermark upper bound of `now() - 5 minutes` is encoded directly
// in the SQL UPDATE in stampWatermark / stampWatermarkOnConn so the
// math runs in the same session that does the write — no app-side
// time.Now() participates. (MUL-2957 review: blocker #3.)

// Result describes what a single backfill run did. Exposed so callers
// (the migrate command and tests) can log or assert on it.
type Result struct {
	// Skipped is a short reason string when the run did no slice work.
	// Empty string means the run actually walked at least one slice.
	Skipped string

	// SlicesProcessed counts monthly slices that were rolled up.
	SlicesProcessed int

	// RowsTouched is the sum of rollup_task_usage_hourly_window's
	// returned counts.
	RowsTouched int64

	// From / To bracket the walk's UTC range.
	From time.Time
	To   time.Time

	// WatermarkStamped reports whether the watermark UPDATE was issued.
	WatermarkStamped bool
}

// HookOptions controls Hook behaviour. The defaults are correct for
// production; tests override fields as needed.
type HookOptions struct {
	// Logger receives slog records about the backfill walk. nil =
	// slog.Default().
	Logger *slog.Logger

	// LagThreshold overrides MaxLagThreshold. Zero = MaxLagThreshold.
	// Tests pass a small value to force a backfill on a deterministic
	// fixture.
	LagThreshold time.Duration

	// SleepBetweenSlices throttles the walk on a busy DB. Zero = no
	// pause. Mirrors the operator-facing flag on the standalone
	// backfill command.
	SleepBetweenSlices time.Duration
}

// Hook is the migration-time entrypoint. It checks whether the
// task_usage_hourly_rollup_state.watermark_at trails the latest
// task_usage event by more than the lag threshold and, if so, runs an
// idempotent monthly-slice backfill of task_usage_hourly. After the
// walk completes (or if no backfill was needed) the watermark is
// stamped to `now() - 5 minutes`, mirroring the standalone backfill
// command's stampWatermark step.
//
// The hook does NOT fail when:
//
//   - task_usage is empty (a fresh database has no history to
//     backfill — the watermark is stamped so migration 103's guard
//     accepts the empty state).
//
//   - the rollup state tables are missing (the hook ran before
//     migration 101 in some unusual ordering — treated as nothing to
//     do, the migration loop will install them next).
//
// It DOES return an error when the rollup walk itself fails: that case
// is identical to the standalone backfill failing, and the migration
// run must abort so the operator can investigate before migration 103
// drops the legacy daily rollups.
//
// The hook acquires advisory lock 4246 on its own session connection
// so it does not collide with the migrate loop's session-level
// migrationAdvisoryLockKey on a different conn.
func Hook(ctx context.Context, pool *pgxpool.Pool, opts HookOptions) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	threshold := opts.LagThreshold
	if threshold <= 0 {
		threshold = MaxLagThreshold
	}

	// Step 1: cheap precondition check — if the rollup state tables
	// have not been created yet, this hook simply has nothing to do
	// (migrations 101/102 install them, and the migration loop will
	// reach 103 only after those have already applied).
	stateExists, err := rollupStateExists(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("check rollup state existence: %w", err)
	}
	if !stateExists {
		log.Info("task_usage hourly rollup hook: rollup state tables not present, skipping",
			"reason", "migrations 101/102 not yet applied")
		return Result{Skipped: "rollup_state_missing"}, nil
	}

	// Step 2: read task_usage range and current watermark on the pool.
	// These do not need to be locked — they are only used to decide
	// whether the lock-protected walk should run at all, and the walk
	// itself is idempotent if the watermark advances under us.
	usageRange, err := loadUsageRange(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("load task_usage range: %w", err)
	}
	watermark, err := loadWatermark(ctx, pool)
	if err != nil {
		return Result{}, fmt.Errorf("load rollup watermark: %w", err)
	}

	if !usageRange.HasRows {
		// Empty database. Stamp the watermark so migration 103's
		// guard accepts the no-history path on a fresh upgrade and
		// the rollup worker starts forward from the stamp. The DB's
		// own clock is used so a clock-skewed app process cannot
		// stamp the watermark into the DB's future.
		if err := stampWatermark(ctx, pool); err != nil {
			return Result{}, err
		}
		log.Info("task_usage hourly rollup hook: task_usage empty, watermark stamped from db now()")
		return Result{Skipped: "task_usage_empty", WatermarkStamped: true}, nil
	}

	if !watermark.Valid {
		// Defensive — schema guarantees the row exists with a default
		// of 1970-01-01, so an invalid value here means somebody has
		// fiddled with the row directly.
		return Result{}, errors.New("task_usage_hourly_rollup_state row is missing or watermark is NULL; manual intervention required before migration 103")
	}

	// Lag is computed against the DB's max_event (already DB-time);
	// comparing to the DB-time watermark avoids any app/DB skew.
	maxEvent := usageRange.MaxEvent
	lag := maxEvent.Sub(watermark.Time)
	if lag <= threshold {
		log.Info("task_usage hourly rollup hook: watermark already current, skipping backfill",
			"watermark_at", watermark.Time.UTC().Format(time.RFC3339),
			"max_event", maxEvent.UTC().Format(time.RFC3339),
			"lag", lag.String(),
			"threshold", threshold.String())
		// Re-stamp from DB now() to bring the value flush with the
		// cron upper bound; the lag-based guard in migration 103 will
		// pass either way, but stamping keeps the post-hook state
		// consistent with the standalone backfill command.
		if err := stampWatermark(ctx, pool); err != nil {
			return Result{}, err
		}
		return Result{Skipped: "watermark_within_threshold", WatermarkStamped: true}, nil
	}

	log.Info("task_usage hourly rollup hook: backfilling under advisory lock",
		"watermark_at", watermark.Time.UTC().Format(time.RFC3339),
		"max_event", maxEvent.UTC().Format(time.RFC3339),
		"lag", lag.String(),
		"threshold", threshold.String())

	// Step 3: serialise against the SQL cron entry / standalone backfill
	// / scheduler handler via advisory lock 4246. We use a dedicated
	// session-pinned conn because pg advisory locks are per-session.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()

	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, AdvisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("acquire advisory lock %d: %w", AdvisoryLockKey, err)
	}
	// Use a fresh context for the unlock so a cancelled ctx does not
	// skip the release. Releasing the connection afterwards would end
	// the session anyway, but an explicit unlock frees it immediately.
	defer func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, AdvisoryLockKey)
	}()

	from := monthFloor(usageRange.MinEvent)
	end := monthFloor(maxEvent).AddDate(0, 1, 0)
	res := Result{From: from, To: end}

	cursor := from
	for cursor.Before(end) {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		next := cursor.AddDate(0, 1, 0)
		var rows int64
		err := lockConn.QueryRow(
			ctx,
			`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
			cursor, next,
		).Scan(&rows)
		if err != nil {
			return res, fmt.Errorf("rollup slice %s..%s: %w",
				cursor.Format(time.RFC3339), next.Format(time.RFC3339), err)
		}
		res.SlicesProcessed++
		res.RowsTouched += rows
		log.Info("task_usage hourly rollup hook: slice complete",
			"from", cursor.Format(time.RFC3339),
			"to", next.Format(time.RFC3339),
			"rows_touched", rows)
		cursor = next
		if opts.SleepBetweenSlices > 0 && cursor.Before(end) {
			select {
			case <-time.After(opts.SleepBetweenSlices):
			case <-ctx.Done():
				return res, ctx.Err()
			}
		}
	}

	if err := stampWatermarkOnConn(ctx, lockConn.Conn()); err != nil {
		return res, err
	}
	res.WatermarkStamped = true

	log.Info("task_usage hourly rollup hook: complete",
		"slices", res.SlicesProcessed,
		"total_rows_touched", res.RowsTouched,
		"watermark_source", "db_now")
	return res, nil
}

type usageRange struct {
	HasRows  bool
	MinEvent time.Time
	MaxEvent time.Time
}

func loadUsageRange(ctx context.Context, pool *pgxpool.Pool) (usageRange, error) {
	var minTS, maxTS pgtype.Timestamptz
	// COALESCE(updated_at, created_at) tracks the same expression
	// migration 103's guard uses, so the lag comparison stays
	// consistent with the value the guard will check next.
	err := pool.QueryRow(ctx, `
		SELECT MIN(created_at), MAX(COALESCE(updated_at, created_at))
		  FROM task_usage
	`).Scan(&minTS, &maxTS)
	if err != nil {
		return usageRange{}, err
	}
	if !minTS.Valid || !maxTS.Valid {
		return usageRange{HasRows: false}, nil
	}
	return usageRange{
		HasRows:  true,
		MinEvent: minTS.Time.UTC(),
		MaxEvent: maxTS.Time.UTC(),
	}, nil
}

func loadWatermark(ctx context.Context, pool *pgxpool.Pool) (pgtype.Timestamptz, error) {
	var watermark pgtype.Timestamptz
	err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermark)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.Timestamptz{}, nil
		}
		return pgtype.Timestamptz{}, err
	}
	return watermark, nil
}

func rollupStateExists(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public'
			   AND table_name = 'task_usage_hourly_rollup_state'
		)
	`).Scan(&exists)
	return exists, err
}

// stampWatermark moves the hourly rollup state watermark to
// `now() - 5 min` using PostgreSQL's clock, NOT the app process clock.
// This matches the cron entry's upper bound and — critically —
// guarantees the watermark cannot be stamped into the DB's future
// because of container clock drift. (MUL-2957 review: see张大彪's
// blocker #3.)
func stampWatermark(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	return nil
}

func stampWatermarkOnConn(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = now() - INTERVAL '5 minutes'
		 WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("stamp watermark: %w", err)
	}
	return nil
}

func monthFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
