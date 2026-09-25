package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestManagerEveryPlanRetriesFailedSamePlanTime exercises the
// `every_plan` retry path that张大彪 flagged on PR #3707:
//
//	"every_plan 的 FAILED retry 路径断了。CatchUpEveryPlan 规划必须把
//	 还在 retry 窗口、attempts < max_attempts 的 FAILED row 先递回去
//	 给 tryClaim 的 retry 分支，不能直接 latestPlan + cadence 跳过"
//
// The previous planner unconditionally advanced the cursor to
// `latestStored + cadence`, so after a FAILED row was written the next
// tick would skip past that plan_time and never re-attempt it — even
// though tryClaim's `(status='FAILED' AND COALESCE(next_retry_at, ...)
// <= now)` branch is the explicit retry path.
//
// This test:
//
//  1. Registers an every_plan job whose handler ALWAYS returns an
//     error.
//  2. Runs a tick → FAILED row at plan_time T, attempt=1, next_retry_at
//     stamped from RetryBackoff[0].
//  3. Forces next_retry_at into the past (test fast-forwards so we
//     don't have to wait for the real backoff).
//  4. Runs a second tick → asserts the SAME plan_time T was retried
//     and is now attempt=2 (not skipped to T+cadence).
//
// We pin a cadence well above the wall-clock difference between the
// two ticks so the rounded "current latest plan" remains the same
// bucket on both ticks, ensuring the cursor's behaviour is what we are
// actually measuring.
func TestManagerEveryPlanRetriesFailedSamePlanTime(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "every_plan_retry"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	job.CatchUpMode = CatchUpEveryPlan
	job.MaxPlansPerTick = 4
	job.CatchUpWindow = 24 * time.Hour
	// Long cadence so two consecutive runOnce calls land in the same
	// plan_time bucket — the test is about the cursor, not the bucket
	// math.
	job.Cadence = time.Hour
	job.MaxAttempts = 3
	job.RetryBackoff = []time.Duration{
		1 * time.Second, // attempt 1 → 2: sleep 1s
		1 * time.Second, // attempt 2 → 3
	}
	job.AllowStaleReentry = false

	var calls atomic.Int32
	job.Handler = func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		calls.Add(1)
		return HandlerResult{}, errors.New("simulated handler failure")
	}

	mgr := NewManager(pool, Options{RunnerID: "retry-runner"})
	if err := mgr.Register(*job); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Tick 1: handler fails at plan_time T attempt 1.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("first runOnce: %v", err)
	}

	rowsAfterTick1 := dumpJobRows(t, pool, job.Name)
	if len(rowsAfterTick1) != 1 {
		t.Fatalf("expected 1 row after tick 1, got %d: %+v", len(rowsAfterTick1), rowsAfterTick1)
	}
	r1 := rowsAfterTick1[0]
	if r1.Status != "FAILED" {
		t.Fatalf("expected FAILED after tick 1, got %q", r1.Status)
	}
	if r1.Attempt != 1 {
		t.Fatalf("expected attempt=1 after tick 1, got %d", r1.Attempt)
	}
	if r1.NextRetryAt.IsZero() {
		t.Fatalf("expected next_retry_at to be set after a retry-eligible failure")
	}

	planT := r1.PlanTime

	// Force next_retry_at into the past so the second tick sees the
	// retry as due. We deliberately use the DB's clock so this stays
	// independent of the app process clock (consistent with the rest
	// of the scheduler's time handling).
	if _, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET next_retry_at = now() - INTERVAL '1 minute'
		 WHERE id = $1
	`, r1.ID); err != nil {
		t.Fatalf("force next_retry_at into the past: %v", err)
	}

	// Tick 2: planner must keep cursor on plan_time T so tryClaim's
	// FAILED-with-retry branch fires.
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("second runOnce: %v", err)
	}

	rowsAfterTick2 := dumpJobRows(t, pool, job.Name)
	// Still exactly one row at plan_time T — the retry reuses the
	// same row, it does not create a new one.
	if len(rowsAfterTick2) != 1 {
		t.Fatalf("expected 1 row after tick 2 (retry reuses row), got %d: %+v",
			len(rowsAfterTick2), rowsAfterTick2)
	}
	r2 := rowsAfterTick2[0]
	if !r2.PlanTime.Equal(planT) {
		t.Fatalf("planner skipped past failed plan_time: tick1=%s tick2=%s",
			planT.Format(time.RFC3339), r2.PlanTime.Format(time.RFC3339))
	}
	if r2.Attempt != 2 {
		t.Fatalf("expected attempt=2 after retry, got %d", r2.Attempt)
	}
	if r2.Status != "FAILED" {
		// Handler still fails, so attempt 2 also lands as FAILED.
		// We still want to confirm the retry actually ran.
		t.Fatalf("expected attempt 2 to land FAILED again (handler still errors), got %q", r2.Status)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected handler called twice across two ticks, got %d calls", calls.Load())
	}
}

// rowSnapshot is the subset of sys_cron_executions fields the test
// inspects.
type rowSnapshot struct {
	ID          string
	PlanTime    time.Time
	Status      string
	Attempt     int
	MaxAttempts int
	NextRetryAt time.Time
}

func dumpJobRows(t *testing.T, pool *pgxpool.Pool, jobName string) []rowSnapshot {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT id, plan_time, status, attempt, max_attempts, COALESCE(next_retry_at, 'epoch'::timestamptz)
		  FROM sys_cron_executions
		 WHERE job_name = $1
		 ORDER BY plan_time ASC
	`, jobName)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	var out []rowSnapshot
	for rows.Next() {
		var r rowSnapshot
		if err := rows.Scan(&r.ID, &r.PlanTime, &r.Status, &r.Attempt, &r.MaxAttempts, &r.NextRetryAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// Treat the 'epoch' COALESCE sentinel as zero so callers can
		// distinguish "no retry scheduled" from "retry scheduled at
		// some real timestamp".
		if r.NextRetryAt.Year() == 1970 {
			r.NextRetryAt = time.Time{}
		} else {
			r.NextRetryAt = r.NextRetryAt.UTC()
		}
		r.PlanTime = r.PlanTime.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iter: %v", err)
	}
	return out
}
