package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLeaseLost is returned by heartbeat / terminal-update primitives
// when the row is no longer owned by the calling runner. The handler
// should stop, and the scheduler will not write a terminal status for
// the lost lease.
var ErrLeaseLost = errors.New("scheduler: lease lost")

// errTerminalIgnored is the sentinel finishSuccess / finishFailure use
// to convey a successful UPDATE that affected zero rows. Internal to
// this package.
var errTerminalIgnored = ErrLeaseLost

// dbNow returns Postgres's notion of "now" as the canonical clock. The
// scheduler uses DB time for every plan calculation and lease window so
// instances with skewed clocks still agree on the same plan_time.
func dbNow(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var t time.Time
	if err := pool.QueryRow(ctx, "SELECT now()").Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("scheduler: read db now: %w", err)
	}
	return t.UTC(), nil
}

// claim is the result of trying to acquire a plan. Only one of the
// boolean outcomes is true at a time.
type claim struct {
	ID         uuid.UUID
	LeaseToken uuid.UUID
	Attempt    int
	Won        bool // fresh insert
	Stole      bool // stale-steal or FAILED retry
	Conflicted bool // another runner already owns this plan, or attempts exhausted
}

// tryClaim attempts to acquire the lease for (job, scope, plan_time).
// Returns Won=true on a fresh insert, Stole=true on a stale-steal or
// retry-after-FAILED, and Conflicted=true if another runner owns the
// plan, the row is already SUCCESS, the FAILED row is not yet
// retry-eligible (next_retry_at in the future), or attempts are
// exhausted. The caller treats every Conflicted outcome the same way
// (no-op); the scheduler distinguishes them through audit-row metrics
// rather than per-call return fields.
func tryClaim(
	ctx context.Context,
	pool *pgxpool.Pool,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	dbTime time.Time,
	runnerID string,
) (claim, error) {
	// Fresh-insert path. ON CONFLICT DO NOTHING means losers do not
	// touch the existing row — we follow up with the steal/retry path
	// only if this insert was a conflict.
	insertSQL := `
		INSERT INTO sys_cron_executions (
			job_name, scope_kind, scope_id, plan_time,
			status, attempt, max_attempts,
			runner_id, lease_token,
			heartbeat_at, stale_after,
			started_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			'RUNNING', 1, $5,
			$6, gen_random_uuid(),
			$7::timestamptz, $7::timestamptz + make_interval(secs => $8),
			$7::timestamptz, $7::timestamptz
		)
		ON CONFLICT ON CONSTRAINT uq_sys_cron_execution DO NOTHING
		RETURNING id, lease_token, attempt
	`
	staleSecs := int64(job.StaleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}

	var c claim
	err := pool.QueryRow(ctx, insertSQL,
		job.Name, scope.Kind, scope.ID, planTime,
		job.MaxAttempts,
		runnerID,
		dbTime, staleSecs,
	).Scan(&c.ID, &c.LeaseToken, &c.Attempt)
	if err == nil {
		c.Won = true
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim insert: %w", err)
	}

	// Conflict path. Try retry-after-FAILED or stale-steal in a single
	// statement. The WHERE clause encodes both branches; the choice is
	// made server-side from the existing row.
	stealSQL := `
		UPDATE sys_cron_executions
		   SET status        = 'RUNNING',
		       attempt       = attempt + 1,
		       runner_id     = $1,
		       lease_token   = gen_random_uuid(),
		       heartbeat_at  = $2::timestamptz,
		       stale_after   = $2::timestamptz + make_interval(secs => $3),
		       started_at    = $2::timestamptz,
		       finished_at   = NULL,
		       duration_ms   = NULL,
		       next_retry_at = NULL,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = $2::timestamptz
		 WHERE job_name   = $4
		   AND scope_kind = $5
		   AND scope_id   = $6
		   AND plan_time  = $7
		   AND attempt < max_attempts
		   AND (
		        (status = 'FAILED' AND COALESCE(next_retry_at, $2::timestamptz) <= $2::timestamptz)
		        OR
		        (status = 'RUNNING' AND stale_after < $2::timestamptz AND $8)
		   )
		RETURNING id, lease_token, attempt
	`
	err = pool.QueryRow(ctx, stealSQL,
		runnerID,
		dbTime, staleSecs,
		job.Name, scope.Kind, scope.ID, planTime,
		job.AllowStaleReentry,
	).Scan(&c.ID, &c.LeaseToken, &c.Attempt)
	if err == nil {
		c.Stole = true
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim{}, fmt.Errorf("scheduler: claim steal: %w", err)
	}

	// No-row update — somebody else owns a fresh RUNNING lease, the
	// row is already SUCCESS, the row is FAILED but not yet
	// retry-eligible, or attempts are exhausted. The caller treats
	// these no-op cases identically.
	c.Conflicted = true
	return c, nil
}

// markStaleAsFailed transitions a single stale RUNNING row to FAILED
// when its job has AllowStaleReentry=false. Returns RowsAffected so the
// sweeper can log how many rows were closed.
func markStaleAsFailed(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobName string,
	dbTime time.Time,
) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status      = 'FAILED',
		       finished_at = $2,
		       error_code  = 'stale_timeout',
		       error_msg   = 'lease expired without heartbeat',
		       updated_at  = $2
		 WHERE job_name    = $1
		   AND status      = 'RUNNING'
		   AND stale_after < $2
	`, jobName, dbTime)
	if err != nil {
		return 0, fmt.Errorf("scheduler: mark stale failed: %w", err)
	}
	return tag.RowsAffected(), nil
}

// heartbeat extends stale_after for the runner that holds the lease.
// Returns ErrLeaseLost if the row is no longer ours (stolen or already
// finalised).
func heartbeat(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	staleTimeout time.Duration,
) error {
	staleSecs := int64(staleTimeout / time.Second)
	if staleSecs <= 0 {
		staleSecs = 1
	}
	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET heartbeat_at = now(),
		       stale_after  = now() + make_interval(secs => $3),
		       updated_at   = now()
		 WHERE id          = $1
		   AND lease_token = $2
		   AND status      = 'RUNNING'
	`, id, leaseToken, staleSecs)
	if err != nil {
		return fmt.Errorf("scheduler: heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// finishSuccess writes a terminal SUCCESS row. The lease_token guard
// prevents an ex-lease-holder from overwriting a newer attempt's state.
func finishSuccess(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	res HandlerResult,
) error {
	resultJSON, err := encodeResult(res.Result)
	if err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'SUCCESS',
		       finished_at   = $3,
		       duration_ms   = $4,
		       rows_affected = $5,
		       result        = $6::jsonb,
		       error_code    = NULL,
		       error_msg     = NULL,
		       updated_at    = $3
		 WHERE id            = $1
		   AND lease_token   = $2
		   AND status        = 'RUNNING'
	`, id, leaseToken, dbTime, durationMs, res.RowsAffected, resultJSON)
	if err != nil {
		return fmt.Errorf("scheduler: finish success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTerminalIgnored
	}
	return nil
}

// finishFailure writes a terminal FAILED row. nextRetryAt may be the
// zero value if no retry is due (max_attempts reached).
func finishFailure(
	ctx context.Context,
	pool *pgxpool.Pool,
	id, leaseToken uuid.UUID,
	dbTime time.Time,
	durationMs int64,
	errorCode, errorMsg string,
	nextRetryAt time.Time,
) error {
	var nextRetry pgtype.Timestamptz
	if !nextRetryAt.IsZero() {
		nextRetry = pgtype.Timestamptz{Time: nextRetryAt, Valid: true}
	}

	if errorCode == "" {
		errorCode = "handler_error"
	}
	if len(errorMsg) > 4000 {
		errorMsg = errorMsg[:4000]
	}

	tag, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET status        = 'FAILED',
		       finished_at   = $3,
		       duration_ms   = $4,
		       next_retry_at = $5,
		       error_code    = $6,
		       error_msg     = $7,
		       updated_at    = $3
		 WHERE id            = $1
		   AND lease_token   = $2
		   AND status        = 'RUNNING'
	`, id, leaseToken, dbTime, durationMs, nextRetry, errorCode, errorMsg)
	if err != nil {
		return fmt.Errorf("scheduler: finish failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTerminalIgnored
	}
	return nil
}

// encodeResult serialises a handler's small structured result to a
// JSONB-ready string. Empty/nil maps encode as `{}` so the column
// default is preserved if no result was set.
func encodeResult(in map[string]any) (string, error) {
	if len(in) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("scheduler: marshal result: %w", err)
	}
	// Defensive cap — keep result-row size predictable. Bigger
	// payloads belong in structured logs.
	if len(b) > 16*1024 {
		return "", fmt.Errorf("scheduler: result payload too large (%d bytes); keep it small or use logs", len(b))
	}
	return string(b), nil
}

// latestPlanInfo returns the latest known plan_time for (job, scope)
// plus the fields the catch-up planner needs to decide whether the row
// is still claimable at the same plan_time (FAILED-with-retry) or
// finished and the next plan_time should advance past it.
type latestPlanInfo struct {
	Found       bool
	PlanTime    time.Time
	Status      string
	Attempt     int
	MaxAttempts int
	// NextRetryAt is zero (NULL in DB) when the row is not in a
	// retry-eligible state, or when the next retry is due immediately
	// (FAILED with no backoff configured).
	NextRetryAt time.Time
}

// RetryEligible reports whether the latest stored row should still be
// considered for the same plan_time on the next tick. True for FAILED
// rows that have remaining attempts and whose next_retry_at has
// passed; the every_plan planner uses this to keep the cursor on the
// retry-eligible bucket so tryClaim's retry-from-FAILED branch can
// fire.
func (i latestPlanInfo) RetryEligible(now time.Time) bool {
	if !i.Found {
		return false
	}
	if i.Status != "FAILED" {
		return false
	}
	if i.Attempt >= i.MaxAttempts {
		return false
	}
	if i.NextRetryAt.IsZero() {
		// COALESCE-style: NULL next_retry_at means "as soon as
		// possible", which is right now.
		return true
	}
	return !i.NextRetryAt.After(now)
}

func latestPlan(
	ctx context.Context,
	pool *pgxpool.Pool,
	jobName string,
	scope Scope,
) (latestPlanInfo, error) {
	var info latestPlanInfo
	var nextRetry pgtype.Timestamptz
	err := pool.QueryRow(ctx, `
		SELECT plan_time, status, attempt, max_attempts, next_retry_at
		  FROM sys_cron_executions
		 WHERE job_name   = $1
		   AND scope_kind = $2
		   AND scope_id   = $3
		 ORDER BY plan_time DESC
		 LIMIT 1
	`, jobName, scope.Kind, scope.ID).Scan(
		&info.PlanTime, &info.Status,
		&info.Attempt, &info.MaxAttempts,
		&nextRetry,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return info, nil
		}
		return info, fmt.Errorf("scheduler: read latest plan: %w", err)
	}
	info.Found = true
	info.PlanTime = info.PlanTime.UTC()
	if nextRetry.Valid {
		info.NextRetryAt = nextRetry.Time.UTC()
	}
	return info, nil
}
