package toolpolicy

import "testing"

// TestResolveWithMode_HardFloorMatchesResolve proves ResolveWithMode(ModeHardFloor, ...)
// is byte-for-byte identical to the pre-unification Resolve on every case Resolve
// itself is tested against (FIR-2351 unification: one function body per mode
// instead of two hand-synced top-level algorithms).
func TestResolveWithMode_HardFloorMatchesResolve(t *testing.T) {
	cases := []Input{
		{},
		{Settings: set(LayerWorkspace, SettingDeny)},
		{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow, LayerUser, SettingAllow)},
		{Settings: set(LayerRuntime, SettingAllow, LayerAgent, SettingAllow, LayerUser, SettingDeny)},
		{Settings: set(LayerUser, SettingAsk), IsSystem: true},
		{Settings: set(LayerAgent, SettingDeny, LayerOnBehalfOf, SettingAllow)},
		{Base: SettingDeny},
		{Base: SettingDeny, Settings: set(LayerUser, SettingAllow)},
	}
	for i, in := range cases {
		want := Resolve(in)
		got := ResolveWithMode(ModeHardFloor, in)
		if got != want {
			t.Fatalf("case %d: ResolveWithMode(ModeHardFloor) = %+v, want %+v (from Resolve)", i, got, want)
		}
	}
}

// TestResolveWithMode_OpenableMatchesResolveMemberOverride proves
// ResolveWithMode(ModeOpenable, ...) is byte-for-byte identical to the
// pre-unification ResolveMemberOverride on every case it is tested against.
func TestResolveWithMode_OpenableMatchesResolveMemberOverride(t *testing.T) {
	cases := []Input{
		{},
		{Settings: set(LayerWorkspace, SettingAllow, LayerUser, SettingAllow, LayerAgent, SettingDeny)},
		{Settings: set(LayerGroup, SettingDeny, LayerUser, SettingAllow)},
		{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow)},
		{Settings: set(LayerGroup, SettingDeny)},
		{Settings: set(LayerWorkspace, SettingAsk)},
		{Settings: set(LayerUser, SettingDeny, LayerAgent, SettingAllow)},
		{Settings: set(LayerUser, SettingAllow, LayerRuntime, SettingAsk)},
		{Settings: set(LayerUser, SettingAsk), IsSystem: true},
		{Settings: set(LayerUser, SettingAllow, LayerOnBehalfOf, SettingDeny)},
		{Settings: set(LayerAgent, SettingDeny, LayerOnBehalfOf, SettingAllow)},
	}
	for i, in := range cases {
		want := ResolveMemberOverride(in)
		got := ResolveWithMode(ModeOpenable, in)
		if got != want {
			t.Fatalf("case %d: ResolveWithMode(ModeOpenable) = %+v, want %+v (from ResolveMemberOverride)", i, got, want)
		}
	}
}

// TestResolveWithMode_ModesDivergeOnLoosening is the invariant the whole
// unification protects: for the SAME input, hard_floor and openable can give
// different answers only in the loosening direction (openable may grant what
// hard_floor denies), never the other way — a mode can never be "more open
// than openable" or "more closed than hard_floor" for the same input.
func TestResolveWithMode_ModesDivergeOnLoosening(t *testing.T) {
	in := Input{Settings: set(LayerWorkspace, SettingDeny, LayerGroup, SettingAllow)}

	hardFloor := ResolveWithMode(ModeHardFloor, in)
	openable := ResolveWithMode(ModeOpenable, in)

	if hardFloor.Setting != SettingDeny {
		t.Fatalf("hard_floor must keep the workspace Deny closed, got %s", hardFloor.Setting)
	}
	if openable.Setting != SettingAllow {
		t.Fatalf("openable must let the group Allow open the workspace default-deny, got %s", openable.Setting)
	}
}

// TestResolveWithMode_GroupNeverSubtracts is the DoD invariant from the
// FIR-2351 build plan: belonging to a more restrictive sibling group must
// never remove access a more permissive group granted, in either mode.
func TestResolveWithMode_GroupNeverSubtracts(t *testing.T) {
	// Two groups collapse to their most permissive setting BEFORE they reach
	// the chain (CombineGroups), so a group Deny never wins over a sibling
	// group Allow — modelled here as the already-combined LayerGroup input.
	combined := CombineGroups(SettingDeny, SettingAllow)
	if combined != SettingAllow {
		t.Fatalf("CombineGroups must let the most permissive group win, got %s", combined)
	}

	in := Input{Settings: set(LayerGroup, combined)}
	for _, mode := range []Mode{ModeHardFloor, ModeOpenable} {
		got := ResolveWithMode(mode, in)
		if got.Setting != SettingAllow {
			t.Fatalf("mode %s: group membership must not subtract access, got %s", mode, got.Setting)
		}
	}
}

// TestResolveWithMode_AgentRuntimeSystemNeverOpenHardFloor proves that, under
// ModeHardFloor, no tighten-only layer (Agent, Runtime, System, on_behalf_of)
// can loosen a Base=Deny floor — the load-bearing invariant for credentials,
// the OS sandbox, repo checkout, and the approval cap.
func TestResolveWithMode_AgentRuntimeSystemNeverOpenHardFloor(t *testing.T) {
	for _, layer := range []Layer{LayerAgent, LayerRuntime, LayerSystem, LayerOnBehalfOf, LayerGroup, LayerUser} {
		in := Input{Base: SettingDeny, Settings: set(layer, SettingAllow)}
		got := ResolveWithMode(ModeHardFloor, in)
		if got.Setting != SettingDeny {
			t.Fatalf("layer %s: hard_floor must never be opened by any layer's Allow, got %s", layer, got.Setting)
		}
	}
}

// TestResolveWithMode_OnBehalfOfNeverOpensEitherMode proves the on_behalf_of
// invariant holds identically in both modes: an Allow at that layer can never
// widen access, only Deny/Ask ever bite.
func TestResolveWithMode_OnBehalfOfNeverOpensEitherMode(t *testing.T) {
	for _, mode := range []Mode{ModeHardFloor, ModeOpenable} {
		in := Input{Settings: set(LayerAgent, SettingDeny, LayerOnBehalfOf, SettingAllow)}
		got := ResolveWithMode(mode, in)
		if got.Setting != SettingDeny {
			t.Fatalf("mode %s: on_behalf_of Allow must never loosen an Agent Deny, got %s", mode, got.Setting)
		}
	}
}
