package toolpolicy

import "testing"

// TestResolveMemberOverride_DisableCannotBeOverridden pins the FIR-2351
// follow-up product decision (2026-07-06): a workspace row set to
// SettingDisable is a hard, unopenable floor — unlike an ordinary workspace
// Deny (see TestResolveMemberOverride_GroupOverridesWorkspace and
// TestResolveMemberOverride_AgentOpensWorkspaceDefault), no Group, User, or
// Agent Allow can loosen it.
func TestResolveMemberOverride_DisableCannotBeOverridden(t *testing.T) {
	// A Group Allow, which would normally override a workspace Deny
	// (GroupOverridesWorkspace), must NOT override a workspace Disable.
	e := ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDisable, LayerGroup, SettingAllow)})
	if e.Setting != SettingDeny {
		t.Fatalf("group Allow must not open a workspace Disable, got %s", e.Setting)
	}
	if e.DecidedBy != LayerWorkspace {
		t.Fatalf("expected DecidedBy=workspace, got %q", e.DecidedBy)
	}
	if e.Reason != "Disabled by workspace" {
		t.Fatalf("expected reason %q, got %q", "Disabled by workspace", e.Reason)
	}

	// A User Allow — the most specific member layer — also must not open it.
	e = ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDisable, LayerUser, SettingAllow)})
	if e.Setting != SettingDeny {
		t.Fatalf("user Allow must not open a workspace Disable, got %s", e.Setting)
	}

	// The Agent-opening exception (AgentOpensWorkspaceDefault) must not fire
	// either: an explicit Agent Allow cannot open a workspace Disable, even
	// though it CAN open an ordinary workspace Deny.
	e = ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDisable, LayerAgent, SettingAllow)})
	if e.Setting != SettingDeny {
		t.Fatalf("agent Allow must not open a workspace Disable, got %s", e.Setting)
	}

	// Every explicit member/agent layer at once — still Deny.
	e = ResolveMemberOverride(Input{Settings: set(
		LayerWorkspace, SettingDisable,
		LayerGroup, SettingAllow,
		LayerUser, SettingAllow,
		LayerAgent, SettingAllow,
	)})
	if e.Setting != SettingDeny {
		t.Fatalf("no combination of layer Allows may open a workspace Disable, got %s", e.Setting)
	}

	// Runtime/agent/on_behalf_of/system may still TIGHTEN — Disable is already
	// the tightest verdict, so this is a no-op, but it must not crash or loosen.
	e = ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDisable, LayerRuntime, SettingAsk)})
	if e.Setting != SettingDeny {
		t.Fatalf("runtime cannot loosen a Disabled floor, got %s", e.Setting)
	}

	// The Effective.Setting contract holds: SettingDisable itself never leaks
	// out of resolution as the verdict.
	e = ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDisable)})
	if e.Setting != SettingDeny {
		t.Fatalf("Effective.Setting must be Deny, never Disable itself, got %q", e.Setting)
	}
}

// TestResolve_DisableBehavesLikeDenyUnderHardFloor proves the tighten-only
// Resolve (used by every deny-by-default floor — credentials, sandbox, repo)
// treats a Disable exactly like Deny: it was already unopenable, so Disable
// changes nothing there. This guards against SettingDisable ever leaking into
// Effective.Setting on the hard-floor path either.
func TestResolve_DisableBehavesLikeDenyUnderHardFloor(t *testing.T) {
	e := Resolve(Input{Settings: set(LayerWorkspace, SettingDisable, LayerAgent, SettingAllow)})
	if e.Setting != SettingDeny {
		t.Fatalf("hard floor must treat Disable like Deny, got %s", e.Setting)
	}
}

// TestResolveMemberOverride_DisableFlagOffPath proves Disable is not gated by
// the general member-override-only behavior: because resolveOpenable is only
// reached when the workspace cerebro_member_override flag is on, a workspace
// still on the plain tighten-only Resolve (flag off) already treats Disable
// like Deny with no loosening possible — Disable adds nothing new there, but
// must not regress it either.
func TestResolveMemberOverride_DisableFlagOffPath(t *testing.T) {
	e := Resolve(Input{Settings: set(LayerWorkspace, SettingDisable)})
	if e.Setting != SettingDeny {
		t.Fatalf("expected Deny, got %s", e.Setting)
	}
	if e.DecidedBy != LayerWorkspace {
		t.Fatalf("expected DecidedBy=workspace, got %q", e.DecidedBy)
	}
}
