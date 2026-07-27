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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

// assignResourceRole creates one active agent role whose permission is scoped
// to an exact resource. Synthetic table rows (repos, credentials and connection
// tools) must expand this package through the same resolver as ordinary rows.
func assignResourceRole(t *testing.T, s *Store, name string, agent pgtype.UUID, toolKey, resource string, setting Setting) {
	t.Helper()
	var roleID pgtype.UUID
	if err := s.pool.QueryRow(context.Background(), `
		INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
		VALUES (
		  $1, $2, 'resource-role parity fixture', $3,
		  jsonb_build_object(
		    $4::text,
		    jsonb_build_array(jsonb_build_object(
		      'setting', $5::text,
		      'resource_pattern', $6::text,
		      'conditions', NULL
		    ))
		  )
		)
		RETURNING id
	`, tpTestWorkspaceID, name, tpTestUserID, toolKey, string(setting), resource).Scan(&roleID); err != nil {
		t.Fatalf("create resource role: %v", err)
	}
	if _, err := s.pool.Exec(context.Background(), `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
		VALUES ($1, 'agent', $2, $3)
	`, roleID, agent, tpTestUserID); err != nil {
		t.Fatalf("assign resource role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	})
}

// TestStoreFromQueries_ResolvesActiveRoleAssignments pins the public Resolve
// seam used by handlers and runtimes that already own generated queries. Role
// bindings must not disappear merely because the caller chose this constructor.
func TestStoreFromQueries_ResolvesActiveRoleAssignments(t *testing.T) {
	if tpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping toolpolicy integration test")
	}
	ctx := context.Background()
	agent := uuidByte(42)
	const tool = "role.protected_tool"

	var roleID pgtype.UUID
	if err := tpTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
		VALUES ($1, 'StoreFromQueries role', 'constructor parity fixture', $2,
		        jsonb_build_object($3::text, jsonb_build_object('setting', 'deny')))
		RETURNING id
	`, tpTestWorkspaceID, tpTestUserID, tool).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	defer func() {
		_, _ = tpTestPool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	}()
	if _, err := tpTestPool.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
		VALUES ($1, 'agent', $2, $3)
	`, roleID, agent, tpTestUserID); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	store := NewStoreFromQueries(cerebrodb.New(tpTestPool))
	effective, err := store.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		AgentID:     agent,
		UserID:      tpTestUserID,
	})
	if err != nil {
		t.Fatalf("resolve role-bound tool: %v", err)
	}
	if effective.Setting != SettingDeny {
		t.Fatalf("setting = %q, want %q from active role binding", effective.Setting, SettingDeny)
	}
	if !strings.Contains(effective.Reason, "via role StoreFromQueries role v1") {
		t.Fatalf("reason = %q, want role name and version provenance", effective.Reason)
	}

	if _, err := tpTestPool.Exec(ctx, `
		UPDATE cerebro_role SET archived_at=now(), version=version+1 WHERE id=$1
	`, roleID); err != nil {
		t.Fatalf("archive role: %v", err)
	}
	effective, err = store.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		AgentID:     agent,
		UserID:      tpTestUserID,
	})
	if err != nil {
		t.Fatalf("resolve after archive: %v", err)
	}
	if effective.Setting != SettingAllow {
		t.Fatalf("setting after archive = %q, want base %q", effective.Setting, SettingAllow)
	}
	if strings.Contains(effective.Reason, "via role") {
		t.Fatalf("reason after archive = %q, archived role must not participate", effective.Reason)
	}
}

// TestStoreFromQueries_RoleAllowCannotOverrideExplicitAgentDeny pins the
// FIR-3403 security floor: a role grant folds into the agent layer TIGHTEN-only,
// so a role `allow` can never cancel an administrator's explicit agent-layer
// `deny`. Before the fix the fold was most-permissive and the tool resolved to
// allow — a role silently reopened a denied tool.
func TestStoreFromQueries_RoleAllowCannotOverrideExplicitAgentDeny(t *testing.T) {
	if tpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping toolpolicy integration test")
	}
	ctx := context.Background()
	agent := uuidByte(43)
	const tool = "role.floor_tool"

	store := NewStore(tpTestPool)
	clearAll(t, store)

	// Administrator explicitly denies the tool for this agent.
	if _, err := store.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, Setting: SettingDeny, UpdatedBy: tpTestUserID,
	}); err != nil {
		t.Fatalf("set explicit agent deny: %v", err)
	}

	// A role the agent holds grants the same tool `allow`.
	var roleID pgtype.UUID
	if err := tpTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_role (workspace_id, name, description, created_by, permissions)
		VALUES ($1, 'Floor role', 'FIR-3403 tighten-only fixture', $2,
		        jsonb_build_object($3::text, jsonb_build_object('setting', 'allow')))
		RETURNING id
	`, tpTestWorkspaceID, tpTestUserID, tool).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	defer func() {
		_, _ = tpTestPool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	}()
	if _, err := tpTestPool.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by)
		VALUES ($1, 'agent', $2, $3)
	`, roleID, agent, tpTestUserID); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	eff, err := store.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		AgentID:     agent,
		UserID:      tpTestUserID,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if eff.Setting != SettingDeny {
		t.Fatalf("setting = %q, want %q — a role allow must not override an explicit agent deny", eff.Setting, SettingDeny)
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

// TestStore_SystemRunConvertsAskToDeny proves the FIR-1609 resolution-context
// fail-safe survives the DB round trip: the same stored Ask rule resolves to Ask
// for a human run (IsSystem=false) but to Deny for a human-less System run
// (IsSystem=true), because a System actor has no one to answer the prompt.
func TestStore_SystemRunConvertsAskToDeny(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	runtime, agent, user := uuidByte(20), uuidByte(21), tpTestUserID
	const tool = "web_fetch"

	// One stored Ask at the agent layer; nothing else opines.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent, SubjectID: agent, Setting: SettingAsk}); err != nil {
		t.Fatalf("set agent ask: %v", err)
	}

	base := Query{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, RuntimeID: runtime, AgentID: agent, UserID: user}

	human := base
	human.IsSystem = false
	if eff, err := s.Resolve(ctx, human); err != nil {
		t.Fatalf("resolve human: %v", err)
	} else if eff.Setting != SettingAsk {
		t.Fatalf("human run setting = %q, want %q", eff.Setting, SettingAsk)
	}

	system := base
	system.IsSystem = true
	eff, err := s.Resolve(ctx, system)
	if err != nil {
		t.Fatalf("resolve system: %v", err)
	}
	if eff.Setting != SettingDeny {
		t.Fatalf("system run setting = %q, want %q", eff.Setting, SettingDeny)
	}
	if eff.DecidedBy != LayerSystem {
		t.Fatalf("system run decidedBy = %q, want %q", eff.DecidedBy, LayerSystem)
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

func TestStoreResolveDeclaredDispatchesByWorkspaceFlag(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	group, user := uuidByte(40), tpTestUserID
	const tool = "web_fetch"

	// Group denies the tool; the member (user) allows it for themselves.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerGroup, SubjectID: group, Setting: SettingDeny}); err != nil {
		t.Fatalf("set group: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser, SubjectID: user, Setting: SettingAllow}); err != nil {
		t.Fatalf("set user: %v", err)
	}

	q := Query{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, UserID: user, GroupIDs: []pgtype.UUID{group}}

	if err := s.q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: tpTestWorkspaceID, FlagKey: FlagMemberOverride, Enabled: false,
	}); err != nil {
		t.Fatalf("disable member override: %v", err)
	}
	off, err := s.ResolveDeclared(ctx, q)
	if err != nil {
		t.Fatalf("resolve general (off): %v", err)
	}
	if off.Setting != SettingDeny || off.CappedBy != LayerGroup {
		t.Fatalf("flag OFF: setting=%q cappedBy=%q, want deny/group (tighten-only)", off.Setting, off.CappedBy)
	}

	if err := s.q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: tpTestWorkspaceID, FlagKey: FlagMemberOverride, Enabled: true,
	}); err != nil {
		t.Fatalf("enable member override: %v", err)
	}
	on, err := s.ResolveDeclared(ctx, q)
	if err != nil {
		t.Fatalf("resolve general (on): %v", err)
	}
	if on.Setting != SettingAllow || on.DecidedBy != LayerUser || on.CappedBy != "" {
		t.Fatalf("flag ON: setting=%q decidedBy=%q cappedBy=%q, want allow/user/none (member override)", on.Setting, on.DecidedBy, on.CappedBy)
	}

	plain, err := s.Resolve(ctx, q)
	if err != nil {
		t.Fatalf("resolve baseline: %v", err)
	}
	if plain.Setting != off.Setting || plain.CappedBy != off.CappedBy {
		t.Fatalf("ResolveDeclared with flag off drifted from hard-floor Resolve: %+v vs %+v", off, plain)
	}
}

func TestStoreResolveDeclaredNeverOpensHardFloorKeyClasses(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()
	agent := uuidByte(41)
	if err := s.q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: tpTestWorkspaceID, FlagKey: FlagMemberOverride, Enabled: true,
	}); err != nil {
		t.Fatalf("enable member override: %v", err)
	}
	for _, key := range []string{
		"credential.reveal",
		"tools:agent-browser",
		RegistryToolKey,
		"repo.checkout",
		"repo.push",
	} {
		t.Run(key, func(t *testing.T) {
			clearAll(t, s)
			if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: key, Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, Setting: SettingDeny}); err != nil {
				t.Fatalf("set workspace deny: %v", err)
			}
			if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: key, Layer: LayerAgent, SubjectID: agent, Setting: SettingAllow}); err != nil {
				t.Fatalf("set agent allow: %v", err)
			}
			effective, err := s.ResolveDeclared(ctx, Query{
				WorkspaceID: tpTestWorkspaceID,
				ToolKey:     key,
				AgentID:     agent,
			})
			if err != nil {
				t.Fatalf("resolve declared: %v", err)
			}
			if effective.Openable || effective.Setting != SettingDeny {
				t.Fatalf("hard-floor key resolved openable: %+v", effective)
			}
		})
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

	if err := s.Clear(ctx, tpTestWorkspaceID, tool, LayerUser, user, "", pgtype.UUID{}); err != nil {
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
	if err := s.Clear(ctx, pgtype.UUID{}, "x", "nonsense", pgtype.UUID{}, "", pgtype.UUID{}); err == nil {
		t.Fatal("Clear with unknown layer should error before hitting the DB")
	}
}

// TestStore_DisableOnlyValidAtWorkspaceLayer pins the FIR-2351 follow-up write
// guard: SettingDisable is a workspace-only state (a hard, unopenable floor —
// see chain_disable_test.go), so authoring it at any other layer is rejected
// before it ever reaches the DB, rather than silently behaving like an
// ordinary Deny (which is what resolveOpenable/resolveHardFloor would do with
// a stray Disable row outside LayerWorkspace).
func TestStore_DisableOnlyValidAtWorkspaceLayer(t *testing.T) {
	s := &Store{} // intentionally no pool: validation must reject before any query.
	ctx := context.Background()

	for _, layer := range []Layer{LayerRuntime, LayerAgent, LayerGroup, LayerUser} {
		if _, err := s.Set(ctx, SetParams{Layer: layer, Setting: SettingDisable}); err == nil {
			t.Fatalf("Set(Disable) at layer %q should error before hitting the DB", layer)
		}
	}
}

// TestStore_ResourcePatternIsolatesRowsByResource proves the FIR-2505 slice 1
// dimension: two rows on the same (tool, layer, subject) that disagree on
// resource_pattern coexist, and Resolve sees only the one whose pattern matches
// its Query. The capability-wide row (resource_pattern = "") is what every
// pre-FIR-2505 caller wrote and still resolves on the empty query — that is the
// backward-compatible default. A per-resource row only enters the resolution
// when the caller asks for that exact pattern.
func TestStore_ResourcePatternIsolatesRowsByResource(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	const tool = "repo.checkout"
	const repoA = "github.com/firtal/alpha"
	const repoB = "github.com/firtal/beta"
	agent := uuidByte(60)

	// Capability-wide row: agent gets Allow for any repo by default.
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, ResourcePattern: "", Setting: SettingAllow,
	}); err != nil {
		t.Fatalf("set capability-wide: %v", err)
	}
	// Per-resource override: deny on repoA only.
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, ResourcePattern: repoA, Setting: SettingDeny,
	}); err != nil {
		t.Fatalf("set per-resource deny: %v", err)
	}

	// Resolve with empty pattern: sees the capability-wide row (Allow).
	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
	})
	if err != nil {
		t.Fatalf("resolve capability-wide: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("capability-wide resolve = %q, want allow (row=%+v)", eff.Setting, eff)
	}

	// Resolve with repoA pattern: sees the per-resource row (Deny).
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
		ResourcePattern: repoA,
	})
	if err != nil {
		t.Fatalf("resolve repoA: %v", err)
	}
	if eff.Setting != SettingDeny {
		t.Fatalf("repoA resolve = %q, want deny (per-resource override)", eff.Setting)
	}

	// Resolve with repoB pattern: no row matches, falls back to Base (Allow).
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
		ResourcePattern: repoB,
	})
	if err != nil {
		t.Fatalf("resolve repoB: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("repoB resolve = %q, want allow (no override; base default)", eff.Setting)
	}

	// Clear the repoA override; capability-wide row stays intact.
	if err := s.Clear(ctx, tpTestWorkspaceID, tool, LayerAgent, agent, repoA, pgtype.UUID{}); err != nil {
		t.Fatalf("clear repoA: %v", err)
	}
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
		ResourcePattern: repoA,
	})
	if err != nil {
		t.Fatalf("resolve repoA after clear: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("repoA resolve after clearing per-resource deny = %q, want allow (fell back to base)", eff.Setting)
	}
	// Capability-wide row untouched.
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, AgentID: agent,
	})
	if err != nil {
		t.Fatalf("resolve capability-wide after clearing repoA: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("capability-wide resolve after clearing repoA = %q, want allow (row untouched)", eff.Setting)
	}
}

// TestStore_ListForSubject_CarriesResourcePattern proves ListForSubject returns
// the resource_pattern alongside each (tool, setting) so the admin table can
// group by it. The same (tool, layer, subject) can appear twice — once
// capability-wide and once per-resource — and both must come back.
func TestStore_ListForSubject_CarriesResourcePattern(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	const tool = "repo.checkout"
	const repoA = "github.com/firtal/alpha"
	agent := uuidByte(61)

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, ResourcePattern: "", Setting: SettingAllow,
	}); err != nil {
		t.Fatalf("set capability-wide: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerAgent,
		SubjectID: agent, ResourcePattern: repoA, Setting: SettingDeny,
	}); err != nil {
		t.Fatalf("set per-resource: %v", err)
	}

	rows, err := s.ListForSubject(ctx, tpTestWorkspaceID, LayerAgent, agent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (capability-wide + per-resource)", len(rows))
	}
	got := map[string]Setting{}
	for _, r := range rows {
		got[r.ResourcePattern] = r.Setting
	}
	if got[""] != SettingAllow {
		t.Fatalf("capability-wide row = %q, want allow", got[""])
	}
	if got[repoA] != SettingDeny {
		t.Fatalf("per-resource row %q = %q, want deny", repoA, got[repoA])
	}
}

// TestStore_SystemLayerAndConditionsRoundTrip exercises the FIR-1609 additions
// end-to-end through the DB: a System-layer row carrying a Condition persists,
// reads back with the Condition intact, and the Condition still gates as
// authored. Proves migration 9088 (system layer) + 9089 (conditions column) +
// the store wiring agree.
func TestStore_SystemLayerAndConditionsRoundTrip(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	system := uuidByte(60)
	const tool = "web_fetch"
	cond := &Condition{HostAllowlist: []string{"*.firtal.com"}}

	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID,
		ToolKey:     tool,
		Layer:       LayerSystem,
		SubjectID:   system,
		Setting:     SettingAllow,
		Conditions:  cond,
	}); err != nil {
		t.Fatalf("set system-layer row with condition: %v", err)
	}

	got, err := s.ListForSubject(ctx, tpTestWorkspaceID, LayerSystem, system)
	if err != nil {
		t.Fatalf("list for system subject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	row := got[0]
	if row.Setting != SettingAllow {
		t.Fatalf("setting round-trip: got %s", row.Setting)
	}
	if row.Conditions == nil {
		t.Fatal("condition was lost on round trip")
	}
	if ok, _ := row.Conditions.Matches(RequestContext{Host: "api.firtal.com"}, nil); !ok {
		t.Fatal("restored condition should match an allowed host")
	}
	if ok, _ := row.Conditions.Matches(RequestContext{Host: "evil.com"}, nil); ok {
		t.Fatal("restored condition should reject a non-allowed host")
	}

	// A row with no condition reads back as nil (NULL), not an empty struct.
	plain := uuidByte(61)
	if _, err := s.Set(ctx, SetParams{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerSystem,
		SubjectID: plain, Setting: SettingDeny,
	}); err != nil {
		t.Fatalf("set conditionless row: %v", err)
	}
	plainRows, err := s.ListForSubject(ctx, tpTestWorkspaceID, LayerSystem, plain)
	if err != nil {
		t.Fatalf("list plain: %v", err)
	}
	if len(plainRows) != 1 || plainRows[0].Conditions != nil {
		t.Fatalf("conditionless row should read back with nil Conditions, got %+v", plainRows)
	}
}

// TestStore_SystemLayerResolvesBySubject proves the System layer joins resolution
// keyed on the autopilot id (Query.SystemID): an explicit System-layer Deny is
// applied only to the run that supplies that exact autopilot, and a run with a
// different (or absent) SystemID never picks the row up (FIR-1609, Phase 2).
func TestStore_SystemLayerResolvesBySubject(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	ctx := context.Background()

	autopilot := uuidByte(70)
	agent := uuidByte(71)
	owner := uuidByte(72)
	const tool = "shell"

	// Owner ceiling allows; the System layer for this autopilot denies.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerUser, SubjectID: owner, Setting: SettingAllow}); err != nil {
		t.Fatalf("set user: %v", err)
	}
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: tool, Layer: LayerSystem, SubjectID: autopilot, Setting: SettingDeny}); err != nil {
		t.Fatalf("set system: %v", err)
	}

	// A system run carrying the matching autopilot id loads the System Deny.
	eff, err := s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		AgentID: agent, UserID: owner, SystemID: autopilot, IsSystem: true,
	})
	if err != nil {
		t.Fatalf("resolve matching system run: %v", err)
	}
	if eff.Setting != SettingDeny || eff.DecidedBy != LayerSystem {
		t.Fatalf("matching system run: got setting=%q decidedBy=%q, want deny/system", eff.Setting, eff.DecidedBy)
	}

	// A different autopilot id must NOT pick up the row — subject isolation.
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		AgentID: agent, UserID: owner, SystemID: uuidByte(99), IsSystem: true,
	})
	if err != nil {
		t.Fatalf("resolve other system run: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("other autopilot should not load the System Deny, got %q (%s)", eff.Setting, eff.Reason)
	}

	// A human run (no SystemID) likewise never matches the System row.
	eff, err = s.Resolve(ctx, Query{
		WorkspaceID: tpTestWorkspaceID, ToolKey: tool,
		AgentID: agent, UserID: owner,
	})
	if err != nil {
		t.Fatalf("resolve human run: %v", err)
	}
	if eff.Setting != SettingAllow {
		t.Fatalf("human run should ignore the System row, got %q (%s)", eff.Setting, eff.Reason)
	}
}
