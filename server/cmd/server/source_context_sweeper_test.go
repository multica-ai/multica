package main

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stalledSourceContextCleaner models an object store that stopped answering:
// every stage blocks until its context is done, which is what a hung or
// throttled endpoint looks like from inside a cleanup round.
type stalledSourceContextCleaner struct {
	intentCalls           atomic.Int32
	abandonedCalls        atomic.Int32
	intentLimit           atomic.Int32
	abandonedLimit        atomic.Int32
	intentDeadlineBounded atomic.Bool
}

func (c *stalledSourceContextCleaner) CleanupSourceContextObjectIntents(ctx context.Context, limit int) (int, error) {
	c.intentCalls.Add(1)
	c.intentLimit.Store(int32(limit))
	if deadline, ok := ctx.Deadline(); ok {
		c.intentDeadlineBounded.Store(time.Until(deadline) <= sourceContextSweepBudget)
	}
	<-ctx.Done()
	return 0, nil
}

func (c *stalledSourceContextCleaner) CleanupAbandonedSourceContexts(ctx context.Context, limit int32) (int, error) {
	c.abandonedCalls.Add(1)
	c.abandonedLimit.Store(limit)
	<-ctx.Done()
	return 0, nil
}

// TestSourceContextSweepRoundEndsAtItsBudget proves a stalled object store
// cannot hold a cleanup round open. Rounds are paced by their own ticker, so
// this budget is what keeps a stuck round from overlapping the next one.
func TestSourceContextSweepRoundEndsAtItsBudget(t *testing.T) {
	cleaner := &stalledSourceContextCleaner{}
	budget := 200 * time.Millisecond

	startedAt := time.Now()
	sweepSourceContextsWithBudget(context.Background(), cleaner, budget)
	elapsed := time.Since(startedAt)

	if elapsed > 2*time.Second {
		t.Fatalf("cleanup round ran %s, want it bounded by its %s budget", elapsed, budget)
	}
	if got := cleaner.intentCalls.Load(); got != 1 {
		t.Fatalf("object intent cleanup calls = %d, want 1", got)
	}
	if got := cleaner.abandonedCalls.Load(); got != 1 {
		t.Fatalf("abandoned capture cleanup calls = %d, want 1 even after the first stage used up the budget", got)
	}
	if got := cleaner.intentLimit.Load(); got != sourceContextCleanupBatchSize {
		t.Fatalf("object intent batch size = %d, want %d", got, sourceContextCleanupBatchSize)
	}
	if got := cleaner.abandonedLimit.Load(); got != sourceContextCleanupBatchSize {
		t.Fatalf("abandoned capture batch size = %d, want %d", got, sourceContextCleanupBatchSize)
	}
}

// TestSourceContextSweepPassesABoundedDeadline keeps the round budget wired to
// the service calls; without a deadline on that context the per-object timeouts
// derived from it would be the only bound left.
func TestSourceContextSweepPassesABoundedDeadline(t *testing.T) {
	cleaner := &stalledSourceContextCleaner{}
	sweepSourceContextsWithBudget(context.Background(), cleaner, 100*time.Millisecond)
	if !cleaner.intentDeadlineBounded.Load() {
		t.Fatal("cleanup stage ran without a deadline bounded by the round budget")
	}
}

// TestSourceContextSweeperStopsWithItsContext keeps shutdown clean: the loop
// must exit on context cancellation rather than waiting out a full interval.
func TestSourceContextSweeperStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		runSourceContextSweeper(ctx, &stalledSourceContextCleaner{})
		close(stopped)
	}()
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("source context sweeper did not stop with its context")
	}
}

// TestRuntimeSweepTickHasNoObjectStoreStage is a regression guard for the
// reason this sweeper exists (MUL-6555 / MUL-6670): source-context cleanup used
// to run at the top of the 30s runtime tick, so a slow object store delayed
// marking runtimes offline and reclaiming their orphaned tasks. Object-store
// work must stay out of that tick; put it in runSourceContextSweeper instead.
func TestRuntimeSweepTickHasNoObjectStoreStage(t *testing.T) {
	source, err := os.ReadFile("runtime_sweeper.go")
	if err != nil {
		t.Fatalf("read runtime_sweeper.go: %v", err)
	}
	for _, stage := range []string{"CleanupSourceContextObjectIntents", "CleanupAbandonedSourceContexts"} {
		if strings.Contains(string(source), stage) {
			t.Fatalf("runtime_sweeper.go calls %s; object-store cleanup belongs on runSourceContextSweeper", stage)
		}
	}
}
