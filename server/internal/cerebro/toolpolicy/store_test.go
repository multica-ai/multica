package toolpolicy

// Integration tests for the database-backed Store. They prove the full path the
// pure chain_test.go cannot: that explicit settings survive a round trip through
// the DB and assemble into the same Effective verdict the design mandates —
// including the FIR-2230 live check (agent Allow + user Deny -> Deny capped by
// user). Tests skip cleanly when no test database is reachable, so unit-only
// environments still run the pure chain tests above.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	tpTestEmail         = "toolpolicy-test@multica.ai"
	tpTestName          = "Tool Policy Test"
	tpTestWorkspaceSlug = "toolpolicy-tests"
)

var (
	tpTestPool        *pgxpool.Pool
	tpTestWorkspaceID pgtype.UUID
	tpTestUserID      pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping toolpolicy integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping toolpolicy integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupTPTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean toolpolicy test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := setupTPTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up toolpolicy test fixture: %v\n", err)
		_ = cleanupTPTestFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}

	tpTestPool = pool
	code := m.Run()

	if err := cleanupTPTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean toolpolicy test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupTPTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, tpTestName, tpTestEmail).Scan(&tpTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Tool Policy Tests", tpTestWorkspaceSlug, "Temporary workspace", "TPT").Scan(&tpTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	return nil
}

func cleanupTPTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// workspace cascade drops cerebro_tool_policy rows via the workspace_id FK.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, tpTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, tpTestEmail); err != nil {
		return err
	}
	return nil
}

// uuidByte builds a deterministic, valid UUID for use as a polymorphic subject
// id (runtime/agent/group). These ids carry no foreign key, so any distinct
// value works.
func uuidByte(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func newTPStore(t *testing.T) *Store {
	t.Helper()
	if tpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping toolpolicy integration test")
	}
	return NewStore(tpTestPool)
}

// clearAll removes every policy row for the test workspace so each subtest
// starts from a clean slate without recreating the workspace fixture.
func clearAll(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear policy rows: %v", err)
	}
}

// TestStore_LiveCheck is Jesper's check from FIR-2230, end-to-end through the
// DB: agent Allow + user Deny on the same tool resolves to Deny, capped by user.
func TestStore_LiveCheck(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	runtime, agent, user := uuidByte(1), uuidByte(2), tpTestUserID
	const tool = "slack.post_message"

	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerRuntime, SubjectID: runtime, Setting: SettingAllow}); err != nil {
		t.Fatalf("set runtime: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow, UpdatedBy: user}); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser, SubjectID: user, Setting: SettingDeny, UpdatedBy: user}); err != nil {
		t.Fatalf("set user: %v", err)
	}

	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		RuntimeID:   runtime,
		AgentID:     agent,
		UserID:      user,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if eff.Setting != SettingDeny {
		t.Fatalf("setting = %q, want %q", eff.Setting, SettingDeny)
	}
	if eff.CappedBy != LayerUser {
		t.Fatalf("cappedBy = %q, want %q", eff.CappedBy, LayerUser)
	}
	if eff.Reason != "Capped by user" {
		t.Fatalf("reason = %q, want %q", eff.Reason, "Capped by user")
	}
}

// TestStore_NoRowsAllowsByDefault proves an unconfigured context keeps working:
// no stored rows resolves to the Base default (Allow).
func TestStore_NoRowsAllowsByDefault(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     "add_comment",
		RuntimeID:   uuidByte(9),
		AgentID:     uuidByte(8),
		UserID:      tpTestUserID,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("setting = %q, want %q", eff.Setting, SettingAllow)
	}
}

// TestStore_GroupsCombineThenUserCaps proves the group fold and the ceiling: two
// groups disagree (most permissive wins), then the user ceiling tightens it.
func TestStore_GroupsCombineThenUserCaps(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	groupA, groupB, user := uuidByte(10), uuidByte(11), tpTestUserID
	const tool = "deploy_restart"

	// One group denies, the other allows. CombineGroups keeps Allow.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerGroup, SubjectID: groupA, Setting: SettingDeny}); err != nil {
		t.Fatalf("set group A: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerGroup, SubjectID: groupB, Setting: SettingAllow}); err != nil {
		t.Fatalf("set group B: %v", err)
	}

	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		UserID:      user,
		GroupIDs:    []pgtype.UUID{groupA, groupB},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("combined groups setting = %q, want %q (most permissive wins)", eff.Setting, SettingAllow)
	}

	// Now the user ceiling asks for approval -> caps the group's Allow up to Ask.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser, SubjectID: user, Setting: SettingAsk}); err != nil {
		t.Fatalf("set user: %v", err)
	}
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		UserID:      user,
		GroupIDs:    []pgtype.UUID{groupA, groupB},
	})
	if err != nil {
		t.Fatalf("resolve after user cap: %v", err)
	}
	if eff.Setting != SettingAsk || eff.CappedBy != LayerUser {
		t.Fatalf("after user cap: setting=%q cappedBy=%q, want ask/user", eff.Setting, eff.CappedBy)
	}
}

// TestStore_SubjectIsolation proves the context query does not leak another
// subject's setting: a Deny on agent A must not affect agent B.
func TestStore_SubjectIsolation(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	const tool = "credential_list"
	agentA, agentB := uuidByte(20), uuidByte(21)

	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agentA, Setting: SettingDeny}); err != nil {
		t.Fatalf("set agent A: %v", err)
	}

	eff, err := s.Resolve(ctx, Query{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agentB})
	if err != nil {
		t.Fatalf("resolve agent B: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("agent B setting = %q, want %q (A's deny must not leak)", eff.Setting, SettingAllow)
	}
}

// TestStore_GroupAutoExpansionFromUser proves the FIR-2230 fix the reviewer
// asked for: when a Query carries a user but no explicit group ids, the Group
// layer is resolved from the user's REAL workspace group membership — so the
// runtime enforcement gate (which only knows the agent's owner) and the agent
// page (which only binds a user id) both get the user's group ceiling without
// enumerating it client-side. Here the user's group denies a tool with no other
// layer set, so the effective verdict must be Deny decided by the group.
func TestStore_GroupAutoExpansionFromUser(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	// Create a real group and put the test user in it.
	var groupID pgtype.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO cerebro_group (workspace_id, name) VALUES ($1, $2) RETURNING id`,
		tpTestWorkspaceID, "auto-expand-grp").Scan(&groupID); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO cerebro_group_member (group_id, user_id) VALUES ($1, $2)`,
		groupID, tpTestUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM cerebro_group WHERE id = $1`, groupID)
	})

	const tool = "deploy_restart"
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerGroup, SubjectID: groupID, Setting: SettingDeny}); err != nil {
		t.Fatalf("set group deny: %v", err)
	}

	// No GroupIDs passed — the store must expand them from the user.
	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		AgentID:     uuidByte(2),
		UserID:      tpTestUserID,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if eff.Setting != SettingDeny {
		t.Fatalf("setting = %q, want %q (group membership must be auto-expanded)", eff.Setting, SettingDeny)
	}
	if eff.DecidedBy != LayerGroup {
		t.Fatalf("decidedBy = %q, want %q", eff.DecidedBy, LayerGroup)
	}

	// Explicit (empty-but-present) GroupIDs are honoured verbatim: passing a
	// non-matching group means the user's real group is NOT consulted.
	effOther, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		UserID:      tpTestUserID,
		GroupIDs:    []pgtype.UUID{uuidByte(99)},
	})
	if err != nil {
		t.Fatalf("resolve explicit groups: %v", err)
	}
	if effOther.Setting != SettingAllow {
		t.Fatalf("explicit non-matching group setting = %q, want %q (no auto-expand when groups given)", effOther.Setting, SettingAllow)
	}
}

// TestStore_ClearReverts proves clearing a layer drops back to Inherit, undoing
// a previous cap.
func TestStore_ClearReverts(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	const tool = "web_fetch"
	agent, user := uuidByte(30), tpTestUserID

	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow}); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser, SubjectID: user, Setting: SettingDeny}); err != nil {
		t.Fatalf("set user: %v", err)
	}
	if eff, _ := s.Resolve(ctx, Query{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent, UserID: user}); eff.Setting != SettingDeny {
		t.Fatalf("before clear: setting = %q, want deny", eff.Setting)
	}

	if err := s.Clear(ctx, tpTestWorkspaceID, tool, LayerUser, user); err != nil {
		t.Fatalf("clear user: %v", err)
	}
	eff, err := s.Resolve(ctx, Query{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent, UserID: user})
	if err != nil {
		t.Fatalf("resolve after clear: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("after clearing user deny: setting = %q, want %q", eff.Setting, SettingAllow)
	}
}

// TestStore_ListForSubject reads back every tool a subject overrides.
func TestStore_ListForSubject(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	agent := uuidByte(40)
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "tool_a", Layer: LayerAgent, SubjectID: agent, Setting: SettingDeny}); err != nil {
		t.Fatalf("set tool_a: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: "tool_b", Layer: LayerAgent, SubjectID: agent, Setting: SettingAsk}); err != nil {
		t.Fatalf("set tool_b: %v", err)
	}

	rows, err := s.ListForSubject(ctx, tpTestWorkspaceID, LayerAgent, agent)
	if err != nil {
		t.Fatalf("list for subject: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	got := map[string]Setting{}
	for _, r := range rows {
		got[r.ToolKey] = r.Setting
	}
	if got["tool_a"] != SettingDeny || got["tool_b"] != SettingAsk {
		t.Fatalf("settings = %v, want tool_a=deny tool_b=ask", got)
	}
}

// TestStore_RejectsBadInput proves the authoring API validates before touching
// the DB. No pool needed — validation short-circuits.
func TestStore_RejectsBadInput(t *testing.T) {
	s := &Store{} // intentionally no pool: validation must reject before any query.
	ctx := context.Background()

	if _, err := s.Set(ctx, SetParams{Layer: "nonsense", Setting: SettingAllow}); err == nil {
		t.Fatal("Set with unknown layer should error before hitting the DB")
	}
	if _, err := s.Set(ctx, SetParams{Layer: LayerAgent, Setting: "nonsense"}); err == nil {
		t.Fatal("Set with unknown setting should error before hitting the DB")
	}
	if err := s.Clear(ctx, pgtype.UUID{}, "x", "nonsense", pgtype.UUID{}); err == nil {
		t.Fatal("Clear with unknown layer should error before hitting the DB")
	}
}
