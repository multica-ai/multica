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
	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
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
		pgtype.UUID{},            // taskID: not task-scoped
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
		pgtype.UUID{},
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

func TestToolExecutorInvoker_OpenLoopStepUsesTaskScopedCapabilityWithoutGrant(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	agentID := createInvokerTestAgent(t, "larry-open-loop-step")
	var issueID, workflowID, taskID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Open loop step', 'member', $2) RETURNING id
	`, runtimeAccountTestWSID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_workflow (
			workspace_id, name, trigger_type, trigger_config, conditions,
			action_type, action_config, created_by_id, created_by_type
		) VALUES ($1, 'Open loop step', 'status_changed', '{}', '[]', 'set_status', '{}', $2, 'member')
		RETURNING id
	`, runtimeAccountTestWSID, runtimeAccountTestUserID).Scan(&workflowID); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	store := cerebroloops.NewStore(pool)
	limits := cerebroloops.PhaseLimits{MaxSteps: 3, MaxRounds: 1, NoProgressStalls: 1}
	current := cerebroloops.StepRef{
		PhaseRunKey: cerebroloops.PhaseRunKey{IssueID: issueID, WorkflowID: workflowID, PhaseID: "build"},
		BlockID:     "build", Number: 1,
	}
	if _, _, err := store.OpenStep(ctx, current, limits); err != nil {
		t.Fatalf("open current step: %v", err)
	}
	taskContext, _ := json.Marshal(map[string]any{
		"type": "workflow_block",
		"loop_step": map[string]any{
			"workflow_id": util.UUIDToString(workflowID), "phase_id": "build", "block_id": "build", "step_number": 1,
			"steps": cerebroloops.StepsConfig{Allowed: true, Max: 2}, "phase_limits": limits,
		},
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
		VALUES ($1, $2, $3, 'queued', 0, $4) RETURNING id
	`, agentID, runtimeAccountTestRuntimeID, issueID, taskContext).Scan(&taskID); err != nil {
		t.Fatalf("create workflow task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM cerebro_workflow WHERE id = $1`, workflowID)
		pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	invoker := &ToolExecutorInvoker{
		Queries: db.New(pool), CerebroQueries: cerebrodb.New(pool), Pool: pool, LoopStore: store,
	}
	if _, err := invoker.Invoke(ctx, agentID, runtimeAccountTestWSID, pgtype.UUID{}, runtimeAccountTestUserID, pgtype.UUID{}, "open_loop_step", map[string]any{}); !errors.Is(err, handler.ErrToolNotPermitted) {
		t.Fatalf("open_loop_step without its task capability was not denied: %v", err)
	}
	result, err := invoker.Invoke(ctx, agentID, runtimeAccountTestWSID, pgtype.UUID{}, runtimeAccountTestUserID, taskID, "open_loop_step", map[string]any{})
	if err != nil {
		t.Fatalf("invoke task-scoped open_loop_step: %v", err)
	}
	var opened map[string]any
	if err := json.Unmarshal([]byte(result), &opened); err != nil {
		t.Fatalf("decode open result: %v", err)
	}
	if opened["step_number"] != float64(2) {
		t.Fatalf("opened result = %v", opened)
	}
	steps, err := store.ListSteps(ctx, current.PhaseRunKey)
	if err != nil || len(steps) != 2 || steps[1].Number != 2 {
		t.Fatalf("durable opened steps = %+v err=%v", steps, err)
	}
}
