package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/hztauth"
)

func TestSafePostAuthPath(t *testing.T) {
	for name, testCase := range map[string]struct {
		input string
		want  string
	}{
		"workspace":       {"/acme/issues?view=mine", "/acme/issues?view=mine"},
		"absolute":        {"https://evil.example/", ""},
		"scheme relative": {"//evil.example/", ""},
		"plain":           {"acme/issues", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := safePostAuthPath(testCase.input); got != testCase.want {
				t.Fatalf("safePostAuthPath(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestHZTWorkspaceRoleUsesExactRoleAllowlist(t *testing.T) {
	h := &Handler{}
	for name, testCase := range map[string]struct {
		identity hztauth.Identity
		want     string
	}{
		"legacy admin":         {hztauth.Identity{Role: "admin"}, "admin"},
		"RBAC manager":         {hztauth.Identity{Role: "operator", Roles: []hztauth.Role{{Slug: "admin_meituan_operator_manager"}}}, "admin"},
		"all-platform manager": {hztauth.Identity{Role: "admin_operator_manager"}, "admin"},
		"Douyin manager":       {hztauth.Identity{Role: "admin_douyin_operator_manager"}, "admin"},
		"XHS manager":          {hztauth.Identity{Role: "admin_xiaohongshu_operator_manager"}, "admin"},
		"Hunliji manager":      {hztauth.Identity{Role: "admin_hunliji_operator_manager"}, "admin"},
		"prefix is not enough": {hztauth.Identity{Role: "admin_unknown"}, "member"},
		"operator":             {hztauth.Identity{Role: "operator"}, "member"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := h.hztWorkspaceRole(testCase.identity); got != testCase.want {
				t.Fatalf("hztWorkspaceRole(%+v) = %q, want %q", testCase.identity, got, testCase.want)
			}
		})
	}
}

func TestNormalizedHZTEmailFallsBackWithoutTrustworthyEmail(t *testing.T) {
	valid := "Admin@Example.com"
	if got := normalizedHZTEmail(hztauth.Identity{ID: "one", Email: &valid}); got != "admin@example.com" {
		t.Fatalf("normalized email = %q", got)
	}
	if got := normalizedHZTEmail(hztauth.Identity{ID: "stable-id"}); got != "hzt-b1def59c1c5d69343801d03c@localhost.local" {
		t.Fatalf("fallback email = %q", got)
	}
}
