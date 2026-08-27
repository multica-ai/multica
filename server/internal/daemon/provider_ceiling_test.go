package daemon

import "testing"

// GAP-21 (issue #29): per-provider concurrency ceilings.

func TestParseProviderCeilings(t *testing.T) {
	cases := []struct {
		raw  string
		want map[string]int
	}{
		{"", nil},
		{"  ", nil},
		{"codex:2", map[string]int{"codex": 2}},
		{"codex:2, claude:3", map[string]int{"codex": 2, "claude": 3}},
		{"codex:2,bogus,claude:0,claude:x,:4", map[string]int{"codex": 2}}, // bad entries skipped
		{"bogus", nil},
	}
	for _, tc := range cases {
		got := parseProviderCeilings(tc.raw)
		if tc.want == nil {
			if got != nil {
				t.Errorf("parseProviderCeilings(%q) = %v, want nil", tc.raw, got)
			}
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseProviderCeilings(%q) = %v, want %v", tc.raw, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("parseProviderCeilings(%q)[%q] = %d, want %d", tc.raw, k, got[k], v)
			}
		}
	}
}

func TestClaimGroups_NoCeilingsSingleBatch(t *testing.T) {
	d := &Daemon{}
	got := d.claimGroups([]string{"rt-a", "rt-b"}, 5)
	if len(got) != 1 || len(got[0].runtimeIDs) != 2 || got[0].maxTasks != 5 {
		t.Fatalf("claimGroups = %+v, want one uncapped batch over both runtimes", got)
	}
}

func TestClaimGroups_CeilingsSplitBatches(t *testing.T) {
	d := &Daemon{
		cfg: Config{ProviderCeilings: map[string]int{"codex": 2}},
		runtimeIndex: map[string]Runtime{
			"rt-codex-1": {ID: "rt-codex-1", Provider: "codex"},
			"rt-codex-2": {ID: "rt-codex-2", Provider: "codex"},
			"rt-claude":  {ID: "rt-claude", Provider: "claude"},
			"rt-orphan":  {ID: "rt-orphan"}, // unknown provider: never capped
		},
	}
	d.runtimeIndex["rt-orphan"] = Runtime{ID: "rt-orphan"}

	groups := d.claimGroups([]string{"rt-codex-1", "rt-claude", "rt-orphan", "rt-codex-2"}, 10)
	var codexMax, openIDs int
	for _, g := range groups {
		for _, id := range g.runtimeIDs {
			switch d.providerFor(id) {
			case "codex":
				codexMax = g.maxTasks
			case "":
				openIDs++
			default:
				openIDs++ // claude pools into the uncapped batch
			}
		}
	}
	if codexMax != 2 {
		t.Errorf("codex batch maxTasks = %d, want ceiling 2", codexMax)
	}
	if openIDs != 2 { // rt-claude + rt-orphan
		t.Errorf("uncapped pool size = %d, want 2 (claude + unknown); groups %+v", openIDs, groups)
	}

	// At ceiling → provider's runtimes excluded entirely this cycle.
	d.providerRunningInc("codex")
	d.providerRunningInc("codex")
	groups = d.claimGroups([]string{"rt-codex-1", "rt-claude"}, 10)
	for _, g := range groups {
		for _, id := range g.runtimeIDs {
			if d.providerFor(id) == "codex" {
				t.Errorf("at-ceiling provider still claimed: groups %+v", groups)
			}
		}
	}
}

func TestProviderRunningCounter(t *testing.T) {
	d := &Daemon{}
	d.providerRunningInc("codex")
	d.providerRunningInc("codex")
	if got := d.providerRunningCount("codex"); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	d.providerRunningDec("codex")
	d.providerRunningDec("codex")
	d.providerRunningDec("codex") // over-decrement must not go negative
	if got := d.providerRunningCount("codex"); got != 0 {
		t.Fatalf("count after dec = %d, want 0", got)
	}
}
