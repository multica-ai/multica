package workflows

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// poolEvalGateStore is a test double for the EvalGateStore seam. It runs the same
// latest-verdict query as evals.Store.LatestRunPassed directly against the pool,
// so the workflows test package need not import evals (which would cycle through
// loops → workflows). The store SQL itself is covered by evals.TestLatestRunPassed.
type poolEvalGateStore struct{ pool *pgxpool.Pool }

func (s poolEvalGateStore) LatestRunPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error) {
	var passed bool
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((
      SELECT r.status = 'passed' FROM cerebro_eval_run r
      WHERE r.workspace_id=$1 AND r.eval_id=$2 AND r.issue_id=$3
      ORDER BY r.created_at DESC LIMIT 1
    ), FALSE)`, workspaceID, evalID, issueID).Scan(&passed)
	return passed, err
}

type allowHookActions struct{}

func (allowHookActions) Can(context.Context, string, HookPermissionActor, HookPermission) bool {
	return true
}
func (allowHookActions) CanAction(context.Context, string, HookPermissionActor, string) bool {
	return true
}

func TestWorkflowHookRunSkillEnqueuesAttachedSkillWithIssueContext(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, fixture, "Run delivery skill", "in_progress", 1, pgtype.UUID{})

	var runtimeID, agentID, skillID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status) VALUES ($1,'Hook runtime','cloud','codex','online') RETURNING id`, fixture.workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_id,status) VALUES ($1,'Hook builder','cloud',$2,'idle') RETURNING id`, fixture.workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO skill (workspace_id,name,description,content,created_by) VALUES ($1,'verification-before-completion','Verify before completion','# Verify',$2) RETURNING id`, fixture.workspaceID, fixture.userID).Scan(&skillID); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_skill (agent_id,skill_id) VALUES ($1,$2)`, agentID, skillID); err != nil {
		t.Fatalf("attach skill: %v", err)
	}

	policy := newTestHookPolicy("skill-policy", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: uuidString(fixture.workspaceID)})
	policy.CreatedByType = "member"
	policy.CreatedByID = uuidString(fixture.userID)
	policy.Handlers[0].Actions = []HookAction{{Type: "skill.run", Config: map[string]any{"skill_name": "verification-before-completion", "agent_id": uuidString(agentID)}}}
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, NewPostgresHookActionExecutor(pool, allowHookActions{}, nil))
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(ctx, HookEvent{
		EventID: "skill-event", Type: HookBeforeTaskComplete, WorkspaceID: uuidString(fixture.workspaceID), IssueID: uuidString(issueID), AgentID: uuidString(agentID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionSuccess {
		t.Fatalf("action result = %#v", result.ActionResults)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE agent_id=$1 AND issue_id=$2 ORDER BY created_at DESC LIMIT 1`, agentID, issueID).Scan(&payload); err != nil {
		t.Fatalf("read queued skill task: %v", err)
	}
	var contextData map[string]any
	if err := json.Unmarshal(payload, &contextData); err != nil {
		t.Fatal(err)
	}
	if contextData["workflow_skill_name"] != "verification-before-completion" || contextData["workflow_target_issue_id"] != uuidString(issueID) {
		t.Fatalf("queued context = %#v", contextData)
	}
}

func TestWorkflowHookJudgeGateCreatesSharedLoopVerdictContract(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, fixture, "Judge delivery", "in_review", 1, pgtype.UUID{})

	var runtimeID, judgeAgentID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status) VALUES ($1,'Judge runtime','cloud','codex','online') RETURNING id`, fixture.workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_id,status) VALUES ($1,'Hook judge','cloud',$2,'idle') RETURNING id`, fixture.workspaceID, runtimeID).Scan(&judgeAgentID); err != nil {
		t.Fatalf("insert judge: %v", err)
	}

	policy := newTestHookPolicy("judge-policy", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: uuidString(issueID)})
	policy.CreatedByType = "member"
	policy.CreatedByID = uuidString(fixture.userID)
	policy.Handlers[0].Actions = []HookAction{{Type: "judge.gate", Config: map[string]any{
		"agent_id": uuidString(judgeAgentID), "rubric": "Every acceptance criterion passes", "check_id": "delivery",
	}}}
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, NewPostgresHookActionExecutor(pool, allowHookActions{}, nil))
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(ctx, HookEvent{
		EventID: "judge-event", Type: HookBeforeTaskComplete, WorkspaceID: uuidString(fixture.workspaceID), IssueID: uuidString(issueID), Attempt: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionSuccess {
		t.Fatalf("action result = %#v", result.ActionResults)
	}

	var gate, checkID, rubric string
	var round int32
	var ran, pass bool
	if err := pool.QueryRow(ctx, `SELECT gate,round,check_id,rubric,ran,pass FROM cerebro_loop_judge_run WHERE issue_id=$1`, issueID).Scan(&gate, &round, &checkID, &rubric, &ran, &pass); err != nil {
		t.Fatalf("read judge contract: %v", err)
	}
	if gate != "hook:judge-policy:judge-event" || round != 2 || checkID != "delivery" || rubric != "Every acceptance criterion passes" || ran || pass {
		t.Fatalf("judge contract gate=%q round=%d check=%q rubric=%q ran=%v pass=%v", gate, round, checkID, rubric, ran, pass)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE agent_id=$1 AND issue_id=$2 ORDER BY created_at DESC LIMIT 1`, judgeAgentID, issueID).Scan(&payload); err != nil {
		t.Fatalf("read judge task: %v", err)
	}
	var contextData map[string]any
	if err := json.Unmarshal(payload, &contextData); err != nil {
		t.Fatal(err)
	}
	if contextData["judge_gate"] != gate || contextData["judge_check_id"] != checkID {
		t.Fatalf("judge task context = %#v", contextData)
	}
}

// seedGateEval inserts an active eval plus a single run of the given status for
// the issue, so eval.gate reads that run as the latest verdict.
func seedGateEval(t *testing.T, pool *pgxpool.Pool, f workflowIntegrationFixture, issueID pgtype.UUID, key, runStatus string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var evalID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO cerebro_eval
		(workspace_id, eval_key, version, title, objective, target, status, created_by_id)
		VALUES ($1,$2,'1.0.0','Gate eval','Objective','{}'::jsonb,'active',$3) RETURNING id`,
		f.workspaceID, key, f.userID).Scan(&evalID); err != nil {
		t.Fatalf("insert eval: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cerebro_eval_run
		(workspace_id, eval_id, eval_version, issue_id, status, results, created_by_id, created_by_type)
		SELECT $1, e.id, e.version, $3, $4, '{}'::jsonb, $5, 'member'
		FROM cerebro_eval e WHERE e.workspace_id=$1 AND e.id=$2`,
		f.workspaceID, evalID, issueID, runStatus, f.userID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	return evalID
}

func runEvalGate(t *testing.T, pool *pgxpool.Pool, f workflowIntegrationFixture, issueID, evalID pgtype.UUID) HookResult {
	t.Helper()
	policy := newTestHookPolicy("eval-gate-policy", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: uuidString(issueID)})
	policy.FailMode = HookFailClosed
	policy.CreatedByType = "member"
	policy.CreatedByID = uuidString(f.userID)
	policy.Handlers[0].Actions = []HookAction{{Type: "eval.gate", Config: map[string]any{"eval_id": uuidString(evalID)}}}
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, NewPostgresHookActionExecutor(pool, allowHookActions{}, poolEvalGateStore{pool}))
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(context.Background(), HookEvent{
		EventID: "eval-gate-" + uuidString(issueID), Type: HookBeforeTaskComplete, WorkspaceID: uuidString(f.workspaceID), IssueID: uuidString(issueID),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEvalGateActionBlocksOnFailedRun(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	f := setupWorkflowIntegrationFixture(t, pool)

	t.Run("failed latest run blocks fail-closed", func(t *testing.T) {
		issueID := insertWorkflowIntegrationIssue(t, pool, f, "Gate blocks", "in_progress", 11, pgtype.UUID{})
		evalID := seedGateEval(t, pool, f, issueID, "gate-failed", "failed")
		result := runEvalGate(t, pool, f, issueID, evalID)
		if len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionFailed {
			t.Fatalf("action result = %#v", result.ActionResults)
		}
		if result.ActionResults[0].Result["passed"] != false {
			t.Fatalf("expected passed:false, got %#v", result.ActionResults[0].Result)
		}
		if result.Decision != HookBlock {
			t.Fatalf("decision = %q, want block", result.Decision)
		}
	})

	t.Run("passed latest run proceeds", func(t *testing.T) {
		issueID := insertWorkflowIntegrationIssue(t, pool, f, "Gate passes", "in_progress", 12, pgtype.UUID{})
		evalID := seedGateEval(t, pool, f, issueID, "gate-passed", "passed")
		result := runEvalGate(t, pool, f, issueID, evalID)
		if len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionSuccess {
			t.Fatalf("action result = %#v", result.ActionResults)
		}
		if result.ActionResults[0].Result["passed"] != true {
			t.Fatalf("expected passed:true, got %#v", result.ActionResults[0].Result)
		}
		if result.Decision == HookBlock {
			t.Fatal("passing gate must not block")
		}
	})
}
