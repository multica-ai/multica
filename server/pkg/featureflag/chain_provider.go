package featureflag

import "context"

// ChainProvider composes multiple providers and returns the first match.
// Earlier providers take precedence, so callers should order them from
// most-specific to most-generic: per-request override, env, db, static.
//
// A ChainProvider that wraps zero providers is valid and always returns
// (zero, false) so the Service falls back to the caller's default.
type ChainProvider struct {
	providers []Provider
}

// NewChainProvider returns a ChainProvider that evaluates the supplied
// providers in order. Nil providers are silently skipped so callers can
// pass optional fields directly without an extra nil check at every site.
func NewChainProvider(providers ...Provider) *ChainProvider {
	cp := &ChainProvider{providers: make([]Provider, 0, len(providers))}
	for _, p := range providers {
		if p != nil {
			cp.providers = append(cp.providers, p)
		}
	}
	return cp
}

// Name implements Provider.
func (*ChainProvider) Name() string { return "chain" }

// Lookup implements Provider. It returns the first decision produced by
// the wrapped providers, in the order they were registered.
func (cp *ChainProvider) Lookup(ctx context.Context, key string) (Decision, bool) {
	for _, p := range cp.providers {
		if d, ok := p.Lookup(ctx, key); ok {
			return d, true
		}
	}
	return Decision{}, false
}

// Providers returns a snapshot of the wrapped providers. The slice itself
// is a copy; the Provider values are shared and must not be mutated.
func (cp *ChainProvider) Providers() []Provider {
	out := make([]Provider, len(cp.providers))
	copy(out, cp.providers)
	return out
}
