package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

// readJobStatus reads one job's status row.
func readJobStatus(t *testing.T, ctx context.Context, name string) (success, failed *time.Time, lastErr string) {
	t.Helper()
	if err := integrationPool(t).QueryRow(ctx,
		`SELECT last_success_at, last_failed_at, last_error FROM job_status WHERE job_name = $1`, name,
	).Scan(&success, &failed, &lastErr); err != nil {
		t.Fatalf("read job_status %s: %v", name, err)
	}
	return success, failed, lastErr
}

// TestFinishedPlansWriteTheJobStatusRow checks spec 4.9: the scheduler writes the
// job_status row when a plan finishes, for a success and for a failure, through
// the one hook every job passes through.
func TestFinishedPlansWriteTheJobStatusRow(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok := newTestJobSpec(uniqueJobName(t, "status-ok"))
	ok.Handler = func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		return HandlerResult{RowsAffected: 1}, nil
	}
	bad := newTestJobSpec(uniqueJobName(t, "status-bad"))
	bad.Handler = func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		return HandlerResult{}, errors.New("simulated failure for the status row")
	}
	for _, j := range []*JobSpec{ok, bad} {
		j := j
		t.Cleanup(func() {
			cleanupExecutions(t, pool, j.Name)
			_, _ = pool.Exec(context.Background(), `DELETE FROM job_status WHERE job_name = $1`, j.Name)
		})
	}

	mgr := NewManager(pool, Options{RunnerID: "status-runner"})
	for _, j := range []*JobSpec{ok, bad} {
		if err := mgr.Register(*j); err != nil {
			t.Fatalf("register %s: %v", j.Name, err)
		}
	}
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("run once: %v", err)
	}

	if success, failed, _ := readJobStatus(t, ctx, ok.Name); success == nil || failed != nil {
		t.Fatalf("succeeding job: success=%v failed=%v, want a success time and no failure", success, failed)
	}
	if success, failed, lastErr := readJobStatus(t, ctx, bad.Name); failed == nil || success != nil || lastErr == "" {
		t.Fatalf("failing job: success=%v failed=%v error=%q, want a failure time, no success, and the error", success, failed, lastErr)
	}
}

// TestJobStatusKeepsTheOtherOutcome checks the upsert: a success keeps the job's
// last failure time, a failure keeps its last success time, and a success clears
// the error.
func TestJobStatusKeepsTheOtherOutcome(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	name := uniqueJobName(t, "status-upsert")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM job_status WHERE job_name = $1`, name) })

	day1 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	day3 := day2.Add(24 * time.Hour)
	recordJobOutcome(ctx, pool, name, day1, true, "")
	recordJobOutcome(ctx, pool, name, day2, false, "boom")
	success, failed, lastErr := readJobStatus(t, ctx, name)
	if success == nil || !success.Equal(day1) || failed == nil || !failed.Equal(day2) || lastErr != "boom" {
		t.Fatalf("after a failure: success=%v failed=%v error=%q", success, failed, lastErr)
	}
	recordJobOutcome(ctx, pool, name, day3, true, "")
	success, failed, lastErr = readJobStatus(t, ctx, name)
	if success == nil || !success.Equal(day3) || failed == nil || !failed.Equal(day2) || lastErr != "" {
		t.Fatalf("after a recovery: success=%v failed=%v error=%q; want success day3, failure day2, no error", success, failed, lastErr)
	}
}
