package platformaccess

import "testing"

func TestSpecialPermissionContractsAreComplete(t *testing.T) {
	want := map[string]Enforcement{
		"hooks:read":                 EnforcementAuthenticatedRead,
		"hooks:write":                EnforcementActorOptIn,
		"hooks:enforce":              EnforcementHumanOptIn,
		"hooks:manage_managed":       EnforcementOwnerOnly,
		"tools:personal-browser":     EnforcementAgentOptIn,
		"tools:test-as-user":         EnforcementHumanOptIn,
		"manage_workspace_overrides": EnforcementHumanOptInOrAdmin,
		"manage_group_overrides":     EnforcementHumanOptInOrAdmin,
	}

	for _, contract := range All() {
		enforcement, ok := want[contract.Key]
		if !ok {
			t.Errorf("unexpected special permission contract %q", contract.Key)
			continue
		}
		if contract.Enforcement != enforcement {
			t.Errorf("%s enforcement = %q, want %q", contract.Key, contract.Enforcement, enforcement)
		}
		delete(want, contract.Key)
	}
	for key := range want {
		t.Errorf("missing special permission contract %q", key)
	}
}
