package runtime

// JEH-1710 bid 5 — integration tests for Registry.GetCascadeEnabledToolsForAgent.
//
// Exercises the cerebro_runtime_tool grant cascade against a real Postgres,
// covering the access rule from migration 9032: default-deny unless the user
// is a workspace owner/admin OR has a user_grant OR is in a group with a
// group_grant; an agent-level override row beats the runtime default.
//
// All test data is created and torn down per-subtest so the cascade subtests
// don't leak state between each other. Uses the shared TestMain fixture
// (runtimeAccountTestPool / WSID / UserID).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// stubTool is a no-op Tool used to populate the in-memory registry so the
// cascade method has something to filter against.
type stubTool struct{ name string }

func (s stubTool) Name() string                                                  { return s.name }
func (s stubTool) Description() string                                           { return "stub " + s.name }
func (s stubTool) InputSchema() map[string]any                                   { return map[string]any{"type": "object"} }
func (s stubTool) Call(_ context.Context, _ map[string]any) (string, error)      { return "", nil }

// cascadeFixture builds an agent + runtime + tool registry rows + grants for
// one subtest. Cleanup deletes everything in reverse order, so concurrent
// subtests can run against the same workspace without colliding (each gets
// its own agent and grants).
type cascadeFixture struct {
	t          *testing.T
	registry   *Registry
	cerebro    *cerebrodb.Queries
	runtimeID  pgtype.UUID
	agentID    pgtype.UUID
	userID     pgtype.UUID
	adminID    pgtype.UUID
	otherID    pgtype.UUID
	groupID    pgtype.UUID
	cleanupSQL []string
}

func newCascadeFixture(t *testing.T) *cascadeFixture {
	t.Helper()
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping cascade integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	wsID := runtimeAccountTestWSID

	f := &cascadeFixture{
		t:       t,
		cerebro: cerebrodb.New(pool),
	}

	// Dedicated runtime so we can list all tools for it without picking up
	// rows from other tests.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info)
		VALUES ($1, $2, $3, 'local', 'claude', 'online', '')
		RETURNING id`,
		wsID, "cascade-test-daemon-"+t.Name(), "Cascade Test "+t.Name(),
	).Scan(&f.runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	f.cleanupSQL = append(f.cleanupSQL, "DELETE FROM agent_runtime WHERE id = $1")

	// Agent bound to that runtime.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, status, visibility, runtime_id, owner_id)
		VALUES ($1, $2, 'local', 'idle', 'private', $3, $4)
		RETURNING id`,
		wsID, "cascade-agent-"+t.Name(), f.runtimeID, runtimeAccountTestUserID,
	).Scan(&f.agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	f.cleanupSQL = append(f.cleanupSQL, "DELETE FROM agent WHERE id = $1")

	// Three users: the shared TestMain user is the workspace owner via the
	// initial seed; we also need a non-admin user and a second non-admin
	// (for negative cases). We make the shared user the admin reference and
	// create two members with role=member.
	f.adminID = runtimeAccountTestUserID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Cascade User '||$1, 'cascade-user-'||$1||'@multica.test')
		RETURNING id`,
		t.Name(),
	).Scan(&f.userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Cascade Other '||$1, 'cascade-other-'||$1||'@multica.test')
		RETURNING id`,
		t.Name(),
	).Scan(&f.otherID); err != nil {
		t.Fatalf("create other user: %v", err)
	}

	// Register the two as workspace members with role=member; the admin user
	// is already a member from TestMain seed but the cascade SQL needs the
	// role to be 'owner' or 'admin' for the admin arm to fire. Upsert to
	// 'owner' to make the assertion deterministic.
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
		ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = 'owner'`,
		wsID, f.adminID,
	); err != nil {
		t.Fatalf("seed admin member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		wsID, f.userID,
	); err != nil {
		t.Fatalf("seed user member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		wsID, f.otherID,
	); err != nil {
		t.Fatalf("seed other member: %v", err)
	}
	f.cleanupSQL = append(f.cleanupSQL, "DELETE FROM member WHERE user_id = $1")

	// A group containing the non-admin user (for the group-grant arm).
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_group (workspace_id, name) VALUES ($1, $2)
		RETURNING id`,
		wsID, "cascade-group-"+t.Name(),
	).Scan(&f.groupID); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_group_member (group_id, user_id) VALUES ($1, $2)`,
		f.groupID, f.userID,
	); err != nil {
		t.Fatalf("add user to group: %v", err)
	}
	f.cleanupSQL = append(f.cleanupSQL, "DELETE FROM cerebro_group WHERE id = $1")

	// In-memory registry with three stub tools so the cascade has something
	// concrete to return.
	f.registry = &Registry{
		tools: make(map[string]Tool),
		db:    pool,
	}
	f.registry.Register(stubTool{name: "alpha"})
	f.registry.Register(stubTool{name: "beta"})
	f.registry.Register(stubTool{name: "gamma"})

	t.Cleanup(func() {
		// Reverse order: child rows before parents.
		ctx := context.Background()
		// Clean grants/overrides/runtime_tool by runtime to keep the cleanup
		// simple even when subtests added them.
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_agent_runtime_tool_override WHERE agent_id = $1`, f.agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_runtime_tool_user_grant WHERE runtime_id = $1`, f.runtimeID)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_runtime_tool_group_grant WHERE runtime_id = $1`, f.runtimeID)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_runtime_tool WHERE runtime_id = $1`, f.runtimeID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_tool_grant WHERE agent_id = $1`, f.agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_group_member WHERE group_id = $1`, f.groupID)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_group WHERE id = $1`, f.groupID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, f.agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, f.userID)
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE user_id = $1`, f.otherID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, f.runtimeID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1 OR id = $2`, f.userID, f.otherID)
	})

	return f
}

// addRuntimeTool inserts a cerebro_runtime_tool row enabled by default.
func (f *cascadeFixture) addRuntimeTool(toolName string, enabled bool) {
	f.t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO cerebro_runtime_tool (runtime_id, tool_name, source, enabled)
		VALUES ($1, $2, 'cloud', $3)`,
		f.runtimeID, toolName, enabled,
	); err != nil {
		f.t.Fatalf("add runtime_tool %s: %v", toolName, err)
	}
}

func (f *cascadeFixture) addUserGrant(toolName string, userID pgtype.UUID) {
	f.t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO cerebro_runtime_tool_user_grant (runtime_id, tool_name, user_id)
		VALUES ($1, $2, $3)`,
		f.runtimeID, toolName, userID,
	); err != nil {
		f.t.Fatalf("add user_grant %s: %v", toolName, err)
	}
}

func (f *cascadeFixture) addGroupGrant(toolName string, groupID pgtype.UUID) {
	f.t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO cerebro_runtime_tool_group_grant (runtime_id, tool_name, group_id)
		VALUES ($1, $2, $3)`,
		f.runtimeID, toolName, groupID,
	); err != nil {
		f.t.Fatalf("add group_grant %s: %v", toolName, err)
	}
}

func (f *cascadeFixture) addOverride(toolName string, enabled bool) {
	f.t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO cerebro_agent_runtime_tool_override (agent_id, tool_name, enabled)
		VALUES ($1, $2, $3)`,
		f.agentID, toolName, enabled,
	); err != nil {
		f.t.Fatalf("add override %s: %v", toolName, err)
	}
}

func (f *cascadeFixture) addLegacyGrant(toolName string) {
	f.t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO agent_tool_grant (agent_id, tool_name, enabled) VALUES ($1, $2, true)`,
		f.agentID, toolName,
	); err != nil {
		f.t.Fatalf("add legacy grant %s: %v", toolName, err)
	}
}

func toolNames(tools []Tool) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		out[i] = tool.Name()
	}
	return out
}

func TestCascadeGetEnabledToolsForAgent_FallsBackWhenNoRuntimeToolsConfigured(t *testing.T) {
	f := newCascadeFixture(t)
	// No cerebro_runtime_tool rows; legacy grant on "alpha". HasCerebroRuntime
	// returns false, so cascade should defer to the legacy path.
	f.addLegacyGrant("alpha")

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if want := []string{"alpha"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("legacy fallback tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_AdminBypassReturnsAllEnabled(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addRuntimeTool("beta", true)
	f.addRuntimeTool("gamma", false) // not enabled — must NOT appear even for admin

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.adminID)

	if want := []string{"alpha", "beta"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("admin tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_NonAdminWithoutGrantsIsDeniedByDefault(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addRuntimeTool("beta", true)

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if len(got) != 0 {
		t.Fatalf("non-admin without grants got tools = %v, want []", toolNames(got))
	}
}

func TestCascadeGetEnabledToolsForAgent_UserGrantUnblocksTool(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addRuntimeTool("beta", true)
	f.addUserGrant("alpha", f.userID)

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if want := []string{"alpha"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("user_grant tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_GroupGrantUnblocksTool(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addRuntimeTool("beta", true)
	f.addGroupGrant("beta", f.groupID)

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if want := []string{"beta"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("group_grant tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_OverrideOffBeatsGrant(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addUserGrant("alpha", f.userID)
	f.addOverride("alpha", false) // tving fra: hide tool from this agent

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if len(got) != 0 {
		t.Fatalf("override-off got tools = %v, want []", toolNames(got))
	}
}

func TestCascadeGetEnabledToolsForAgent_OverrideOnKeepsGrant(t *testing.T) {
	f := newCascadeFixture(t)
	f.addRuntimeTool("alpha", true)
	f.addUserGrant("alpha", f.userID)
	f.addOverride("alpha", true)

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, f.userID)

	if want := []string{"alpha"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("override-on tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_InvalidUserFallsBackToLegacy(t *testing.T) {
	f := newCascadeFixture(t)
	// New system IS configured for this runtime, but the task has no
	// originating user. Fall back to legacy so autopilots don't lose tools.
	f.addRuntimeTool("alpha", true)
	f.addLegacyGrant("beta")

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), f.cerebro, f.agentID, pgtype.UUID{})

	if want := []string{"beta"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("no-user fallback tools = %v, want %v", toolNames(got), want)
	}
}

func TestCascadeGetEnabledToolsForAgent_NilCerebroFallsBack(t *testing.T) {
	f := newCascadeFixture(t)
	f.addLegacyGrant("alpha")

	got := f.registry.GetCascadeEnabledToolsForAgent(context.Background(), nil, f.agentID, f.userID)

	if want := []string{"alpha"}; !equalStringSlice(toolNames(got), want) {
		t.Fatalf("nil cerebro tools = %v, want %v", toolNames(got), want)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
