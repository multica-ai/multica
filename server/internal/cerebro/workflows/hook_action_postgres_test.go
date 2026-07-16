package workflows

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

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
	registerVersionOneHookActions(registry, NewPostgresHookActionExecutor(pool, allowHookActions{}))
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
