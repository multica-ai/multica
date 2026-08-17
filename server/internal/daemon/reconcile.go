package daemon

import (
	"sync"
	"time"
)

// reconcileBroadcaster fans out a "reconcile now" signal to any number of
// listeners using the close-and-replace channel pattern. Subscribers call
// notify() to obtain a snapshot channel that closes on the next broadcast;
// after waking, a subscriber re-acquires a fresh snapshot via notify() to
// receive the next signal.
//
// The broadcaster exists because some daemon loops run on coarse tickers
// (task-cancellation polling at 5s, workspace sync at 30s). When the daemon's
// WS connection drops and reconnects, anything the server changed during the
// gap is invisible to those loops until their next tick fires. broadcast()
// lets the WS connect path nudge every waiter to re-check immediately,
// without disturbing the ticker cadence.
//
// The broadcaster is edge-triggered with one-slot replay: if broadcast()
// fires while nobody is subscribed, the next notify() call returns an
// already-closed channel so the late subscriber observes the missed event
// exactly once. This closes the daemon-startup race where workspaceSyncLoop
// (or another late subscriber) parks on its ticker just after a broadcast
// landed during the WS connect, and would otherwise wait a full ticker
// period to learn about it.
//
// broadcast() debounces back-to-back calls inside minBroadcastInterval so a
// flapping WS connection cannot translate a network blip into a stampede of
// GetTaskStatus / ListWorkspaces requests. The interval is intentionally
// short: a real reconnect still converts quickly, but ten reconnects in a
// second collapse into one immediate fan-out plus, at most, one follow-up
// after the interval elapses.
type reconcileBroadcaster struct {
	mu                   sync.Mutex
	ch                   chan struct{}
	pending              bool
	lastBroadcast        time.Time
	minBroadcastInterval time.Duration
	// now is injected in tests to make the debounce window deterministic;
	// production code uses time.Now.
	now func() time.Time
}

func newReconcileBroadcaster() *reconcileBroadcaster {
	return &reconcileBroadcaster{
		ch:                   make(chan struct{}),
		minBroadcastInterval: time.Second,
		now:                  time.Now,
	}
}

// notify returns a channel that closes on the next broadcast. Subscribers
// should call notify() again after waking to receive the next signal — the
// returned channel is single-shot.
//
// If a broadcast arrived while there were no subscribers, the channel
// returned by the next notify() call is already closed. The replay flag is
// cleared by that call: a second concurrent late subscriber does NOT see
// the same replayed event. Callers that need broadcast delivery to every
// goroutine must subscribe before the broadcast happens.
func (b *reconcileBroadcaster) notify() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending {
		// Replay the missed broadcast exactly once: hand back a fresh,
		// already-closed channel without disturbing b.ch for real
		// subscribers, and clear pending so the next notify() resumes
		// edge-triggered behaviour.
		b.pending = false
		replay := make(chan struct{})
		close(replay)
		return replay
	}
	return b.ch
}

// broadcast wakes every current subscriber, then installs a fresh channel
// for future subscribers. If there are no current subscribers, a one-slot
// replay flag is set so the next notify() observes the missed event once.
//
// Calls within minBroadcastInterval of the previous broadcast are dropped;
// the function reports whether the signal fired so callers can log
// debug-level traces of suppressed broadcasts.
func (b *reconcileBroadcaster) broadcast() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if !b.lastBroadcast.IsZero() && now.Sub(b.lastBroadcast) < b.minBroadcastInterval {
		return false
	}
	b.lastBroadcast = now
	b.pending = true
	close(b.ch)
	b.ch = make(chan struct{})
	return true
}

// workspaceChangeSignal is a single-consumer dirty flag for workspace-set
// changes. The one-slot buffer preserves an event that arrives before the sync
// loop starts or while an API read is in flight, while coalescing any larger
// burst into one trailing reconciliation of the latest server state.
type workspaceChangeSignal struct {
	ch chan struct{}
}

func newWorkspaceChangeSignal() *workspaceChangeSignal {
	return &workspaceChangeSignal{ch: make(chan struct{}, 1)}
}

func (s *workspaceChangeSignal) notify() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}

func (s *workspaceChangeSignal) broadcast() bool {
	if s == nil {
		return false
	}
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}
