package daemon

import (
	"sync"
	"testing"
	"time"
)

// TestReconcileBroadcaster_FansOutToManySubscribers pins the close-and-replace
// fan-out: every subscriber registered before broadcast wakes from its
// snapshot channel.
func TestReconcileBroadcaster_FansOutToManySubscribers(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0 // disable debounce for this test

	const subs = 16
	var wg sync.WaitGroup
	wg.Add(subs)
	wokeUp := make(chan struct{}, subs)
	for i := 0; i < subs; i++ {
		ch := b.notify()
		go func() {
			defer wg.Done()
			<-ch
			wokeUp <- struct{}{}
		}()
	}

	// Give subscribers a moment to park on <-ch.
	time.Sleep(20 * time.Millisecond)

	if !b.broadcast() {
		t.Fatalf("broadcast() = false, want true")
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("only %d/%d subscribers woke up", len(wokeUp), subs)
	}
}

// TestReconcileBroadcaster_ReplaysMissedBroadcastToFirstLateSubscriber pins
// the level-triggered safety net: if a broadcast fires while no one is
// subscribed, the next notify() returns an already-closed channel so the
// late subscriber observes the missed event exactly once. This closes the
// daemon-startup race where a sync loop subscribes a beat after the WS
// connect broadcast.
func TestReconcileBroadcaster_ReplaysMissedBroadcastToFirstLateSubscriber(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	// No subscribers yet — fire.
	if !b.broadcast() {
		t.Fatalf("broadcast() = false, want true")
	}

	// First late subscriber must see the replay.
	first := b.notify()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatalf("first late subscriber did not receive replayed broadcast")
	}

	// Second late subscriber must NOT see the same replay; it should park.
	second := b.notify()
	select {
	case <-second:
		t.Fatalf("second late subscriber received a stale replay; should be fresh")
	case <-time.After(50 * time.Millisecond):
		// Expected: parked.
	}
}

// TestReconcileBroadcaster_ReplayPersistsAcrossSubscriberDelay pins that an
// unobserved pending replay is preserved indefinitely until the first
// notify() consumes it.
func TestReconcileBroadcaster_ReplayPersistsAcrossSubscriberDelay(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	b.broadcast()
	time.Sleep(100 * time.Millisecond)

	ch := b.notify()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("pending replay was lost after 100ms delay")
	}
}

// TestReconcileBroadcaster_DebouncesFlappingReconnects pins the safety
// property that a flapping WS cannot translate ten reconnects per second
// into ten fan-out broadcasts. The clock is injected so the debounce
// window is deterministic.
func TestReconcileBroadcaster_DebouncesFlappingReconnects(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = time.Second
	var nowVal time.Time
	b.now = func() time.Time { return nowVal }

	nowVal = time.Unix(1_700_000_000, 0)
	if !b.broadcast() {
		t.Fatalf("first broadcast suppressed")
	}

	// Ten back-to-back calls within 900ms — all suppressed.
	for i := 0; i < 10; i++ {
		nowVal = nowVal.Add(90 * time.Millisecond)
		if b.broadcast() {
			t.Fatalf("broadcast at +%dms not debounced", (i+1)*90)
		}
	}

	// Cross the threshold — next broadcast fires.
	nowVal = nowVal.Add(time.Second)
	if !b.broadcast() {
		t.Fatalf("broadcast past debounce window suppressed")
	}
}

// TestReconcileBroadcaster_DebounceBoundaryIsExact pins the exact comparison
// (strict less-than): a call landing at exactly minBroadcastInterval after
// the previous broadcast must succeed, not be suppressed.
func TestReconcileBroadcaster_DebounceBoundaryIsExact(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = time.Second
	var nowVal time.Time
	b.now = func() time.Time { return nowVal }

	nowVal = time.Unix(1_700_000_000, 0)
	if !b.broadcast() {
		t.Fatal("first broadcast suppressed")
	}

	// Exactly at the interval — must fire (>= boundary is allowed).
	nowVal = nowVal.Add(time.Second)
	if !b.broadcast() {
		t.Fatal("broadcast at exact debounce boundary was suppressed")
	}

	// Just below the next boundary — must be suppressed.
	nowVal = nowVal.Add(999 * time.Millisecond)
	if b.broadcast() {
		t.Fatal("broadcast at boundary-minus-1ms was not suppressed")
	}
}

// TestReconcileBroadcaster_ReSubscribesEachWake pins the edge-triggered
// contract for ACTIVE subscribers: after the first wake, the same channel
// stays closed; the subscriber must call notify() again to receive future
// broadcasts.
func TestReconcileBroadcaster_ReSubscribesEachWake(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	ch1 := b.notify()
	b.broadcast()
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatalf("first wake did not arrive on ch1")
	}

	// ch1 is now closed; broadcasting again must not panic.
	b.broadcast()
	select {
	case <-ch1:
		// Already closed — still drains cleanly.
	default:
		t.Fatalf("ch1 should remain closed after second broadcast")
	}
}

// TestReconcileBroadcaster_ConcurrentBroadcastAndNotify exercises the lock
// boundary: heavy concurrent notify/broadcast traffic must not panic, dead-
// lock, or fail under the race detector. Run with `go test -race`.
func TestReconcileBroadcaster_ConcurrentBroadcastAndNotify(t *testing.T) {
	b := newReconcileBroadcaster()
	b.minBroadcastInterval = 0

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 8 subscribers in tight loops.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ch := b.notify()
				select {
				case <-ch:
				case <-stop:
					return
				}
			}
		}()
	}

	// 4 broadcasters in tight loops.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				b.broadcast()
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)

	// Bound the join — if anything deadlocks we want a clear failure rather
	// than a hung test.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent broadcast/notify did not converge after stop")
	}
}
