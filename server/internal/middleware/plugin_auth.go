package middleware

import (
	"net/http"
	"strings"
)

// PluginAuth lets the Action API be reached two ways without weakening either.
//
// The original way is the only one a SURFACE has: no credential at all. The
// iframe posts a message to the host page, the host re-issues the call on the
// signed-in user's session, and this middleware sends it through the ordinary
// Auth chain like any other request.
//
// Hooks add a second way. A plugin's own server has no session and never will,
// so when it presents a plugin bearer token this middleware steps aside and
// lets the handler resolve the token itself — which it must, because only the
// handler knows which installation and which scopes that token stands for.
//
// Stepping aside is not the same as skipping authentication: every Action API
// handler starts by resolving a caller, and a request that arrives with neither
// a session nor a valid token fails there. What this avoids is the session
// chain rejecting a token-bearing request before the handler can look at it.
func PluginAuth(sessionAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		sessionProtected := sessionAuth(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsPluginBearerToken(BearerToken(r)) {
				next.ServeHTTP(w, r)
				return
			}
			sessionProtected.ServeHTTP(w, r)
		})
	}
}

// BearerToken pulls the raw credential out of an Authorization header.
func BearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// IsPluginBearerToken reports whether a credential is one of ours to resolve.
//
// Prefix-matched rather than validated: this only decides which code path gets
// to look at the token, and an invalid token routed here is refused by the
// handler a moment later. Deciding by prefix keeps a plugin token from being
// tried against the PAT cache and a PAT from being tried against installations.
func IsPluginBearerToken(token string) bool {
	return strings.HasPrefix(token, "mpi_") || strings.HasPrefix(token, "mpc_")
}
