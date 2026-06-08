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
