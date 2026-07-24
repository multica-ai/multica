package ghsnapshot

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"
)

func enabledClient(t *testing.T) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{appID: "1", privateKey: key, tokens: map[int64]cachedToken{}, now: time.Now}
}

// TestManagerDisabledNoOps is the clean-degradation guarantee (acceptance
// criterion 4): with no App key the manager touches nothing.
func TestManagerDisabledNoOps(t *testing.T) {
	m := NewManager(nil, nil, nil, nil)
	if m.Enabled() {
		t.Fatal("nil-client manager must be disabled")
	}
	m.Enqueue(1, "o", "r", 2)
	m.MaybeEnqueueOnView(1, "o", "r", 2, time.Time{}, false)
	if len(m.queue) != 0 {
		t.Fatalf("disabled manager enqueued %d items, want 0", len(m.queue))
	}
	// Start must be a safe no-op (no workers, no panic).
	m.Start(context.Background())
}

// TestEnqueueCoalesces proves the dedup / single-in-flight key (acceptance
// criterion 3): the same PR address enqueued repeatedly is coalesced to one
// pending item; distinct addresses are not.
func TestEnqueueCoalesces(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	// Workers are NOT started, so items accumulate in the queue for inspection.
	m.Enqueue(1, "o", "r", 7)
	m.Enqueue(1, "o", "r", 7)
	m.Enqueue(1, "o", "r", 7)
	if len(m.queue) != 1 {
		t.Fatalf("same address enqueued 3× produced %d queued items, want 1", len(m.queue))
	}
	m.Enqueue(1, "o", "r", 8) // different PR
	m.Enqueue(1, "o", "other", 7)
	if len(m.queue) != 3 {
		t.Fatalf("queue length = %d, want 3 distinct", len(m.queue))
	}
}

// TestMaybeEnqueueOnViewRespectsTTL: a fresh snapshot is not refreshed on view;
// a stale or missing one is.
func TestMaybeEnqueueOnViewRespectsTTL(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(10000, 0)
	m.now = func() time.Time { return now }

	m.MaybeEnqueueOnView(1, "o", "r", 1, now.Add(-10*time.Second), true) // fresh (<60s)
	if len(m.queue) != 0 {
		t.Fatal("fresh snapshot should not refresh on view")
	}
	m.MaybeEnqueueOnView(1, "o", "r", 2, now.Add(-5*time.Minute), true) // stale
	m.MaybeEnqueueOnView(1, "o", "r", 3, time.Time{}, false)            // never fetched
	if len(m.queue) != 2 {
		t.Fatalf("stale/missing snapshots enqueued %d, want 2", len(m.queue))
	}
}

// TestProcessRateLimitedSetsPause proves a rate-limited fetch records a pause
// only for that installation (acceptance criterion 3) and writes nothing.
func TestProcessRateLimitedSetsPause(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(20000, 0)
	m.now = func() time.Time { return now }
	m.jitter = func() time.Duration { return 0 }
	// A cancelled ctx keeps the rescheduled retry from lingering after the test.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		return nil, &RateLimitError{RetryAfter: 90 * time.Second}
	}
	// queries/pool are nil; the fetch errors before any DB access, proving the
	// rate-limit path never touches storage.
	m.process(ctx, address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1})

	if got := m.rateUntil[1]; !got.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("rateUntil = %v, want %v", got, now.Add(90*time.Second))
	}
}

func TestRateLimitedInstallationDoesNotOccupyWorkers(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.concurrency = 12
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }
	m.extendRateLimit(1, 2*time.Second)

	limitedFetched := make(chan struct{}, 1)
	otherFetched := make(chan struct{}, 1)
	m.fetch = func(_ context.Context, _ *Client, installationID int64, _ string, _ string, _ int32) (*PRSnapshot, error) {
		switch installationID {
		case 1:
			limitedFetched <- struct{}{}
		case 2:
			otherFetched <- struct{}{}
		}
		return nil, errors.New("stop after fetch")
	}

	// Fill an entire worker pool with addresses from the paused installation,
	// then queue an unrelated tenant behind them.
	for number := int32(1); number <= int32(m.concurrency); number++ {
		m.Enqueue(1, "o", "r", number)
	}
	m.Enqueue(2, "o", "r", 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)

	select {
	case <-otherFetched:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rate-limited installation occupied the global worker pool")
	}
	select {
	case <-limitedFetched:
		t.Fatal("paused installation fetched before Retry-After")
	default:
	}
}

func TestRateLimitDeadlineNeverShortens(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(21000, 0)
	m.now = func() time.Time { return now }

	m.extendRateLimit(1, 90*time.Second)
	m.extendRateLimit(1, 30*time.Second)
	if got := m.rateUntil[1]; !got.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("shorter Retry-After replaced deadline: %v", got)
	}

	m.extendRateLimit(1, 2*time.Minute)
	if got := m.rateUntil[1]; !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("later Retry-After was not retained: %v", got)
	}
}

func TestRateLimitIsolatedByInstallation(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	now := time.Unix(22000, 0)
	m.now = func() time.Time { return now }
	m.jitter = func() time.Duration { return 0 }
	m.extendRateLimit(1, time.Hour)

	called := false
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		called = true
		return nil, errors.New("stop after fetch")
	}
	m.process(context.Background(), address{InstallationID: 2, Owner: "o", Repo: "r", Number: 1})
	if !called {
		t.Fatal("installation 1 rate limit blocked installation 2")
	}
	if pause := m.rateLimitPause(2); pause > 0 {
		t.Fatalf("installation 2 unexpectedly paused for %v", pause)
	}
}

func TestPersistentRateLimitReturnsToTTLSweep(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.jitter = func() time.Duration { return 0 }
	m.ctx = context.Background()
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		return nil, &RateLimitError{RetryAfter: time.Millisecond}
	}

	m.process(context.Background(), address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1})
	// The old implementation scheduled an unbounded direct retry here. The
	// manager now records Retry-After and returns ownership to the bounded
	// open/draft TTL sweep or a later external event.
	time.Sleep(10 * time.Millisecond)
	if len(m.queue) != 0 {
		t.Fatalf("rate-limited fetch scheduled %d direct retries, want 0", len(m.queue))
	}
}

// TestScheduleChaseBounded proves the chase window terminates (acceptance
// criterion 7): after maxChaseAttempts the address stops being rescheduled.
func TestScheduleChaseBounded(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	addr := address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1}
	for i := 0; i < maxChaseAttempts; i++ {
		m.scheduleChase(addr)
		if m.attempts[addr] != i+1 {
			t.Fatalf("attempt %d recorded as %d", i+1, m.attempts[addr])
		}
	}
	// One more chase past the cap clears tracking and does not reschedule.
	m.scheduleChase(addr)
	if _, ok := m.attempts[addr]; ok {
		t.Fatal("chase past the cap must stop and clear attempts")
	}
}
