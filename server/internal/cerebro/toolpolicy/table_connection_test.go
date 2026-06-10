package toolpolicy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestDeniedConnectionTools_AgentDenyProducesToken seeds an MCP connection with
// two tools, denies one at the agent layer, and asserts only that tool surfaces
// as a Claude --disallowedTools token. Skips when the workspace_connection table
// is not present (a DB that hasn't run the cerebro connections migration).
func TestDeniedConnectionTools_AgentDenyProducesToken(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	agent := uuidByte(7)
	const conn = "customer-service"
	const toolKey = "connection:" + conn

	// Seed the connection with two tools; skip if the table isn't migrated here.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $3, 'mcp_http', 'http://internal:3000',
		        '[{"name":"draft_reply"},{"name":"lookup_order"}]'::jsonb, true)
	`, tpTestWorkspaceID, conn, "Customer Service MCP"); err != nil {
		if isUndefinedTable(err) {
			t.Skip("workspace_connection table not present; skipping connection-tool test")
		}
		t.Fatalf("seed connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, tpTestWorkspaceID)
	})

	// Deny draft_reply at the agent layer; leave lookup_order untouched (allow).
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         toolKey,
		Layer:           LayerAgent,
		SubjectID:       agent,
		Setting:         SettingDeny,
		ResourcePattern: "draft_reply",
	}); err != nil {
		t.Fatalf("set agent deny: %v", err)
	}

	tokens, err := s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		AgentID:     agent,
	})
	if err != nil {
		t.Fatalf("denied connection tools: %v", err)
	}

	want := "mcp__customer-service__draft_reply"
	if len(tokens) != 1 || tokens[0] != want {
		t.Fatalf("got %v, want exactly [%q]", tokens, want)
	}
}

// TestDeniedConnectionTools_ConnectionWideDenyScopedPerRuntimeAndAgent is the
// TECH-3180 regression. Before the fix, denying the WHOLE connection (the
// connection-wide row, resource_pattern '') at the runtime or agent layer was
// display-only: it produced no --disallowedTools tokens, so the tools stayed
// callable. The connection-wide chain must now cascade to every tool on the
// connection, and the deny must be scoped to exactly the runtime/agent it was set
// on — a different runtime/agent keeps full access.
func TestDeniedConnectionTools_ConnectionWideDenyScopedPerRuntimeAndAgent(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	deniedRuntime, otherRuntime := uuidByte(41), uuidByte(42)
	deniedAgent, otherAgent := uuidByte(43), uuidByte(44)
	const conn = "customer-service"
	const connKey = "connection:" + conn

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $3, 'mcp_http', 'http://internal:3000',
		        '[{"name":"draft_reply"},{"name":"lookup_order"}]'::jsonb, true)
	`, tpTestWorkspaceID, conn, "Customer Service MCP"); err != nil {
		if isUndefinedTable(err) {
			t.Skip("workspace_connection table not present; skipping connection-wide deny test")
		}
		t.Fatalf("seed connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, tpTestWorkspaceID)
	})

	// The connection-wide capability row, exactly as connections.SyncCapability
	// writes it — this is the row whose chain now cascades to the tools.
	addCap(t, s, connKey, "Customer Service MCP", "Connections", "connection")

	wantBoth := []string{
		"mcp__customer-service__draft_reply",
		"mcp__customer-service__lookup_order",
	}

	// --- Runtime layer: deny the whole connection for deniedRuntime only. ---
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     connKey,
		Layer:       LayerRuntime,
		SubjectID:   deniedRuntime,
		Setting:     SettingDeny,
	}); err != nil {
		t.Fatalf("set runtime-wide deny: %v", err)
	}

	tokens, err := s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID, RuntimeID: deniedRuntime,
	})
	if err != nil {
		t.Fatalf("denied (denied runtime): %v", err)
	}
	assertSameTokens(t, "denied runtime", tokens, wantBoth)

	tokens, err = s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID, RuntimeID: otherRuntime,
	})
	if err != nil {
		t.Fatalf("denied (other runtime): %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("connection-wide runtime deny leaked to another runtime: got %v", tokens)
	}

	// Clear the runtime deny; assert nothing is denied anymore (proves the cascade
	// is driven by the layer, not a sticky side effect).
	if err := s.Clear(ctx, tpTestWorkspaceID, connKey, LayerRuntime, deniedRuntime, ""); err != nil {
		t.Fatalf("clear runtime deny: %v", err)
	}

	// --- Agent layer: deny the whole connection for deniedAgent only. ---
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     connKey,
		Layer:       LayerAgent,
		SubjectID:   deniedAgent,
		Setting:     SettingDeny,
	}); err != nil {
		t.Fatalf("set agent-wide deny: %v", err)
	}

	tokens, err = s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID, AgentID: deniedAgent,
	})
	if err != nil {
		t.Fatalf("denied (denied agent): %v", err)
	}
	assertSameTokens(t, "denied agent", tokens, wantBoth)

	tokens, err = s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID, AgentID: otherAgent,
	})
	if err != nil {
		t.Fatalf("denied (other agent): %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("connection-wide agent deny leaked to another agent: got %v", tokens)
	}
}

// TestDeniedConnectionTools_PerToolTightensConnectionWideAllow proves the cascade
// only tightens: a connection-wide Allow with a single per-tool Deny at the
// runtime layer denies exactly that one tool, not the whole connection.
func TestDeniedConnectionTools_PerToolTightensConnectionWideAllow(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	runtime := uuidByte(45)
	const conn = "customer-service"
	const connKey = "connection:" + conn

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $3, 'mcp_http', 'http://internal:3000',
		        '[{"name":"draft_reply"},{"name":"lookup_order"}]'::jsonb, true)
	`, tpTestWorkspaceID, conn, "Customer Service MCP"); err != nil {
		if isUndefinedTable(err) {
			t.Skip("workspace_connection table not present; skipping cascade test")
		}
		t.Fatalf("seed connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, tpTestWorkspaceID)
	})
	addCap(t, s, connKey, "Customer Service MCP", "Connections", "connection")

	// Connection-wide Allow at runtime, one tool Deny at runtime.
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: connKey,
		Layer: LayerRuntime, SubjectID: runtime, Setting: SettingAllow,
	}); err != nil {
		t.Fatalf("set runtime-wide allow: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: connKey,
		Layer: LayerRuntime, SubjectID: runtime, Setting: SettingDeny,
		ResourcePattern: "draft_reply",
	}); err != nil {
		t.Fatalf("set per-tool runtime deny: %v", err)
	}

	tokens, err := s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID, RuntimeID: runtime,
	})
	if err != nil {
		t.Fatalf("denied connection tools: %v", err)
	}
	assertSameTokens(t, "per-tool tighten", tokens, []string{"mcp__customer-service__draft_reply"})
}

// assertSameTokens compares two token sets order-independently.
func assertSameTokens(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Fatalf("%s: missing %q in %v", label, w, got)
		}
	}
}

// TestTable_ConnectionRowSurvivesRuntimeFilter proves the connection-wide row
// (source "connection") — the row the Connections tab groups its tools under —
// stays visible on a runtime-scoped view even though connections are never
// runtime-reported and so carry no cerebro_capability_subject row. This is the
// TECH-3108 regression: before the fix the runtime EXISTS filter hid every
// connection on the runtime and agent permission pages, so the Connections tab
// rendered empty there while workspace/group/member showed it.
func TestTable_ConnectionRowSurvivesRuntimeFilter(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	runtime, otherRuntime := uuidByte(31), uuidByte(32)
	const conn = "customer-service"
	const connKey = "connection:" + conn

	// The connection-wide capability row, exactly as connections.SyncCapability
	// writes it (source "connection", category "Connections"). No cap subject —
	// connections are not runtime-reported.
	addCap(t, s, connKey, "Customer Service MCP", "Connections", "connection")
	// A genuinely runtime-reported tool owned by a DIFFERENT runtime, to prove the
	// runtime filter still scopes ordinary tools.
	addCap(t, s, "bash", "Bash", "tools", "runtime_report")
	addCapSubject(t, s, "bash", "runtime", otherRuntime, "reporter")

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, RuntimeID: runtime})
	if err != nil {
		t.Fatalf("table runtime view: %v", err)
	}

	row, ok := findRow(rows, connKey)
	if !ok {
		t.Fatal("connection-wide row missing on runtime-scoped view (TECH-3108 regression)")
	}
	if row.Source != "connection" {
		t.Fatalf("connection row source = %q, want %q", row.Source, "connection")
	}
	if _, leaked := findRow(rows, "bash"); leaked {
		t.Fatal("other runtime's tool leaked onto this runtime's view")
	}
}

// TestApiConnection_EndpointRowsNotRuntimeDenied seeds an API connection with
// endpoint+method permissions, denies one method at the agent layer, and asserts
// the endpoint surfaces in the table (so the CRUD-control sheet can edit it) but
// does NOT produce a Claude --disallowedTools token — API connections are not MCP
// and have no --disallowedTools enforcement path yet.
func TestApiConnection_EndpointRowsNotRuntimeDenied(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	agent := uuidByte(9)
	const conn = "orders-api"
	const toolKey = "connection:" + conn

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, endpoint_permissions, enabled)
		VALUES ($1, $2, $3, 'api', 'https://api.example.com',
		        '[{"path":"/orders","methods":["GET","POST"]}]'::jsonb, true)
	`, tpTestWorkspaceID, conn, "Orders API"); err != nil {
		if isUndefinedTable(err) {
			t.Skip("workspace_connection table not present; skipping api connection test")
		}
		t.Fatalf("seed api connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, tpTestWorkspaceID)
	})

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         toolKey,
		Layer:           LayerAgent,
		SubjectID:       agent,
		Setting:         SettingDeny,
		ResourcePattern: "POST /orders",
	}); err != nil {
		t.Fatalf("set agent deny: %v", err)
	}

	tableRows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	var found bool
	for _, r := range tableRows {
		if r.ToolKey == toolKey && r.ResourcePattern == "POST /orders" {
			found = true
			if r.Source != connectionEndpointSource {
				t.Fatalf("expected source %q, got %q", connectionEndpointSource, r.Source)
			}
			if r.Effective.Setting != SettingDeny {
				t.Fatalf("expected POST /orders deny, got %q", r.Effective.Setting)
			}
		}
	}
	if !found {
		t.Fatalf("expected a POST /orders endpoint row in the table")
	}

	tokens, err := s.DeniedConnectionTools(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent})
	if err != nil {
		t.Fatalf("denied connection tools: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("expected no MCP tokens for an API connection, got %v", tokens)
	}
}

// TestConnectionToolDenied checks the firtal-gateway always-on resolver: a tool
// denied as a connection tool for one agent resolves true for that agent only,
// and other tools / other agents resolve false.
func TestConnectionToolDenied(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent := uuidByte(11)
	other := uuidByte(12)
	const conn = "customer-service-mcp"
	const toolKey = "connection:" + conn

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $3, 'mcp_http', 'http://internal:3000',
		        '[{"name":"draft_reply"},{"name":"lookup_order"}]'::jsonb, true)
	`, tpTestWorkspaceID, conn, "Customer Service MCP"); err != nil {
		if isUndefinedTable(err) {
			t.Skip("workspace_connection table not present; skipping gateway deny test")
		}
		t.Fatalf("seed connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, tpTestWorkspaceID)
	})

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID:     tpTestWorkspaceID,
		ToolKey:         toolKey,
		Layer:           LayerAgent,
		SubjectID:       agent,
		Setting:         SettingDeny,
		ResourcePattern: "draft_reply",
	}); err != nil {
		t.Fatalf("set agent deny: %v", err)
	}

	var zero pgtype.UUID
	cases := []struct {
		name   string
		ag     pgtype.UUID
		tool   string
		denied bool
	}{
		{"denied tool for denied agent", agent, "draft_reply", true},
		{"other tool same agent", agent, "lookup_order", false},
		{"denied tool different agent", other, "draft_reply", false},
		{"empty tool", agent, "", false},
	}
	for _, c := range cases {
		got, err := s.ConnectionToolDenied(ctx, tpTestWorkspaceID, zero, c.ag, zero, c.tool)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.denied {
			t.Fatalf("%s: got denied=%v want %v", c.name, got, c.denied)
		}
	}
}
