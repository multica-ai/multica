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

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

// addCapSubject ties a capability to a subject (e.g. the runtime that reported
// it) so the runtime-scoped table filter keeps it. Looks the capability id up
// by its workspace+key. relation is one of reporter/owner/user.
func addCapSubject(t *testing.T, s *Store, capKey, subjectType string, subjectID pgtype.UUID, relation string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO cerebro_capability_subject
			(capability_id, workspace_id, subject_type, subject_id, relation, metadata, first_seen_at, last_seen_at)
		SELECT c.id, c.workspace_id, $3, $4, $5, '{}', now(), now()
		FROM cerebro_capability c
		WHERE c.workspace_id = $1 AND c.capability_key = $2
		ON CONFLICT (capability_id, subject_type, subject_id, relation) DO NOTHING
	`, tpTestWorkspaceID, capKey, subjectType, subjectID, relation); err != nil {
		t.Fatalf("attach subject %s to %q: %v", subjectType, capKey, err)
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

// TestTable_WorkspaceRootLayer is the FIR-2284 Bid 5 check: a setting authored
// at the workspace root (subject_id = the workspace itself) shows up on the
// workspace view as its own layer AND flows into every other view as the
// inherited base default below the rest of the chain.
func TestTable_WorkspaceRootLayer(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent := uuidByte(2)
	addCap(t, s, "shell_exec", "Run shell command", "Shell", "builtin")

	// Workspace root denies the tool for the whole workspace.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "shell_exec", Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, Setting: SettingDeny}); err != nil {
		t.Fatalf("set workspace: %v", err)
	}

	// Workspace view: the row carries the workspace layer and resolves to Deny
	// decided by workspace, not capped (root default).
	wsRows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("workspace table: %v", err)
	}
	wsRow, ok := findRow(wsRows, "shell_exec")
	if !ok {
		t.Fatal("shell_exec missing from workspace view")
	}
	if wsRow.Layers[LayerWorkspace] != SettingDeny {
		t.Fatalf("workspace layer = %q, want deny", wsRow.Layers[LayerWorkspace])
	}
	if wsRow.Effective.Setting != SettingDeny || wsRow.Effective.DecidedBy != LayerWorkspace || wsRow.Effective.CappedBy != "" {
		t.Fatalf("workspace effective = %q by %q capped %q, want deny/workspace/none", wsRow.Effective.Setting, wsRow.Effective.DecidedBy, wsRow.Effective.CappedBy)
	}

	// Agent view on the same tool: the workspace root is inherited as the base,
	// so the effective verdict is still Deny even though the agent set nothing.
	agentRows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent})
	if err != nil {
		t.Fatalf("agent table: %v", err)
	}
	agentRow, ok := findRow(agentRows, "shell_exec")
	if !ok {
		t.Fatal("shell_exec missing from agent view")
	}
	if agentRow.Layers[LayerWorkspace] != SettingDeny {
		t.Fatalf("agent view should inherit workspace layer, got %v", agentRow.Layers)
	}
	if agentRow.Effective.Setting != SettingDeny {
		t.Fatalf("agent effective = %q, want deny inherited from workspace root", agentRow.Effective.Setting)
	}
}

// TestTable_GroupCapNamesTheGroup is the TECH-3287 hul 5 check: when a group
// layer caps a tool to Deny, the row carries the blocking group's name AND its
// owner (the creator), so the UI can say "Capped by group <name> (owner: …)"
// instead of an anonymous "group".
func TestTable_GroupCapNamesTheGroup(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	addCap(t, s, "create_issue", "Create issue", "Issues", "builtin")

	// A real group, owned by the test user, that the user belongs to.
	var groupID pgtype.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO cerebro_group (workspace_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`,
		tpTestWorkspaceID, "All members", tpTestUserID).Scan(&groupID); err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM cerebro_group WHERE id = $1`, groupID)
	})
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO cerebro_group_member (group_id, user_id) VALUES ($1, $2)`,
		groupID, tpTestUserID); err != nil {
		t.Fatalf("add group member: %v", err)
	}

	// The group denies the tool; the user layer is silent, so the group caps it.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "create_issue", Layer: LayerGroup, SubjectID: groupID, Setting: SettingDeny}); err != nil {
		t.Fatalf("set group: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, UserID: tpTestUserID})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	row, ok := findRow(rows, "create_issue")
	if !ok {
		t.Fatal("create_issue missing from table")
	}
	if row.Effective.Setting != SettingDeny || row.Effective.CappedBy != LayerGroup {
		t.Fatalf("effective = %q capped=%q, want deny/group", row.Effective.Setting, row.Effective.CappedBy)
	}
	if len(row.CappedByGroups) != 1 {
		t.Fatalf("CappedByGroups = %v, want exactly one blocking group", row.CappedByGroups)
	}
	if row.CappedByGroups[0].Name != "All members" {
		t.Fatalf("blocking group name = %q, want %q", row.CappedByGroups[0].Name, "All members")
	}
	if row.CappedByGroups[0].Owner != tpTestName {
		t.Fatalf("blocking group owner = %q, want %q", row.CappedByGroups[0].Owner, tpTestName)
	}
}

// TestTable_NoGroupCapNoAttribution proves a row that is not group-capped carries
// no group attribution — we never name a group that did not shape the verdict.
func TestTable_NoGroupCapNoAttribution(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	addCap(t, s, "read_file", "Read file", "Files", "builtin")

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, UserID: tpTestUserID})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	row, ok := findRow(rows, "read_file")
	if !ok {
		t.Fatal("read_file missing")
	}
	if len(row.CappedByGroups) != 0 {
		t.Fatalf("unconfigured row should have no group attribution, got %v", row.CappedByGroups)
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
	// The runtime reported this tool, so it survives the runtime-scoped filter.
	addCapSubject(t, s, "deploy_restart", "runtime", runtime, "reporter")

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

// TestTable_RuntimeViewShowsOnlyThatRuntimesTools proves the runtime-scoped
// table lists only the tools the queried runtime reported — a tool another
// runtime reported must not bleed onto this runtime's page (FIR-2284: "open a
// runtime, see what IT can do"). Without a runtime in scope the full workspace
// universe still shows.
func TestTable_RuntimeViewShowsOnlyThatRuntimesTools(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	runtimeA, runtimeB := uuidByte(20), uuidByte(21)
	addCap(t, s, "bash", "Bash", "tools", "runtime_report")
	addCap(t, s, "firtal_registry", "firtal_registry", "tools", "scan")
	// runtimeA reported Bash; runtimeB reported the bq query tool.
	addCapSubject(t, s, "bash", "runtime", runtimeA, "reporter")
	addCapSubject(t, s, "firtal_registry", "runtime", runtimeB, "reporter")

	rowsA, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, RuntimeID: runtimeA})
	if err != nil {
		t.Fatalf("table runtimeA: %v", err)
	}
	if _, ok := findRow(rowsA, "bash"); !ok {
		t.Fatal("runtimeA page missing its own tool bash")
	}
	if _, leaked := findRow(rowsA, "firtal_registry"); leaked {
		t.Fatalf("runtimeB's tool leaked onto runtimeA page: %d rows", len(rowsA))
	}
	if len(rowsA) != 1 {
		t.Fatalf("runtimeA page got %d rows, want 1 (only its own tool)", len(rowsA))
	}

	// No runtime in scope → full workspace universe (both tools).
	rowsAll, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("table no-runtime: %v", err)
	}
	if len(rowsAll) != 2 {
		t.Fatalf("workspace view got %d rows, want 2 (full universe)", len(rowsAll))
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

// TestTable_SurfacesConditions is the FIR-1609 Phase 4 read-model check: a rule
// authored with a Condition (the WHEN layer) surfaces that condition on the
// table row at its single-subject layer, so the editor can render and edit it
// next to the Decision pill. A row with no condition leaves the layer's
// condition nil ("always applies").
func TestTable_SurfacesConditions(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()

	agent := uuidByte(2)
	addCap(t, s, "web_fetch", "Fetch a URL", "Web", "builtin")
	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	// web_fetch: Allow, but only for the firtal.com host family — the host
	// allowlist folded into the rule as a Condition.
	cond := &Condition{HostAllowlist: []string{"firtal.com", "*.firtal.com"}}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "web_fetch", Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow, Conditions: cond}); err != nil {
		t.Fatalf("set web_fetch with condition: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent})
	if err != nil {
		t.Fatalf("table: %v", err)
	}

	web, ok := findRow(rows, "web_fetch")
	if !ok {
		t.Fatal("web_fetch missing from table")
	}
	got := web.Conditions[LayerAgent]
	if got == nil {
		t.Fatalf("web_fetch agent condition missing; want host allowlist, conditions=%v", web.Conditions)
	}
	if len(got.HostAllowlist) != 2 || got.HostAllowlist[0] != "firtal.com" || got.HostAllowlist[1] != "*.firtal.com" {
		t.Fatalf("web_fetch host allowlist = %v, want [firtal.com *.firtal.com]", got.HostAllowlist)
	}
	// The condition does not change the setting — Allow stays Allow.
	if web.Layers[LayerAgent] != SettingAllow {
		t.Fatalf("web_fetch agent setting = %q, want allow (condition never decides)", web.Layers[LayerAgent])
	}

	// A tool with no condition leaves the layer's condition nil.
	comment, ok := findRow(rows, "add_comment")
	if !ok {
		t.Fatal("add_comment missing from table")
	}
	if c := comment.Conditions[LayerAgent]; c != nil {
		t.Fatalf("add_comment should carry no condition, got %v", c)
	}
}

// setWebFetchPolicy upserts the standalone web_fetch host policy for the test
// workspace and registers cleanup so it never leaks into a later test's view.
func setWebFetchPolicy(t *testing.T, s *Store, mode string, rules string) {
	t.Helper()
	if _, err := s.q.UpsertCerebroWebFetchPolicy(context.Background(), cerebrodb.UpsertCerebroWebFetchPolicyParams{
		WorkspaceID: tpTestWorkspaceID,
		Mode:        mode,
		HostRules:   []byte(rules),
	}); err != nil {
		t.Fatalf("upsert web_fetch policy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(),
			`DELETE FROM cerebro_web_fetch_policy WHERE workspace_id = $1`, tpTestWorkspaceID)
	})
}

// TestTable_FoldsWebFetchPolicyAsCondition is the FIR-1609 fold-in check: the
// standalone per-workspace web_fetch host policy surfaces on the web_fetch row as
// a workspace-layer Allow rule conditioned on the host allow-list, so the unified
// Permissions surface shows it instead of a separate page. An explicit admin
// override wins, and disallow-list mode is left to the dedicated page.
func TestTable_FoldsWebFetchPolicyAsCondition(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()
	addCap(t, s, "web_fetch", "Fetch a URL", "Web", "builtin")

	// Allow-list policy → projected as workspace Allow + host_allowlist condition.
	setWebFetchPolicy(t, s, "allow", `["firtal.com","*.firtal.com"]`)

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	web, ok := findRow(rows, "web_fetch")
	if !ok {
		t.Fatal("web_fetch missing from table")
	}
	if web.Layers[LayerWorkspace] != SettingAllow {
		t.Fatalf("workspace layer = %q, want allow (the host policy as a rule)", web.Layers[LayerWorkspace])
	}
	cond := web.Conditions[LayerWorkspace]
	if cond == nil {
		t.Fatalf("expected folded host condition on workspace layer, conditions=%v", web.Conditions)
	}
	if len(cond.HostAllowlist) != 2 || cond.HostAllowlist[0] != "firtal.com" || cond.HostAllowlist[1] != "*.firtal.com" {
		t.Fatalf("folded host allowlist = %v, want [firtal.com *.firtal.com]", cond.HostAllowlist)
	}
	if web.Effective.Setting != SettingAllow {
		t.Fatalf("effective = %q, want allow", web.Effective.Setting)
	}

	// An explicit admin override on the workspace layer wins — the projection must
	// not clobber it, nor inject a condition.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "web_fetch", Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, Setting: SettingDeny}); err != nil {
		t.Fatalf("set workspace override: %v", err)
	}
	rows, err = s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("table after override: %v", err)
	}
	web, _ = findRow(rows, "web_fetch")
	if web.Layers[LayerWorkspace] != SettingDeny {
		t.Fatalf("override workspace layer = %q, want deny (admin choice wins)", web.Layers[LayerWorkspace])
	}
	if c := web.Conditions[LayerWorkspace]; c != nil {
		t.Fatalf("projection must not add a condition over an explicit override, got %v", c)
	}
}

// TestTable_WebFetchDisallowNotFolded proves a disallow-list policy is NOT folded
// in: a host_allowlist cannot represent "everything except these", so that mode
// is left to the dedicated Web fetch page and the row stays unauthored.
func TestTable_WebFetchDisallowNotFolded(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	ctx := context.Background()
	addCap(t, s, "web_fetch", "Fetch a URL", "Web", "builtin")
	setWebFetchPolicy(t, s, "disallow", `["evil.com"]`)

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	web, ok := findRow(rows, "web_fetch")
	if !ok {
		t.Fatal("web_fetch missing from table")
	}
	if _, has := web.Layers[LayerWorkspace]; has {
		t.Fatalf("disallow policy must not inject a workspace rule, got %q", web.Layers[LayerWorkspace])
	}
	if c := web.Conditions[LayerWorkspace]; c != nil {
		t.Fatalf("disallow policy must not inject a condition, got %v", c)
	}
}
