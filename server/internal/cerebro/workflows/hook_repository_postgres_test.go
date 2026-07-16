package workflows

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresHookRepositoryListReturnsOnlyLatestPolicyVersionPerFamily(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	familyID := uuid.New()

	for version, mode := range []string{"enforce", "dry_run"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO cerebro_workflow_hook_policy (
				family_id, workspace_id, name, policy_version, mode,
				fail_mode, created_by_id, created_by_type
			) VALUES ($1, $2, $3, $4, $5, 'open', $6, 'member')
		`, familyID, fixture.workspaceID, "Versioned policy", version+1, mode, fixture.userID); err != nil {
			t.Fatalf("insert policy version %d: %v", version+1, err)
		}
	}

	policies, err := NewPostgresHookRepository(pool).List(ctx, uuidString(fixture.workspaceID))
	if err != nil {
		t.Fatalf("list policies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("effective policies = %d, want only the latest family version", len(policies))
	}
	if policies[0].Version != 2 || policies[0].Mode != HookModeDryRun {
		t.Fatalf("effective policy = version %d mode %q, want version 2 dry_run", policies[0].Version, policies[0].Mode)
	}
}

func TestPostgresHookRepositoryRunsReturnsCompleteExplanation(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	repo := NewPostgresHookRepository(pool)
	policy := newTestHookPolicy("", HookRequire, HookModeDryRun, HookBinding{Kind: HookScopeProject, ID: uuid.NewString()})
	policy.FailMode = HookFailWarn
	policy.Conditions = []Condition{{Field: "issue.status", Op: "eq", Value: "in_review"}}
	created, err := repo.Create(ctx, uuidString(fixture.workspaceID), HookPermissionActor{Type: "member", ID: uuidString(fixture.userID)}, policy)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	run := HookRunRecord{
		PolicyID: created.ID, PolicyVersion: created.Version,
		Event:       HookEvent{EventID: "event-explanation", Type: HookBeforeTaskComplete, WorkspaceID: uuidString(fixture.workspaceID), IssueID: uuid.NewString()},
		SourceScope: policy.Bindings[0], LatencyMS: 17,
		Result: HookResult{Decision: HookAllow, WouldDecision: HookRequire, MatchedConditions: policy.Conditions, Requirements: []string{"Add delivery evidence"}},
	}
	if err := repo.RecordRun(ctx, uuidString(fixture.workspaceID), run); err != nil {
		t.Fatalf("record run: %v", err)
	}
	runs, err := repo.Runs(ctx, uuidString(fixture.workspaceID), created.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.FailMode != HookFailWarn || !reflect.DeepEqual(got.Result.MatchedConditions, policy.Conditions) || !reflect.DeepEqual(got.Result.Requirements, []string{"Add delivery evidence"}) {
		t.Fatalf("explanation = %#v", got)
	}
}
