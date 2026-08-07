package outbox

import "sync"

// WakeRegistry maps installation IDs to coalesced wake channels so producers
// can nudge a local consumer without blocking.
//
// It is a latency optimization, never a correctness requirement: a consumer
// also polls, so a wake that lands on the wrong replica (or on none, because
// the lease moved) only costs the poll interval. That is why Wake ignores
// unknown keys instead of reporting them.
type WakeRegistry struct {
	mu    sync.Mutex
	wakes map[string]chan struct{}
}

// NewWakeRegistry builds an empty wake registry.
func NewWakeRegistry() *WakeRegistry {
	return &WakeRegistry{wakes: make(map[string]chan struct{})}
}

// Register creates a buffered wake channel for installationID and returns the
// receive side for the local consumer. Repeated registration for the same ID
// returns the existing channel so waiters are not dropped.
func (r *WakeRegistry) Register(installationID string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.wakes[installationID]; ok {
		return ch
	}
	ch := make(chan struct{}, 1)
	r.wakes[installationID] = ch
	return ch
}

// Unregister drops the wake channel for installationID. The channel is not
// closed; the consumer exits via its own context, so closing here would race
// a concurrent Wake into a send-on-closed panic.
func (r *WakeRegistry) Unregister(installationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.wakes, installationID)
}

// Wake performs a non-blocking coalesced notify for installationID. A full
// buffer needs no second token — the consumer drains the whole queue on each
// pass — and a missing key means no consumer is local, which is not an error.
func (r *WakeRegistry) Wake(installationID string) {
	r.mu.Lock()
	ch, ok := r.wakes[installationID]
	r.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// WakeAll notifies every registered installation.
func (r *WakeRegistry) WakeAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.wakes))
	for id := range r.wakes {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.Wake(id)
	}
}
