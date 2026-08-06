package wecom

import "sync"

// OutboundWakeRegistry maps installation IDs to coalesced wake channels so
// outbound producers can nudge the local WS consumer without blocking.
type OutboundWakeRegistry struct {
	mu    sync.Mutex
	wakes map[string]chan struct{}
}

// NewOutboundWakeRegistry builds an empty wake registry.
func NewOutboundWakeRegistry() *OutboundWakeRegistry {
	return &OutboundWakeRegistry{
		wakes: make(map[string]chan struct{}),
	}
}

// Register creates a buffered wake channel for installationID and returns the
// receive side for the local WS consumer. Repeated registration for the same
// ID returns the existing channel so waiters are not dropped.
func (r *OutboundWakeRegistry) Register(installationID string) <-chan struct{} {
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
// closed; the consumer should exit via its own context to avoid send-on-closed
// or double-close races.
func (r *OutboundWakeRegistry) Unregister(installationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.wakes, installationID)
}

// Wake performs a non-blocking coalesced notify for installationID. Missing
// keys are ignored.
func (r *OutboundWakeRegistry) Wake(installationID string) {
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
func (r *OutboundWakeRegistry) WakeAll() {
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
