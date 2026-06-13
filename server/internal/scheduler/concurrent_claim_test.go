package scheduler

import (
	"context"
	"sync"
	"testing"
)

// TestConcurrentClaimsSingleWinner covers RFC §14:
//
//	"pg_cron 并跑 | app scheduler 与 pg_cron 同时调用函数,
//	 无重复窗口写入"
//
// The legacy `pg_cron` tick and the in-process scheduler both call
// `rollup_task_usage_hourly()`, so the SQL function's advisory lock
// 4246 prevents double-writes of the rollup itself. The scheduler adds
// a second layer via `sys_cron_executions`: even if multiple ticks (a
// scheduler in a second replica, a manual SQL call, a leftover
// `pg_cron` job) arrive at the same plan_time, only one row exists per
// (job, scope, plan_time) and only one runner gets Won=true. The rest
// fall through the conflict path and no-op.
//
// We simulate this by firing N concurrent claims at the same plan_time
// from distinct runner ids and asserting the table contract:
//
//   - Exactly ONE caller observes Won=true.
//   - Every other caller observes Conflicted=true (no Won, no Stole).
//   - sys_cron_executions has exactly one row for the plan.
//   - The row's runner_id matches the winner.
//
// This is the same single-winner property the SQL advisory lock 4246
// gives at the function-execution layer; the `sys_cron_executions`
// uniqueness key gives it at the scheduler layer, so a `pg_cron` tick
// running alongside the in-process scheduler cannot produce a duplicate
// SUCCESS audit row.
func TestConcurrentClaimsSingleWinner(t *testing.T) {
	pool := integrationPool(t)
	job := newTestJobSpec(uniqueJobName(t, "concurrent_claim"))
	t.Cleanup(func() { cleanupExecutions(t, pool, job.Name) })

	ctx := context.Background()
	now, err := dbNow(ctx, pool)
	if err != nil {
		t.Fatalf("dbNow: %v", err)
	}
	planTime := FloorPlan(now, job.Cadence)

	const contenders = 8
	type result struct {
		runnerID string
		c        claim
		err      error
	}
	results := make([]result, contenders)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range contenders {
		i := i
		runnerID := "runner-" + string(rune('A'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c, err := tryClaim(ctx, pool, job, ScopeGlobal, planTime, now, runnerID)
			results[i] = result{runnerID: runnerID, c: c, err: err}
		}()
	}
	close(start)
	wg.Wait()

	wins := 0
	conflicts := 0
	steals := 0
	var winner string
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("contender %s: %v", r.runnerID, r.err)
		}
		switch {
		case r.c.Won:
			wins++
			winner = r.runnerID
		case r.c.Stole:
			steals++
		case r.c.Conflicted:
			conflicts++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly 1 fresh winner, got %d (conflicts=%d steals=%d)",
			wins, conflicts, steals)
	}
	if steals != 0 {
		t.Fatalf("a fresh insert race must not produce a stale steal, got %d steals", steals)
	}
	if conflicts != contenders-1 {
		t.Fatalf("expected %d conflicts, got %d", contenders-1, conflicts)
	}

	// Database-side proof: exactly one row, runner_id matches.
	var rowCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sys_cron_executions WHERE job_name = $1 AND plan_time = $2
	`, job.Name, planTime).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 row in sys_cron_executions, got %d", rowCount)
	}

	var dbRunner string
	if err := pool.QueryRow(ctx, `
		SELECT runner_id FROM sys_cron_executions WHERE job_name = $1 AND plan_time = $2
	`, job.Name, planTime).Scan(&dbRunner); err != nil {
		t.Fatalf("scan winner: %v", err)
	}
	if dbRunner != winner {
		t.Fatalf("DB winner %q != local winner %q", dbRunner, winner)
	}
}
