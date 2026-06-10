package runtime

// CEREBRO-PATCH(tool-executor-invoker-test): TECH-3226 — integration tests for
// ToolExecutorInvoker.Invoke: real cascade permission check + real tool execution,
// not mocks. Validates that the invoke path is equivalent to firtal-gateway.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createInvokerTestAgent inserts a minimal agent in the shared test workspace
// and registers a cleanup. Returns the agent's UUID.
func createInvokerTestAgent(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	pool := runtimeAccountTestPool
	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		) VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb)
		RETURNING id
	`, runtimeAccountTestWSID, name, runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent %q: %v", name, err)
	}
	t.Cleanup(func() {
		runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// grantCascadeTool upserts a cerebro_runtime_tool row (enabled) and a user
// grant for runtimeAccountTestUserID, which is what ResolveCerebroAgentToolAccess
// checks when the acting user is not workspace owner/admin.
func grantCascadeTool(t *testing.T, toolName string) {
	t.Helper()
	ctx := context.Background()
	pool := runtimeAccountTestPool
	rid := runtimeAccountTestRuntimeID
	uid := runtimeAccountTestUserID

	// Upsert the tool registration on the runtime (source='cloud', enabled).
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_runtime_tool (runtime_id, tool_name, source, enabled)
		VALUES ($1, $2, 'cloud', true)
		ON CONFLICT (runtime_id, tool_name, mcp_server_name) DO UPDATE SET enabled = true
	`, rid, toolName); err != nil {
		t.Fatalf("upsert cerebro_runtime_tool %q: %v", toolName, err)
	}
	// Grant the tool to the test user directly.
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_runtime_tool_user_grant (runtime_id, tool_name, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (runtime_id, tool_name, user_id) DO NOTHING
	`, rid, toolName, uid); err != nil {
		t.Fatalf("insert user grant for %q: %v", toolName, err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `
			DELETE FROM cerebro_runtime_tool_user_grant
			WHERE runtime_id = $1 AND tool_name = $2 AND user_id = $3
		`, rid, toolName, uid)
		pool.Exec(context.Background(), `
			DELETE FROM cerebro_runtime_tool
			WHERE runtime_id = $1 AND tool_name = $2
		`, rid, toolName)
	})
}

// TestToolExecutorInvoker_ListIssues_RealDB verifies that Invoke runs the real
// list_issues tool against the DB when the cascade grants allow it.
// Sets up cerebro_runtime_tool + user grant (the production cascade path).
func TestToolExecutorInvoker_ListIssues_RealDB(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createInvokerTestAgent(t, "larry-invoke-list-issues")
	grantCascadeTool(t, "list_issues")

	invoker := &ToolExecutorInvoker{
		Queries:        db.New(runtimeAccountTestPool),
		CerebroQueries: cerebrodb.New(runtimeAccountTestPool),
	}

	result, err := invoker.Invoke(
		ctx,
		agentID, runtimeAccountTestWSID,
		pgtype.UUID{},            // userID: zero = agent authorship
		runtimeAccountTestUserID, // cascadeUserID drives the cascade check
		"list_issues",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	var issues []any
	if err := json.Unmarshal([]byte(result), &issues); err != nil {
		t.Fatalf("result is not valid JSON array: %v\nresult: %s", err, result)
	}
	t.Logf("list_issues returned %d issues: %s", len(issues), result)
}

// TestToolExecutorInvoker_ToolNotGranted_ReturnsErrToolNotPermitted verifies
// that Invoke returns ErrToolNotPermitted when the agent has no cascade grant
// for the requested tool.
func TestToolExecutorInvoker_ToolNotGranted_ReturnsErrToolNotPermitted(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Agent with NO grants (no cerebro_runtime_tool and no agent_tool_grant).
	agentID := createInvokerTestAgent(t, "larry-invoke-no-grant")

	invoker := &ToolExecutorInvoker{
		Queries:        db.New(runtimeAccountTestPool),
		CerebroQueries: cerebrodb.New(runtimeAccountTestPool),
	}

	_, err := invoker.Invoke(
		ctx,
		agentID, runtimeAccountTestWSID,
		pgtype.UUID{},
		runtimeAccountTestUserID,
		"list_issues",
		map[string]any{},
	)
	if err == nil {
		t.Fatal("expected ErrToolNotPermitted, got nil")
	}
	if !errors.Is(err, handler.ErrToolNotPermitted) {
		t.Fatalf("expected ErrToolNotPermitted, got: %v", err)
	}
}
