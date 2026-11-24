package middleware

import "context"

type credentialContextKey struct{}

// withPersonalAccessToken marks a request authenticated by a mul_ personal
// access token. It is a context value rather than X-Actor-Source on purpose:
// handlers treat any non-empty X-Actor-Source as a machine credential (see
// requireInboxOwnerAccess), and a PAT must keep behaving like its human
// everywhere except where a handler opts in to telling them apart.
func withPersonalAccessToken(ctx context.Context) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, true)
}

// IsPersonalAccessToken reports whether Auth authenticated this request with a
// mul_ personal access token. Only Auth sets it, so clients cannot forge it.
func IsPersonalAccessToken(ctx context.Context) bool {
	v, _ := ctx.Value(credentialContextKey{}).(bool)
	return v
}
