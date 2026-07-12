package scheduler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// rollupSingletonTestLock guards the global task_usage_hourly_rollup_state
// singleton (id = 1) and the cron entrypoint rollup_task_usage_hourly()
// across concurrently-running test binaries.
//
// `go test ./...` compiles internal/scheduler and internal/handler into
// separate binaries and runs them in parallel against the SAME DATABASE_URL.
// Both mutate that one singleton row and contend for the function's own
// advisory lock 4246. Without a cross-process guard they interleave and fail
// flakily (MUL-3980): TestPgCronConcurrentNoDoubleWrite forces the watermark
// back and expects exactly one of six concurrent callers to advance it, but a
// handler rollup tick concurrently pushes the same watermark past the window,
// so this test sees winners=0.
//
// A dedicated session-level advisory lock — deliberately distinct from the
// rollup function's own 4246 (reusing 4246 would make the function's
// pg_try_advisory_lock fail from other pool connections) — serialises every
// test that touches the singleton. The value MUST match the one in
// internal/handler/rollup_guard_test.go so the two binaries serialise against
// each other.
const rollupSingletonTestLock int64 = 42463980

// lockRollupSingleton blocks until this test owns the rollup-singleton guard,
// then releases it (and the pinned connection) on cleanup. Call it at the very
// TOP of any test that writes task_usage_hourly_rollup_state or invokes
// rollup_task_usage_hourly().
func lockRollupSingleton(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Advisory locks are per-session, so pin one connection for the lock's
	// lifetime (same idiom as internal/taskusagebackfill/backfill.go).
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire rollup-guard connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, rollupSingletonTestLock); err != nil {
		conn.Release()
		t.Fatalf("acquire rollup singleton guard: %v", err)
	}
	t.Cleanup(func() {
		// Fresh context so a cancelled test context cannot skip the unlock.
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, rollupSingletonTestLock); err != nil {
			t.Logf("release rollup singleton guard: %v", err)
		}
		conn.Release()
	})
}
