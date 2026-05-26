package toolpolicy

// Integration tests for the table read model (table.go). They prove the join
// the pure chain_test.go and the single-tool store_test.go cannot: that every
// capability in a workspace turns into one row carrying its per-layer settings
// for a context AND the resolved Effective verdict — the exact shape the admin
// screen renders. They share the store_test.go fixture (TestMain) and skip
// cleanly when no test database is reachable.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// clearCaps removes every capability row for the test workspace so each Table
// subtest controls the exact tool universe it asserts on.
func clearCaps(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_capability WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear capability rows: %v", err)
	}
}

// addCap inserts one capability (tool) into the register so Table can list it.
func addCap(t *testing.T, s *Store, key, title, category, source string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO cerebro_capability
			(workspace_id, capability_key, title, category, description, source, metadata, last_reported_at, updated_at)
		VALUES ($1, $2, $3, $4, '', $5, '{}', now(), now())
		ON CONFLICT (workspace_id, capability_key) DO UPDATE SET title = EXCLUDED.title
	`, tpTestWorkspaceID, key, title, category, source); err != nil {
		t.Fatalf("insert capability %q: %v", key, err)
	}
}

func findRow(rows []TableRow, key string) (TableRow, bool) {
	for _, r := range rows {
		if r.ToolKey == key {
			return r, true
		}
	}
	return TableRow{}, false
}

// TestTable_ListsEveryToolWithEffective is the core data-layer check: every
// capability shows up once, a tool with no settings resolves to the Base
// default (Allow), and a tool with agent Allow + user Deny resolves to Deny
// capped by user — all in the same listing the screen renders.
func TestTable_ListsEveryToolWithEffective(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent, user := uuidByte(2), tpTestUserID
	addCap(t, s, "slack.post_message", "Post Slack message", "Slack", "scan")
	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	// slack.post_message: agent Allow but user Deny -> capped by user.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "slack.post_message", Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow}); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "slack.post_message", Layer: LayerUser, SubjectID: user, Setting: SettingDeny}); err != nil {
		t.Fatalf("set user: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent, UserID: user})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one per capability)", len(rows))
	}

	slack, ok := findRow(rows, "slack.post_message")
	if !ok {
		t.Fatal("slack.post_message missing from table")
	}
	if slack.Effective.Setting != SettingDeny || slack.Effective.CappedBy != LayerUser {
		t.Fatalf("slack effective = %q capped=%q, want deny/user", slack.Effective.Setting, slack.Effective.CappedBy)
	}
	if slack.Layers[LayerAgent] != SettingAllow || slack.Layers[LayerUser] != SettingDeny {
		t.Fatalf("slack layers = %v, want agent=allow user=deny", slack.Layers)
	}
	if slack.Title != "Post Slack message" || slack.Source != "scan" {
		t.Fatalf("slack labels = %q/%q, want human title + source", slack.Title, slack.Source)
	}

	comment, ok := findRow(rows, "add_comment")
	if !ok {
		t.Fatal("add_comment missing from table")
	}
	if comment.Effective.Setting != SettingAllow {
		t.Fatalf("unconfigured tool effective = %q, want allow (base default)", comment.Effective.Setting)
	}
	if len(comment.Layers) != 0 {
		t.Fatalf("unconfigured tool should have no explicit layers, got %v", comment.Layers)
	}
}

// TestTable_RuntimeViewIgnoresOtherSubjects proves a runtime-only view (no agent
// or user) reflects just the runtime layer — an agent override on the same tool
// must not bleed into the runtime page.
func TestTable_RuntimeViewIgnoresOtherSubjects(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	runtime, otherAgent := uuidByte(1), uuidByte(7)
	addCap(t, s, "deploy_restart", "Restart deploy", "Deploy", "builtin")

	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "deploy_restart", Layer: LayerRuntime, SubjectID: runtime, Setting: SettingAsk}); err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	// An unrelated agent denies the same tool; the runtime view must not see it.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "deploy_restart", Layer: LayerAgent, SubjectID: otherAgent, Setting: SettingDeny}); err != nil {
		t.Fatalf("set other agent: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, RuntimeID: runtime})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	row, ok := findRow(rows, "deploy_restart")
	if !ok {
		t.Fatal("deploy_restart missing")
	}
	if row.Layers[LayerRuntime] != SettingAsk {
		t.Fatalf("runtime layer = %q, want ask", row.Layers[LayerRuntime])
	}
	if _, leaked := row.Layers[LayerAgent]; leaked {
		t.Fatalf("agent override leaked into runtime view: %v", row.Layers)
	}
	if row.Effective.Setting != SettingAsk || row.Effective.DecidedBy != LayerRuntime {
		t.Fatalf("effective = %q by %q, want ask/runtime", row.Effective.Setting, row.Effective.DecidedBy)
	}
}

// TestTable_GroupsCombinedInRow proves the table collapses several group rows
// into the single combined LayerGroup value (most permissive group wins).
func TestTable_GroupsCombinedInRow(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	groupA, groupB := uuidByte(10), uuidByte(11)
	addCap(t, s, "web_fetch", "Fetch web page", "Web", "builtin")

	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "web_fetch", Layer: LayerGroup, SubjectID: groupA, Setting: SettingDeny}); err != nil {
		t.Fatalf("set group A: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "web_fetch", Layer: LayerGroup, SubjectID: groupB, Setting: SettingAllow}); err != nil {
		t.Fatalf("set group B: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{
		WorkspaceID: tpTestWorkspaceID,
		GroupIDs:    []pgtype.UUID{groupA, groupB},
	})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	row, ok := findRow(rows, "web_fetch")
	if !ok {
		t.Fatal("web_fetch missing")
	}
	if row.Layers[LayerGroup] != SettingAllow {
		t.Fatalf("combined group layer = %q, want allow (most permissive)", row.Layers[LayerGroup])
	}
	if row.Effective.Setting != SettingAllow {
		t.Fatalf("effective = %q, want allow", row.Effective.Setting)
	}
}
