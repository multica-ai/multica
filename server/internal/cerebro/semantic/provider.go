package semantic

import (
	"context"
	"errors"
)

// EmbeddingDim is the dimension every Provider must return. Wired into the
// 9056 migration's `vector(EmbeddingDim)` column type — changing this in code
// without a matching migration corrupts the store, so the constant is the
// single source of truth.
const EmbeddingDim = 384

// ErrProviderDisabled is returned by no-op providers. Callers treat it as
// "skip semantic" rather than as a runtime failure, so search degrades to
// keyword-only without polluting logs.
var ErrProviderDisabled = errors.New("semantic provider disabled")

// Provider turns text into a fixed-dimension embedding. Implementations must
// always return a slice of length EmbeddingDim and SHOULD return a unit
// vector — the search code computes cosine via dot product on a unit-norm
// query, so a non-normalised provider distorts ranking.
type Provider interface {
	// Embed returns the embedding for text. Implementations should respect
	// ctx for cancellation/timeout and surface model-side errors verbatim.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Model is the identifier persisted alongside the embedding so we can
	// invalidate the store when the underlying model changes. A semver-like
	// "provider:model:vN" string works fine.
	Model() string
}

// disabledProvider is the sentinel returned when no provider is configured.
// It exists so the caller never has to nil-check before calling Embed.
type disabledProvider struct{}

func (disabledProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, ErrProviderDisabled
}

func (disabledProvider) Model() string { return "disabled" }

// Disabled returns the sentinel disabled provider.
func Disabled() Provider { return disabledProvider{} }

// IsDisabled reports whether the provider is the disabled sentinel.
func IsDisabled(p Provider) bool {
	_, ok := p.(disabledProvider)
	return ok
}
