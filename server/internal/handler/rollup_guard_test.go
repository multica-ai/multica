package handler

import (
	"context"
	"testing"
)

// rollupSingletonTestLock guards the global task_usage_hourly_rollup_state
// singleton (id = 1) and the cron entrypoint rollup_task_usage_hourly()
// across concurrently-running test binaries.
//
// `go test ./...` compiles internal/handler and internal/scheduler into
// separate binaries and runs them in parallel against the SAME
// DATABASE_URL. Both mutate that one singleton row and contend for the
// function's own advisory lock 4246. Without a cross-process guard they
// interleave and fail flakily (MUL-3980): the scheduler's
// TestPgCronConcurrentNoDoubleWrite forces the watermark ~90 min back and
// expects exactly one of six concurrent callers to advance it, while a
// handler rollup tick concurrently advances the same watermark past the
// window — so the scheduler sees winners=0, and the handler's
// TestRollupTaskUsageHourlyCapsWindowAtOneDay reads the scheduler's
// 90-min-old watermark (0.063 days) instead of "now".
//
// A dedicated session-level advisory lock — deliberately distinct from the
// rollup function's own 4246 (reusing 4246 would make the function's
// pg_try_advisory_lock fail from other pool connections) — serialises every
// test that touches the singleton. 42463980 = 4246 (rollup family) + 3980
// (the tracking issue) and collides with no production key.
const rollupSingletonTestLock int64 = 42463980

// lockRollupSingleton blocks until this test owns the rollup-singleton guard,
// then releases it (and the pinned connection) on cleanup. Call it at the very
// TOP of any test that writes task_usage_hourly_rollup_state or invokes
// rollup_task_usage_hourly(). Because it registers its release via t.Cleanup
// first, later cleanups in the same test (e.g. restoring the watermark) still
// run while the guard is held, then the guard is released last.
func lockRollupSingleton(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	// Advisory locks are per-session, so pin one connection for the lock's
	// lifetime (same idiom as internal/taskusagebackfill/backfill.go).
	conn, err := testPool.Acquire(ctx)
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
