package handler

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestBudgetService_HardKillBlocksClaim verifies that once today's
// workspace spend reaches workspace_daily_hard_kill_cents, CheckPreClaim
// denies further task claims with the kill reason.
func TestBudgetService_HardKillBlocksClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := pgtype.UUID{}
	_ = wsID.Scan(testWorkspaceID)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: load agent: %v", err)
	}
	agentUUID := pgtype.UUID{}
	_ = agentUUID.Scan(agentID)

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_budget_config (workspace_id, workspace_daily_hard_kill_cents, configured_at, configured_by)
		VALUES ($1, 1000, now(), $2)
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: insert config: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_budget_config WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM budget_state WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Headroom: spend below cap → allowed.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO budget_state (workspace_id, scope_type, scope_id, window_type, window_start, cents_spent)
		VALUES ($1, 'workspace', $1, 'day', $2, 500)
	`, testWorkspaceID, dayStart); err != nil {
		t.Fatalf("setup: seed budget state: %v", err)
	}
	if d := testHandler.BudgetService.CheckPreClaim(ctx, wsID, agentUUID); !d.Allowed {
		t.Fatalf("expected allowed at 500/1000, got denied: %s", d.Reason)
	}

	// Bump workspace spend past hard kill → denied.
	if _, err := testPool.Exec(ctx, `
		UPDATE budget_state SET cents_spent = 1000
		WHERE workspace_id = $1 AND scope_type = 'workspace' AND window_type = 'day' AND window_start = $2
	`, testWorkspaceID, dayStart); err != nil {
		t.Fatalf("update budget state: %v", err)
	}
	d := testHandler.BudgetService.CheckPreClaim(ctx, wsID, agentUUID)
	if d.Allowed {
		t.Fatalf("expected denied at hard kill, got allowed")
	}
	if d.Reason != "workspace daily hard kill exceeded" {
		t.Fatalf("expected workspace hard kill reason, got %q", d.Reason)
	}
}

// TestBudgetService_AgentDailyCapBlocksClaim verifies the per-agent
// override / workspace-default cap path. Agent override $5 with $5
// already spent → denied; bump cap to $10 → allowed.
func TestBudgetService_AgentDailyCapBlocksClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := pgtype.UUID{}
	_ = wsID.Scan(testWorkspaceID)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: load agent: %v", err)
	}
	agentUUID := pgtype.UUID{}
	_ = agentUUID.Scan(agentID)

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_budget_config (workspace_id, configured_at, configured_by)
		VALUES ($1, now(), $2)
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: insert config: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_budget_override (workspace_id, agent_id, daily_cap_cents, set_by)
		VALUES ($1, $2, 500, $3)
	`, testWorkspaceID, agentID, testUserID); err != nil {
		t.Fatalf("setup: insert override: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO budget_state (workspace_id, scope_type, scope_id, window_type, window_start, cents_spent)
		VALUES ($1, 'agent', $2, 'day', $3, 500)
	`, testWorkspaceID, agentID, dayStart); err != nil {
		t.Fatalf("setup: seed agent budget: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_budget_config WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_budget_override WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM budget_state WHERE workspace_id = $1`, testWorkspaceID)
	})

	d := testHandler.BudgetService.CheckPreClaim(ctx, wsID, agentUUID)
	if d.Allowed {
		t.Fatalf("expected denied at agent cap, got allowed")
	}
	if d.Reason != "agent daily cap exceeded" {
		t.Fatalf("expected agent cap reason, got %q", d.Reason)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_budget_override SET daily_cap_cents = 1000
		WHERE workspace_id = $1 AND agent_id = $2
	`, testWorkspaceID, agentID); err != nil {
		t.Fatalf("bump cap: %v", err)
	}
	if d := testHandler.BudgetService.CheckPreClaim(ctx, wsID, agentUUID); !d.Allowed {
		t.Fatalf("expected allowed at 500/1000, got denied: %s", d.Reason)
	}
}

// TestBudgetService_NoConfigAllows verifies the staged-rollout
// behavior: workspaces without a config row pass through. Removing
// this gate is contingent on the JEH-327 setup-wizard sub-issue.
func TestBudgetService_NoConfigAllows(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsID := pgtype.UUID{}
	_ = wsID.Scan(testWorkspaceID)

	// Ensure no config row.
	testPool.Exec(ctx, `DELETE FROM workspace_budget_config WHERE workspace_id = $1`, testWorkspaceID)

	if d := testHandler.BudgetService.CheckPreClaim(ctx, wsID, pgtype.UUID{}); !d.Allowed {
		t.Fatalf("expected allowed without config, got denied: %s", d.Reason)
	}
}

