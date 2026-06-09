package lark

import (
	"sync"
	"time"
)

// DefaultChatRunBatchWindow is the silence window the inbound debouncer
// waits before triggering an agent run for a chat session. Owner-aligned
// at 3s on MUL-2968: long enough to absorb a "forward a transcript, then
// type a note" burst into one run, short enough that the bot's first
// reply is not perceptibly late.
const DefaultChatRunBatchWindow = 3 * time.Second

// stoppableTimer is the slice of *time.Timer the batcher depends on.
// Pinned to an interface so unit tests inject a manually-fired fake
// instead of sleeping real wall-clock seconds. *time.Timer satisfies it
// directly (Stop() bool).
type stoppableTimer interface {
	Stop() bool
}

// pendingBatcher debounces the per-chat_session run trigger. Each inbound
// Lark message that lands in a session calls Schedule, which (re)arms a
// single timer for that session; when the session goes quiet for the
// configured window the latest-registered flush runs exactly once. This
// collapses a burst of messages into ONE agent run instead of one run per
// message — safe because the chat task reads the WHOLE session history at
// run time, so the individually-persisted messages are all visible to the
// single run. Only the run TRIGGER is debounced; the chat_message rows,
// per-message dedup, and frame ACK already happened synchronously upstream.
//
// State is in-process only, keyed by chat_session_id (a globally-unique
// UUID, so no installation qualifier is needed). The WS lease guarantees a
// single active owner per installation, so a session is only ever debounced
// by one process. A hard crash inside the window drops the pending trigger
// (the messages are already durable in chat_session; they simply do not
// fire a run until the next message arrives) — an accepted low-frequency
// boundary per MUL-2968 decision 5. Graceful shutdown calls FlushAll so
// that boundary is not hit on a normal restart.
//
// The batcher is goroutine-safe: a single instance is shared across all
// supervisor goroutines.
type pendingBatcher struct {
	window time.Duration

	// afterFunc builds a timer that invokes fn after d. Defaults to
	// time.AfterFunc; tests substitute a fake so flushes are deterministic.
	afterFunc func(d time.Duration, fn func()) stoppableTimer

	mu      sync.Mutex
	pending map[string]*pendingEntry
	// seq mints a monotonic generation per (re)schedule. onFire carries
	// the generation it was armed with and bails if a newer schedule has
	// superseded it — this fences the classic AfterFunc race where a timer
	// fires concurrently with the Stop() that was meant to cancel it.
	seq     uint64
	stopped bool
	// inflight tracks flush callbacks that are currently executing (timer
	// already fired) so FlushAll can join them before a graceful shutdown
	// proceeds.
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

// realAfterFunc adapts time.AfterFunc to the stoppableTimer seam.
func realAfterFunc(d time.Duration, fn func()) stoppableTimer {
	return time.AfterFunc(d, fn)
}

// Schedule (re)arms the silence window for key. The most recent flush wins:
// only session-level information is needed to fire a run, so keeping the
// latest closure (which captures the latest installation/message context
// for the offline/archived notice) is sufficient. Calling Schedule after
// FlushAll runs the flush inline best-effort rather than silently dropping
// it; this only happens on the shutdown race where a message arrives after
// the drain has begun.
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
		// Reset the window: cancel the prior timer and arm a fresh one.
		// The gen bump means a stale fire from the cancelled timer is a
		// no-op even if Stop loses the race.
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

// onFire runs the flush for key if it is still the live, armed generation.
// It is the timer callback; in production it runs on time.AfterFunc's own
// goroutine, so the flush is naturally detached from the inbound path.
func (b *pendingBatcher) onFire(key string, gen uint64) {
	b.mu.Lock()
	e, ok := b.pending[key]
	if !ok || b.stopped || e.gen != gen {
		// Superseded by a newer Schedule, already flushed, or draining via
		// FlushAll (which owns this entry now). Do nothing.
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

// FlushAll stops the batcher and runs every still-pending flush exactly
// once, then waits for any concurrently-firing flushes to finish. Intended
// to be called once from the graceful-shutdown path AFTER inbound delivery
// has stopped, so a normal restart does not drop a window's worth of
// triggers. After FlushAll the batcher is terminal: later Schedule calls
// run inline.
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
	// Join flushes whose timer had already fired before we set stopped.
	b.inflight.Wait()
}

// pendingCount reports how many sessions currently have an armed window.
// Used by tests and useful for ops visibility.
func (b *pendingBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
