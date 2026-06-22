package credentialpolicy

import "testing"

func TestResolve_DenyByDefault(t *testing.T) {
	// No layer has an opinion -> least-privilege deny.
	got := Resolve(Input{})
	if got.Allowed() {
		t.Fatalf("empty chain should deny, got %+v", got)
	}
	if got.Setting != SettingDeny {
		t.Fatalf("want deny, got %s", got.Setting)
	}
	if got.DecidedBy != "" {
		t.Fatalf("want no decider on default, got %s", got.DecidedBy)
	}
	if got.Reason != "Denied by default" {
		t.Fatalf("unexpected reason %q", got.Reason)
	}
}

func TestResolve_GrantAtAgentOverridesDefault(t *testing.T) {
	// The core grant UX: workspace default off, tick this one agent on.
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerAgent: SettingAllow,
	}})
	if !got.Allowed() {
		t.Fatalf("agent allow should grant, got %+v", got)
	}
	if got.DecidedBy != LayerAgent {
		t.Fatalf("want decided by agent, got %s", got.DecidedBy)
	}
	if got.Reason != "Allowed by agent" {
		t.Fatalf("unexpected reason %q", got.Reason)
	}
}

func TestResolve_CeilingOverridesLowerGrant(t *testing.T) {
	// A layer closer to the ceiling wins. Agent grants, user revokes -> denied.
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerWorkspace: SettingAllow,
		LayerAgent:     SettingAllow,
		LayerUser:      SettingDeny,
	}})
	if got.Allowed() {
		t.Fatalf("user deny should cap, got %+v", got)
	}
	if got.DecidedBy != LayerUser {
		t.Fatalf("want decided by user, got %s", got.DecidedBy)
	}
}

func TestResolve_HigherAllowOverridesLowerDeny(t *testing.T) {
	// Override-by-specificity: a higher layer can re-grant what a lower denied.
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerGroup: SettingDeny,
		LayerUser:  SettingAllow,
	}})
	if !got.Allowed() {
		t.Fatalf("user allow above group deny should grant, got %+v", got)
	}
	if got.DecidedBy != LayerUser {
		t.Fatalf("want decided by user, got %s", got.DecidedBy)
	}
}

func TestResolve_SystemCeiling(t *testing.T) {
	// System is a peer of user at the ceiling and decides for human-less runs.
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerAgent:  SettingAllow,
		LayerSystem: SettingDeny,
	}})
	if got.Allowed() {
		t.Fatalf("system deny should cap, got %+v", got)
	}
	if got.DecidedBy != LayerSystem {
		t.Fatalf("want decided by system, got %s", got.DecidedBy)
	}
}

func TestResolve_InheritPassesThrough(t *testing.T) {
	// An explicit Inherit must not count as an opinion.
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerRuntime: SettingAllow,
		LayerAgent:   SettingInherit,
	}})
	if !got.Allowed() {
		t.Fatalf("inherit should defer to runtime allow, got %+v", got)
	}
	if got.DecidedBy != LayerRuntime {
		t.Fatalf("want decided by runtime, got %s", got.DecidedBy)
	}
}

func TestCombineGroups(t *testing.T) {
	cases := []struct {
		name string
		in   []Setting
		want Setting
	}{
		{"empty is inherit", nil, SettingInherit},
		{"all inherit", []Setting{SettingInherit, SettingInherit}, SettingInherit},
		{"any allow wins over deny", []Setting{SettingDeny, SettingAllow}, SettingAllow},
		{"allow wins regardless of order", []Setting{SettingAllow, SettingDeny}, SettingAllow},
		{"deny when no group grants", []Setting{SettingDeny, SettingInherit}, SettingDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CombineGroups(tc.in...); got != tc.want {
				t.Fatalf("CombineGroups(%v) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestCombineGroups_FeedsResolve(t *testing.T) {
	// A grant in one group, capped by the user ceiling, resolves to deny.
	groupSetting := CombineGroups(SettingDeny, SettingAllow)
	if groupSetting != SettingAllow {
		t.Fatalf("group combine should allow, got %s", groupSetting)
	}
	got := Resolve(Input{Settings: map[Layer]Setting{
		LayerGroup: groupSetting,
		LayerUser:  SettingDeny,
	}})
	if got.Allowed() {
		t.Fatalf("user ceiling should cap the group grant, got %+v", got)
	}
}
