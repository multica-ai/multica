package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// fakeBatchWriter records the ids passed to each flush and can be told to fail
// or panic. Concurrency-safe.
type fakeBatchWriter struct {
	mu       sync.Mutex
	batches  [][]pgtype.UUID
	failNext bool
	panicNow bool
	calls    int
}

func (f *fakeBatchWriter) UpdatePersonalAccessTokensLastUsed(ctx context.Context, ids []pgtype.UUID) error {
	f.mu.Lock()
	f.calls++
	fail, panicNow := f.failNext, f.panicNow
	cp := append([]pgtype.UUID(nil), ids...)
	f.mu.Unlock()
	if panicNow {
		panic("boom")
	}
	if fail {
		return errors.New("db down")
	}
	f.mu.Lock()
	f.batches = append(f.batches, cp)
	f.mu.Unlock()
	return nil
}

func (f *fakeBatchWriter) flushedIDs() []pgtype.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []pgtype.UUID
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func (f *fakeBatchWriter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func uid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

func newTestRecorder(w lastUsedBatchWriter) *BatchedPATLastUsedRecorder {
	return &BatchedPATLastUsedRecorder{
		queries:       w,
		flushInterval: time.Hour, // we drive flushOnce directly unless overridden
		maxPending:    4,
		maxBatch:      2,
		flushBudget:   time.Second,
		shutdownDur:   time.Second,
		pending:       make(map[pgtype.UUID]struct{}),
		stopped:       make(chan struct{}),
		done:          make(chan struct{}),
		metrics:       atomicNoopMetrics{},
	}
}

func TestRecord_DedupsAndFlushes(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)
	r.Record(uid(1))
	r.Record(uid(1)) // dup — must not add a second slot
	r.Record(uid(2))
	if got := len(r.pending); got != 2 {
		t.Fatalf("pending = %d, want 2 (dup merged)", got)
	}
	r.flushOnce(time.Second)
	if got := len(w.flushedIDs()); got != 2 {
		t.Fatalf("flushed %d ids, want 2", got)
	}
	if len(r.pending) != 0 {
		t.Fatalf("pending not drained after flush")
	}
}

func TestRecord_FullDropsNewButDedupsExisting(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w) // maxPending=4
	for i := byte(1); i <= 4; i++ {
		r.Record(uid(i))
	}
	// Map full. An existing id must still be treated as dedup, not drop.
	r.Record(uid(1)) // dedup
	r.Record(uid(9)) // drop
	if len(r.pending) != 4 {
		t.Fatalf("pending = %d, want 4", len(r.pending))
	}
}

func TestFlush_ChunksByMaxBatch(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w) // maxBatch=2
	for i := byte(1); i <= 4; i++ {
		r.Record(uid(i))
	}
	r.flushOnce(time.Second)
	if got := w.callCount(); got != 2 {
		t.Fatalf("got %d chunks, want 2 (4 ids / maxBatch 2)", got)
	}
}

// TestFlush_DBErrorStopsRound asserts the first DB error ends the round: the
// writer is invoked exactly once even though there are two chunks.
func TestFlush_DBErrorStopsRound(t *testing.T) {
	w := &fakeBatchWriter{failNext: true}
	r := newTestRecorder(w) // maxBatch=2
	for i := byte(1); i <= 4; i++ {
		r.Record(uid(i))
	}
	r.flushOnce(time.Second) // must not panic
	if got := w.callCount(); got != 1 {
		t.Fatalf("writer called %d times, want 1 (first error stops the round)", got)
	}
	if len(w.flushedIDs()) != 0 {
		t.Fatalf("no ids should have been recorded on failure")
	}
}

// TestFlush_WholeFlushBudgetSkipsRemaining pins that the deadline bounds the
// ENTIRE flush: with an already-exhausted budget nothing is written, and the
// writer is never called (all chunks skipped at the budget gate).
func TestFlush_WholeFlushBudgetSkipsRemaining(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)
	for i := byte(1); i <= 4; i++ {
		r.Record(uid(i))
	}
	r.flushOnce(0) // zero budget: ctx is already expired on entry
	if got := w.callCount(); got != 0 {
		t.Fatalf("writer called %d times, want 0 (budget exhausted)", got)
	}
}

func TestFlush_PanicRecoveredWorkerSurvives(t *testing.T) {
	w := &fakeBatchWriter{panicNow: true}
	r := newTestRecorder(w)
	r.Record(uid(1))
	r.flushOnce(time.Second) // recovers, does not crash

	// Next round with a healthy writer still works.
	w.mu.Lock()
	w.panicNow = false
	w.mu.Unlock()
	r.Record(uid(2))
	r.flushOnce(time.Second)
	if len(w.flushedIDs()) != 1 {
		t.Fatalf("worker did not recover for the next round")
	}
}

// gateWriter blocks each write until release is closed OR its ctx is cancelled,
// returning ctx.Err() in the latter case. It signals (once) when the first
// write has begun. This lets a test interleave a sweepCtx cancel with a write
// that is already in flight.
type gateWriter struct {
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
	mu          sync.Mutex
	written     []pgtype.UUID
}

func newGateWriter() *gateWriter {
	return &gateWriter{started: make(chan struct{}), release: make(chan struct{})}
}

func (g *gateWriter) UpdatePersonalAccessTokensLastUsed(ctx context.Context, ids []pgtype.UUID) error {
	g.startedOnce.Do(func() { close(g.started) })
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	g.mu.Lock()
	g.written = append(g.written, ids...)
	g.mu.Unlock()
	return nil
}

func (g *gateWriter) writtenCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.written)
}

// TestShutdown_InFlightPeriodicFlushNotLost is the P1a regression. A periodic
// flush swaps the pending set out and is mid-write when sweepCtx is cancelled.
// Because the flush DB deadline is rooted in Background (not sweepCtx), the
// in-flight write must still complete rather than being aborted and dropped.
// With the old sweepCtx-rooted deadline this write would return context
// canceled, the batch would be dropped, and Stop's final flush (pending now
// empty) could not recover it.
func TestShutdown_InFlightPeriodicFlushNotLost(t *testing.T) {
	g := newGateWriter()
	r := newTestRecorder(g)
	r.flushInterval = 5 * time.Millisecond

	r.Record(uid(7))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)

	<-g.started // a periodic flush has swapped pending and is now blocked in the write
	cancel()    // sweepCancel() lands mid-flush
	close(g.release)
	r.Stop()

	if got := g.writtenCount(); got != 1 {
		t.Fatalf("in-flight batch lost on shutdown: wrote %d ids, want 1", got)
	}
}

func TestStop_FinalFlushRunsAfterCancel(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)

	// Cancel the worker ctx, THEN Stop. The final flush must still write
	// despite the worker ctx being dead (it is rooted in Background).
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	r.Record(uid(7))
	cancel()
	r.Stop()

	if got := len(w.flushedIDs()); got != 1 {
		t.Fatalf("final flush wrote %d ids, want 1", got)
	}
}

func TestStop_Idempotent(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)
	r.Stop()
	r.Stop() // must not panic / double-close
}

// TestStop_WithoutRunReturnsPromptly asserts Stop does not spend the join
// budget waiting for a Run that was never started.
func TestStop_WithoutRunReturnsPromptly(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)
	r.shutdownDur = 5 * time.Second // would be a long wait if we joined a non-running Run
	r.Record(uid(1))

	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked waiting for a Run that never started")
	}
	if len(w.flushedIDs()) != 1 {
		t.Fatalf("final flush wrote %d ids, want 1", len(w.flushedIDs()))
	}
}

func TestNoopRecorder(t *testing.T) {
	var r PATLastUsedRecorder = NoopPATLastUsedRecorder{}
	r.Record(uid(1)) // no panic, no state
}

func TestRecord_ConcurrentDedup(t *testing.T) {
	w := &fakeBatchWriter{}
	r := newTestRecorder(w)
	r.maxPending = 1000
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.Record(uid(42)) }()
	}
	wg.Wait()
	if len(r.pending) != 1 {
		t.Fatalf("concurrent dup: pending = %d, want 1", len(r.pending))
	}
}
