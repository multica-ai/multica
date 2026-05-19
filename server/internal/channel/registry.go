package channel

import (
	"errors"
	"sync"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

var (
	// ErrDuplicateChannel is returned by Register when a channel with the same
	// name has already been registered.
	ErrDuplicateChannel = errors.New("duplicate channel")

	// ErrChannelNotFound is returned by Get and Unregister when the requested
	// channel does not exist in the registry.
	ErrChannelNotFound = errors.New("channel not found")

	// ErrNotImplemented is returned by adapters from optional methods that
	// have not yet been wired up for a given platform (e.g. SendCard before
	// T16). Callers MUST check via errors.Is so a future implementation
	// flipping from "not implemented" to "implemented" is transparent.
	ErrNotImplemented = errors.New("channel: not implemented")
)

// Registry holds the set of active port.Channel adapters. It is safe for
// concurrent use.
type Registry struct {
	mu   sync.RWMutex
	slots map[string]port.Channel
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		slots: make(map[string]port.Channel),
	}
}

// Register adds a channel to the registry. If a channel with the same Name()
// already exists, it returns ErrDuplicateChannel.
func (r *Registry) Register(ch port.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := ch.Name()
	if _, exists := r.slots[name]; exists {
		return ErrDuplicateChannel
	}
	r.slots[name] = ch
	return nil
}

// Unregister removes a channel from the registry by name. If the channel does
// not exist, it returns ErrChannelNotFound so callers can treat the operation
// as idempotent when desired.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.slots[name]; !exists {
		return ErrChannelNotFound
	}
	delete(r.slots, name)
	return nil
}

// Get returns the channel with the given name. If it is not registered, it
// returns ErrChannelNotFound.
func (r *Registry) Get(name string) (port.Channel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ch, exists := r.slots[name]
	if !exists {
		return nil, ErrChannelNotFound
	}
	return ch, nil
}

// List returns a snapshot of all currently registered channels.
func (r *Registry) List() []port.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]port.Channel, 0, len(r.slots))
	for _, ch := range r.slots {
		out = append(out, ch)
	}
	return out
}
