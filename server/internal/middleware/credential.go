package middleware

import (
	"context"
	"net/http"
)

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

type workspaceTokenContextKey struct{}

// WithWorkspaceToken marks a request authenticated by a workspace token
// (mwt_) and records the one workspace the token is confined to. A workspace
// token is a personal access token, so its owner's requests keep behaving
// like a human's inside that workspace; like withPersonalAccessToken, the
// mark is a context value so it does not read as a machine credential.
//
// Auth and DaemonAuth set it. It is exported for the daemon WS RPC bridge,
// which replays a connection's credential into the in-process request it
// hands to an HTTP handler.
func WithWorkspaceToken(ctx context.Context, workspaceID string) context.Context {
	return context.WithValue(withPersonalAccessToken(ctx), workspaceTokenContextKey{}, workspaceID)
}

// WorkspaceTokenWorkspaceID returns the workspace a workspace token is
// confined to. ok is false for every other credential. Only the auth
// middleware sets it, so clients cannot forge it.
func WorkspaceTokenWorkspaceID(ctx context.Context) (workspaceID string, ok bool) {
	workspaceID, ok = ctx.Value(workspaceTokenContextKey{}).(string)
	return workspaceID, ok
}

// BoundWorkspace returns the workspace this request's credential is confined
// to. bound is true for an agent task token and for a workspace token; the
// request may then act in workspaceID and nowhere else, whatever workspace it
// names. An empty workspaceID with bound=true is a credential that lost its
// workspace: callers must refuse it, never treat it as unbound.
func BoundWorkspace(r *http.Request) (workspaceID string, bound bool) {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		return r.Header.Get("X-Workspace-ID"), true
	}
	return WorkspaceTokenWorkspaceID(r.Context())
}
