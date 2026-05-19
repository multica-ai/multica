package leader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool returns a pgxpool aimed at the same DATABASE_URL the rest of
// the integration test suite uses. If the database is unreachable we skip —
// advisory-lock semantics cannot be faithfully simulated outside Postgres,
// so a pure in-memory test would only encode our own bug.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("leader: cannot parse DATABASE_URL: %v", err)
	}
	// Keep MaxConns small but >1 — the elector takes one connection per
	// session-level advisory lock holder. With two electors plus the
	// follower's ping connection we want enough headroom for -count=N
	// stress runs without pool exhaustion.
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("leader: could not create pool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("leader: database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// freshLockID picks an advisory-lock id unlikely to collide with a real
// elector or with parallel test runs. Mixing the test name and current
// nanos keeps reruns and parallel packages from sharing a slot.
func freshLockID(t *testing.T) int64 {
	t.Helper()
	var h int64
	for _, c := range t.Name() {
		h = h*131 + int64(c)
	}
	return h ^ time.Now().UnixNano()
}

// electorHandle is a small bundle of test-side bookkeeping for one Elector.
type electorHandle struct {
	elector  *Elector
	ctx      context.Context
	cancel   context.CancelFunc
	acquired atomic.Int32
	released atomic.Int32
	runErr   chan error
}

func newElectorHandle(pool *pgxpool.Pool, lockID int64, ping time.Duration) *electorHandle {
	ctx, cancel := context.WithCancel(context.Background())
	h := &electorHandle{
		ctx:    ctx,
		cancel: cancel,
		runErr: make(chan error, 1),
	}
	e := NewElector(pool, lockID, ping)
	e.OnAcquire(func(ctx context.Context) error {
		h.acquired.Add(1)
		return nil
	})
	e.OnRelease(func(ctx context.Context) error {
		h.released.Add(1)
		return nil
	})
	h.elector = e
	go func() { h.runErr <- e.Run(ctx) }()
	return h
}

// TestTryAcquireOnlyOneHolds — TC-leader-1.
//
// Two electors targeting the same lock id, both Run concurrently. Exactly
// one fires OnAcquire; the other sits in its ping loop and never fires.
func TestTryAcquireOnlyOneHolds(t *testing.T) {
	pool := newTestPool(t)
	lockID := freshLockID(t)

	a := newElectorHandle(pool, lockID, 200*time.Millisecond)
	b := newElectorHandle(pool, lockID, 200*time.Millisecond)
	defer func() {
		a.cancel()
		b.cancel()
		<-a.runErr
		<-b.runErr
	}()

	// Give them up to 2 seconds to settle into leader/follower.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.acquired.Load()+b.acquired.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait two ping intervals more to make sure the follower isn't
	// going to acquire on the next tick.
	time.Sleep(500 * time.Millisecond)

	total := a.acquired.Load() + b.acquired.Load()
	if total != 1 {
		t.Fatalf("expected exactly one elector to acquire, got a=%d b=%d", a.acquired.Load(), b.acquired.Load())
	}
}

// TestFollowerAcquiresAfterLeaderDies — TC-leader-2.
//
// Start two electors, identify the leader, cancel its context. The follower
// must acquire within 5.5 seconds (ping interval is 1s; worst case ~1s
// plus Postgres lock-release latency).
func TestFollowerAcquiresAfterLeaderDies(t *testing.T) {
	pool := newTestPool(t)
	lockID := freshLockID(t)

	a := newElectorHandle(pool, lockID, 1*time.Second)
	b := newElectorHandle(pool, lockID, 1*time.Second)
	defer func() {
		a.cancel()
		b.cancel()
	}()

	// Wait for one to become leader.
	var leader, follower *electorHandle
	leaderDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(leaderDeadline) {
		if a.acquired.Load() == 1 {
			leader, follower = a, b
			break
		}
		if b.acquired.Load() == 1 {
			leader, follower = b, a
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leader == nil {
		t.Fatalf("no elector became leader within 3s (a=%d b=%d)", a.acquired.Load(), b.acquired.Load())
	}

	// Sanity: follower must NOT have fired OnAcquire yet.
	if follower.acquired.Load() != 0 {
		t.Fatalf("follower fired OnAcquire prematurely (%d)", follower.acquired.Load())
	}

	// Kill the leader.
	leader.cancel()
	select {
	case <-leader.runErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("leader Run did not return after cancel")
	}
	if leader.released.Load() != 1 {
		t.Fatalf("dying leader did not call OnRelease (got %d)", leader.released.Load())
	}

	// Follower must acquire within 5.5s.
	deadline := time.Now().Add(5500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if follower.acquired.Load() == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("follower did not acquire within 5.5s after leader died")
}

// TestLeaderRotationDrivesAdapter — TC-adapt-5 contract at the leader
// level.
//
// The point of leader election is exactly-one-Connect across processes:
// when the leader rotates, the new leader must Connect the adapter and
// the old leader must have Disconnected first. We model that with a
// fakeChannelStub whose Connect / Disconnect we observe through
// OnAcquire / OnRelease. A full rotation cycle (initial Connect → leader
// dies → Disconnect → follower acquires → Connect again) must complete
// within 10s.
func TestLeaderRotationDrivesAdapter(t *testing.T) {
	pool := newTestPool(t)
	lockID := freshLockID(t)

	stub := &fakeChannelStub{}

	type rotHandle struct {
		elector *Elector
		ctx     context.Context
		cancel  context.CancelFunc
		runErr  chan error
	}

	mk := func() *rotHandle {
		ctx, cancel := context.WithCancel(context.Background())
		e := NewElector(pool, lockID, 500*time.Millisecond)
		e.OnAcquire(stub.Connect)
		e.OnRelease(stub.Disconnect)
		h := &rotHandle{
			elector: e,
			ctx:     ctx,
			cancel:  cancel,
			runErr:  make(chan error, 1),
		}
		go func() { h.runErr <- e.Run(ctx) }()
		return h
	}

	handles := []*rotHandle{mk(), mk()}
	defer func() {
		for _, h := range handles {
			h.cancel()
		}
	}()

	// Wait for the first Connect.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && stub.connects.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if stub.connects.Load() != 1 {
		t.Fatalf("expected 1 Connect after initial leader election, got %d", stub.connects.Load())
	}

	// Identify and cancel the leader. IsLeader is exposed precisely so
	// callers (and tests) can target the actual lock holder rather than
	// guess and risk cancelling the follower.
	var leader *rotHandle
	for _, h := range handles {
		if h.elector.IsLeader() {
			leader = h
			break
		}
	}
	if leader == nil {
		t.Fatalf("no handle reported IsLeader despite stub.connects=%d", stub.connects.Load())
	}
	leader.cancel()
	select {
	case <-leader.runErr:
	case <-time.After(2 * time.Second):
		t.Fatalf("leader Run did not return after cancel")
	}

	// Within 10 seconds we expect at least one full Disconnect+Connect
	// cycle (i.e. connects==2, disconnects>=1).
	rotationDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(rotationDeadline) {
		if stub.connects.Load() >= 2 && stub.disconnects.Load() >= 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("rotation did not complete within 10s: %s", stub)
}

// fakeChannelStub stands in for the real feishu.Adapter — we only care
// about Connect / Disconnect call counts and ordering for the leader
// rotation contract. It returns a recognisable error if called in a bad
// order so a regression in the elector lifecycle (e.g. OnRelease without a
// prior OnAcquire) surfaces as a test failure rather than a silent miscount.
type fakeChannelStub struct {
	mu          sync.Mutex
	connects    atomic.Int32
	disconnects atomic.Int32
	connected   bool
}

var errFakeAlreadyConnected = errors.New("fake: already connected")
var errFakeNotConnected = errors.New("fake: not connected")

func (f *fakeChannelStub) Connect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connected {
		return errFakeAlreadyConnected
	}
	f.connected = true
	f.connects.Add(1)
	return nil
}

func (f *fakeChannelStub) Disconnect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.connected {
		return errFakeNotConnected
	}
	f.connected = false
	f.disconnects.Add(1)
	return nil
}

func (f *fakeChannelStub) String() string {
	return fmt.Sprintf("fakeChannelStub{connects=%d disconnects=%d}",
		f.connects.Load(), f.disconnects.Load())
}
