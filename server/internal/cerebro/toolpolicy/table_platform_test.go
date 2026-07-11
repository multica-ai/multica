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

	"github.com/jackc/pgx/v5/pgtype"

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

// TestPlatformCapabilitiesEnabled_HonorsWorkspaceOverride pins the FIR-2672 fix:
// the gate must honor the workspace-level "Force on for the whole workspace"
// override (stored under the all-zero sentinel user_id), not just a per-user
// row. Before the fix the gate read only the requester's own row, so the admin
// screen's workspace toggle had no effect and platform actions never surfaced.
func TestPlatformCapabilitiesEnabled_HonorsWorkspaceOverride(t *testing.T) {
	s := newTPStore(t)
	clearFlags(t, s)
	ctx := context.Background()
	// A requester with no personal override row of their own.
	requester := uuidByte(0x9)

	// Default (no override anywhere) → off.
	if s.PlatformCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("no override: want disabled (registry default OFF)")
	}

	// Locked workspace override ON → enabled for everyone, incl. a user with no
	// personal row. This is the exact scenario Jesper configured.
	setWorkspaceFlag(t, s, FlagPlatformCapabilities, true, true)
	if !s.PlatformCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("locked workspace ON: want enabled for a user with no personal row")
	}

	// Locked workspace OFF wins outright over a personal ON.
	setWorkspaceFlag(t, s, FlagPlatformCapabilities, false, true)
	setPersonalFlag(t, s, requester, FlagPlatformCapabilities, true)
	if s.PlatformCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("locked workspace OFF: must win over personal ON")
	}

	// Unlocked workspace OFF + personal ON → personal wins.
	setWorkspaceFlag(t, s, FlagPlatformCapabilities, false, false)
	if !s.PlatformCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("unlocked workspace OFF + personal ON: personal must win")
	}
	clearFlags(t, s)
}

func clearFlags(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear feature flags: %v", err)
	}
}

func setWorkspaceFlag(t *testing.T, s *Store, key string, enabled, locked bool) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled, locked)
		VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3, $4)
		ON CONFLICT (workspace_id, user_id, flag_key)
		DO UPDATE SET enabled = EXCLUDED.enabled, locked = EXCLUDED.locked`,
		tpTestWorkspaceID, key, enabled, locked); err != nil {
		t.Fatalf("set workspace flag: %v", err)
	}
}

func setPersonalFlag(t *testing.T, s *Store, userID pgtype.UUID, key string, enabled bool) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, user_id, flag_key)
		DO UPDATE SET enabled = EXCLUDED.enabled`,
		tpTestWorkspaceID, userID, key, enabled); err != nil {
		t.Fatalf("set personal flag: %v", err)
	}
}

// TestTable_AgentStartSurfaceGate proves the FIR-3091 slice 4 surface: with the
// agent-start gate on (and the full-catalog flag off) the table shows EXACTLY the
// surfaced "start someone else's agent" family and none of the rest of the
// platform catalog; with neither gate set the table is unchanged (no platform
// rows). This is what lets an admin see/set those rules without opening the whole
// catalog.
func TestTable_AgentStartSurfaceGate(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	// Neither gate: no platform rows at all (backwards-compatible default).
	none, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("Table (none): %v", err)
	}
	for _, k := range platformcatalog.SurfacedKeys() {
		if _, ok := findRow(none, k); ok {
			t.Errorf("surfaced row %q present with no gate", k)
		}
	}
	if _, ok := findRow(none, "create_issue"); ok {
		t.Error("non-surfaced platform row create_issue present with no gate")
	}

	// Agent-start gate only: exactly the surfaced family, nothing else.
	on, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, IncludeAgentStart: true})
	if err != nil {
		t.Fatalf("Table (agent-start): %v", err)
	}
	for _, k := range platformcatalog.SurfacedKeys() {
		row, ok := findRow(on, k)
		if !ok {
			t.Errorf("surfaced key %q missing under agent-start gate", k)
			continue
		}
		if row.Source != platformcatalog.Source {
			t.Errorf("%q source = %q, want %q", k, row.Source, platformcatalog.Source)
		}
	}
	// A non-surfaced platform action must NOT leak through the light surface.
	if _, ok := findRow(on, "create_issue"); ok {
		t.Error("non-surfaced platform row create_issue leaked through agent-start gate")
	}
	if _, ok := findRow(on, "add_comment"); ok {
		t.Error("non-surfaced platform row add_comment leaked through agent-start gate")
	}
}

// TestTable_AgentStartRowSettable proves a surfaced row carries its stored
// per-layer settings and resolves the chain like any tool, so the surface is
// genuinely settable — not read-only.
func TestTable_AgentStartRowSettable(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_other_agent",
		Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, Setting: SettingDeny, UpdatedBy: tpTestUserID,
	}); err != nil {
		t.Fatalf("set workspace Deny: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, IncludeAgentStart: true})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	row, ok := findRow(rows, "trigger_other_agent")
	if !ok {
		t.Fatal("trigger_other_agent row missing under agent-start gate")
	}
	if got := row.Layers[LayerWorkspace]; got != SettingDeny {
		t.Errorf("workspace layer = %q, want deny", got)
	}
	if row.Effective.Setting != SettingDeny {
		t.Errorf("effective = %q, want deny", row.Effective.Setting)
	}
}

// TestAgentStartCapabilitiesEnabled_HonorsWorkspaceOverride mirrors the
// platform-flag precedence test for the agent-start gate: locked workspace wins,
// else personal, else unlocked workspace, else registry default OFF.
func TestAgentStartCapabilitiesEnabled_HonorsWorkspaceOverride(t *testing.T) {
	s := newTPStore(t)
	clearFlags(t, s)
	ctx := context.Background()
	requester := uuidByte(0x9)

	if s.AgentStartCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("no override: want disabled (registry default OFF)")
	}
	setWorkspaceFlag(t, s, FlagAgentStartCapabilities, true, true)
	if !s.AgentStartCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("locked workspace ON: want enabled for a user with no personal row")
	}
	setWorkspaceFlag(t, s, FlagAgentStartCapabilities, false, true)
	setPersonalFlag(t, s, requester, FlagAgentStartCapabilities, true)
	if s.AgentStartCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("locked workspace OFF: must win over personal ON")
	}
	setWorkspaceFlag(t, s, FlagAgentStartCapabilities, false, false)
	if !s.AgentStartCapabilitiesEnabled(ctx, tpTestWorkspaceID, requester) {
		t.Fatal("unlocked workspace OFF + personal ON: personal must win")
	}
	clearFlags(t, s)
}
