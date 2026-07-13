package featureflag

import "context"

// EvalContext is the per-request context used to evaluate dynamic flags such
// as percent rollouts and per-user allow/deny lists.
//
// All fields are optional. A zero EvalContext is valid and matches no
// targeting rules, which means percent rollouts default to bucket 0 (always
// off) and allow/deny lookups silently miss.
type EvalContext struct {
	// UserID is the canonical identifier used for per-user targeting and
	// for the default percent-rollout bucketing key. Free-form string;
	// the framework never parses it.
	UserID string

	// WorkspaceID identifies the multica workspace that issued the
	// request. Useful for workspace-scoped rollouts.
	WorkspaceID string

	// Attributes holds any other targeting attributes the caller wants
	// to expose to rules, for example "country", "plan", or "client".
	// Keys are case-sensitive.
	Attributes map[string]string
}

// Lookup returns the value of attribute name in the order:
// UserID, WorkspaceID, then Attributes[name]. The well-known names
// "user_id" and "workspace_id" map to the dedicated fields so rules can use
// them by name without callers having to also populate Attributes.
//
// The bool return signals whether a non-empty value was found, which lets
// callers distinguish "missing" from "explicitly empty".
func (ec EvalContext) Lookup(name string) (string, bool) {
	switch name {
	case "user_id":
		if ec.UserID != "" {
			return ec.UserID, true
		}
		return "", false
	case "workspace_id":
		if ec.WorkspaceID != "" {
			return ec.WorkspaceID, true
		}
		return "", false
	}
	if ec.Attributes == nil {
		return "", false
	}
	v, ok := ec.Attributes[name]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

type evalContextKey struct{}

// WithEvalContext returns a derived context that carries ec for later
// retrieval via EvalContextFrom. Passing the zero EvalContext is allowed and
// effectively clears any previously attached context.
func WithEvalContext(parent context.Context, ec EvalContext) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, evalContextKey{}, ec)
}

// EvalContextFrom extracts the EvalContext previously attached with
// WithEvalContext. It returns the zero value when the context carries no
// EvalContext, never nil, so callers can read fields unconditionally.
func EvalContextFrom(ctx context.Context) EvalContext {
	if ctx == nil {
		return EvalContext{}
	}
	v, ok := ctx.Value(evalContextKey{}).(EvalContext)
	if !ok {
		return EvalContext{}
	}
	return v
}
