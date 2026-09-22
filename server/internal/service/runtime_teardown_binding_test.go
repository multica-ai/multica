package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestTeardownRuntimePromotesNextBinding pins invariant I14: deleting a runtime
// removes it from every agent's ordered pool and promotes each affected agent's
// next binding, so an agent with a fallback runtime stays bound and its
// autopilot keeps running. Only an agent whose pool is emptied by the delete is
// unbound and has its autopilot paused, exactly as a single-runtime agent was
// before the pool existed.
func TestTeardownRuntimePromotesNextBinding(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	userID := insertBindingTeardownRow(t, pool, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Binding Teardown", fmt.Sprintf("binding-teardown-%d@multica.ai", suffix))
	workspaceID := insertBindingTeardownRow(t, pool, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'BTD') RETURNING id`,
		"Binding Teardown", fmt.Sprintf("binding-teardown-%d", suffix))
	execBindingTeardown(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	runtimeA := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "Runtime A")
	runtimeB := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "Runtime B")

	// agentPromote runs on A first, B second. Deleting A must promote it to B.
	agentPromote := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "Agent Promote", runtimeA)
	insertBinding(t, pool, workspaceID, agentPromote, runtimeA, 0)
	insertBinding(t, pool, workspaceID, agentPromote, runtimeB, 1)
	autopilotPromote := insertAutopilotForBindingTeardown(t, pool, workspaceID, agentPromote, userID)

	// agentUnbind has only A. Deleting A must unbind it and pause its autopilot.
	agentUnbind := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "Agent Unbind", runtimeA)
	insertBinding(t, pool, workspaceID, agentUnbind, runtimeA, 0)
	autopilotUnbind := insertAutopilotForBindingTeardown(t, pool, workspaceID, agentUnbind, userID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	result, err := TeardownRuntime(ctx, db.New(tx), util.MustParseUUID(runtimeA), RuntimeTeardownOptions{CancelNonTerminalTasks: true})
	if err != nil {
		t.Fatalf("TeardownRuntime: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Only the emptied agent is reported unbound.
	if got := teardownAgentIDs(result.UnboundAgents); len(got) != 1 || got[0] != agentUnbind {
		t.Errorf("UnboundAgents = %v, want [%s]", got, agentUnbind)
	}
	// Only the emptied agent's autopilot is paused.
	if len(result.PausedAutopilots) != 1 || util.UUIDToString(result.PausedAutopilots[0].ID) != autopilotUnbind {
		t.Errorf("PausedAutopilots = %d autopilots, want only %s", len(result.PausedAutopilots), autopilotUnbind)
	}

	// agentPromote is now bound to B, keeps its B binding, and is not unbound.
	if got := scanString(t, pool, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentPromote); got != runtimeB {
		t.Errorf("agentPromote runtime_id = %s, want %s (promoted to B)", got, runtimeB)
	}
	if n := scanInt(t, pool, `SELECT count(*) FROM agent_runtime_binding WHERE agent_id = $1`, agentPromote); n != 1 {
		t.Errorf("agentPromote binding count = %d, want 1 (only B remains)", n)
	}
	if n := scanInt(t, pool, `SELECT count(*) FROM agent_runtime_binding WHERE agent_id = $1 AND runtime_id = $2`, agentPromote, runtimeB); n != 1 {
		t.Errorf("agentPromote must still be bound to B")
	}
	if got := scanString(t, pool, `SELECT status FROM autopilot WHERE id = $1`, autopilotPromote); got != "active" {
		t.Errorf("agentPromote autopilot status = %s, want active", got)
	}

	// agentUnbind has no runtime and no bindings; its autopilot is paused.
	if got := scanString(t, pool, `SELECT COALESCE(runtime_id::text, '') FROM agent WHERE id = $1`, agentUnbind); got != "" {
		t.Errorf("agentUnbind runtime_id = %q, want NULL", got)
	}
	if n := scanInt(t, pool, `SELECT count(*) FROM agent_runtime_binding WHERE agent_id = $1`, agentUnbind); n != 0 {
		t.Errorf("agentUnbind binding count = %d, want 0", n)
	}
	if got := scanString(t, pool, `SELECT status FROM autopilot WHERE id = $1`, autopilotUnbind); got != "paused" {
		t.Errorf("agentUnbind autopilot status = %s, want paused", got)
	}

	// The deleted runtime is gone from every pool.
	if n := scanInt(t, pool, `SELECT count(*) FROM agent_runtime_binding WHERE runtime_id = $1`, runtimeA); n != 0 {
		t.Errorf("runtime A still referenced by %d bindings", n)
	}
}

func insertBindingTeardownRow(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("seed row: %v\nSQL: %s", err, sql)
	}
	return id
}

func execBindingTeardown(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("seed exec: %v\nSQL: %s", err, sql)
	}
}

func insertRuntimeForBindingTeardown(t *testing.T, pool *pgxpool.Pool, workspaceID, userID, name string) string {
	t.Helper()
	id := insertBindingTeardownRow(t, pool, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'binding_teardown_test', 'online', '', '{}'::jsonb, now(), 'private', $3)
		RETURNING id`, workspaceID, name, userID)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, id) })
	return id
}

func insertAgentForBindingTeardown(t *testing.T, pool *pgxpool.Pool, workspaceID, userID, name, runtimeID string) string {
	t.Helper()
	id := insertBindingTeardownRow(t, pool, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id`, workspaceID, name, runtimeID, userID)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id) })
	return id
}

func insertBinding(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, runtimeID string, priority int) {
	t.Helper()
	execBindingTeardown(t, pool, `
		INSERT INTO agent_runtime_binding (workspace_id, agent_id, runtime_id, priority)
		VALUES ($1, $2, $3, $4)`, workspaceID, agentID, runtimeID, priority)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_runtime_binding WHERE agent_id = $1 AND runtime_id = $2`, agentID, runtimeID)
	})
}

func insertAutopilotForBindingTeardown(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, creatorID string) string {
	t.Helper()
	id := insertBindingTeardownRow(t, pool, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'binding-teardown-ap', $2, 'run_only', 'member', $3)
		RETURNING id`, workspaceID, agentID, creatorID)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, id) })
	return id
}

func teardownAgentIDs(agents []db.Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = util.UUIDToString(a.ID)
	}
	return out
}

func scanString(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("scan string: %v\nSQL: %s", err, sql)
	}
	return s
}

func scanInt(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("scan int: %v\nSQL: %s", err, sql)
	}
	return n
}
