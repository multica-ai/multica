package featureflag

import (
	"context"
	"log/slog"
)

// Service is the framework-level Toggle Router. Business code asks the
// Service for flag decisions; the Service in turn consults its configured
// Provider. The Service is safe for concurrent use and is the only type
// callers should hold a reference to.
//
// A nil *Service is valid and behaves as if every flag were missing: every
// call returns the supplied default with Reason=ReasonDefault. This lets
// callers compose Service without first guarding against nil, which in
// practice is the most common cause of feature-flag-related nil panics.
type Service struct {
	provider Provider
	logger   *slog.Logger
}

// Option configures optional Service behavior.
type Option func(*Service)

// WithLogger attaches a structured logger that the Service will use to emit
// warnings for malformed flag configuration. By default the Service is
// silent so it can be embedded in tests without polluting output.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// NewService returns a Service backed by the supplied provider. Passing a
// nil provider is allowed and is equivalent to the always-default behavior;
// see the package doc for the rationale.
func NewService(provider Provider, opts ...Option) *Service {
	s := &Service{provider: provider}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// IsEnabled returns true when the named flag evaluates to an "on" state for
// the EvalContext attached to ctx. When the flag is unknown or its provider
// errors, the supplied default is returned so business code can ship with
// confidence that a missing flag never crashes a request.
//
// IsEnabled is the most common Toggle Point in business code:
//
//	if flags.IsEnabled(ctx, "billing_new_invoice_email", false) {
//	    return s.sendNewInvoiceEmail(ctx, invoice)
//	}
//	return s.sendLegacyInvoiceEmail(ctx, invoice)
func (s *Service) IsEnabled(ctx context.Context, key string, defaultVal bool) bool {
	return s.Decision(ctx, key, defaultVal).Enabled
}

// Variant returns the raw variant value for the named flag, falling back to
// defaultVal when no provider matches. Use Variant for multi-arm flags
// (A/B/C tests, "control"/"experiment"/"holdout"). For simple on/off flags,
// prefer IsEnabled.
func (s *Service) Variant(ctx context.Context, key string, defaultVal string) string {
	d := s.decisionWithVariantDefault(ctx, key, defaultVal)
	return d.Variant
}

// Decision returns the full structured Decision for a flag. The supplied
// boolean default is used to populate both Variant and Enabled when no
// provider matches the key. Diagnostic endpoints and tests use this entry
// point to surface Reason and Source.
func (s *Service) Decision(ctx context.Context, key string, defaultVal bool) Decision {
	if s == nil || s.provider == nil {
		return defaultDecision(key, boolToVariant(defaultVal), defaultVal)
	}
	d, ok := s.provider.Lookup(ctx, key)
	if !ok {
		return defaultDecision(key, boolToVariant(defaultVal), defaultVal)
	}
	if d.Reason == ReasonError && s.logger != nil {
		s.logger.WarnContext(ctx, "feature flag provider returned an error decision",
			slog.String("key", key),
			slog.String("source", d.Source),
		)
	}
	d.Key = key
	return d
}

// decisionWithVariantDefault is the variant-aware twin of Decision. It is
// kept private because callers who care about reasons can rely on Decision
// + IsEnabled; Variant is a convenience.
func (s *Service) decisionWithVariantDefault(ctx context.Context, key, defaultVariant string) Decision {
	if s == nil || s.provider == nil {
		return defaultDecision(key, defaultVariant, variantEnabled(defaultVariant))
	}
	d, ok := s.provider.Lookup(ctx, key)
	if !ok {
		return defaultDecision(key, defaultVariant, variantEnabled(defaultVariant))
	}
	d.Key = key
	return d
}

// Provider exposes the wrapped Provider so diagnostic endpoints can iterate
// known flags. Callers MUST NOT mutate the returned Provider; the contract
// is read-only.
func (s *Service) Provider() Provider {
	if s == nil {
		return nil
	}
	return s.provider
}

func defaultDecision(key, variant string, enabled bool) Decision {
	return Decision{
		Key:     key,
		Enabled: enabled,
		Variant: variant,
		Reason:  ReasonDefault,
		Source:  "default",
	}
}

// boolToVariant produces the canonical variant string for a boolean flag.
// "on" / "off" is used rather than "true" / "false" so that string-typed
// providers (e.g. env vars) do not collide with the user's own bool-as-text
// values.
func boolToVariant(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// variantEnabled reports whether a variant string projects to "enabled".
// Empty and "off" are the only false values; everything else, including
// arbitrary variant identifiers like "experiment-v2", is enabled. Callers
// who care about specific variants should compare with == directly.
func variantEnabled(v string) bool {
	switch v {
	case "", "off", "false", "0":
		return false
	}
	return true
}
