package featureflag

import "context"

// Reason identifies why a Decision returned the value it did. Reasons are
// observable strings so they can be exposed in metadata endpoints and
// structured logs.
type Reason string

const (
	// ReasonStatic means a provider returned an unconditional value
	// (Rule.Default, an Allow hit, a Deny hit, or a Variant lookup).
	ReasonStatic Reason = "static"

	// ReasonPercent means the value came from a deterministic percent
	// rollout bucket. The same (key, identifier) pair always yields the
	// same bucket.
	ReasonPercent Reason = "percent"

	// ReasonOverride means a per-request override was applied (for
	// example a debug header or a cookie). Overrides win over normal
	// rules so they should never be exposed to untrusted callers.
	ReasonOverride Reason = "override"

	// ReasonDefault means no provider matched the key and the caller's
	// default value was returned. This is the only Reason callers ever
	// see when their default is used.
	ReasonDefault Reason = "default"

	// ReasonError means a provider attempted to evaluate the flag but
	// failed (for example a malformed env var). The default is returned
	// and the error reason is recorded for diagnostics.
	ReasonError Reason = "error"
)

// Decision is the structured result of a flag evaluation. Callers typically
// use Service.IsEnabled or Service.Variant which collapse Decision into a
// single value, but Decision is exposed for diagnostics endpoints and tests.
type Decision struct {
	// Key is the flag identifier that was evaluated.
	Key string

	// Enabled is the boolean projection of the decision. For variant
	// flags it is true when Variant != "" and Variant != "off".
	Enabled bool

	// Variant is the raw value the provider produced. Boolean flags use
	// "on" / "off". Variant flags use arbitrary identifiers such as
	// "control", "experiment-v2".
	Variant string

	// Reason records why this decision was made (see Reason constants).
	Reason Reason

	// Source is the name of the provider that produced the decision, or
	// "default" when no provider matched. Useful for debugging which
	// configuration layer is winning in a ChainProvider setup.
	Source string
}

// Provider is the configuration backend for the feature flag Service.
// Implementations must be safe for concurrent use; the Service reads
// providers from many goroutines without additional locking.
//
// A Lookup call returns (decision, true) when the provider knows about the
// key and (zero, false) when it does not. Callers must rely on the boolean,
// not on the Decision content, because Decision is otherwise the zero value
// when found is false.
type Provider interface {
	// Lookup evaluates a single flag against the supplied context.
	// Implementations should never panic; on internal failures they
	// should return a Decision with Reason=ReasonError and found=true so
	// the Service can record the failure without falling through to a
	// less specific provider.
	Lookup(ctx context.Context, key string) (decision Decision, found bool)

	// Name returns a stable, human-readable identifier used in Decision.Source
	// and in diagnostic endpoints. Two provider instances of the same type
	// may share a name; uniqueness is not required.
	Name() string
}
