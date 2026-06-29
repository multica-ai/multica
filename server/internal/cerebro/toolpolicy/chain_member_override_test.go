package toolpolicy

import "testing"

// TestResolveMemberOverride_JespersExample is the canonical example from the
// FIR-2175 design: an agent Deny wins even when workspace and member both Allow.
// Stage A makes the member Allow; Stage B lets the agent tighten it to Deny.
func TestResolveMemberOverride_JespersExample(t *testing.T) {
	e := ResolveMemberOverride(Input{
		Settings: set(
			LayerWorkspace, SettingAllow,
			LayerUser, SettingAllow,
			LayerAgent, SettingDeny,
		),
	})
	if e.Setting != SettingDeny {
		t.Fatalf("expected Deny, got %s", e.Setting)
	}
	if e.CappedBy != LayerAgent {
		t.Fatalf("expected CappedBy=agent, got %q", e.CappedBy)
	}
	if e.DecidedBy != LayerAgent {
		t.Fatalf("expected DecidedBy=agent, got %q", e.DecidedBy)
	}
}

// TestResolveMemberOverride_MemberOverridesGroup is the core behavioural change
// vs. Resolve: a member (User) Allow OVERRIDES a Group Deny. Under Resolve this
// would be Deny ("Capped by group"); under the member-override model the member
// is the most specific human layer and wins.
func TestResolveMemberOverride_MemberOverridesGroup(t *testing.T) {
	in := Input{Settings: set(LayerGroup, SettingDeny, LayerUser, SettingAllow)}

	// New model: member wins → Allow.
	got := ResolveMemberOverride(in)
	if got.Setting != SettingAllow {
		t.Fatalf("member Allow must override group Deny, got %s", got.Setting)
	}
	if got.DecidedBy != LayerUser {
		t.Fatalf("expected DecidedBy=user, got %q", got.DecidedBy)
	}
	if got.CappedBy != "" {
		t.Fatalf("member override is not a cap, got CappedBy=%q", got.CappedBy)
	}

	// Contrast with today's tighten-only Resolve: same input → Deny by group.
	old := Resolve(in)
	if old.Setting != SettingDeny || old.CappedBy != LayerGroup {
		t.Fatalf("Resolve baseline drift: expected Deny capped by group, got %s capped %q", old.Setting, old.CappedBy)
	}
}

// TestResolveMemberOverride_GroupOverridesWorkspace proves Stage A specificity
// between the inherited layers: a Group Allow overrides a Workspace Deny when the
// member sets nothing.
func TestResolveMemberOverride_GroupOverridesWorkspace(t *testing.T) {
	e := ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow)})
	if e.Setting != SettingAllow {
		t.Fatalf("group Allow must override workspace Deny, got %s", e.Setting)
	}
	if e.DecidedBy != LayerGroup {
		t.Fatalf("expected DecidedBy=group, got %q", e.DecidedBy)
	}
}

// TestResolveMemberOverride_InheritsWhenMemberSilent proves the member inherits
// the group (then workspace) verdict when it sets nothing.
func TestResolveMemberOverride_InheritsWhenMemberSilent(t *testing.T) {
	// Only group has an opinion → member inherits group Deny.
	g := ResolveMemberOverride(Input{Settings: set(LayerGroup, SettingDeny)})
	if g.Setting != SettingDeny || g.DecidedBy != LayerGroup {
		t.Fatalf("expected Deny by group, got %s by %q", g.Setting, g.DecidedBy)
	}
	// Only workspace has an opinion → member inherits workspace Ask.
	w := ResolveMemberOverride(Input{Settings: set(LayerWorkspace, SettingAsk)})
	if w.Setting != SettingAsk || w.DecidedBy != LayerWorkspace {
		t.Fatalf("expected Ask by workspace, got %s by %q", w.Setting, w.DecidedBy)
	}
}

// TestResolveMemberOverride_AgentCannotExceedMember proves Stage B is a ceiling:
// an agent Allow cannot loosen a member Deny.
func TestResolveMemberOverride_AgentCannotExceedMember(t *testing.T) {
	e := ResolveMemberOverride(Input{
		Settings: set(
			LayerUser, SettingDeny,
			LayerAgent, SettingAllow, // tries to exceed the member ceiling — ignored
		),
	})
	if e.Setting != SettingDeny {
		t.Fatalf("agent Allow must not exceed member Deny, got %s", e.Setting)
	}
	if e.DecidedBy != LayerUser {
		t.Fatalf("expected DecidedBy=user, got %q", e.DecidedBy)
	}
}

// TestResolveMemberOverride_RuntimeTightens proves runtime can tighten the
// member ceiling (and is attributed as a cap).
func TestResolveMemberOverride_RuntimeTightens(t *testing.T) {
	e := ResolveMemberOverride(Input{
		Settings: set(
			LayerUser, SettingAllow,
			LayerRuntime, SettingAsk,
		),
	})
	if e.Setting != SettingAsk {
		t.Fatalf("expected Ask, got %s", e.Setting)
	}
	if e.CappedBy != LayerRuntime {
		t.Fatalf("expected CappedBy=runtime, got %q", e.CappedBy)
	}
}

// TestResolveMemberOverride_BaseDefaultAllow proves an empty chain defaults to
// Allow, like Resolve.
func TestResolveMemberOverride_BaseDefaultAllow(t *testing.T) {
	e := ResolveMemberOverride(Input{})
	if e.Setting != SettingAllow {
		t.Fatalf("empty chain must default to Allow, got %s", e.Setting)
	}
	if e.DecidedBy != "" {
		t.Fatalf("default decision has no decider, got %q", e.DecidedBy)
	}
}

// TestResolveMemberOverride_SystemAskBecomesDeny proves the System fail-safe is
// preserved: a System actor's Ask resolves to Deny (no human to approve).
func TestResolveMemberOverride_SystemAskBecomesDeny(t *testing.T) {
	e := ResolveMemberOverride(Input{
		Settings: set(LayerUser, SettingAsk),
		IsSystem: true,
	})
	if e.Setting != SettingDeny {
		t.Fatalf("system Ask must fail safe to Deny, got %s", e.Setting)
	}
	if e.DecidedBy != LayerSystem {
		t.Fatalf("expected DecidedBy=system, got %q", e.DecidedBy)
	}
}
