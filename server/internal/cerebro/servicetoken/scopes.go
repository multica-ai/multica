// Package servicetoken implements scoped, non-personal, workspace-bound API
// credentials ("msv_" service tokens) for external systems and provisioned
// agents. FIR-3608.
//
// A service token is the machine analogue of a Personal Access Token: it
// authenticates API calls, but it is bound to a WORKSPACE (never a user), it
// carries an explicit least-privilege scope set, it expires, it can be
// revoked, and its issuance, use, and revocation are audited. It reuses the proven
// machine-token pattern of the existing mat_/mdt_ credentials.
//
// Scopes are "host:action" strings deliberately aligned with the tool-policy
// HostOf/ActionOf model, so this coarse pre-grant stays forward-compatible
// with a future tool-policy-native resolver (design Direction C) without a
// token, table, or UI migration.
package servicetoken

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Prefix is the raw-token prefix that routes a bearer token to this package
// in the auth middleware. Parallel to "mul_" (PAT), "mat_" (task token),
// "mdt_" (daemon token).
const Prefix = "msv_"

// ServicePathPrefix is the only URL namespace a service token may reach. The
// auth middleware rejects a service-token request to any path outside this
// prefix, so a service token can never touch a human/member route — the
// strongest possible fail-closed boundary, independent of any downstream
// guard. Every route mounted here is additionally gated by RequireScope.
const ServicePathPrefix = "/api/service/"

// ActorSource is the server-set X-Actor-Source value stamped on a service
// token request. Like "task_token"/"cloud_pat" it is forgery-proof (the auth
// middleware strips any client-supplied value first) and marks a machine
// actor that human-only guards reject.
const ActorSource = "service_token"

// Known scopes. Service tokens are deliberately read-only: a machine
// credential can inspect only the areas selected when it is minted and can
// never mutate workspace state.
const (
	ScopeSkillsRead = "skills:read"
	ScopeAgentsRead = "agents:read"
	ScopeIssuesRead = "issues:read"
)

// knownScopes is the closed set a token may be granted. An unknown scope is
// rejected at mint time so a typo can never silently widen or narrow access.
var knownScopes = map[string]struct{}{
	ScopeSkillsRead: {},
	ScopeAgentsRead: {},
	ScopeIssuesRead: {},
}

// KnownScopes returns the sorted list of grantable scopes (for the UI /
// validation error messages).
func KnownScopes() []string {
	out := make([]string, 0, len(knownScopes))
	for s := range knownScopes {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// NormalizeScopes validates, de-duplicates, and sorts a requested scope set.
// It rejects an empty set (a token with no scopes can do nothing and is
// almost certainly a mistake) and any unknown scope.
func NormalizeScopes(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := knownScopes[s]; !ok {
			return nil, fmt.Errorf("unknown scope %q (known: %s)", s, strings.Join(KnownScopes(), ", "))
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one scope is required")
	}
	sort.Strings(out)
	return out, nil
}

// marshalScopes / unmarshalScopes convert between the []string domain form
// and the JSONB column form ([]byte).
func marshalScopes(scopes []string) ([]byte, error) { return json.Marshal(scopes) }

func unmarshalScopes(b []byte) ([]string, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type scopeCtxKey struct{}

// withScopes attaches a resolved scope set to the request context.
func withScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopeCtxKey{}, scopes)
}

// ScopesFromContext returns the scope set attached by the auth middleware,
// or nil when the request was not authenticated with a service token.
func ScopesFromContext(ctx context.Context) []string {
	s, _ := ctx.Value(scopeCtxKey{}).([]string)
	return s
}

// HasScope reports whether the context carries the given scope.
func HasScope(ctx context.Context, scope string) bool {
	for _, s := range ScopesFromContext(ctx) {
		if s == scope {
			return true
		}
	}
	return false
}
