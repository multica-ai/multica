package platformcatalog

import "testing"

// approvedExternallyManaged pins the exact set of catalog entries that may
// carry ManagedExternally. The end state decided in FIR-4220 — reached in
// slice 2 — is the three machine-intake boundaries (autopilot_webhook,
// daemon_runtime_callback, gateway_channel_delivery), whose caller
// authenticates with a secret/token and has no person or agent for
// Allow/Ask/Deny to judge. Each carries WorkspaceIntakeSwitch: its
// workspace-layer policy row is a live off-switch consulted at the intake
// point (platformaction.IntakeAllowed).
//
// This is the tripwire the FIR-4220 plan requires: a new capability CANNOT be
// added as an advisory "Managed externally" row without editing this list,
// which requires an explicit, reviewed decision naming its real control point.
var approvedExternallyManaged = map[string]bool{
	"autopilot_webhook":        true,
	"daemon_runtime_callback":  true,
	"gateway_channel_delivery": true,
}

func TestManagedExternallySetIsPinned(t *testing.T) {
	seen := map[string]bool{}
	for _, capability := range All() {
		if !capability.ManagedExternally {
			continue
		}
		seen[capability.Key] = true
		if !approvedExternallyManaged[capability.Key] {
			t.Errorf("%q is a NEW advisory (ManagedExternally) row — FIR-4220 forbids adding these silently; either enforce it through the tool-policy engine or get the exception approved and add it to approvedExternallyManaged with its real control point", capability.Key)
		}
	}
	for key := range approvedExternallyManaged {
		if !seen[key] {
			t.Errorf("%q is approved as externally managed but no longer marked in the catalog — remove it from approvedExternallyManaged", key)
		}
	}
}

// TestFIR4220EnforcedKeys locks the eight capabilities flipped in FIR-4220
// (slices 1 and 2) to policy-engine enforcement, so a regression back to
// advisory cannot land silently.
func TestFIR4220EnforcedKeys(t *testing.T) {
	for _, key := range []string{
		"rerun_issue",
		"trigger_autopilot",
		"autopilot_scope",
		"schedule_agent_wakeup",
		"manage_project_access",
		"use_other_runtime",
		"read_issues",
		"read_projects",
	} {
		if !Enforced(key) {
			t.Errorf("%q must be policy-engine enforced (FIR-4220)", key)
		}
	}
}

// TestIntakeSwitchKeysArePinned locks the WorkspaceIntakeSwitch marker to the
// three machine-intake boundaries: every intake key carries it, and no
// policy-enforced key does (the marker changes what Settings accepts and what
// the table shows — see externallyManagedWriteError and appendPlatformRows).
func TestIntakeSwitchKeysArePinned(t *testing.T) {
	for _, capability := range All() {
		want := approvedExternallyManaged[capability.Key]
		if capability.WorkspaceIntakeSwitch != want {
			t.Errorf("%q WorkspaceIntakeSwitch = %v, want %v", capability.Key, capability.WorkspaceIntakeSwitch, want)
		}
	}
}
