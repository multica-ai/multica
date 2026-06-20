package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

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
