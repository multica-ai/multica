package toolpolicy

// FIR-2269 integration test: the firtal_registry data-source picker must surface
// on every actor layer, not only the agent's Tools tab. AppendRegistryDataSourceRows
// is the non-agent-scope fold-in; it crosses the catalog with the authored chain
// rows so a rule set at the runtime (or workspace/group/user) layer shows up — and
// reads back — exactly where the gate enforces it.

import (
	"context"
	"testing"
)

func TestAppendRegistryDataSourceRows_RuntimeLayerAuthored(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	runtime := uuidByte(7)
	const denied = "ds-finance-pii"
	const open = "ds-orders"

	// A Deny authored at the runtime layer for one data source — what an admin sets
	// on the runtime's Permissions view.
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         RegistryToolKey,
		Layer:           LayerRuntime,
		SubjectID:       runtime,
		ResourcePattern: denied,
		Setting:         SettingDeny,
	}); err != nil {
		t.Fatalf("author runtime-layer deny: %v", err)
	}

	catalog := []RegistryDataSource{{ID: denied, Name: "Finance (PII)"}, {ID: open, Name: "Orders"}}
	rows, err := s.AppendRegistryDataSourceRows(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		RuntimeID:   runtime,
		Base:        SettingAllow,
	}, catalog, nil)
	if err != nil {
		t.Fatalf("append rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per data source)", len(rows))
	}

	denyRow, ok := rowByPattern(rows, denied)
	if !ok {
		t.Fatalf("denied source row missing")
	}
	if denyRow.Source != registryDataSourceSource || denyRow.Title != "Finance (PII)" {
		t.Fatalf("denied row source/title = %q/%q", denyRow.Source, denyRow.Title)
	}
	// The authored runtime-layer Deny rides on the row and drives the verdict.
	if denyRow.Layers[LayerRuntime] != SettingDeny {
		t.Fatalf("denied row runtime layer = %q, want deny", denyRow.Layers[LayerRuntime])
	}
	if denyRow.Effective.Setting != SettingDeny {
		t.Fatalf("denied row effective = %q, want deny", denyRow.Effective.Setting)
	}

	// A source with no rule is authorable but unset (Inherit), effective Allow via Base.
	openRow, ok := rowByPattern(rows, open)
	if !ok {
		t.Fatalf("open source row missing")
	}
	if _, set := openRow.Layers[LayerRuntime]; set {
		t.Fatalf("open row should carry no runtime setting, got %q", openRow.Layers[LayerRuntime])
	}
	if openRow.Effective.Setting != SettingAllow {
		t.Fatalf("open row effective = %q, want allow", openRow.Effective.Setting)
	}
}
