package middleware

import "context"

type fullDelegationContextKey struct{}

// FullDelegationPrincipal keeps the effective permission user separate from
// the caller that produced the request. Only Auth may construct this value.
type FullDelegationPrincipal struct {
	GrantorUserID      string
	AgentID            string
	TaskID             string
	HomeWorkspaceID    string
	RuntimeOwnerUserID string
}

func FullDelegationFromContext(ctx context.Context) (FullDelegationPrincipal, bool) {
	p, ok := ctx.Value(fullDelegationContextKey{}).(FullDelegationPrincipal)
	return p, ok
}

func withFullDelegation(ctx context.Context, p FullDelegationPrincipal) context.Context {
	return context.WithValue(ctx, fullDelegationContextKey{}, p)
}
