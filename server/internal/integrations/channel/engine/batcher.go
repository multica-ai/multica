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

// RunBatchID names the set of messages this debouncer collected into one agent
// run. It is the batcher's own verdict, not an inference from it: Schedule
// decides under the same lock that arms and retires the window, so two
// messages share an id if and only if the same flush will answer both.
//
// A platform that shows a per-run affordance (WeCom paints a streaming bubble
// the answer later replaces in place) needs exactly this and cannot re-derive
// it. Re-measuring the gap on the ingest path answers the same question with a
// second clock, and the two disagree near the window boundary — the run count
// and the affordance count then differ, which is visible to the user in both
// directions. See wecom/stream_store.go for what the id is used for.
//
// Ids are unique per process and never zero. Minting starts at 1 and
// Router.scheduleRunMode mints one line above the branch that checks for a
// debouncer, so a notifier is never handed a zero: with batching disabled each
// message still gets a fresh id of its own, and OnIngested only fires for a
// message that scheduled a run. The one run that never reaches this file —
// /new, whose task is committed inside StartSession's transaction — mints its
// id in the Router for the same reason.
//
// They are NOT a clock. The counter is atomic, but it is read outside the
// batcher's lock, so a message holding 6 can reach the batcher ahead of one
// holding 5 — "larger id" does not order two batches minted concurrently.
// Nothing depends on it: the only comparison in the tree is wecom's
// queuedBehind, which chooses between two strings for an answer that came back
// empty. What an id does guarantee is identity — two messages carry the same
// one if and only if the same flush will answer both — and that is what a
// per-run affordance actually needs.
type RunBatchID uint64

// stoppableTimer is the slice of *time.Timer the batcher depends on, pinned to
// an interface so tests inject a manually-fired fake. *time.Timer satisfies it.
type stoppableTimer interface {
	Stop() bool
}

// pendingBatcher debounces a per-(chat_session, context generation) run trigger.
// Each inbound message calls Schedule, which (re)arms one timer for that
// generation; when it goes quiet the latest flush runs exactly once. This
// collapses an in-generation burst into one agent run while preserving /clear as
// a hard batch boundary. Only the run trigger is debounced; chat_message rows,
// dedup, and frame ACK already happened synchronously upstream.
//
// State is in-process, keyed by chat_session_id plus context revision. The
// WS lease guarantees a single active owner per installation, so a session is
// debounced by one process. A hard crash inside the window drops the pending
// trigger (messages are durable; they just do not fire a run until the next
// message). Callers include the durable context revision in the key, so a
// /clear boundary can never replace the pending flush for the preceding context.
// Graceful shutdown calls FlushAll so that boundary is not hit on a normal
// restart. Goroutine-safe; one instance is shared across supervisors.
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
	flush func(RunBatchID)
	gen   uint64
	// batch names the run this entry's messages are being collected into. It
	// is minted once, when the entry is created, and survives every re-arm —
	// the entry lives exactly as long as the batch does, because onFire and
	// FlushAll delete it as they hand the flush over.
	batch RunBatchID
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

// Schedule (re)arms the silence window for key and reports which run batch the
// message belongs to: the one already collecting for this key, or newBatch when
// this message starts one. The most recent flush wins: only session-level
// information is needed to fire a run, so keeping the latest closure (which
// captures the latest installation/message context) suffices. Calling Schedule
// after FlushAll runs the flush inline rather than dropping it (the shutdown
// race where a message arrives after the drain has begun).
//
// The returned id is decided under the same lock that arms and retires the
// window, which is what makes it authoritative. A message racing the timer it
// is about to re-arm resolves one way or the other exactly once: either this
// call wins the lock and the entry survives (fire finds a newer gen and bails,
// so the message joins the batch already collecting), or the fire wins and the
// entry is gone (so the message starts newBatch and a new flush answers it).
// Both outcomes leave one batch id per flush, with no gap and no overlap.
func (b *pendingBatcher) Schedule(key string, newBatch RunBatchID, flush func(RunBatchID)) RunBatchID {
	return b.schedule(key, newBatch, flush, true)
}

// ScheduleIfAbsent arms key only when this process has no live window for it.
// Crash recovery uses this for older durable context generations: a missing
// timer must be restored, but a message on a newer generation must not reset an
// older generation's already-running silence window or replace its sender
// metadata.
//
// It reports a batch id on the same terms as Schedule: the live window's id
// when one is already collecting for key, newBatch when this call armed it.
func (b *pendingBatcher) ScheduleIfAbsent(key string, newBatch RunBatchID, flush func(RunBatchID)) RunBatchID {
	return b.schedule(key, newBatch, flush, false)
}

func (b *pendingBatcher) schedule(key string, newBatch RunBatchID, flush func(RunBatchID), replace bool) RunBatchID {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		flush(newBatch)
		return newBatch
	}
	b.seq++
	gen := b.seq
	fire := func() { b.onFire(key, gen) }
	if e, ok := b.pending[key]; ok {
		if !replace {
			batch := e.batch
			b.mu.Unlock()
			return batch
		}
		e.timer.Stop()
		e.flush = flush
		e.gen = gen
		e.timer = b.afterFunc(b.window, fire)
		batch := e.batch
		b.mu.Unlock()
		return batch
	}
	b.pending[key] = &pendingEntry{
		flush: flush,
		gen:   gen,
		batch: newBatch,
		timer: b.afterFunc(b.window, fire),
	}
	b.mu.Unlock()
	return newBatch
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
	flush, batch := e.flush, e.batch
	b.inflight.Add(1)
	b.mu.Unlock()

	defer b.inflight.Done()
	flush(batch)
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
		e.flush(e.batch)
	}
	b.inflight.Wait()
}

// pendingCount reports how many sessions currently have an armed window.
func (b *pendingBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
