package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationPool returns a pool against the configured DATABASE_URL,
// or skips the test if the database is not reachable. Mirrors the
// pattern used by server/cmd/server/integration_test.go and
// internal/handler/handler_test.go (see those files' TestMain).
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("scheduler integration tests require Postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("scheduler integration tests require Postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func cleanupExecutions(t *testing.T, pool *pgxpool.Pool, jobName string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM sys_cron_executions WHERE job_name = $1`, jobName); err != nil {
		t.Fatalf("clean executions: %v", err)
	}
}

// uniqueJobName isolates concurrent CI runs so each test has its own
// job-name partition in sys_cron_executions.
func uniqueJobName(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()[:8])
}

func newTestJobSpec(name string) *JobSpec {
	return &JobSpec{
		Name:              name,
		Cadence:           5 * time.Minute,
		ScheduleDelay:     0,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        time.Minute,
		StaleTimeout:      2 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, nil
		},
	}
}

// TestStaleStealTerminalUpdateIgnored covers RFC §14:
//
//	"stale steal | winner heartbeat stops, stale_after expires, another
//	 runner steal lease; old lease terminal update doesn't take effect."
//
// We claim a plan, simulate a stuck handler by setting stale_after into
// the past, steal it as a second runner, then verify that the FIRST
// runner's terminal SUCCESS write is rejected by the lease_token guard
// — the row stays in the second runner's RUNNING state.
func TestStaleStealTerminalUpdateIgnored(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "stale_steal"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	ctx := context.Background()
	now, err := dbNow(ctx, pool)
	if err != nil {
		t.Fatalf("dbNow: %v", err)
	}
	planTime := FloorPlan(now, job.Cadence)

	// Step 1: original runner claims the plan.
	original, err := tryClaim(ctx, pool, job, ScopeGlobal, planTime, now, "runner-A")
	if err != nil {
		t.Fatalf("initial claim: %v", err)
	}
	if !original.Won {
		t.Fatalf("expected fresh win, got %+v", original)
	}

	// Step 2: simulate a stuck handler — stale_after moved to the past.
	if _, err := pool.Exec(ctx, `
		UPDATE sys_cron_executions
		   SET stale_after  = now() - INTERVAL '1 minute',
		       heartbeat_at = now() - INTERVAL '5 minute'
		 WHERE id = $1
	`, original.ID); err != nil {
		t.Fatalf("force stale: %v", err)
	}

	// Step 3: a different runner steals the lease via the same primitive.
	stealNow, err := dbNow(ctx, pool)
	if err != nil {
		t.Fatalf("dbNow: %v", err)
	}
	stolen, err := tryClaim(ctx, pool, job, ScopeGlobal, planTime, stealNow, "runner-B")
	if err != nil {
		t.Fatalf("steal claim: %v", err)
	}
	if !stolen.Stole {
		t.Fatalf("expected stale steal, got %+v", stolen)
	}
	if stolen.ID != original.ID {
		t.Fatalf("steal must reuse the same row id, got original=%s stolen=%s",
			original.ID, stolen.ID)
	}
	if stolen.LeaseToken == original.LeaseToken {
		t.Fatalf("steal must rotate lease_token; got %s == %s",
			stolen.LeaseToken, original.LeaseToken)
	}
	if stolen.Attempt != 2 {
		t.Fatalf("steal must increment attempt; got %d", stolen.Attempt)
	}

	// Step 4: the original runner returns and tries to write SUCCESS
	// with its OLD lease_token. The guard must reject the update so
	// the row stays in runner-B's RUNNING state.
	doneTime, _ := dbNow(ctx, pool)
	err = finishSuccess(ctx, pool, original.ID, original.LeaseToken,
		doneTime, 1234, HandlerResult{RowsAffected: 7})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost from old lease finish, got %v", err)
	}

	// Step 5: verify the row is still RUNNING under runner-B.
	var status, runner string
	var attempt int
	if err := pool.QueryRow(ctx, `
		SELECT status, runner_id, attempt
		  FROM sys_cron_executions
		 WHERE id = $1
	`, original.ID).Scan(&status, &runner, &attempt); err != nil {
		t.Fatalf("scan post-steal row: %v", err)
	}
	if status != "RUNNING" {
		t.Fatalf("expected status RUNNING after old-runner overwrite was rejected, got %q", status)
	}
	if runner != "runner-B" {
		t.Fatalf("expected runner_id runner-B, got %q", runner)
	}
	if attempt != 2 {
		t.Fatalf("expected attempt=2 (B's fresh attempt), got %d", attempt)
	}

	// Step 6: runner B finishes successfully with its own token — that
	// must be accepted.
	if err := finishSuccess(ctx, pool, stolen.ID, stolen.LeaseToken,
		doneTime, 4321, HandlerResult{RowsAffected: 11}); err != nil {
		t.Fatalf("legitimate finishSuccess from runner-B: %v", err)
	}
	var finalStatus string
	var finalRows int64
	if err := pool.QueryRow(ctx, `
		SELECT status, rows_affected
		  FROM sys_cron_executions
		 WHERE id = $1
	`, original.ID).Scan(&finalStatus, &finalRows); err != nil {
		t.Fatalf("scan final row: %v", err)
	}
	if finalStatus != "SUCCESS" {
		t.Fatalf("expected final status SUCCESS, got %q", finalStatus)
	}
	if finalRows != 11 {
		t.Fatalf("expected rows_affected=11 (B's value), got %d", finalRows)
	}
}
