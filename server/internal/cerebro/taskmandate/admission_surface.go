package taskmandate

import (
	"fmt"
	"strings"
)

// AdmissionCandidate is one exact callable considered for the final task
// surface. Aliases are human-facing inventory names accepted by an existing
// SessionModeConfig. ConnectionScopeIdentities are server-owned typed scopes;
// callers cannot turn an untyped name into a grant.
type AdmissionCandidate struct {
	CallableIdentity          string
	Aliases                   []string
	ConnectionScopeIdentities []string
}

// AdmissionSurface is the normalized result shared by local and Gateway claim
// producers. SourceIndexes maps the admitted callables back to caller-owned
// tool definitions without copying runtime-specific types into this package.
type AdmissionSurface struct {
	callableIdentities        []string
	authorizationIdentities   []string
	connectionScopeIdentities []string
	sourceIndexes             []int
}

// CompatibilityAdmissionAliases is the one legacy SessionModeConfig spelling
// accepted by every claim producer. Keeping it here prevents local and Gateway
// runtimes from interpreting the same AllowedTools value differently.
func CompatibilityAdmissionAliases(callable string) []string {
	if callable == "" || strings.HasPrefix(callable, "tools:") {
		return nil
	}
	return []string{"tools:" + callable}
}

func (surface AdmissionSurface) CallableIdentities() []string {
	return append([]string(nil), surface.callableIdentities...)
}

func (surface AdmissionSurface) AuthorizationIdentities() []string {
	return append([]string(nil), surface.authorizationIdentities...)
}

func (surface AdmissionSurface) ConnectionScopeIdentities() []string {
	return append([]string(nil), surface.connectionScopeIdentities...)
}

func (surface AdmissionSurface) SourceIndexes() []int {
	return append([]int(nil), surface.sourceIndexes...)
}

// CompileAdmissionSurface applies the published SessionModeConfig.AllowedTools
// ceiling to the exact final runtime inventory. An empty ceiling preserves the
// established default. A non-empty ceiling admits a callable only by its exact
// identity, an exact display alias, or one of its typed Connection scopes.
func CompileAdmissionSurface(candidates []AdmissionCandidate, allowedTools []string) (AdmissionSurface, error) {
	allowed, err := validateAdmissionIdentities("allowed tool", allowedTools, nil)
	if err != nil {
		return AdmissionSurface{}, err
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, identity := range allowed {
		allowedSet[identity] = struct{}{}
	}

	var out AdmissionSurface
	seenCallable := make(map[string]struct{}, len(candidates))
	seenAuthorization := map[string]struct{}{}
	seenConnection := map[string]struct{}{}
	appendAuthorization := func(identity string) {
		if _, duplicate := seenAuthorization[identity]; duplicate {
			return
		}
		seenAuthorization[identity] = struct{}{}
		out.authorizationIdentities = append(out.authorizationIdentities, identity)
	}

	for index, candidate := range candidates {
		callable, err := validateAdmissionIdentity("callable", candidate.CallableIdentity, nil)
		if err != nil {
			return AdmissionSurface{}, err
		}
		if _, duplicate := seenCallable[callable]; duplicate {
			return AdmissionSurface{}, fmt.Errorf("task mandate admission: duplicate callable identity %q", callable)
		}
		seenCallable[callable] = struct{}{}
		aliases, err := validateAdmissionIdentities("alias", candidate.Aliases, nil)
		if err != nil {
			return AdmissionSurface{}, err
		}
		scopes, err := validateAdmissionIdentities("Connection scope", candidate.ConnectionScopeIdentities, validAdmissionScope)
		if err != nil {
			return AdmissionSurface{}, err
		}

		admitted := len(allowedSet) == 0 || containsAdmissionIdentity(allowedSet, callable, aliases, scopes)
		if !admitted {
			continue
		}
		out.sourceIndexes = append(out.sourceIndexes, index)
		out.callableIdentities = append(out.callableIdentities, callable)
		appendAuthorization(callable)
		for _, scope := range scopes {
			if strings.HasPrefix(scope, "connection:") {
				if _, duplicate := seenConnection[scope]; !duplicate {
					seenConnection[scope] = struct{}{}
					out.connectionScopeIdentities = append(out.connectionScopeIdentities, scope)
				}
			}
			if len(allowedSet) == 0 {
				appendAuthorization(scope)
				continue
			}
			if _, selected := allowedSet[scope]; selected {
				appendAuthorization(scope)
			}
		}
	}
	return out, nil
}

func containsAdmissionIdentity(allowed map[string]struct{}, callable string, aliases, scopes []string) bool {
	if _, ok := allowed[callable]; ok {
		return true
	}
	for _, identities := range [][]string{aliases, scopes} {
		for _, identity := range identities {
			if _, ok := allowed[identity]; ok {
				return true
			}
		}
	}
	return false
}

func validateAdmissionIdentities(kind string, identities []string, valid func(string) bool) ([]string, error) {
	out := make([]string, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		normalized, err := validateAdmissionIdentity(kind, identity, valid)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func validateAdmissionIdentity(kind, identity string, valid func(string) bool) (string, error) {
	if identity == "" || strings.TrimSpace(identity) != identity || (valid != nil && !valid(identity)) {
		return "", fmt.Errorf("task mandate admission: invalid %s identity %q", kind, identity)
	}
	return identity, nil
}

func validAdmissionScope(identity string) bool {
	if strings.HasPrefix(identity, "connection:") {
		return strings.TrimPrefix(identity, "connection:") != ""
	}
	if !strings.HasPrefix(identity, "mcp__") || !strings.HasSuffix(identity, "__*") {
		return false
	}
	server := strings.TrimSuffix(strings.TrimPrefix(identity, "mcp__"), "__*")
	return server != "" && !strings.Contains(server, "__")
}
