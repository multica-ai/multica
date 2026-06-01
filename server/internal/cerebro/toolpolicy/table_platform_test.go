package toolpolicy

// Integration tests for the platform-capability rows (table_platform.go,
// FIR-2594). They prove the three things the admin screen depends on: the
// code-owned platform catalog appears in the table (and only when
// IncludePlatform is set), a platform action is settable Allow/Ask/Deny like any
// reported tool, and the externally-managed marker is carried through. They share
// the store_test.go fixture (TestMain) and skip when no test database is reachable.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

// TestTable_PlatformRowsGatedByIncludeFlag proves the catalog is appended only
// when IncludePlatform is true, so an unflagged workspace lists exactly what it
// did before FIR-2594.
func TestTable_PlatformRowsGatedByIncludeFlag(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	// Without the flag: no platform rows.
	off, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("Table (off): %v", err)
	}
	if _, ok := findRow(off, "create_issue"); ok {
		t.Fatal("platform row create_issue present without IncludePlatform")
	}

	// With the flag: every catalog capability shows up exactly once.
	on, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, IncludePlatform: true})
	if err != nil {
		t.Fatalf("Table (on): %v", err)
	}
	for _, c := range platformcatalog.All() {
		row, ok := findRow(on, c.Key)
		if !ok {
			t.Errorf("platform capability %q missing from table", c.Key)
			continue
		}
		if row.Source != platformcatalog.Source {
			t.Errorf("%q source = %q, want %q", c.Key, row.Source, platformcatalog.Source)
		}
		if row.ManagedExternally != c.ManagedExternally {
			t.Errorf("%q managed_externally = %v, want %v", c.Key, row.ManagedExternally, c.ManagedExternally)
		}
		// With no stored settings the row resolves to the Base default (Allow).
		if row.Effective.Setting != SettingAllow {
			t.Errorf("%q effective = %q, want allow (unset → base)", c.Key, row.Effective.Setting)
		}
	}
}

// TestTable_PlatformRowSettable proves a platform action carries its stored
// per-layer settings and resolves the chain exactly like a reported tool: an
// agent Ask capped by a user Deny resolves to Deny.
func TestTable_PlatformRowSettable(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent, user := uuidByte(2), tpTestUserID
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_autopilot",
		Layer: LayerAgent, SubjectID: agent, Setting: SettingAsk, UpdatedBy: user,
	}); err != nil {
		t.Fatalf("set agent Ask: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_autopilot",
		Layer: LayerUser, SubjectID: user, Setting: SettingDeny, UpdatedBy: user,
	}); err != nil {
		t.Fatalf("set user Deny: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		AgentID:         agent,
		UserID:          user,
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	row, ok := findRow(rows, "trigger_autopilot")
	if !ok {
		t.Fatal("trigger_autopilot row missing")
	}
	if got := row.Layers[LayerAgent]; got != SettingAsk {
		t.Errorf("agent layer = %q, want ask", got)
	}
	if got := row.Layers[LayerUser]; got != SettingDeny {
		t.Errorf("user layer = %q, want deny", got)
	}
	if row.Effective.Setting != SettingDeny {
		t.Errorf("effective = %q, want deny (user caps agent)", row.Effective.Setting)
	}
	if row.Effective.CappedBy != LayerUser {
		t.Errorf("capped_by = %q, want user", row.Effective.CappedBy)
	}
}

// TestTable_PlatformRowsSurviveRuntimeFilter proves platform rows appear even on
// a runtime-scoped view (where the register query keeps only that runtime's
// reported tools): a platform action is workspace-global, not bound to a machine.
func TestTable_PlatformRowsSurviveRuntimeFilter(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	runtime := uuidByte(1)
	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		RuntimeID:       runtime,
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	if _, ok := findRow(rows, "add_comment"); !ok {
		t.Error("platform row add_comment hidden by runtime filter")
	}
}
