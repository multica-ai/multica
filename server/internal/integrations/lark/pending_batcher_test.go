package lark

import (
	"sync"
	"testing"
	"time"
)

// fakeBatchTimer is a manually-fired stand-in for *time.Timer so debounce
// behaviour can be asserted without sleeping real wall-clock seconds.
type fakeBatchTimer struct {
	fn      func()
	stopped bool
	fired   bool
}

// Stop mirrors *time.Timer.Stop: returns true only if the timer was still
// armed (not already stopped or fired).
func (t *fakeBatchTimer) Stop() bool {
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

// fakeTimerFactory hands out fakeBatchTimers and lets a test fire whichever
// ones are currently armed — modelling the wall clock advancing past the
// window for every pending session at once.
type fakeTimerFactory struct {
	mu  sync.Mutex
	all []*fakeBatchTimer
}

func (f *fakeTimerFactory) after(_ time.Duration, fn func()) stoppableTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeBatchTimer{fn: fn}
	f.all = append(f.all, t)
	return t
}

// fireArmed invokes every timer that is armed (not stopped, not already
// fired) right now.
func (f *fakeTimerFactory) fireArmed() {
	f.mu.Lock()
	armed := make([]*fakeBatchTimer, 0, len(f.all))
	for _, t := range f.all {
		if !t.stopped && !t.fired {
			t.fired = true
			armed = append(armed, t)
		}
	}
	f.mu.Unlock()
	for _, t := range armed {
		t.fn()
	}
}

func (f *fakeTimerFactory) armedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.all {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// newTestBatcher builds a batcher whose timers are driven by f. Shared with
// dispatcher_test.go (same package) to drive the debounce coalescing test.
func newTestBatcher(f *fakeTimerFactory) *pendingBatcher {
	return &pendingBatcher{
		window:    DefaultChatRunBatchWindow,
		afterFunc: f.after,
		pending:   make(map[string]*pendingEntry),
	}
}

func TestPendingBatcher_DebounceCoalesces(t *testing.T) {
	f := &fakeTimerFactory{}
	b := newTestBatcher(f)
	calls := 0
	flush := func() { calls++ }

	b.Schedule("s", flush)
	b.Schedule("s", flush)
	b.Schedule("s", flush)

	if got := b.pendingCount(); got != 1 {
		t.Fatalf("three Schedules on one session must keep a single pending entry; got %d", got)
	}
	if got := f.armedCount(); got != 1 {
		t.Fatalf("each reschedule must cancel the prior timer, leaving one armed; got %d", got)
	}

	f.fireArmed()
	if calls != 1 {
		t.Fatalf("a debounced burst must flush exactly once; got %d", calls)
	}
	if got := b.pendingCount(); got != 0 {
		t.Fatalf("the session entry must be cleaned up after flush; pending=%d", got)
	}
}

func TestPendingBatcher_MultiSessionIndependent(t *testing.T) {
	f := &fakeTimerFactory{}
	b := newTestBatcher(f)
	var a, c int

	b.Schedule("a", func() { a++ })
	b.Schedule("c", func() { c++ })

	if got := b.pendingCount(); got != 2 {
		t.Fatalf("two distinct sessions must hold two windows; got %d", got)
	}
	f.fireArmed()
	if a != 1 || c != 1 {
		t.Fatalf("each session must flush once and not cross-talk; a=%d c=%d", a, c)
	}
}

func TestPendingBatcher_StaleTimerFireIsNoop(t *testing.T) {
	// Reproduces the AfterFunc race: a timer fires concurrently with the
	// Stop() that was meant to cancel it after a reschedule. The
	// generation guard must make the stale fire a no-op so the burst still
	// flushes exactly once.
	f := &fakeTimerFactory{}
	b := newTestBatcher(f)
	calls := 0

	b.Schedule("s", func() { calls++ })
	first := f.all[0]
	b.Schedule("s", func() { calls++ }) // resets: cancels first, arms a new timer

	// First timer fires anyway despite having been Stop()ed.
	first.fired = true
	first.fn()
	if calls != 0 {
		t.Fatalf("a superseded timer firing must not flush; got %d", calls)
	}

	f.fireArmed()
	if calls != 1 {
		t.Fatalf("the live timer must still flush exactly once; got %d", calls)
	}
}

func TestPendingBatcher_FlushAllDrainsPending(t *testing.T) {
	f := &fakeTimerFactory{}
	b := newTestBatcher(f)
	var a, c int

	b.Schedule("a", func() { a++ })
	b.Schedule("c", func() { c++ })

	b.FlushAll()
	if a != 1 || c != 1 {
		t.Fatalf("FlushAll must flush every pending session once; a=%d c=%d", a, c)
	}
	if got := b.pendingCount(); got != 0 {
		t.Fatalf("FlushAll must clear pending state; got %d", got)
	}

	// After FlushAll the batcher is terminal: a later Schedule runs inline
	// rather than silently dropping (the shutdown-race best-effort path).
	ran := false
	b.Schedule("d", func() { ran = true })
	if !ran {
		t.Fatalf("Schedule after FlushAll must run inline")
	}
}

func TestNewPendingBatcher_DefaultsWindow(t *testing.T) {
	if b := newPendingBatcher(0); b.window != DefaultChatRunBatchWindow {
		t.Fatalf("non-positive window must default to %v; got %v", DefaultChatRunBatchWindow, b.window)
	}
	if b := newPendingBatcher(500 * time.Millisecond); b.window != 500*time.Millisecond {
		t.Fatalf("explicit window must be honoured; got %v", b.window)
	}
}

func TestPendingBatcher_RealTimerFlushes(t *testing.T) {
	// Exercises the production afterFunc (time.AfterFunc) path with a short
	// real window so a mis-wired default would be caught.
	b := newPendingBatcher(15 * time.Millisecond)
	done := make(chan struct{})
	b.Schedule("s", func() { close(done) })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("real-timer flush did not fire within 2s")
	}
}
