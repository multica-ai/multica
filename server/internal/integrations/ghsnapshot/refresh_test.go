package ghsnapshot

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dbreader"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type emptyRows struct{}

func (*emptyRows) Close()                                       {}
func (*emptyRows) Err() error                                   { return nil }
func (*emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (*emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*emptyRows) Next() bool                                   { return false }
func (*emptyRows) Scan(...any) error                            { return nil }
func (*emptyRows) Values() ([]any, error)                       { return nil, nil }
func (*emptyRows) RawValues() [][]byte                          { return nil }
func (*emptyRows) Conn() *pgx.Conn                              { return nil }
func (*emptyRows) TypeMap() *pgtype.Map                         { return nil }

type queryCountingDB struct {
	queries int
}

func (*queryCountingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (d *queryCountingDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.queries++
	return &emptyRows{}, nil
}

func (*queryCountingDB) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func enabledClient(t *testing.T) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{appID: "1", privateKey: key, tokens: map[int64]cachedToken{}, now: time.Now}
}

func TestListGitHubPRRowsByAddressUsesConfiguredReplica(t *testing.T) {
	primaryDB := &queryCountingDB{}
	replicaDB := &queryCountingDB{}
	primaryQueries := db.New(primaryDB)
	replicaQueries := db.New(replicaDB)
	m := NewManager(nil, primaryQueries, nil, nil)
	m.SetReadSelector(dbreader.New(primaryQueries, replicaQueries, nil))

	rows, err := m.listGitHubPRRowsByAddress(context.Background(), db.ListGitHubPRRowsByAddressParams{
		InstallationID: 1,
		RepoOwner:      "owner",
		RepoName:       "repo",
		PrNumber:       2,
	})
	if err != nil {
		t.Fatalf("list rows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty result", rows)
	}
	if primaryDB.queries != 0 || replicaDB.queries != 1 {
		t.Fatalf("query calls: primary=%d replica=%d, want primary=0 replica=1", primaryDB.queries, replicaDB.queries)
	}
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

// TestRateLimitedRefreshRetriesAfterPause covers a PR whose checks finished
// while GitHub was rate limiting the installation: no later event asks for it
// again, so the refresh must survive the 403 and run once the pause ends.
func TestRateLimitedRefreshRetriesAfterPause(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.concurrency = 1
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }

	const retryAfter = 200 * time.Millisecond
	fetches := make(chan time.Time, 4)
	var calls atomic.Int32
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		fetches <- time.Now()
		if calls.Add(1) == 1 {
			return nil, &RateLimitError{RetryAfter: retryAfter}
		}
		return nil, errors.New("stop after fetch")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)
	addr := address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1}
	m.Enqueue(addr.InstallationID, addr.Owner, addr.Repo, addr.Number)

	limited := <-fetches
	select {
	case retried := <-fetches:
		if wait := retried.Sub(limited); wait < retryAfter-20*time.Millisecond {
			t.Fatalf("retried after %v, before Retry-After %v", wait, retryAfter)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rate-limited refresh was dropped instead of retried after the pause")
	}
	select {
	case <-fetches:
		t.Fatal("a failed retry was retried again")
	case <-time.After(3 * retryAfter):
	}
}

// TestPersistentRateLimitBacksOffAndStops keeps GitHub answering 403: each
// retry waits at least Retry-After, doubled per consecutive 403, and the
// address is given up after maxRateLimitRetries retries.
func TestPersistentRateLimitBacksOffAndStops(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.concurrency = 1
	m.sweepInterval = time.Hour
	m.jitter = func() time.Duration { return 0 }

	const retryAfter = 40 * time.Millisecond
	fetches := make(chan time.Time, 16)
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		fetches <- time.Now()
		return nil, &RateLimitError{RetryAfter: retryAfter}
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)
	addr := address{InstallationID: 1, Owner: "o", Repo: "r", Number: 1}
	m.Enqueue(addr.InstallationID, addr.Owner, addr.Repo, addr.Number)

	last := <-fetches
	for retry := 1; retry <= maxRateLimitRetries; retry++ {
		want := retryAfter << (retry - 1)
		select {
		case at := <-fetches:
			if gap := at.Sub(last); gap < want-10*time.Millisecond {
				t.Fatalf("retry %d after %v, want at least %v", retry, gap, want)
			}
			last = at
		case <-time.After(2 * time.Second):
			t.Fatalf("retry %d never ran", retry)
		}
	}
	select {
	case <-fetches:
		t.Fatalf("retried past %d consecutive 403s", maxRateLimitRetries)
	case <-time.After(8 * retryAfter):
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[addr] || m.rateRetries[addr] != 0 {
		t.Fatalf("given-up address still tracked: active=%v retries=%d", m.active[addr], m.rateRetries[addr])
	}
}

// TestPauseDuringJitterDefersFetch: a worker that dequeued an address before
// another worker's 403 must not send its request until the pause ends.
func TestPauseDuringJitterDefersFetch(t *testing.T) {
	m := NewManager(enabledClient(t), nil, nil, nil)
	m.concurrency = 2
	m.sweepInterval = time.Hour
	var jitters atomic.Int32
	m.jitter = func() time.Duration {
		if jitters.Add(1) == 2 {
			return 100 * time.Millisecond
		}
		return 0
	}

	const retryAfter = 400 * time.Millisecond
	fetches := make(chan time.Time, 8)
	var calls atomic.Int32
	m.fetch = func(context.Context, *Client, int64, string, string, int32) (*PRSnapshot, error) {
		fetches <- time.Now()
		if calls.Add(1) == 1 {
			return nil, &RateLimitError{RetryAfter: retryAfter}
		}
		return nil, errors.New("stop after fetch")
	}

	m.Enqueue(1, "o", "r", 1)
	m.Enqueue(1, "o", "r", 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.Start(ctx)

	limited := <-fetches
	select {
	case at := <-fetches:
		if gap := at.Sub(limited); gap < retryAfter-20*time.Millisecond {
			t.Fatalf("second worker fetched %v after the 403, inside the %v pause", gap, retryAfter)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no fetch after the pause")
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
