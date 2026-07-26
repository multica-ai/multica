// CEREBRO-PATCH(cerebro-feature-cli-test): FIR-3009 cerebro-only file — feature flag CLI tests.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testFeatureDef(key string, def bool) cerebroFeatureDef {
	return cerebroFeatureDef{Key: key, Label: "Label " + key, Description: "desc", Group: "agents", Default: def}
}

// TestCerebroFeatureCatalogEmbedded — the generated catalog must parse and be
// internally consistent (mirrors the vitest checks on the TS registry, but
// against the JSON the released binary actually ships).
func TestCerebroFeatureCatalogEmbedded(t *testing.T) {
	catalog, err := loadCerebroFeatureCatalog()
	if err != nil {
		t.Fatalf("loadCerebroFeatureCatalog: %v", err)
	}
	if len(catalog.Flags) == 0 || len(catalog.Groups) == 0 {
		t.Fatalf("catalog is empty: %d flags, %d groups — run 'pnpm generate:feature-catalog'", len(catalog.Flags), len(catalog.Groups))
	}
	groups := make(map[string]bool, len(catalog.Groups))
	for _, g := range catalog.Groups {
		if g.Key == "" || g.Label == "" {
			t.Errorf("group %+v missing key or label", g)
		}
		if groups[g.Key] {
			t.Errorf("duplicate group key %q", g.Key)
		}
		groups[g.Key] = true
	}
	seen := make(map[string]bool, len(catalog.Flags))
	for _, f := range catalog.Flags {
		if f.Key == "" || f.Label == "" {
			t.Errorf("flag %+v missing key or label", f)
		}
		if seen[f.Key] {
			t.Errorf("duplicate flag key %q", f.Key)
		}
		seen[f.Key] = true
		if !groups[f.Group] {
			t.Errorf("flag %q references unknown group %q", f.Key, f.Group)
		}
	}
}

// TestResolveCerebroFeaturePrecedence mirrors resolveFlag in
// packages/cerebro-feature-flags/store.ts: locked workspace > personal >
// unlocked workspace > default.
func TestResolveCerebroFeaturePrecedence(t *testing.T) {
	key := "cerebro_test_flag"
	cases := []struct {
		name       string
		def        cerebroFeatureDef
		ov         cerebroFeatureOverrides
		wantOn     bool
		wantSource string
		wantState  string
	}{
		{
			name:       "default off, no overrides",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{},
			wantOn:     false,
			wantSource: "default",
			wantState:  "off",
		},
		{
			name:       "default on, no overrides",
			def:        testFeatureDef(key, true),
			ov:         cerebroFeatureOverrides{},
			wantOn:     true,
			wantSource: "default",
			wantState:  "on",
		},
		{
			name:       "personal override beats default",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{Overrides: map[string]bool{key: true}},
			wantOn:     true,
			wantSource: "personal",
			wantState:  "on",
		},
		{
			name:       "personal override beats unlocked workspace value",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{Overrides: map[string]bool{key: false}, WorkspaceOverrides: map[string]bool{key: true}},
			wantOn:     false,
			wantSource: "personal",
			wantState:  "off",
		},
		{
			name:       "unlocked workspace value beats default",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{WorkspaceOverrides: map[string]bool{key: true}},
			wantOn:     true,
			wantSource: "workspace",
			wantState:  "on",
		},
		{
			name:       "locked workspace value beats personal override",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{Overrides: map[string]bool{key: true}, WorkspaceOverrides: map[string]bool{key: false}, Locked: map[string]bool{key: true}},
			wantOn:     false,
			wantSource: "workspace (locked)",
			wantState:  "forced off",
		},
		{
			name:       "locked on is forced on",
			def:        testFeatureDef(key, false),
			ov:         cerebroFeatureOverrides{WorkspaceOverrides: map[string]bool{key: true}, Locked: map[string]bool{key: true}},
			wantOn:     true,
			wantSource: "workspace (locked)",
			wantState:  "forced on",
		},
		{
			name:       "locked without workspace value falls through to default",
			def:        testFeatureDef(key, true),
			ov:         cerebroFeatureOverrides{Locked: map[string]bool{key: true}},
			wantOn:     true,
			wantSource: "default",
			wantState:  "on",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCerebroFeature(tc.def, tc.ov)
			if got.Enabled != tc.wantOn {
				t.Errorf("enabled = %t, want %t", got.Enabled, tc.wantOn)
			}
			if got.Source != tc.wantSource {
				t.Errorf("source = %q, want %q", got.Source, tc.wantSource)
			}
			if got.State != tc.wantState {
				t.Errorf("state = %q, want %q", got.State, tc.wantState)
			}
		})
	}
}

// TestCerebroFeatureStatesCoversEveryFlagOnce — grouping for display must not
// drop or duplicate flags.
func TestCerebroFeatureStatesCoversEveryFlagOnce(t *testing.T) {
	catalog, err := loadCerebroFeatureCatalog()
	if err != nil {
		t.Fatalf("loadCerebroFeatureCatalog: %v", err)
	}
	states := cerebroFeatureStates(catalog, cerebroFeatureOverrides{})
	if len(states) != len(catalog.Flags) {
		t.Fatalf("resolved %d states for %d flags", len(states), len(catalog.Flags))
	}
	seen := make(map[string]bool, len(states))
	for _, s := range states {
		if seen[s.Key] {
			t.Errorf("flag %q resolved twice", s.Key)
		}
		seen[s.Key] = true
	}
}

// TestFeatureListFetchesWorkspaceOverrides — `feature list` must hit the
// workspace feature-flags endpoint and succeed against the real response shape.
func TestFeatureListFetchesWorkspaceOverrides(t *testing.T) {
	const wsID = "ws-feature-1"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"overrides":           map[string]bool{},
			"workspace_overrides": map[string]bool{},
			"locked":              map[string]bool{},
		})
	}))
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"group", "state", "output"} {
			f := featureListCmd.Flags().Lookup(n)
			_ = featureListCmd.Flags().Set(n, f.DefValue)
			f.Changed = false
		}
	})
	if err := featureListCmd.Flags().Set("output", "json"); err != nil {
		t.Fatalf("set output: %v", err)
	}

	withCLIEnv(t, srv.URL, wsID, func() {
		if err := runFeatureList(featureListCmd, nil); err != nil {
			t.Fatalf("runFeatureList: %v", err)
		}
	})

	want := "/api/workspaces/" + wsID + "/feature-flags"
	if gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}
