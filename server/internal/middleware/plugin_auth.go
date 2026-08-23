package middleware

import (
	"net/http"
	"strings"
)

// PluginBearerOnly keeps the public Action API on a machine-credential trust
// boundary. Browser sessions use the separate, session-authenticated bridge
// route; the public /v1 contract accepts only installation and callback tokens.
//
// Prefix matching is only routing. The Action handler still resolves the token
// against its store and rejects an invalid or expired credential.
func PluginBearerOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsPluginBearerToken(BearerToken(r)) {
			writeError(w, http.StatusUnauthorized, "plugin bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
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
