package inboxread

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type testTx struct{ pgx.Tx }

func (testTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (testTx) Commit(context.Context) error   { return nil }
func (testTx) Rollback(context.Context) error { return nil }

type testStarter struct{ calls atomic.Int32 }

func (s *testStarter) Begin(context.Context) (pgx.Tx, error) { s.calls.Add(1); return testTx{}, nil }

type blockedStarter struct{}

func (blockedStarter) Begin(ctx context.Context) (pgx.Tx, error) { <-ctx.Done(); return nil, ctx.Err() }

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBoundedExecutionAndQueue(t *testing.T) {
	g := New(Config{Concurrency: 2})
	starter := &testStarter{}
	gate := make(chan struct{})
	defer close(gate)
	var executing atomic.Int32
	query := func(context.Context, pgx.Tx) error { executing.Add(1); <-gate; return nil }
	results := make(chan error, 22)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 2 {
		go func() { results <- g.Run(ctx, "list", starter, query) }()
	}
	eventually(t, func() bool { return executing.Load() == 2 })
	for range 20 {
		go func() { results <- g.Run(ctx, "snoozed", starter, query) }()
	}
	eventually(t, func() bool { return len(g.queued) == 20 })
	if err := g.Run(ctx, "summary", starter, query); !errors.Is(err, ErrBusy) {
		t.Fatalf("overflow = %v", err)
	}
	if n := starter.calls.Load(); n != 2 {
		t.Fatalf("queued/rejected calls acquired connections: %d", n)
	}
	cancel()
	for range 20 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("queued cancel = %v", err)
		}
	}
	// Already executing SQL owns its permits until the database work finishes.
	if len(g.active) != 2 {
		t.Fatal("cancellation released active database capacity")
	}
	gate <- struct{}{}
	gate <- struct{}{}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(g.active) != 0 || len(g.queued) != 0 {
		t.Fatal("capacity leaked")
	}
}

func TestQueueWaitAndPoolAcquireAreBounded(t *testing.T) {
	g := New(Config{Concurrency: 1})
	starter := &testStarter{}
	release, err := g.acquire(context.Background(), "list")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = g.Run(context.Background(), "list", starter, func(context.Context, pgx.Tx) error { t.Fatal("entered SQL"); return nil })
	release()
	if !errors.Is(err, ErrBusy) || time.Since(start) > time.Second || starter.calls.Load() != 0 {
		t.Fatalf("queue wait = %v", err)
	}
	start = time.Now()
	err = g.Run(context.Background(), "list", blockedStarter{}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second || len(g.active) != 0 {
		t.Fatalf("pool acquire = %v", err)
	}
}

func TestSummaryCancellationIsolationAndNoCache(t *testing.T) {
	g := New(Config{Concurrency: 2})
	ctx, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	query := func(ctx context.Context) (any, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-gate
		return "shared", ctx.Err()
	}
	first := make(chan error, 1)
	go func() { _, err := g.Summary(ctx, "user-a", query); first <- err }()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first = %v", err)
	}
	second := make(chan error, 1)
	go func() {
		value, err := g.Summary(context.Background(), "user-a", query)
		if value != "shared" && err == nil {
			err = errors.New("wrong shared value")
		}
		second <- err
	}()
	eventually(t, func() bool { return len(g.summaryWaiters) == 2 })
	// A different account never shares this account's data.
	value, err := g.Summary(context.Background(), "user-b", func(context.Context) (any, error) { return "other", nil })
	if err != nil || value != "other" {
		t.Fatalf("different user = %v, %v", value, err)
	}
	close(gate)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one fresh read for the late caller, got: %d", calls.Load())
	}
	eventually(t, func() bool { return len(g.summaryWaiters) == 0 })
	value, err = g.Summary(context.Background(), "user-a", func(context.Context) (any, error) { return "fresh", nil })
	if err != nil || value != "fresh" {
		t.Fatalf("summary cached stale result: %v %v", value, err)
	}
}

func TestSummaryWaitersRemainBoundedAfterCancellation(t *testing.T) {
	g := New(Config{Concurrency: 1})
	gate := make(chan struct{})
	var executions atomic.Int32
	query := func(context.Context) (any, error) { executions.Add(1); <-gate; return nil, nil }
	for index := range cap(g.summaryWaiters) {
		ctx, cancel := context.WithCancel(context.Background())
		returned := make(chan error, 1)
		go func() { _, err := g.Summary(ctx, "same-user", query); returned <- err }()
		eventually(t, func() bool { return len(g.summaryWaiters) == index+1 })
		cancel()
		if err := <-returned; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter = %v", err)
		}
	}
	if _, err := g.Summary(context.Background(), "same-user", query); !errors.Is(err, ErrBusy) {
		t.Fatalf("waiter overflow = %v", err)
	}
	close(gate)
	eventually(t, func() bool { return len(g.summaryWaiters) == 0 })
	if executions.Load() > 2 {
		t.Fatalf("queued callers were not coalesced: %d executions", executions.Load())
	}
}

func TestConfigDefaultsAndInvalidValues(t *testing.T) {
	for _, name := range []string{"INBOX_READ_CONCURRENCY", "INBOX_READ_STATEMENT_TIMEOUT_MS", "INBOX_READ_TIMEOUT_MS"} {
		t.Setenv(name, "")
	}
	if cfg := ConfigFromEnv(); cfg.Concurrency != 2 || cfg.StatementTimeout != 3*time.Second || cfg.RequestTimeout != 5*time.Second {
		t.Fatalf("defaults = %+v", cfg)
	}
	t.Setenv("INBOX_READ_CONCURRENCY", "0")
	t.Setenv("INBOX_READ_STATEMENT_TIMEOUT_MS", "garbage")
	if cfg := ConfigFromEnv(); cfg.Concurrency != 2 || cfg.StatementTimeout != 3*time.Second {
		t.Fatalf("invalid config = %+v", cfg)
	}
	t.Setenv("INBOX_READ_STATEMENT_TIMEOUT_MS", "6000")
	if cfg := ConfigFromEnv(); cfg.RequestTimeout < cfg.StatementTimeout+time.Second {
		t.Fatalf("missing deadline headroom = %+v", cfg)
	}
}

func TestTransportFailureRetainsPermitUntilServerBudgetExpires(t *testing.T) {
	g := New(Config{Concurrency: 1, StatementTimeout: 20 * time.Millisecond})
	start := time.Now()
	err := g.Run(context.Background(), "list", &testStarter{}, func(context.Context, pgx.Tx) error { return io.ErrUnexpectedEOF })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("uncertain SQL permit duration = %s", elapsed)
	}
	if len(g.active) != 0 {
		t.Fatal("permit leaked after server budget expired")
	}
}

func TestSummaryLateRefreshDoesNotReusePreMutationSnapshot(t *testing.T) {
	g := New(Config{Concurrency: 2})
	var current, calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	query := func(context.Context) (any, error) {
		snapshot := current.Load()
		if calls.Add(1) == 1 {
			close(started)
			<-finish
		}
		return snapshot, nil
	}
	first, second := make(chan any, 1), make(chan any, 1)
	read := func(results chan<- any) {
		value, err := g.Summary(context.Background(), "user", query)
		if err != nil {
			results <- err
		} else {
			results <- value
		}
	}
	go read(first)
	<-started
	current.Store(1) // A committed mutation and its subsequent refresh event.
	go read(second)
	eventually(t, func() bool {
		g.summaryMu.Lock()
		defer g.summaryMu.Unlock()
		return g.summaryCalls["user"].next != nil
	})
	close(finish)
	if value := <-first; value != int32(0) {
		t.Fatalf("original generation lost its successful result: %v", value)
	}
	if value := <-second; value != int32(1) {
		t.Fatalf("refresh accepted stale snapshot: %v", value)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one follow-up read, got %d", calls.Load())
	}
	g.summaryMu.Lock()
	defer g.summaryMu.Unlock()
	if len(g.summaryCalls) != 0 {
		t.Fatal("completed summary version leaked")
	}
}

func TestSummarySlowReadReturnsEachGenerationWithItsOwnBudget(t *testing.T) {
	g := New(Config{Concurrency: 2})
	starter := &testStarter{}
	var runs atomic.Int32
	var executing atomic.Int32
	started := make(chan struct{})
	secondGate := make(chan struct{})
	var releaseOnce sync.Once
	releaseSecond := func() { releaseOnce.Do(func() { close(secondGate) }) }
	defer releaseSecond()
	type result struct {
		value any
		err   error
	}
	query := func(ctx context.Context) (any, error) {
		var value int32
		err := g.Run(ctx, "summary", starter, func(context.Context, pgx.Tx) error {
			if n := executing.Add(1); n != 1 {
				t.Errorf("same account ran %d queries concurrently", n)
			}
			defer executing.Add(-1)
			value = runs.Add(1)
			if value == 1 {
				close(started)
			} else {
				<-secondGate // The first caller must return before this read finishes.
			}
			time.Sleep(1500 * time.Millisecond)
			return nil
		})
		return value, err
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() { value, err := g.Summary(context.Background(), "user", query); first <- result{value, err} }()
	<-started
	time.Sleep(300 * time.Millisecond)
	go func() { value, err := g.Summary(context.Background(), "user", query); second <- result{value, err} }()
	for _, waiter := range []struct {
		result <-chan result
		want   int32
	}{{first, 1}, {second, 2}} {
		select {
		case got := <-waiter.result:
			if got.err != nil || got.value != waiter.want {
				t.Fatalf("generation result = %+v, want %d", got, waiter.want)
			}
			if waiter.want == 1 {
				releaseSecond()
			}
		case <-time.After(5 * time.Second):
			t.Fatal("generation did not finish")
		}
	}
	if runs.Load() != 2 {
		t.Fatalf("SQL executions = %d, want 2", runs.Load())
	}
}
