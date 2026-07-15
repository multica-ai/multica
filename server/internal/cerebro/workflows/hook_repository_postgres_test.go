package workflows

import (
	"context"
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
