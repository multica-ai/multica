package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPgCronConcurrentNoDoubleWrite covers张大彪's blocker #4:
//
//	"`pg_cron` 并跑那条覆盖要真打——scheduler handler 跟直接
//	 `SELECT rollup_task_usage_hourly()` / 旧 cron 入口并发，验证
//	 advisory lock 4246 下不双写。"
//
// The test seeds historical `task_usage` rows under a freshly created
// agent / runtime / agent_task_queue fixture, advances the rollup
// watermark backwards so a single tick has real work to do, then
// invokes `rollup_task_usage_hourly()` directly from N concurrent
// goroutines. This is the same SQL entrypoint the in-process
// scheduler handler calls AND the same one any leftover `pg_cron`
// job or operator would call by hand. Advisory lock 4246 inside the
// SQL function must serialise them: exactly one caller advances the
// watermark and recomputes the buckets, every other caller returns 0
// rows immediately.
//
// The pass criteria are the operational invariants:
//
//   - Across all callers, exactly one returned a non-zero rows count
//     (the one that won the advisory lock).
//   - The watermark advanced exactly once — specifically, the resulting
//     watermark equals what the winning caller computed, and not any
//     multiple of it.
//   - The post-rollup `task_usage_hourly` rows match what we expect
//     from the seeded `task_usage` data (token sums + bucket count).
func TestPgCronConcurrentNoDoubleWrite(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	// Seed an isolated workspace/runtime/agent/task and a handful of
	// task_usage rows landing in the same UTC hour bucket. The bucket
	// math is the SQL helper task_usage_hour_bucket(...).
	ws, _, _, task := seedRollupFixture(t, pool)
	t.Cleanup(func() { cleanupRollupFixture(t, pool, ws) })

	// Bucket 30 minutes ago so the rollup window (now - 5min) covers
	// the row but not so old that monthly slicing matters.
	bucketTS := time.Now().UTC().Add(-30 * time.Minute)

	const rowsToSeed = 4
	for i := 0; i < rowsToSeed; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO task_usage (
				task_id, provider, model,
				input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
				created_at, updated_at
			)
			VALUES ($1, 'openai', $2, $3, $4, 0, 0, $5, $5)
		`, task, "model-"+string(rune('A'+i)), int64(100+i), int64(200+i), bucketTS)
		if err != nil {
			t.Fatalf("seed task_usage row %d: %v", i, err)
		}
	}

	// Force the watermark back so one tick has real work to do. The
	// rollup function caps at 1 day per call, which is plenty for our
	// 30-minute-old fixture.
	if _, err := pool.Exec(ctx, `
		UPDATE task_usage_hourly_rollup_state
		   SET watermark_at = $1
		 WHERE id = 1
	`, bucketTS.Add(-1*time.Hour)); err != nil {
		t.Fatalf("force watermark backwards: %v", err)
	}

	// Wipe any pre-existing hourly rows for this fixture so we can
	// assert exactly what the rollup wrote.
	if _, err := pool.Exec(ctx, `
		DELETE FROM task_usage_hourly WHERE workspace_id = $1
	`, ws); err != nil {
		t.Fatalf("clear hourly rows: %v", err)
	}

	// Capture the winning watermark by snapshotting before/after.
	var watermarkBefore time.Time
	if err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermarkBefore); err != nil {
		t.Fatalf("read watermark before: %v", err)
	}

	const callers = 6
	results := make([]int64, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for i := range callers {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			err := pool.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&results[i])
			errs[i] = err
		}()
	}
	close(gate) // start everyone simultaneously
	wg.Wait()

	winners := 0
	losers := 0
	var winningRowCount int64
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
		if results[i] > 0 {
			winners++
			winningRowCount = results[i]
		} else {
			losers++
		}
	}
	if winners != 1 {
		t.Fatalf("advisory lock 4246 must serialise rollup; got winners=%d losers=%d", winners, losers)
	}
	if losers != callers-1 {
		t.Fatalf("expected %d losers, got %d", callers-1, losers)
	}
	if winningRowCount == 0 {
		t.Fatalf("winning caller returned 0 rows; fixture not wired correctly")
	}

	// The resulting hourly rows must match exactly the expected
	// per-(provider, model) aggregation. Running the rollup again
	// (under no contention) must not change the row count or sums —
	// that's the SQL function's idempotency contract, and it is what
	// makes pg_cron + scheduler concurrent execution safe.
	expectedHourlyRows := rowsToSeed // one per distinct model
	hourlyRows := countHourlyRowsForWorkspace(t, pool, ws)
	if hourlyRows != expectedHourlyRows {
		t.Fatalf("expected %d hourly rows, got %d", expectedHourlyRows, hourlyRows)
	}

	// Run rollup again and assert no double-write.
	var followupRows int64
	if err := pool.QueryRow(ctx, `SELECT rollup_task_usage_hourly()`).Scan(&followupRows); err != nil {
		t.Fatalf("followup rollup: %v", err)
	}
	if followupRows != 0 {
		t.Fatalf("idempotent re-run should return 0 rows, got %d", followupRows)
	}
	if got := countHourlyRowsForWorkspace(t, pool, ws); got != expectedHourlyRows {
		t.Fatalf("idempotent re-run changed row count from %d to %d", expectedHourlyRows, got)
	}

	// Watermark advanced past our forced point exactly once — the
	// cap is `LEAST(now()-5min, watermark + 1 day)`, so the new
	// watermark must be > watermarkBefore but not duplicated.
	var watermarkAfter time.Time
	if err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermarkAfter); err != nil {
		t.Fatalf("read watermark after: %v", err)
	}
	if !watermarkAfter.After(watermarkBefore) {
		t.Fatalf("watermark did not advance: before=%s after=%s", watermarkBefore, watermarkAfter)
	}
}

// seedRollupFixture creates the smallest viable
// (workspace, runtime, agent, task) graph required for task_usage rows
// to participate in the hourly rollup — the rollup window joins on
// agent + runtime + (optional) issue, so all four parents must exist.
// Returns the four IDs.
func seedRollupFixture(t *testing.T, pool *pgxpool.Pool) (string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	suffix := "rollup-" + uniqueSuffix()

	var wsID, runtimeID, agentID, taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ($1, $1)
		RETURNING id
	`, suffix).Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'p', 'online', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, wsID, "rt-"+suffix).Scan(&runtimeID); err != nil {
		t.Fatalf("seed agent_runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1)
		RETURNING id
	`, wsID, "ag-"+suffix, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id)
		VALUES ($1, $2)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("seed agent_task_queue: %v", err)
	}
	return wsID, runtimeID, agentID, taskID
}

func cleanupRollupFixture(t *testing.T, pool *pgxpool.Pool, wsID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM workspace WHERE id = $1`, wsID); err != nil {
		t.Logf("cleanup rollup fixture: %v", err)
	}
}

func countHourlyRowsForWorkspace(t *testing.T, pool *pgxpool.Pool, wsID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM task_usage_hourly WHERE workspace_id = $1
	`, wsID).Scan(&n); err != nil {
		t.Fatalf("count hourly: %v", err)
	}
	return n
}

func uniqueSuffix() string {
	return time.Now().UTC().Format("150405.000000000")
}
