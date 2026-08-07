package outbox

// wake_test.go — the wake registry's coalescing and lifecycle. It is a latency
// optimization, so the contract that matters is that it never blocks and never
// panics, not that every nudge is observed.

import (
	"sync"
	"testing"
)

func TestWakeRegistry_CoalescesRepeatedWakes(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	ch := r.Register("inst-1")

	// A full buffer needs no second token: the consumer drains the whole queue
	// on each pass, so one pending nudge is as good as ten.
	for range 10 {
		r.Wake("inst-1")
	}
	received := 0
	for {
		select {
		case <-ch:
			received++
			continue
		default:
		}
		break
	}
	if received != 1 {
		t.Errorf("received %d wakes, want 1 (coalesced)", received)
	}
}

func TestWakeRegistry_RegisterIsIdempotent(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	first := r.Register("inst-1")
	second := r.Register("inst-1")
	// A reconnect must not orphan the waiter the previous Connect is selecting
	// on, so the same channel comes back.
	r.Wake("inst-1")
	select {
	case <-first:
	default:
		t.Error("re-registration replaced the live channel; the existing waiter was orphaned")
	}
	select {
	case <-second:
		t.Error("second Register returned a distinct channel")
	default:
	}
}

func TestWakeRegistry_WakeUnknownInstallationIsANoop(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	// No consumer local to this replica. Not an error — the lease holder's
	// consumer will pick the row up on its poll.
	r.Wake("nobody-here")
}

func TestWakeRegistry_UnregisterStopsDelivery(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	ch := r.Register("inst-1")
	r.Unregister("inst-1")
	r.Wake("inst-1")
	select {
	case <-ch:
		t.Error("a wake reached an unregistered installation")
	default:
	}
}

func TestWakeRegistry_WakeAllNotifiesEveryRegistration(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	a := r.Register("inst-a")
	b := r.Register("inst-b")
	r.WakeAll()
	for name, ch := range map[string]<-chan struct{}{"inst-a": a, "inst-b": b} {
		select {
		case <-ch:
		default:
			t.Errorf("%s received no wake", name)
		}
	}
}

// Register/Wake/Unregister race on every reconnect, so the registry must be
// safe under concurrent use.
func TestWakeRegistry_ConcurrentUse(t *testing.T) {
	t.Parallel()
	r := NewWakeRegistry()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(3)
		id := string(rune('a' + i%26))
		go func() { defer wg.Done(); r.Register(id) }()
		go func() { defer wg.Done(); r.Wake(id) }()
		go func() { defer wg.Done(); r.Unregister(id) }()
	}
	wg.Wait()
}
