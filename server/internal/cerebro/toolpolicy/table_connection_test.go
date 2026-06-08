package toolpolicy

import (
	"context"
	"testing"
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
