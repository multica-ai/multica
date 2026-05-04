package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestResolveTaskTokenScope_DefaultsToTask covers the steady state:
// member toggle ON → strict per-task scope, regardless of how the
// task got created.
func TestResolveTaskTokenScope_DefaultsToTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsUUID := pgtype.UUID{}
	_ = wsUUID.Scan(testWorkspaceID)
	userUUID := pgtype.UUID{}
	_ = userUUID.Scan(testUserID)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: load agent: %v", err)
	}
	agentUUID := pgtype.UUID{}
	_ = agentUUID.Scan(agentID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id)
		VALUES ($1, 'scope toggle default test 9008', 'todo', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	issueUUID := pgtype.UUID{}
	_ = issueUUID.Scan(issueID)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	task := db.AgentTaskQueue{
		AgentID:           agentUUID,
		IssueID:           issueUUID,
		TriggeredByUserID: userUUID,
	}
	if got := testHandler.resolveTaskTokenScope(ctx, task); got != "task" {
		t.Fatalf("expected 'task' scope (toggle on), got %q", got)
	}
}

// TestResolveTaskTokenScope_ToggleOffPromotesToUser verifies the
// JEH-324 admin opt-out: member.scope_enforcement_enabled=false →
// minted token is scope='user'.
func TestResolveTaskTokenScope_ToggleOffPromotesToUser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	wsUUID := pgtype.UUID{}
	_ = wsUUID.Scan(testWorkspaceID)
	userUUID := pgtype.UUID{}
	_ = userUUID.Scan(testUserID)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("setup: load agent: %v", err)
	}
	agentUUID := pgtype.UUID{}
	_ = agentUUID.Scan(agentID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id)
		VALUES ($1, 'scope toggle off test', 'todo', 'member', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	issueUUID := pgtype.UUID{}
	_ = issueUUID.Scan(issueID)

	if _, err := testPool.Exec(ctx, `
		UPDATE member SET scope_enforcement_enabled = false
		WHERE workspace_id = $1 AND user_id = $2
	`, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("setup: flip toggle: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE member SET scope_enforcement_enabled = true WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	task := db.AgentTaskQueue{
		AgentID:           agentUUID,
		IssueID:           issueUUID,
		TriggeredByUserID: userUUID,
	}
	if got := testHandler.resolveTaskTokenScope(ctx, task); got != "user" {
		t.Fatalf("expected 'user' scope (toggle off), got %q", got)
	}
}

// TestResolveTaskTokenScope_NoTriggeringUserStaysTask covers system
// triggers (autopilot, scheduled jobs) — no user attribution → strict
// scope, never the wide fallback.
func TestResolveTaskTokenScope_NoTriggeringUserStaysTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	task := db.AgentTaskQueue{
		AgentID:           pgtype.UUID{Valid: false},
		TriggeredByUserID: pgtype.UUID{Valid: false},
	}
	if got := testHandler.resolveTaskTokenScope(ctx, task); got != "task" {
		t.Fatalf("expected 'task' scope (no triggering user), got %q", got)
	}
}
