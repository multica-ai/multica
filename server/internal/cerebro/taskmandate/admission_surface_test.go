package taskmandate

import (
	"reflect"
	"testing"
)

func TestCompileAdmissionSurfaceAppliesExactAndConnectionWildcards(t *testing.T) {
	t.Parallel()
	candidates := []AdmissionCandidate{
		{CallableIdentity: "create_issue", Aliases: []string{"Create issue"}},
		{CallableIdentity: "registry__get_v1_orders", ConnectionScopeIdentities: []string{"connection:registry"}},
		{CallableIdentity: "mcp__catalog__search", ConnectionScopeIdentities: []string{"connection:catalog", "mcp__catalog__*"}},
		{CallableIdentity: "mcp__billing__search", ConnectionScopeIdentities: []string{"connection:billing", "mcp__billing__*"}},
	}

	compiled, err := CompileAdmissionSurface(candidates, []string{
		"create_issue",
		"connection:registry",
		"mcp__catalog__*",
	})
	if err != nil {
		t.Fatalf("CompileAdmissionSurface: %v", err)
	}
	if got, want := compiled.CallableIdentities(), []string{"create_issue", "registry__get_v1_orders", "mcp__catalog__search"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CallableIdentities = %v, want %v", got, want)
	}
	if got, want := compiled.AuthorizationIdentities(), []string{
		"create_issue", "registry__get_v1_orders", "connection:registry",
		"mcp__catalog__search", "mcp__catalog__*",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizationIdentities = %v, want %v", got, want)
	}
	if got, want := compiled.ConnectionScopeIdentities(), []string{"connection:registry", "connection:catalog"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ConnectionScopeIdentities = %v, want %v", got, want)
	}
	if got := compiled.SourceIndexes(); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("SourceIndexes = %v, want [0 1 2]", got)
	}
}

func TestCompileAdmissionSurfaceWithoutModeCeilingPreservesAllCandidates(t *testing.T) {
	t.Parallel()
	compiled, err := CompileAdmissionSurface([]AdmissionCandidate{
		{CallableIdentity: "mcp__catalog__search", ConnectionScopeIdentities: []string{"connection:catalog", "mcp__catalog__*"}},
		{CallableIdentity: "create_issue"},
	}, nil)
	if err != nil {
		t.Fatalf("CompileAdmissionSurface: %v", err)
	}
	if got, want := compiled.AuthorizationIdentities(), []string{
		"mcp__catalog__search", "connection:catalog", "mcp__catalog__*", "create_issue",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizationIdentities = %v, want %v", got, want)
	}
}

func TestCompileAdmissionSurfaceRejectsMalformedIdentities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		candidates []AdmissionCandidate
		allowed    []string
	}{
		{name: "callable whitespace", candidates: []AdmissionCandidate{{CallableIdentity: " create_issue"}}},
		{name: "blank callable", candidates: []AdmissionCandidate{{CallableIdentity: ""}}},
		{name: "scope is not typed", candidates: []AdmissionCandidate{{CallableIdentity: "create_issue", ConnectionScopeIdentities: []string{"registry"}}}},
		{name: "invalid MCP wildcard", candidates: []AdmissionCandidate{{CallableIdentity: "create_issue", ConnectionScopeIdentities: []string{"mcp__catalog__search"}}}},
		{name: "blank mode identity", candidates: []AdmissionCandidate{{CallableIdentity: "create_issue"}}, allowed: []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CompileAdmissionSurface(tt.candidates, tt.allowed); err == nil {
				t.Fatal("CompileAdmissionSurface succeeded, want validation error")
			}
		})
	}
}
