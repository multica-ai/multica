package mcp

import "context"

type callableIdentityContextKey struct{}

func withCallableIdentity(ctx context.Context, callable string) context.Context {
	return context.WithValue(ctx, callableIdentityContextKey{}, callable)
}

// WithCallableIdentity pins one exact callable to an individual downstream
// request. CLI commands use it for helper reads that precede a mutation, so a
// get_issue lookup never inherits update_issue from the outer command.
func WithCallableIdentity(ctx context.Context, callable string) context.Context {
	return withCallableIdentity(ctx, callable)
}

// CallableIdentity returns the exact registered MCP tool currently being
// dispatched. Downstream REST bridges carry it as immutable request metadata so
// Task Mandate checks never infer a broad permission family from a route.
func CallableIdentity(ctx context.Context) string {
	callable, _ := ctx.Value(callableIdentityContextKey{}).(string)
	return callable
}
