package channel

import (
	"fmt"
	"sort"
	"sync"
)

// ErrUnknownType is returned by Registry.Build when no Factory is
// registered for the requested Type. Callers can test for it with
// errors.Is.
var ErrUnknownType = fmt.Errorf("channel: no factory registered for type")

// Registry maps a channel Type to the Factory that builds it. Adding a
// platform is "register a factory here", never "edit the core". The
// Registry is safe for concurrent use.
//
// Registration is last-writer-wins: registering a Type that already has a
// Factory replaces it silently. This mirrors the plugin-registry pattern
// from the reference design (MUL-3506) where the last adapter to register
// a type wins, so a deployment can override a built-in adapter by
// registering its own afterwards without a removal step.
type Registry struct {
	mu        sync.RWMutex
	factories map[Type]Factory
}

// NewRegistry returns an empty Registry ready for use.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[Type]Factory)}
}

// Register binds factory to t, replacing any factory previously
// registered for t (last-writer-wins). A nil factory or an empty Type is
// ignored — registering either would only set up a guaranteed failure at
// Build time, so the Registry refuses to record it.
func (r *Registry) Register(t Type, factory Factory) {
	if t == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[t] = factory
}

// Lookup returns the Factory registered for t and whether one exists.
func (r *Registry) Lookup(t Type) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[t]
	return factory, ok
}

// Build instantiates a Channel for cfg.Type using the registered Factory.
// It returns ErrUnknownType (wrapped, with the type name) when no Factory
// is registered, and otherwise returns whatever the Factory returns.
func (r *Registry) Build(cfg Config) (Channel, error) {
	factory, ok := r.Lookup(cfg.Type)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, cfg.Type)
	}
	return factory(cfg)
}

// Types returns the registered types sorted lexicographically, so the
// result is stable across calls (map iteration order is not). Useful for
// diagnostics and for enumerating which platforms a deployment supports.
func (r *Registry) Types() []Type {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Type, 0, len(r.factories))
	for t := range r.factories {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
