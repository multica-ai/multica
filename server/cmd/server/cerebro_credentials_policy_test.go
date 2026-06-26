package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// TestVaultResourceDisjointFromCredentialScopes is the FIR-1739 v1 safety guard.
// The vault-level path authors `credential.reveal` Allow on `agentvault-vault:<vault>`,
// sharing the SAME ToolKey that multicaCredentialPolicy.Check resolves the chain
// with. Check matches resource_pattern by EQUALITY (never glob) and only ever
// builds two scopes — `cerebro-credential:<uuid>` (id) and
// `cerebro-credential-type:<type>` (type). This test pins that the vault prefix
// can never collide with either scope, so:
//   - a vault grant can never leak into a registry-credential reveal decision
//     (Check never queries an agentvault-vault: resource), and
//   - a credential id/type resource is never mistaken for a vault by the
//     connector's VaultFromResourcePattern.
// If anyone renames a prefix and the two namespaces start overlapping, this
// fails — exactly the cross-contamination the equality match exists to prevent.
func TestVaultResourceDisjointFromCredentialScopes(t *testing.T) {
	// The two scopes Check builds (cerebro_credentials_policy.go), verbatim shape.
	idResource := "cerebro-credential:" + "1b9d0c7e-0000-0000-0000-000000000000"
	typeResource := "cerebro-credential-type:" + "api_key"

	// A credential id/type resource must NOT be read as a vault grant.
	for _, r := range []string{idResource, typeResource} {
		if v, ok := agentvault.VaultFromResourcePattern(r); ok {
			t.Errorf("VaultFromResourcePattern(%q) = (%q, true); a credential scope must not project as a vault", r, v)
		}
	}

	// A vault resource must NOT carry either credential scope prefix.
	vaultResource := agentvault.VaultResourcePrefix + "bigquery"
	if strings.HasPrefix(vaultResource, "cerebro-credential:") ||
		strings.HasPrefix(vaultResource, "cerebro-credential-type:") {
		t.Errorf("vault resource %q collides with a credential scope prefix", vaultResource)
	}
	if v, ok := agentvault.VaultFromResourcePattern(vaultResource); !ok || v != "bigquery" {
		t.Errorf("VaultFromResourcePattern(%q) = (%q, %v); want (bigquery, true)", vaultResource, v, ok)
	}
}

// TestFoldCredentialCap_TightenOnly is the security invariant for FIR-1609 Phase
// 7: the unified tool-policy chain is layered on top of the deny-by-default grant
// floor as a CAP, and a cap may only ever tighten. The full 3×3 matrix of
// (grant floor) × (admin/System cap) must never produce a verdict looser than the
// grant floor alone — otherwise a credential reveal could be widened by the
// default-allow chain, the exact regression this design exists to prevent.
func TestFoldCredentialCap_TightenOnly(t *testing.T) {
	rank := map[toolpolicy.Setting]int{
		toolpolicy.SettingAllow: 0,
		toolpolicy.SettingAsk:   1,
		toolpolicy.SettingDeny:  2,
	}
	settings := []toolpolicy.Setting{toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny}

	for _, floor := range settings {
		for _, cap := range settings {
			final, reason := foldCredentialCap(floor, cap, "floor-reason", "cap-reason")

			// Invariant 1: never looser than the floor.
			if rank[final] < rank[floor] {
				t.Errorf("floor=%s cap=%s -> final=%s LOOSENED below floor (security hole)", floor, cap, final)
			}
			// Invariant 2: never looser than the cap (cap is a real ceiling too).
			if rank[final] < rank[cap] {
				t.Errorf("floor=%s cap=%s -> final=%s LOOSENED below cap", floor, cap, final)
			}
			// Invariant 3: final is exactly the more-restrictive of the two.
			want := floor
			if rank[cap] > rank[floor] {
				want = cap
			}
			if final != want {
				t.Errorf("floor=%s cap=%s -> final=%s, want %s", floor, cap, final, want)
			}
			// Invariant 4: attribution — cap reason only when the cap strictly tightened.
			wantReason := "floor-reason"
			if rank[cap] > rank[floor] {
				wantReason = "cap-reason"
			}
			if reason != wantReason {
				t.Errorf("floor=%s cap=%s -> reason=%q, want %q", floor, cap, reason, wantReason)
			}
		}
	}
}

// TestFoldCredentialCap_NoCapKeepsFloor pins the unconfigured-chain case: with no
// rows the cap resolves to Allow (Base=Allow), which must leave the grant floor
// untouched at every floor value — an admin who has authored nothing changes
// nothing about credential governance.
func TestFoldCredentialCap_NoCapKeepsFloor(t *testing.T) {
	for _, floor := range []toolpolicy.Setting{toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny} {
		final, reason := foldCredentialCap(floor, toolpolicy.SettingAllow, "floor-reason", "cap-reason")
		if final != floor {
			t.Errorf("no-cap floor=%s -> final=%s, want unchanged %s", floor, final, floor)
		}
		if reason != "floor-reason" {
			t.Errorf("no-cap floor=%s -> reason=%q, want floor-reason", floor, reason)
		}
	}
}

// TestFoldCredentialCap_PerCredentialDenyCaps documents the per-credential
// revocation guarantee the adversarial review flagged: a credential GRANTED by
// the floor (Allow) but carrying an admin Deny cap (e.g. an id-scope Deny on one
// production secret) resolves to Deny — the broad grant does not override the
// targeted admin Deny.
func TestFoldCredentialCap_PerCredentialDenyCaps(t *testing.T) {
	final, reason := foldCredentialCap(toolpolicy.SettingAllow, toolpolicy.SettingDeny, "granted", "admin deny")
	if final != toolpolicy.SettingDeny {
		t.Fatalf("granted+adminDeny -> %s, want deny", final)
	}
	if reason != "admin deny" {
		t.Fatalf("granted+adminDeny -> reason %q, want admin deny", reason)
	}
}

// TestFoldCredentialVerdict_NoGrantIsPreviousBehaviour is the keystone's safety
// invariant (FIR-1609 Phase 7): with granted=false — the case for the flag OFF, for
// no credential rows, and for any no-row Base=Allow default — the verdict MUST be
// byte-for-byte foldCredentialCap(floor, cap). The chain grant path adds nothing
// until an explicit Allow row AND the flag flip both occur.
func TestFoldCredentialVerdict_NoGrantIsPreviousBehaviour(t *testing.T) {
	settings := []toolpolicy.Setting{toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny}
	for _, floor := range settings {
		for _, cap := range settings {
			gotS, gotR := foldCredentialVerdict(floor, "floor-reason", false, "grant-reason", cap, "cap-reason")
			wantS, wantR := foldCredentialCap(floor, cap, "floor-reason", "cap-reason")
			if gotS != wantS || gotR != wantR {
				t.Errorf("floor=%s cap=%s no-grant -> (%s,%q), want (%s,%q) — keystone changed default behaviour",
					floor, cap, gotS, gotR, wantS, wantR)
			}
		}
	}
}

// TestFoldCredentialVerdict_ExplicitGrantLiftsFloor pins the keystone's added
// power: an explicit chain Allow grant lifts an Ask/Deny floor to Allow (the chain
// becomes an Allow-source), but only when no cap tightens it back down.
func TestFoldCredentialVerdict_ExplicitGrantLiftsFloor(t *testing.T) {
	for _, floor := range []toolpolicy.Setting{toolpolicy.SettingAsk, toolpolicy.SettingDeny} {
		final, reason := foldCredentialVerdict(floor, "no grant", true, "granted by tool-policy", toolpolicy.SettingAllow, "")
		if final != toolpolicy.SettingAllow {
			t.Errorf("floor=%s + chain grant + no cap -> %s, want allow", floor, final)
		}
		if reason != "granted by tool-policy" {
			t.Errorf("floor=%s grant -> reason %q, want grant attribution", floor, reason)
		}
	}
}

// TestFoldCredentialVerdict_CapStillOverridesGrant is the security ceiling: a chain
// Allow GRANT must never beat a tighten-only Deny/Ask cap. A secret freshly granted
// on the chain but carrying a per-credential admin Deny still resolves to Deny —
// the grant is an Allow-SOURCE, not an override of an explicit denial.
func TestFoldCredentialVerdict_CapStillOverridesGrant(t *testing.T) {
	final, reason := foldCredentialVerdict(toolpolicy.SettingDeny, "no grant", true, "granted by tool-policy", toolpolicy.SettingDeny, "admin deny")
	if final != toolpolicy.SettingDeny {
		t.Fatalf("grant + adminDeny -> %s, want deny (cap must win)", final)
	}
	if reason != "admin deny" {
		t.Fatalf("grant + adminDeny -> reason %q, want admin deny", reason)
	}
	// Ask cap on a granted credential routes to approval, never silently allows.
	askFinal, _ := foldCredentialVerdict(toolpolicy.SettingDeny, "no grant", true, "granted", toolpolicy.SettingAsk, "needs approval")
	if askFinal != toolpolicy.SettingAsk {
		t.Fatalf("grant + adminAsk -> %s, want ask", askFinal)
	}
}
