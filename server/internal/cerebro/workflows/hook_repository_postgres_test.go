package workflows

import (
	"context"
	"errors"
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
			) VALUES ($1, $2, $3, $4, $5, 'warn', $6, 'member')
		`, familyID, fixture.workspaceID, "Versioned policy", version+1, mode, fixture.userID); err != nil {
			t.Fatalf("insert policy version %d: %v", version+1, err)
		}
	}

	policies, err := NewPostgresHookRepository(pool).List(ctx, uuidString(fixture.workspaceID))
	if err != nil {
		t.Fatalf("list policies: %v", err)
	}
	var versioned []HookPolicy
	for _, policy := range policies {
		if policy.Name == "Versioned policy" {
			versioned = append(versioned, policy)
		}
	}
	if len(versioned) != 1 {
		t.Fatalf("versioned policies = %d, want only the latest family version", len(versioned))
	}
	if versioned[0].Version != 2 || versioned[0].Mode != HookModeDryRun {
		t.Fatalf("effective policy = version %d mode %q, want version 2 dry_run", versioned[0].Version, versioned[0].Mode)
	}
}

func TestPostgresHookRepositoryEnsuresManagedMessagePolicies(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	repo := NewPostgresHookRepository(pool)
	workspaceID := uuidString(fixture.workspaceID)

	for call := 0; call < 2; call++ {
		policies, err := repo.List(ctx, workspaceID)
		if err != nil {
			t.Fatalf("list policies call %d: %v", call+1, err)
		}
		var managed []HookPolicy
		for _, policy := range policies {
			if policy.Mode == HookModeManaged {
				managed = append(managed, policy)
			}
		}
		if len(managed) != len(managedHookPolicies(workspaceID)) {
			t.Fatalf("managed policies after call %d = %d, want %d", call+1, len(managed), len(managedHookPolicies(workspaceID)))
		}
		for _, policy := range managed {
			if policy.FailMode != HookFailClosed || len(policy.Bindings) != 1 || policy.Bindings[0].Kind != HookScopeWorkspace || policy.Bindings[0].ID != workspaceID || len(policy.Handlers) != 1 {
				t.Fatalf("managed policy is incomplete: %#v", policy)
			}
		}
	}
}

// An owner may pause any code-defined managed policy, and re-seeding must not
// undo it. Seeding runs once per process, so "survives re-seeding" is the same
// claim as "survives a server restart".
func TestPostgresHookRepositoryKeepsOwnerPauseOnManagedPolicy(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	repo := NewPostgresHookRepository(pool)
	workspaceID := uuidString(fixture.workspaceID)
	target := managedHookPolicies(workspaceID)[0].Policy.ID
	if _, err := repo.List(ctx, workspaceID); err != nil {
		t.Fatalf("seed policies: %v", err)
	}

	member := HookPermissionActor{Type: "member", ID: uuidString(fixture.userID)}
	if _, err := repo.Disable(ctx, workspaceID, member, target); !errors.Is(err, ErrManagedHookLocked) {
		t.Fatalf("non-owner disable error = %v, want ErrManagedHookLocked", err)
	}

	owner := HookPermissionActor{Type: "member", ID: uuidString(fixture.userID), IsOwner: true}
	paused, err := repo.Disable(ctx, workspaceID, owner, target)
	if err != nil {
		t.Fatalf("owner disable: %v", err)
	}
	if paused.Mode != HookModeOff {
		t.Fatalf("mode after disable = %q, want %q", paused.Mode, HookModeOff)
	}

	repo.managedMu.Lock()
	delete(repo.managedWorkspaces, workspaceID)
	repo.managedMu.Unlock()
	policies, err := repo.List(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list after re-seed: %v", err)
	}
	for _, policy := range policies {
		if policy.ID == target && policy.Mode != HookModeOff {
			t.Fatalf("mode after re-seed = %q, want %q", policy.Mode, HookModeOff)
		}
	}
}

func TestPostgresHookRepositoryRetiresOpenFailModeOnWrite(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	fixture := setupWorkflowIntegrationFixture(t, pool)
	repo := NewPostgresHookRepository(pool)
	policy := newTestHookPolicy("", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: ""})
	policy.FailMode = HookFailMode("open")

	created, err := repo.Create(ctx, uuidString(fixture.workspaceID), HookPermissionActor{Type: "member", ID: uuidString(fixture.userID)}, policy)
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if created.FailMode != HookFailWarn {
		t.Fatalf("fail mode = %q, want %q", created.FailMode, HookFailWarn)
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
