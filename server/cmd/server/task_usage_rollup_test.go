package main

import (
	"context"
	"testing"
	"time"
)

// TestTickTaskUsageRollup_Callable guards against drift between this caller and
// migration 102's rollup_task_usage_hourly() definition: a renamed function or
// changed return type would surface here rather than as a silent no-op in prod.
func TestTickTaskUsageRollup_Callable(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	var rows int64
	if err := testPool.QueryRow(context.Background(), `SELECT rollup_task_usage_hourly()`).Scan(&rows); err != nil {
		t.Fatalf("rollup_task_usage_hourly() not callable: %v", err)
	}
	if rows < 0 {
		t.Fatalf("expected non-negative rows_touched, got %d", rows)
	}

	// The ticker's tick wrapper must also run cleanly against the real schema.
	tickTaskUsageRollup(context.Background(), testPool)
}

// TestRunTaskUsageRollup_StopsOnCancel verifies the loop returns when its
// context is cancelled, so server shutdown doesn't leak the goroutine.
func TestRunTaskUsageRollup_StopsOnCancel(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		runTaskUsageRollup(ctx, testPool)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runTaskUsageRollup did not return after context cancel")
	}
}
