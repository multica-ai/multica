package platformcatalog

import "testing"

func TestEveryCatalogPermissionHasOneDeclaredSecurityOwner(t *testing.T) {
	for _, capability := range All() {
		t.Run(capability.Key, func(t *testing.T) {
			switch {
			case capability.ManagedExternally:
				if len(capability.Ops) == 0 && len(capability.Evidence) == 0 {
					t.Fatal("externally managed permission has no route or domain safety owner")
				}
				if capability.ExternalSecurityOwner == "" {
					t.Fatal("externally managed permission has no named security owner")
				}
				if Enforced(capability.Key) {
					t.Fatal("externally managed permission reported as policy-engine enforced")
				}
			default:
				if capability.ExternalSecurityOwner != "" {
					t.Fatal("policy-engine permission also declares an external security owner")
				}
				if !Enforced(capability.Key) {
					t.Fatal("policy-engine permission reported as external")
				}
			}
		})
	}

	if Enforced("unknown_future_permission") {
		t.Fatal("unknown permission must fail closed until it enters the catalog contract")
	}
}
