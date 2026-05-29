package toolpolicy

import "testing"

// set is a tiny helper to build a chain from named layers in tests.
func set(pairs ...any) map[Layer]Setting {
	m := map[Layer]Setting{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(Layer)] = pairs[i+1].(Setting)
	}
	return m
}

// TestResolve_IssueCheck is the live check from the FIR-2230 plan:
// Allow on the agent + Deny on the user for the same tool → Effective Deny,
// attributed to the user ceiling ("Capped by user").
func TestResolve_IssueCheck(t *testing.T) {
	e := Resolve(Input{
		Settings: set(
			LayerRuntime, SettingAllow,
			LayerAgent, SettingAllow,
			LayerUser, SettingDeny,
		),
	})
	if e.Setting != SettingDeny {
		t.Fatalf("expected Deny, got %s", e.Setting)
	}
	if e.CappedBy != LayerUser {
		t.Fatalf("expected CappedBy=user, got %q", e.CappedBy)
	}
	if e.DecidedBy != LayerUser {
		t.Fatalf("expected DecidedBy=user, got %q", e.DecidedBy)
	}
	if e.Reason != "Capped by user" {
		t.Fatalf("expected reason 'Capped by user', got %q", e.Reason)
	}
	if e.Allowed() {
		t.Fatal("Allowed() must be false on Deny")
	}
}

// TestResolve_CeilingCannotBeLoosened proves the ceiling only tightens: a
// permissive user setting cannot raise an agent's Deny back to Allow.
func TestResolve_CeilingCannotBeLoosened(t *testing.T) {
	e := Resolve(Input{
		Settings: set(
			LayerRuntime, SettingAllow,
			LayerAgent, SettingDeny,
			LayerUser, SettingAllow, // tries to loosen — must be ignored
		),
	})
	if e.Setting != SettingDeny {
		t.Fatalf("agent Deny must stand against a permissive user, got %s", e.Setting)
	}
	if e.DecidedBy != LayerAgent {
		t.Fatalf("expected DecidedBy=agent, got %q", e.DecidedBy)
	}
}

// TestResolve_MockupRows walks every row from the FIR-2230 mockup and asserts
// the effective verdict and attribution match what the design promises.
func TestResolve_MockupRows(t *testing.T) {
	cases := []struct {
		name      string
		settings  map[Layer]Setting
		want      Setting
		decidedBy Layer
		cappedBy  Layer
	}{
		{"add_comment", set(LayerRuntime, SettingAllow, LayerAgent, SettingInherit), SettingAllow, LayerRuntime, ""},
		{"get_issue", set(LayerRuntime, SettingAllow, LayerAgent, SettingInherit), SettingAllow, LayerRuntime, ""},
		{"credential_list", set(LayerRuntime, SettingAllow, LayerAgent, SettingAsk), SettingAsk, LayerAgent, LayerAgent},
		{"shell_exec", set(LayerRuntime, SettingAllow, LayerAgent, SettingInherit), SettingAllow, LayerRuntime, ""},
		{"web_fetch", set(LayerRuntime, SettingAsk, LayerAgent, SettingInherit), SettingAsk, LayerRuntime, ""},
		{"deploy_restart", set(LayerRuntime, SettingAsk, LayerAgent, SettingDeny), SettingDeny, LayerAgent, LayerAgent},
		{"bigquery.query", set(LayerRuntime, SettingAllow, LayerAgent, SettingInherit), SettingAllow, LayerRuntime, ""},
		{"bigquery.list_datasets", set(LayerRuntime, SettingAllow, LayerAgent, SettingInherit), SettingAllow, LayerRuntime, ""},
		// slack.post_message: runtime Allow, agent Allow, but the user ceiling denies.
		{"slack.post_message", set(LayerRuntime, SettingAllow, LayerAgent, SettingAllow, LayerUser, SettingDeny), SettingDeny, LayerUser, LayerUser},
		{"infisical.get_secret", set(LayerRuntime, SettingAsk, LayerAgent, SettingInherit), SettingAsk, LayerRuntime, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Resolve(Input{Settings: tc.settings})
			if e.Setting != tc.want {
				t.Fatalf("setting: got %s want %s (reason %q)", e.Setting, tc.want, e.Reason)
			}
			if e.DecidedBy != tc.decidedBy {
				t.Fatalf("decidedBy: got %q want %q", e.DecidedBy, tc.decidedBy)
			}
			if e.CappedBy != tc.cappedBy {
				t.Fatalf("cappedBy: got %q want %q", e.CappedBy, tc.cappedBy)
			}
		})
	}
}

// TestResolve_GroupTightensUnderUser checks the middle layers: a group Deny
// caps even when runtime and agent allow, attributed to the group.
func TestResolve_GroupTightensUnderUser(t *testing.T) {
	e := Resolve(Input{
		Settings: set(
			LayerRuntime, SettingAllow,
			LayerAgent, SettingAllow,
			LayerGroup, SettingDeny,
			LayerUser, SettingInherit,
		),
	})
	if e.Setting != SettingDeny || e.CappedBy != LayerGroup {
		t.Fatalf("expected Deny capped by group, got %s capped by %q", e.Setting, e.CappedBy)
	}
	if e.Reason != "Capped by group" {
		t.Fatalf("expected 'Capped by group', got %q", e.Reason)
	}
}

// TestResolve_AllInheritFallsToBase confirms an all-Inherit chain resolves to
// the supplied base default and is not attributed to any layer.
func TestResolve_AllInheritFallsToBase(t *testing.T) {
	allow := Resolve(Input{Settings: set(LayerAgent, SettingInherit), Base: SettingAllow})
	if allow.Setting != SettingAllow || allow.DecidedBy != "" {
		t.Fatalf("expected Allow by default, got %s decidedBy %q", allow.Setting, allow.DecidedBy)
	}
	deny := Resolve(Input{Settings: nil, Base: SettingDeny})
	if deny.Setting != SettingDeny || deny.CappedBy != "" {
		t.Fatalf("expected base Deny with no cap, got %s capped %q", deny.Setting, deny.CappedBy)
	}
}

// TestResolve_EmptyBaseDefaultsToAllow documents the default starting point.
func TestResolve_EmptyBaseDefaultsToAllow(t *testing.T) {
	e := Resolve(Input{})
	if e.Setting != SettingAllow {
		t.Fatalf("empty input should default to Allow, got %s", e.Setting)
	}
}

func TestCombineGroups(t *testing.T) {
	cases := []struct {
		name string
		in   []Setting
		want Setting
	}{
		{"no groups", nil, SettingInherit},
		{"all inherit", []Setting{SettingInherit, SettingInherit}, SettingInherit},
		{"most permissive wins", []Setting{SettingDeny, SettingAllow}, SettingAllow},
		{"ask beats deny", []Setting{SettingDeny, SettingAsk}, SettingAsk},
		{"inherit ignored", []Setting{SettingInherit, SettingAsk}, SettingAsk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CombineGroups(tc.in...); got != tc.want {
				t.Fatalf("CombineGroups(%v) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestCombineGroups_CeilingStillApplies proves the permissive-group rule does
// not escape the user ceiling: the most-permissive group resolves to Allow, but
// a user Deny above still caps the whole chain.
func TestCombineGroups_CeilingStillApplies(t *testing.T) {
	group := CombineGroups(SettingDeny, SettingAllow) // Allow
	e := Resolve(Input{
		Settings: set(
			LayerRuntime, SettingAllow,
			LayerGroup, group,
			LayerUser, SettingDeny,
		),
	})
	if e.Setting != SettingDeny || e.CappedBy != LayerUser {
		t.Fatalf("user ceiling must cap the permissive group, got %s capped %q", e.Setting, e.CappedBy)
	}
}

func TestMoreRestrictive(t *testing.T) {
	cases := []struct {
		a, b, want Setting
	}{
		{SettingAllow, SettingDeny, SettingDeny},
		{SettingDeny, SettingAllow, SettingDeny},
		{SettingAsk, SettingAllow, SettingAsk},
		{SettingInherit, SettingAsk, SettingAsk},
		{SettingInherit, SettingInherit, SettingInherit},
		{SettingAllow, SettingAllow, SettingAllow},
	}
	for _, tc := range cases {
		if got := MoreRestrictive(tc.a, tc.b); got != tc.want {
			t.Fatalf("MoreRestrictive(%s,%s) = %s, want %s", tc.a, tc.b, got, tc.want)
		}
	}
}
