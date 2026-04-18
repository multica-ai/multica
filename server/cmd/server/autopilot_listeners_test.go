package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedAutopilotFixture creates a minimal autopilot + run pair for testing.
// Returns (autopilotID, runID) as UUID strings.
// The autopilot is linked to the integration-test workspace's owner agent.
func seedAutopilotFixture(t *testing.T, ctx context.Context, workspaceID, creatorID string) (string, string) {
	t.Helper()

	// Find an existing agent in this workspace to assign to. TestMain seeds one.
	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, workspaceID).Scan(&agentID); err != nil {
		t.Fatalf("seedAutopilotFixture: no agent in workspace: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_id,
			priority, status, execution_mode,
			created_by_type, created_by_id
		) VALUES (
			$1, 'test autopilot', 'desc', $2,
			'medium', 'active', 'create_issue',
			'member', $3
		)
		RETURNING id
	`, workspaceID, agentID, creatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seedAutopilotFixture: insert autopilot: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (
			autopilot_id, source, status, trigger_payload
		) VALUES (
			$1, 'manual', 'issue_created', '{}'::jsonb
		)
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("seedAutopilotFixture: insert run: %v", err)
	}

	return autopilotID, runID
}

// seedIssueForAutopilot inserts an issue and optionally sets gitlab_iid.
// Returns the sqlc-loaded db.Issue.
func seedIssueForAutopilot(t *testing.T, ctx context.Context, workspaceID, creatorID string, gitlabIid int32) db.Issue {
	t.Helper()

	var gitlabIidParam any
	if gitlabIid > 0 {
		gitlabIidParam = gitlabIid
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority,
			creator_type, creator_id, position,
			gitlab_iid
		) VALUES (
			$1, 'autopilot test issue', 'todo', 'medium',
			'member', $2, 0,
			$3
		)
		RETURNING id
	`, workspaceID, creatorID, gitlabIidParam).Scan(&issueID); err != nil {
		t.Fatalf("seedIssueForAutopilot: %v", err)
	}

	queries := db.New(testPool)
	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("seedIssueForAutopilot: GetIssue: %v", err)
	}
	return issue
}

// cleanupAutopilotFixture removes issue, run, autopilot in correct FK order.
func cleanupAutopilotFixture(t *testing.T, ctx context.Context, issueID, runID, autopilotID string) {
	t.Helper()
	testPool.Exec(ctx, `DELETE FROM autopilot_issue WHERE autopilot_run_id = $1`, runID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM autopilot_run WHERE id = $1`, runID)
	testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
}

// TestAutopilotSyncRunFromIssue_PrefersAutopilotIssueMapping verifies that when
// an autopilot_issue mapping (workspace_id + gitlab_iid) exists, the listener
// resolves the run through it even if origin_type is NOT set on the issue.
// This is the Phase 4 path — future-facing, for issues created via GitLab
// write-through that do not carry an origin marker.
func TestAutopilotSyncRunFromIssue_PrefersAutopilotIssueMapping(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	autopilotID, runID := seedAutopilotFixture(t, ctx, testWorkspaceID, testUserID)

	// Issue has gitlab_iid; mapping links it to the autopilot run.
	issue := seedIssueForAutopilot(t, ctx, testWorkspaceID, testUserID, 800)

	// Seed the Phase 4 mapping row.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO autopilot_issue (autopilot_run_id, workspace_id, gitlab_iid)
		VALUES ($1, $2, $3)
	`, runID, testWorkspaceID, 800); err != nil {
		t.Fatalf("insert autopilot_issue mapping: %v", err)
	}

	defer cleanupAutopilotFixture(t, ctx, util.UUIDToString(issue.ID), runID, autopilotID)

	// Drive the status through the service. Move issue to done then sync.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, util.UUIDToString(issue.ID)); err != nil {
		t.Fatalf("update issue status: %v", err)
	}

	queries := db.New(testPool)
	reloaded, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}

	bus := events.New()
	svc := service.NewAutopilotService(queries, testPool, bus, nil)

	svc.SyncRunFromIssue(ctx, reloaded)

	// Expect the run to be completed.
	var runStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot_run WHERE id = $1`, runID).Scan(&runStatus); err != nil {
		t.Fatalf("read run status: %v", err)
	}
	if runStatus != "completed" {
		t.Fatalf("expected run status 'completed' via autopilot_issue mapping, got %q", runStatus)
	}
}

// TestAutopilotSyncRunFromIssue_NoAssociationNoOp verifies that an issue with
// no autopilot_issue mapping stays a no-op — no spurious updates, no panic.
func TestAutopilotSyncRunFromIssue_NoAssociationNoOp(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	autopilotID, runID := seedAutopilotFixture(t, ctx, testWorkspaceID, testUserID)

	// Plain issue — no autopilot_issue mapping.
	issue := seedIssueForAutopilot(t, ctx, testWorkspaceID, testUserID, 0)

	defer cleanupAutopilotFixture(t, ctx, util.UUIDToString(issue.ID), runID, autopilotID)

	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, util.UUIDToString(issue.ID)); err != nil {
		t.Fatalf("update issue status: %v", err)
	}

	queries := db.New(testPool)
	reloaded, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}

	bus := events.New()
	svc := service.NewAutopilotService(queries, testPool, bus, nil)

	svc.SyncRunFromIssue(ctx, reloaded)

	// Run should still be 'issue_created' — no change.
	var runStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot_run WHERE id = $1`, runID).Scan(&runStatus); err != nil {
		t.Fatalf("read run status: %v", err)
	}
	if runStatus != "issue_created" {
		t.Fatalf("expected run status unchanged ('issue_created'), got %q", runStatus)
	}
}

