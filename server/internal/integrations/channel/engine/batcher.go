package engine

import (
	"sync"
	"time"
)

// DefaultChatRunBatchWindow is the silence window the inbound debouncer waits
// before triggering an agent run for a chat session. 3s (MUL-2968): long
// enough to absorb a "forward a transcript, then type a note" burst into one
// run, short enough that the bot's first reply is not perceptibly late.
const DefaultChatRunBatchWindow = 3 * time.Second

// stoppableTimer is the slice of *time.Timer the batcher depends on, pinned to
// an interface so tests inject a manually-fired fake. *time.Timer satisfies it.
type stoppableTimer interface {
	Stop() bool
}

// pendingBatcher debounces the per-chat_session run trigger. Each inbound
// message that lands in a session calls Schedule, which (re)arms a single
// timer for that session; when the session goes quiet for the window the
// latest flush runs exactly once. This collapses a burst into ONE agent run —
// safe because the chat task reads the WHOLE session at run time. Only the run
// TRIGGER is debounced; the chat_message rows, dedup, and frame ACK already
// happened synchronously upstream.
//
// State is in-process, keyed by chat_session_id (a globally-unique UUID). The
// WS lease guarantees a single active owner per installation, so a session is
// debounced by one process. A hard crash inside the window drops the pending
// trigger (messages are durable; they just do not fire a run until the next
// message). Graceful shutdown calls FlushAll so that boundary is not hit on a
// normal restart. Goroutine-safe; one instance is shared across supervisors.
type pendingBatcher struct {
	window time.Duration

	// afterFunc builds a timer that invokes fn after d. Defaults to
	// time.AfterFunc; tests substitute a fake for deterministic flushes.
	afterFunc func(d time.Duration, fn func()) stoppableTimer

	mu      sync.Mutex
	pending map[string]*pendingEntry
	// seq mints a monotonic generation per (re)schedule. onFire carries the
	// generation it was armed with and bails if a newer schedule superseded
	// it — fencing the AfterFunc race where a timer fires concurrently with
	// the Stop() meant to cancel it.
	seq      uint64
	stopped  bool
	inflight sync.WaitGroup
}

type pendingEntry struct {
	timer stoppableTimer
	flush func()
	gen   uint64
}

// newPendingBatcher returns a batcher with the given silence window. A
// non-positive window falls back to DefaultChatRunBatchWindow.
func newPendingBatcher(window time.Duration) *pendingBatcher {
	if window <= 0 {
		window = DefaultChatRunBatchWindow
	}
	return &pendingBatcher{
		window:    window,
		afterFunc: realAfterFunc,
		pending:   make(map[string]*pendingEntry),
	}
}

func realAfterFunc(d time.Duration, fn func()) stoppableTimer {
	return time.AfterFunc(d, fn)
}

// Schedule (re)arms the silence window for key. The most recent flush wins:
// only session-level information is needed to fire a run, so keeping the latest
// closure (which captures the latest installation/message context) suffices.
// Calling Schedule after FlushAll runs the flush inline rather than dropping it
// (the shutdown race where a message arrives after the drain has begun).
func (b *pendingBatcher) Schedule(key string, flush func()) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		flush()
		return
	}
	b.seq++
	gen := b.seq
	fire := func() { b.onFire(key, gen) }
	if e, ok := b.pending[key]; ok {
		e.timer.Stop()
		e.flush = flush
		e.gen = gen
		e.timer = b.afterFunc(b.window, fire)
		b.mu.Unlock()
		return
	}
	b.pending[key] = &pendingEntry{
		flush: flush,
		gen:   gen,
		timer: b.afterFunc(b.window, fire),
	}
	b.mu.Unlock()
}

// onFire runs the flush for key if it is still the live, armed generation. It
// is the timer callback; in production it runs on time.AfterFunc's goroutine,
// so the flush is naturally detached from the inbound path.
func (b *pendingBatcher) onFire(key string, gen uint64) {
	b.mu.Lock()
	e, ok := b.pending[key]
	if !ok || b.stopped || e.gen != gen {
		b.mu.Unlock()
		return
	}
	delete(b.pending, key)
	flush := e.flush
	b.inflight.Add(1)
	b.mu.Unlock()

	defer b.inflight.Done()
	flush()
}

// FlushAll stops the batcher and runs every still-pending flush exactly once,
// then waits for concurrently-firing flushes to finish. Call once from
// graceful shutdown AFTER inbound delivery has stopped. After FlushAll the
// batcher is terminal: later Schedule calls run inline.
func (b *pendingBatcher) FlushAll() {
	b.mu.Lock()
	b.stopped = true
	entries := make([]*pendingEntry, 0, len(b.pending))
	for _, e := range b.pending {
		e.timer.Stop()
		entries = append(entries, e)
	}
	b.pending = make(map[string]*pendingEntry)
	b.mu.Unlock()

	for _, e := range entries {
		e.flush()
	}
	b.inflight.Wait()
}

// pendingCount reports how many sessions currently have an armed window.
func (b *pendingBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
