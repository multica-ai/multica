package toolpolicy

// Pure unit tests for the FIR-1609 Phase 5 registry data-source projection. No DB
// or registry API — the projection is a pure function over (data sources, grant).

import "testing"

func rowByPattern(rows []TableRow, pattern string) (TableRow, bool) {
	for _, r := range rows {
		if r.ResourcePattern == pattern {
			return r, true
		}
	}
	return TableRow{}, false
}

func TestProjectRegistryDataSourceRows_AllowlistVsDeny(t *testing.T) {
	dataSources := []RegistryDataSource{
		{ID: "ds-1", Name: "Orders"},
		{ID: "ds-2", Name: "Customers"},
		{ID: "ds-3", Name: "Finance (PII)"},
	}
	grant := RegistryGrant{AllowedDataSources: []string{"ds-1", "ds-3"}}

	rows := ProjectRegistryDataSourceRows(dataSources, grant, LayerAgent, SettingAllow)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for _, tc := range []struct {
		id   string
		want Setting
	}{
		{"ds-1", SettingAllow},
		{"ds-2", SettingDeny},
		{"ds-3", SettingAllow},
	} {
		r, ok := rowByPattern(rows, tc.id)
		if !ok {
			t.Fatalf("row %s missing", tc.id)
		}
		if r.ToolKey != RegistryToolKey {
			t.Fatalf("row %s tool_key = %q, want %q", tc.id, r.ToolKey, RegistryToolKey)
		}
		if r.Layers[LayerAgent] != tc.want {
			t.Fatalf("row %s agent layer = %q, want %q", tc.id, r.Layers[LayerAgent], tc.want)
		}
		if r.Effective.Setting != tc.want {
			t.Fatalf("row %s effective = %q, want %q", tc.id, r.Effective.Setting, tc.want)
		}
		if r.Source != registryDataSourceSource || r.Category != registryDataSourceCategory {
			t.Fatalf("row %s source/category = %q/%q", tc.id, r.Source, r.Category)
		}
	}
	// The display name rides along so the UI never shows a raw id alone.
	if r, _ := rowByPattern(rows, "ds-3"); r.Title != "Finance (PII)" {
		t.Fatalf("ds-3 title = %q, want %q", r.Title, "Finance (PII)")
	}
}

func TestProjectRegistryDataSourceRows_AllowedAll(t *testing.T) {
	dataSources := []RegistryDataSource{{ID: "ds-1", Name: "A"}, {ID: "ds-2", Name: "B"}}
	rows := ProjectRegistryDataSourceRows(dataSources, RegistryGrant{AllowedAll: true}, LayerAgent, SettingAllow)
	for _, r := range rows {
		if r.Effective.Setting != SettingAllow {
			t.Fatalf("allowed_all: %s = %q, want allow", r.ResourcePattern, r.Effective.Setting)
		}
	}
}

func TestAppendRegistryProjection_AppendsAndDedupes(t *testing.T) {
	// The table already carries the firtal_registry tool row plus an admin-authored
	// explicit row for ds-2 (resource_pattern=ds-2). The fold-in must append the
	// per-source rows but leave the authored ds-2 untouched (admin override wins).
	existing := []TableRow{
		{ToolKey: RegistryToolKey, ResourcePattern: ""}, // the whole-tool row
		{ToolKey: RegistryToolKey, ResourcePattern: "ds-2", Layers: map[Layer]Setting{LayerAgent: SettingAllow}},
		{ToolKey: "web_fetch", ResourcePattern: ""}, // unrelated tool, must survive
	}
	proj := RegistryProjection{
		DataSources: []RegistryDataSource{
			{ID: "ds-1", Name: "Orders"},
			{ID: "ds-2", Name: "Customers"}, // already authored → skipped
			{ID: "ds-3", Name: "Finance"},
		},
		Grant: RegistryGrant{AllowedDataSources: []string{"ds-1"}},
	}

	out := AppendRegistryProjection(existing, proj, LayerAgent, SettingAllow)

	// ds-1 and ds-3 are appended; ds-2 is not duplicated.
	dsRows := 0
	for _, r := range out {
		if r.ToolKey == RegistryToolKey && r.ResourcePattern != "" {
			dsRows++
		}
	}
	if dsRows != 3 { // ds-1 (new), ds-2 (authored, kept once), ds-3 (new)
		t.Fatalf("got %d firtal_registry resource rows, want 3 (no ds-2 duplicate)", dsRows)
	}

	// The authored ds-2 row is the original, not a projected Deny.
	r2, ok := rowByPattern(out, "ds-2")
	if !ok || r2.Source == registryDataSourceSource {
		t.Fatalf("ds-2 should remain the admin-authored row, got source=%q ok=%v", r2.Source, ok)
	}
	if r2.Layers[LayerAgent] != SettingAllow {
		t.Fatalf("authored ds-2 should keep Allow, got %q", r2.Layers[LayerAgent])
	}

	// ds-1 was granted → projected Allow; ds-3 not granted → projected Deny.
	if r1, _ := rowByPattern(out, "ds-1"); r1.Effective.Setting != SettingAllow || r1.Source != registryDataSourceSource {
		t.Fatalf("ds-1 should be a projected Allow row, got %q/%q", r1.Effective.Setting, r1.Source)
	}
	if r3, _ := rowByPattern(out, "ds-3"); r3.Effective.Setting != SettingDeny {
		t.Fatalf("ds-3 should be projected Deny, got %q", r3.Effective.Setting)
	}
}

func TestAppendRegistryProjection_AddsBaseRowWhenRuntimeDidNotReportRegistryTool(t *testing.T) {
	existing := []TableRow{
		{ToolKey: "web_fetch", ResourcePattern: ""}, // unrelated tool, must survive
	}
	proj := RegistryProjection{
		DataSources: []RegistryDataSource{
			{ID: "ds-1", Name: "Orders"},
			{ID: "ds-2", Name: "Finance"},
		},
		Grant: RegistryGrant{AllowedDataSources: []string{"ds-1"}},
	}

	out := AppendRegistryProjection(existing, proj, LayerAgent, SettingAllow)

	baseRows := 0
	for _, r := range out {
		if r.ToolKey == RegistryToolKey && r.ResourcePattern == "" {
			baseRows++
			if r.Title != registryToolTitle {
				t.Fatalf("base row title = %q, want %q", r.Title, registryToolTitle)
			}
			if r.Category != registryToolCategory || r.Source != registryToolSource {
				t.Fatalf("base row category/source = %q/%q", r.Category, r.Source)
			}
		}
	}
	if baseRows != 1 {
		t.Fatalf("got %d registry base rows, want 1", baseRows)
	}

	if r1, ok := rowByPattern(out, "ds-1"); !ok || r1.Effective.Setting != SettingAllow {
		t.Fatalf("ds-1 should be projected Allow, got ok=%v row=%+v", ok, r1)
	}
	if r2, ok := rowByPattern(out, "ds-2"); !ok || r2.Effective.Setting != SettingDeny {
		t.Fatalf("ds-2 should be projected Deny, got ok=%v row=%+v", ok, r2)
	}
}

func TestAppendRegistryProjection_NoDataSourcesIsNoop(t *testing.T) {
	existing := []TableRow{{ToolKey: RegistryToolKey, ResourcePattern: ""}}
	out := AppendRegistryProjection(existing, RegistryProjection{}, LayerAgent, SettingAllow)
	if len(out) != 1 {
		t.Fatalf("empty projection should not add rows, got %d", len(out))
	}
}

func TestProjectRegistryDataSourceRows_Hygiene(t *testing.T) {
	// Case-insensitive + whitespace-tolerant matching mirrors gateway enforcement;
	// an empty id is skipped (it cannot key a resource_pattern).
	dataSources := []RegistryDataSource{
		{ID: "AbCd", Name: "mixed case"},
		{ID: "  ds-x  ", Name: "padded"},
		{ID: "", Name: "no id"},
	}
	grant := RegistryGrant{AllowedDataSources: []string{"abcd", "ds-x"}}
	rows := ProjectRegistryDataSourceRows(dataSources, grant, LayerAgent, SettingAllow)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (empty id dropped)", len(rows))
	}
	for _, r := range rows {
		if r.Effective.Setting != SettingAllow {
			t.Fatalf("%q should match allowlist, got %q", r.ResourcePattern, r.Effective.Setting)
		}
	}
}
