package toolpolicy

// Integration tests for the platform-capability rows (table_platform.go,
// FIR-2594). They prove the three things the admin screen depends on: the
// code-owned platform catalog appears in the table (and only when
// IncludePlatform is set), a platform action is settable Allow/Ask/Deny like any
// reported tool, and the externally-managed marker is carried through. They share
// the store_test.go fixture (TestMain) and skip when no test database is reachable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

func TestSettingsTableAlwaysIncludesPlatformPermissions(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	// A stale row for the retired rollout flag must not be able to hide a live
	// permission from Settings.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled, locked)
		VALUES ($1, '00000000-0000-0000-0000-000000000000',
			'cerebro_platform_capabilities', false, true)
		ON CONFLICT (workspace_id, user_id, flag_key)
		DO UPDATE SET enabled = false, locked = true`,
		tpTestWorkspaceID,
	); err != nil {
		t.Fatalf("seed retired flag row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `
			DELETE FROM cerebro_feature_flags
			WHERE workspace_id = $1 AND flag_key = 'cerebro_platform_capabilities'`,
			tpTestWorkspaceID,
		)
	})

	rec := httptest.NewRecorder()
	NewHandler(s).Table(rec, usageRequest("admin", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var response struct {
		Tools []toolPolicyRow `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode Settings response: %v", err)
	}
	for _, row := range response.Tools {
		if row.ToolKey == "create_issue" {
			return
		}
	}
	t.Fatal("Settings response hid create_issue behind the retired rollout flag")
}

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
		// Ordinary policy capabilities inherit the Base. Capabilities with a
		// code-owned actor contract fail closed when this workspace-only query
		// supplies no authenticated actor context.
		want := SettingAllow
		if _, special := platformaccess.ForKey(c.Key); special {
			want = SettingDeny
		}
		if row.Effective.Setting != want {
			t.Errorf("%q effective = %q, want %q", c.Key, row.Effective.Setting, want)
		}
	}
}

func TestTable_WorkflowHookRowsMatchAgentEnforcement(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()
	agent, user := uuidByte(2), tpTestUserID

	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		AgentID:         agent,
		UserID:          user,
		Base:            SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatalf("Table without hook grant: %v", err)
	}
	wantWithoutGrant := map[string]Setting{
		"hooks:read":           SettingAllow,
		"hooks:write":          SettingDeny,
		"hooks:enforce":        SettingDeny,
		"hooks:manage_managed": SettingDeny,
	}
	for key, want := range wantWithoutGrant {
		row, ok := findRow(rows, key)
		if !ok {
			t.Fatalf("%s row missing", key)
		}
		if row.Effective.Setting != want {
			t.Errorf("%s effective = %q, want %q", key, row.Effective.Setting, want)
		}
	}

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     "hooks:write",
		Layer:       LayerAgent,
		SubjectID:   agent,
		Setting:     SettingAllow,
		UpdatedBy:   user,
	}); err != nil {
		t.Fatalf("grant hooks:write: %v", err)
	}
	rows, err = s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		AgentID:         agent,
		UserID:          user,
		Base:            SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatalf("Table with hook grant: %v", err)
	}
	write, ok := findRow(rows, "hooks:write")
	if !ok {
		t.Fatal("hooks:write row missing after grant")
	}
	if write.Effective.Setting != SettingAllow || write.Effective.DecidedBy != LayerAgent {
		t.Fatalf("hooks:write effective = %+v, want agent Allow", write.Effective)
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

// TestTable_PlatformRowsUseTheCompleteActorContext prevents the platform
// authoring surface from maintaining a narrower copy of the permission query
// than the live resolver. Every synthetic resource family must surface the same
// conditions and mandate layers (system / on_behalf_of) that call time reads.
func TestTable_PlatformRowsUseTheCompleteActorContext(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent, system, member := uuidByte(31), uuidByte(32), uuidByte(33)
	condition := &Condition{Actions: []string{"run"}}
	for _, p := range []SetParams{
		{
			WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_autopilot",
			Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow,
			Conditions: condition, UpdatedBy: tpTestUserID,
		},
		{
			WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_autopilot",
			Layer: LayerSystem, SubjectID: system, Setting: SettingAsk,
			UpdatedBy: tpTestUserID,
		},
		{
			WorkspaceID: tpTestWorkspaceID, ToolKey: "trigger_autopilot",
			Layer: LayerOnBehalfOf, SubjectID: member, Setting: SettingDeny,
			UpdatedBy: tpTestUserID,
		},
	} {
		if _, err := s.Set(ctx, p); err != nil {
			t.Fatalf("set %s row: %v", p.Layer, err)
		}
	}

	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID:     tpTestWorkspaceID,
		AgentID:         agent,
		SystemID:        system,
		OnBehalfOfID:    member,
		IsSystem:        true,
		RequestContext:  RequestContext{Action: "run"},
		IncludePlatform: true,
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	row, ok := findRow(rows, "trigger_autopilot")
	if !ok {
		t.Fatal("trigger_autopilot row missing")
	}
	if got := row.Layers[LayerSystem]; got != SettingAsk {
		t.Errorf("system layer = %q, want ask", got)
	}
	if got := row.Layers[LayerOnBehalfOf]; got != SettingDeny {
		t.Errorf("on_behalf_of layer = %q, want deny", got)
	}
	if got := row.Conditions[LayerAgent]; got == nil || len(got.Actions) != 1 || got.Actions[0] != "run" {
		t.Errorf("agent condition = %+v, want action [run]", got)
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
