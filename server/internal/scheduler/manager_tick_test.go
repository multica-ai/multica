package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestManagerTickClosesAbandonedRunning exercises张大彪's blocker #1
// directly through the Manager.runOnce tick path:
//
//	"Manager 在 plan 推进后必须能扫到旧的 stale RUNNING row 重入或
//	 转 FAILED，不能只盯当前 latest——得加用例直接打 Manager 的 tick
//	 路径，别再只测 tryClaim"
//
// We seed a stuck RUNNING row at an old plan_time (representing a
// crashed pod's lease) and run Manager.runOnce. The tick must:
//
//   - close out the abandoned lease (status='FAILED',
//     error_code='stale_timeout'), and
//   - separately claim + complete the current latest plan_time.
//
// We verify both branches by registering a no-op handler that always
// succeeds, so the current plan ends in SUCCESS while the old plan
// ends in FAILED.
//
// The test runs the assertion for both AllowStaleReentry=true and
// AllowStaleReentry=false, because before this fix the reentrant path
// only swept stale rows during a same-plan_time replay, which never
// happens in latest_only mode.
func TestManagerTickClosesAbandonedRunning(t *testing.T) {
	for _, allowStaleReentry := range []bool{true, false} {
		t.Run(boolName("reentrant", allowStaleReentry), func(t *testing.T) {
			pool := integrationPool(t)
			job := newTestJobSpec(uniqueJobName(t, "tick_stale"))
			job.AllowStaleReentry = allowStaleReentry
			t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

			ctx := context.Background()
			now, err := dbNow(ctx, pool)
			if err != nil {
				t.Fatalf("dbNow: %v", err)
			}

			// Seed a stale RUNNING row at the previous bucket. Lease
			// is stamped to a long-dead runner so the Manager has no
			// chance to take credit for finishing it.
			oldPlan := FloorPlan(now, job.Cadence).Add(-job.Cadence)
			oldRunner := "ghost-runner"
			oldLease := uuid.New()
			if _, err := pool.Exec(ctx, `
				INSERT INTO sys_cron_executions (
					job_name, scope_kind, scope_id, plan_time,
					status, attempt, max_attempts,
					runner_id, lease_token,
					heartbeat_at, stale_after,
					started_at, updated_at
				) VALUES (
					$1, 'global', 'global', $2,
					'RUNNING', 1, $3,
					$4, $5,
					now() - INTERVAL '10 minutes',
					now() - INTERVAL '5 minutes',
					now() - INTERVAL '10 minutes',
					now() - INTERVAL '10 minutes'
				)
			`, job.Name, oldPlan, job.MaxAttempts, oldRunner, oldLease); err != nil {
				t.Fatalf("seed stale RUNNING row: %v", err)
			}

			mgr := NewManager(pool, Options{RunnerID: "manager-under-test"})
			if err := mgr.Register(*job); err != nil {
				t.Fatalf("register: %v", err)
			}
			if err := mgr.RunOnce(ctx); err != nil {
				t.Fatalf("runOnce: %v", err)
			}

			// Assert the old row is now FAILED with stale_timeout.
			var oldStatus, oldErr string
			if err := pool.QueryRow(ctx, `
				SELECT status, COALESCE(error_code, '')
				  FROM sys_cron_executions
				 WHERE job_name = $1 AND plan_time = $2
			`, job.Name, oldPlan).Scan(&oldStatus, &oldErr); err != nil {
				t.Fatalf("scan old row: %v", err)
			}
			if oldStatus != "FAILED" {
				t.Fatalf("old stale RUNNING row should be FAILED, got %q", oldStatus)
			}
			if oldErr != "stale_timeout" {
				t.Fatalf("old row should carry error_code=stale_timeout, got %q", oldErr)
			}

			// Assert the current plan got picked up + finished SUCCESS
			// by this Manager.
			currentPlan := FloorPlan(now, job.Cadence)
			var curStatus, curRunner string
			if err := pool.QueryRow(ctx, `
				SELECT status, runner_id
				  FROM sys_cron_executions
				 WHERE job_name = $1 AND plan_time = $2
			`, job.Name, currentPlan).Scan(&curStatus, &curRunner); err != nil {
				t.Fatalf("scan current row: %v", err)
			}
			if curStatus != "SUCCESS" {
				t.Fatalf("current plan should be SUCCESS after tick, got %q", curStatus)
			}
			if curRunner != "manager-under-test" {
				t.Fatalf("current plan runner should be the test Manager, got %q", curRunner)
			}
		})
	}
}

// TestManagerHandlerPanicWritesFailed exercises张大彪's blocker #2:
//
//	"handler 的 panic recover 必须把 panic 写进 handlerErr 走
//	 finishFailure，绝对不能记成 SUCCESS，加测试钉死。"
//
// A panicking handler must NOT result in a SUCCESS audit row. This
// test installs a handler that always panics, runs one tick, and
// asserts the resulting row is FAILED with error_code=handler_panic
// and a non-empty error_msg containing the panic value.
func TestManagerHandlerPanicWritesFailed(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "panic"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	job.Handler = func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		panic("simulated handler boom")
	}

	mgr := NewManager(pool, Options{RunnerID: "panic-runner"})
	if err := mgr.Register(*job); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mgr.RunOnce(ctx); err != nil {
		t.Fatalf("runOnce: %v", err)
	}

	var status, errCode, errMsg string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_code, ''), COALESCE(error_msg, '')
		  FROM sys_cron_executions
		 WHERE job_name = $1
		 ORDER BY plan_time DESC
		 LIMIT 1
	`, job.Name).Scan(&status, &errCode, &errMsg); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("panicking handler must NOT write SUCCESS; got status=%q", status)
	}
	if errCode != "handler_panic" {
		t.Fatalf("expected error_code=handler_panic, got %q", errCode)
	}
	if errMsg == "" {
		t.Fatalf("expected non-empty error_msg containing panic detail")
	}
	// Sanity: error_msg should mention the panic value so on-call can
	// triage from the audit row alone.
	if !contains(errMsg, "simulated handler boom") {
		t.Fatalf("expected error_msg to include panic value, got %q", errMsg)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func boolName(label string, v bool) string {
	if v {
		return label + "=true"
	}
	return label + "=false"
}
