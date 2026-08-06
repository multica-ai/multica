package runtime

// CEREBRO-PATCH(tool-executor-invoker-test): TECH-3226 — integration tests for
// ToolExecutorInvoker.Invoke: real policy permission check + real tool execution,
// not mocks. Validates that the invoke path is equivalent to firtal-gateway.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type toolFailureHookRecorder struct {
	event  workflows.HookEvent
	result workflows.HookResult
}

func (r *toolFailureHookRecorder) Evaluate(_ context.Context, event workflows.HookEvent) (workflows.HookResult, error) {
	r.event = event
	if r.result.Decision == "" {
		return workflows.HookResult{Evaluated: true, Decision: workflows.HookAllow}, nil
	}
	return r.result, nil
}

func failingToolTask() db.AgentTaskQueue {
	taskID, _ := util.ParseUUID("11111111-1111-1111-1111-111111111111")
	agentID, _ := util.ParseUUID("22222222-2222-2222-2222-222222222222")
	issueID, _ := util.ParseUUID("44444444-4444-4444-4444-444444444444")
	return db.AgentTaskQueue{ID: taskID, AgentID: agentID, IssueID: issueID, Status: "running", Attempt: 2}
}

func TestToolExecutorInvokerRaisesToolFailureEvent(t *testing.T) {
	recorder := &toolFailureHookRecorder{}
	invoker := &ToolExecutorInvoker{Hooks: recorder}
	task := failingToolTask()
	workspaceID, _ := util.ParseUUID("33333333-3333-3333-3333-333333333333")

	invoker.decideToolFailure(context.Background(), task, task.AgentID, workspaceID,
		"web_fetch", map[string]any{"url": "https://example.com"}, errors.New("connection refused"), 1)

	if recorder.event.Type != workflows.HookOnToolFailure || recorder.event.Attempt != 2 {
		t.Fatalf("event = %+v", recorder.event)
	}
	tool := recorder.event.Context["tool"].(map[string]any)
	if tool["name"] != "web_fetch" || tool["error"] != "connection refused" {
		t.Fatalf("tool context = %+v", tool)
	}
	keys := tool["argument_keys"].([]string)
	if len(keys) != 1 || keys[0] != "url" {
		t.Fatalf("argument_keys = %+v", keys)
	}
	if _, leaked := tool["arguments"]; leaked {
		t.Fatal("tool failure event persisted raw arguments")
	}
	if len(recorder.event.MutableFields) == 0 {
		t.Fatal("tool failure event exposed no mutable field, so Workflow cannot decide")
	}
}

// The Workflow decision must reach the caller, not just the audit trail.
func TestToolExecutorInvokerAppliesToolFailureDecision(t *testing.T) {
	task := failingToolTask()
	workspaceID, _ := util.ParseUUID("33333333-3333-3333-3333-333333333333")
	toolErr := errors.New("connection refused")

	for name, testCase := range map[string]struct {
		result workflows.HookResult
		want   workflows.ToolFailureDecision
	}{
		"retry": {
			result: workflows.HookResult{Evaluated: true, Decision: workflows.HookModify,
				Modifications: map[string]any{"failure_action": "retry", "retry_limit": 2}},
			want: workflows.ToolFailureDecision{Action: workflows.ToolFailureRetry, RetryLimit: 2, Evaluated: true},
		},
		"pause maps to stop": {
			result: workflows.HookResult{Evaluated: true, Decision: workflows.HookModify,
				Modifications: map[string]any{"failure_action": "pause", "user_message": "wait for the operator"}},
			want: workflows.ToolFailureDecision{Action: workflows.ToolFailureStop, Message: "wait for the operator", Evaluated: true},
		},
		"block stops the call": {
			result: workflows.HookResult{Evaluated: true, Decision: workflows.HookBlock,
				Requirements: []string{"stop calling web_fetch"}},
			want: workflows.ToolFailureDecision{Action: workflows.ToolFailureStop, Message: "stop calling web_fetch", Evaluated: true},
		},
		"no policy surfaces the raw error": {
			result: workflows.HookResult{Decision: workflows.HookAllow},
			want:   workflows.ToolFailureDecision{Action: workflows.ToolFailureSurface},
		},
	} {
		t.Run(name, func(t *testing.T) {
			invoker := &ToolExecutorInvoker{Hooks: &toolFailureHookRecorder{result: testCase.result}}
			got := invoker.decideToolFailure(context.Background(), task, task.AgentID, workspaceID,
				"web_fetch", map[string]any{"url": "https://example.com"}, toolErr, 1)
			if got != testCase.want {
				t.Fatalf("decision = %+v want %+v", got, testCase.want)
			}
		})
	}
}

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

func setInvokerToolPolicy(t *testing.T, agentID pgtype.UUID, toolName string, setting toolpolicy.Setting) {
	t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, setting)
		VALUES ($1, $2, 'agent', $3, $4)
		ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern)
		DO UPDATE SET setting = EXCLUDED.setting
	`, runtimeAccountTestWSID, toolName, agentID, string(setting)); err != nil {
		t.Fatalf("set tool policy %q: %v", toolName, err)
	}
	t.Cleanup(func() {
		runtimeAccountTestPool.Exec(context.Background(), `
			DELETE FROM cerebro_tool_policy
			WHERE workspace_id = $1 AND tool_key = $2 AND layer = 'agent' AND subject_id = $3
		`, runtimeAccountTestWSID, toolName, agentID)
	})
}

// TestToolExecutorInvoker_ListIssues_RealDB verifies that Invoke runs the real
// list_issues tool against the DB when the cascade grants allow it.
// Sets up the canonical tool-policy decision used by the production path.
func TestToolExecutorInvoker_ListIssues_RealDB(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createInvokerTestAgent(t, "larry-invoke-list-issues")
	setInvokerToolPolicy(t, agentID, "list_issues", toolpolicy.SettingAllow)

	invoker := &ToolExecutorInvoker{
		Queries:        db.New(runtimeAccountTestPool),
		CerebroQueries: cerebrodb.New(runtimeAccountTestPool),
		Pool:           runtimeAccountTestPool,
		Policy:         toolpolicy.NewStore(runtimeAccountTestPool),
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

	// Agent with an explicit canonical policy denial.
	agentID := createInvokerTestAgent(t, "larry-invoke-no-grant")
	setInvokerToolPolicy(t, agentID, "list_issues", toolpolicy.SettingDeny)

	invoker := &ToolExecutorInvoker{
		Queries:        db.New(runtimeAccountTestPool),
		CerebroQueries: cerebrodb.New(runtimeAccountTestPool),
		Pool:           runtimeAccountTestPool,
		Policy:         toolpolicy.NewStore(runtimeAccountTestPool),
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

func TestToolExecutorInvoker_AskRequiresApprovalAwarePath(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createInvokerTestAgent(t, "larry-invoke-ask")
	setInvokerToolPolicy(t, agentID, "list_issues", toolpolicy.SettingAsk)

	invoker := &ToolExecutorInvoker{
		Queries:        db.New(runtimeAccountTestPool),
		CerebroQueries: cerebrodb.New(runtimeAccountTestPool),
		Pool:           runtimeAccountTestPool,
		Policy:         toolpolicy.NewStore(runtimeAccountTestPool),
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
	if !errors.Is(err, handler.ErrToolNotPermitted) {
		t.Fatalf("Ask executed through a path without approval context: %v", err)
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
