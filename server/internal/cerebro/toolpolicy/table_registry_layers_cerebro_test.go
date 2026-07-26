package toolpolicy

// FIR-2269 integration test: the firtal_registry data-source picker must surface
// on every actor layer, not only the agent's Tools tab. AppendRegistryDataSourceRows
// is the non-agent-scope fold-in; it crosses the catalog with the authored chain
// rows so a rule set at the runtime (or workspace/group/user) layer shows up — and
// reads back — exactly where the gate enforces it.

import (
	"context"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func rowByPattern(rows []TableRow, pattern string) (TableRow, bool) {
	for _, row := range rows {
		if row.ResourcePattern == pattern {
			return row, true
		}
	}
	return TableRow{}, false
}

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

func TestAppendRegistryDataSourceRows_AgentLayerUsesCanonicalPolicy(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	agent := uuidByte(8)
	const authoredAllow = "ds-orders"
	const legacyOnly = "ds-finance"

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         RegistryToolKey,
		Layer:           LayerAgent,
		SubjectID:       agent,
		ResourcePattern: authoredAllow,
		Setting:         SettingAllow,
	}); err != nil {
		t.Fatalf("author agent-layer allow: %v", err)
	}

	catalog := []RegistryDataSource{
		{ID: authoredAllow, Name: "Orders"},
		{ID: legacyOnly, Name: "Finance"},
	}

	rows, err := s.AppendRegistryDataSourceRows(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		AgentID:     agent,
		Base:        SettingAllow,
	}, catalog, nil)
	if err != nil {
		t.Fatalf("append registry rows: %v", err)
	}

	if got, ok := rowByPattern(rows, authoredAllow); !ok {
		t.Fatalf("authored allow source row missing")
	} else if got.Layers[LayerAgent] != SettingAllow || got.Effective.Setting != SettingAllow {
		t.Fatalf("authored allow source = layer %q effective %q, want allow/allow", got.Layers[LayerAgent], got.Effective.Setting)
	}

	if got, ok := rowByPattern(rows, legacyOnly); !ok {
		t.Fatalf("unconfigured source row missing")
	} else if _, authored := got.Layers[LayerAgent]; authored || got.Effective.Setting != SettingAllow {
		t.Fatalf("unconfigured source = layers %v effective %q, want inherited allow", got.Layers, got.Effective.Setting)
	}
}

func TestRegistryDataSourceTableMatchesHardFloorGateWithMemberOverride(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	agent := uuidByte(19)
	const dataSource = "firtal-registry-finance"

	if err := s.q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: tpTestWorkspaceID,
		FlagKey:     FlagMemberOverride,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("enable member override: %v", err)
	}
	t.Cleanup(func() {
		_ = s.q.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: tpTestWorkspaceID,
			FlagKey:     FlagMemberOverride,
		})
	})

	for _, rule := range []SetParams{
		{
			WorkspaceID:     tpTestWorkspaceID,
			ToolKey:         RegistryToolKey,
			Layer:           LayerWorkspace,
			SubjectID:       tpTestWorkspaceID,
			ResourcePattern: dataSource,
			Setting:         SettingDeny,
		},
		{
			WorkspaceID:     tpTestWorkspaceID,
			ToolKey:         RegistryToolKey,
			Layer:           LayerAgent,
			SubjectID:       agent,
			ResourcePattern: dataSource,
			Setting:         SettingAllow,
		},
	} {
		if _, err := s.Set(ctx, rule); err != nil {
			t.Fatalf("author %s %s: %v", rule.Layer, rule.Setting, err)
		}
	}

	rows, err := s.AppendRegistryDataSourceRows(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		AgentID:     agent,
		Base:        SettingAllow,
	}, []RegistryDataSource{{ID: dataSource, Name: "Finance"}}, nil)
	if err != nil {
		t.Fatalf("append registry rows: %v", err)
	}
	tableRow, ok := rowByPattern(rows, dataSource)
	if !ok {
		t.Fatalf("registry data-source row missing")
	}

	gate, err := s.Resolve(ctx, Query{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         RegistryToolKey,
		ResourcePattern: dataSource,
		AgentID:         agent,
		Base:            SettingAllow,
	})
	if err != nil {
		t.Fatalf("resolve registry gate: %v", err)
	}
	if tableRow.Effective.Setting != gate.Setting {
		t.Fatalf("surface drift: Settings table=%q call-time gate=%q", tableRow.Effective.Setting, gate.Setting)
	}
	if gate.Setting != SettingDeny {
		t.Fatalf("registry safety floor=%q, want deny", gate.Setting)
	}
}

func TestRegistryDataSourceParityCoversRolesConditionsSystemAndOnBehalfOf(t *testing.T) {
	const dataSource = "firtal-registry-parity-source"

	assertParity := func(t *testing.T, s *Store, tableQuery TableQuery, gateQuery Query, want Setting) {
		t.Helper()
		rows, err := s.AppendRegistryDataSourceRows(context.Background(), tableQuery,
			[]RegistryDataSource{{ID: dataSource, Name: "Parity source"}}, nil)
		if err != nil {
			t.Fatalf("append registry rows: %v", err)
		}
		row, ok := rowByPattern(rows, dataSource)
		if !ok {
			t.Fatalf("registry data-source row missing")
		}
		gate, err := s.ResolveDeclared(context.Background(), gateQuery)
		if err != nil {
			t.Fatalf("resolve registry gate: %v", err)
		}
		if row.Effective.Setting != gate.Setting {
			t.Fatalf("surface drift: Settings table=%q call-time gate=%q", row.Effective.Setting, gate.Setting)
		}
		if gate.Setting != want {
			t.Fatalf("verdict=%q, want %q", gate.Setting, want)
		}
	}

	t.Run("active Role", func(t *testing.T) {
		s := newTPStore(t)
		clearAll(t, s)
		agent := uuidByte(20)
		assignResourceRole(t, s, "Registry parity role", agent, RegistryToolKey, dataSource, SettingDeny)

		assertParity(t, s,
			TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent, Base: SettingAllow},
			Query{WorkspaceID: tpTestWorkspaceID, ToolKey: RegistryToolKey, ResourcePattern: dataSource, AgentID: agent, Base: SettingAllow},
			SettingDeny,
		)
	})

	t.Run("System", func(t *testing.T) {
		s := newTPStore(t)
		clearAll(t, s)
		system := uuidByte(21)
		if _, err := s.Set(context.Background(), SetParams{
			WorkspaceID:     tpTestWorkspaceID,
			ToolKey:         RegistryToolKey,
			Layer:           LayerSystem,
			SubjectID:       system,
			ResourcePattern: dataSource,
			Setting:         SettingAsk,
		}); err != nil {
			t.Fatalf("author system Ask: %v", err)
		}

		assertParity(t, s,
			TableQuery{WorkspaceID: tpTestWorkspaceID, SystemID: system, IsSystem: true, Base: SettingAllow},
			Query{WorkspaceID: tpTestWorkspaceID, ToolKey: RegistryToolKey, ResourcePattern: dataSource, SystemID: system, IsSystem: true, Base: SettingAllow},
			SettingDeny,
		)
	})

	t.Run("on_behalf_of", func(t *testing.T) {
		s := newTPStore(t)
		clearAll(t, s)
		member := uuidByte(22)
		if _, err := s.Set(context.Background(), SetParams{
			WorkspaceID:     tpTestWorkspaceID,
			ToolKey:         RegistryToolKey,
			Layer:           LayerOnBehalfOf,
			SubjectID:       member,
			ResourcePattern: dataSource,
			Setting:         SettingDeny,
		}); err != nil {
			t.Fatalf("author on_behalf_of Deny: %v", err)
		}

		assertParity(t, s,
			TableQuery{WorkspaceID: tpTestWorkspaceID, OnBehalfOfID: member, Base: SettingAllow},
			Query{WorkspaceID: tpTestWorkspaceID, ToolKey: RegistryToolKey, ResourcePattern: dataSource, OnBehalfOfID: member, Base: SettingAllow},
			SettingDeny,
		)
	})

	t.Run("conditioned wide scope", func(t *testing.T) {
		s := newTPStore(t)
		clearAll(t, s)
		condition := &Condition{ArgAllowlist: []ArgAllow{{
			Arg:    "data_source_id",
			Values: []string{"another-source"},
		}}}
		if _, err := s.Set(context.Background(), SetParams{
			WorkspaceID: tpTestWorkspaceID,
			ToolKey:     RegistryToolKey,
			Layer:       LayerWorkspace,
			SubjectID:   tpTestWorkspaceID,
			Setting:     SettingAllow,
			Conditions:  condition,
		}); err != nil {
			t.Fatalf("author conditioned wide Allow: %v", err)
		}
		requestContext := RequestContext{Action: "execute", ArgValues: map[string]string{"data_source_id": dataSource}}

		// The concrete call is the intersection of the exact row (Allow base)
		// and the capability-wide whitelist (Deny because this source is absent).
		rows, err := s.AppendRegistryDataSourceRows(context.Background(), TableQuery{
			WorkspaceID:    tpTestWorkspaceID,
			Base:           SettingAllow,
			RequestContext: requestContext,
		}, []RegistryDataSource{{ID: dataSource, Name: "Parity source"}}, nil)
		if err != nil {
			t.Fatalf("append registry rows: %v", err)
		}
		row, ok := rowByPattern(rows, dataSource)
		if !ok {
			t.Fatalf("registry data-source row missing")
		}
		exact, err := s.ResolveDeclared(context.Background(), Query{
			WorkspaceID:     tpTestWorkspaceID,
			ToolKey:         RegistryToolKey,
			ResourcePattern: dataSource,
			Base:            SettingAllow,
			RequestContext:  requestContext,
		})
		if err != nil {
			t.Fatalf("resolve exact registry row: %v", err)
		}
		wide, err := s.ResolveDeclared(context.Background(), Query{
			WorkspaceID:    tpTestWorkspaceID,
			ToolKey:        RegistryToolKey,
			Base:           SettingAllow,
			RequestContext: requestContext,
		})
		if err != nil {
			t.Fatalf("resolve wide registry row: %v", err)
		}
		callSetting := MoreRestrictive(exact.Setting, wide.Setting)
		if row.Effective.Setting != callSetting || callSetting != SettingDeny {
			t.Fatalf("condition parity: Settings table=%q call-time gate=%q, want deny", row.Effective.Setting, callSetting)
		}
	})
}
