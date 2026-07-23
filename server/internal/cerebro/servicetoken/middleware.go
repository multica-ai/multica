package servicetoken

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// Authenticator resolves a raw "msv_" token to an Identity. *TokenService
// implements it; the auth middleware calls it through the package-level
// authenticator registered at wiring time.
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (*Identity, error)
}

// authenticator is set once during router construction (SetAuthenticator).
// It stays nil for a router built without a database (wiring inspection), in
// which case AuthBranch fails closed with 401.
var authenticator Authenticator

// SetAuthenticator wires the resolver the auth middleware uses for "msv_"
// tokens. Called once at startup.
func SetAuthenticator(a Authenticator) { authenticator = a }

// AuthBranch handles an inbound request bearing an "msv_" service token. It is
// the single upstream touch point (called from a marked one-line branch in
// internal/middleware/auth.go). It:
//
//  1. resolves the token (revoked/expired tokens fail here → 401);
//  2. enforces the fail-closed path boundary — a service token may ONLY reach
//     the /api/service/ surface; any other path is 403, so a service token can
//     never touch a human/member route regardless of downstream guards;
//  3. strips client-supplied identity headers and stamps the
//     server-authoritative workspace, token id, and X-Actor-Source; and
//  4. attaches the scope set to the request context for RequireScope.
func AuthBranch(w http.ResponseWriter, r *http.Request, next http.Handler, rawToken string) {
	if authenticator == nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	id, err := authenticator.Authenticate(r.Context(), rawToken)
	if err != nil {
		slog.Warn("auth: invalid service token", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Fail-closed boundary: service tokens live entirely inside /api/service/.
	if !strings.HasPrefix(r.URL.Path, ServicePathPrefix) {
		writeError(w, http.StatusForbidden, "service tokens may only access the /api/service API")
		return
	}

	// A service token is not a person and not an agent: strip any client
	// identity headers before stamping the server-authoritative machine
	// identity. Mirrors the mat_ branch's forgery-proofing.
	r.Header.Del("X-User-ID")
	r.Header.Del("X-Agent-ID")
	r.Header.Del("X-Task-ID")
	r.Header.Set("X-Workspace-ID", id.WorkspaceID)
	r.Header.Set("X-Service-Token-ID", id.TokenID)
	r.Header.Set("X-Actor-Source", ActorSource)
	// Attribution-only: the minting user for writes performed via this token.
	// Deliberately NOT X-User-ID — a service token never authenticates AS the
	// minter; this header is read solely by the /api/service write handlers.
	r.Header.Del("X-Service-Token-Created-By")
	if id.CreatedBy != "" {
		r.Header.Set("X-Service-Token-Created-By", id.CreatedBy)
	}

	next.ServeHTTP(w, r.WithContext(withScopes(r.Context(), id.Scopes)))
}

// RequireScope gates a route behind a single required scope. The /api/service
// surface is machine-only: a request that is not a service token (no
// X-Actor-Source: service_token) is rejected, and a service token lacking the
// scope is rejected — both 403, fail-closed.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Actor-Source") != ActorSource {
				writeError(w, http.StatusForbidden, "this endpoint requires a service token")
				return
			}
			if !HasScope(r.Context(), scope) {
				writeError(w, http.StatusForbidden, "service token is missing required scope: "+scope)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
