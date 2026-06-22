package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func loadIssueRow(t *testing.T, ctx context.Context, id string) db.Issue {
	t.Helper()
	wsUUID := parseUUID(testWorkspaceID)
	issueUUID := parseUUID(id)
	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		t.Fatalf("load issue %s: %v", id, err)
	}
	return issue
}

func pendingTaskCount(t *testing.T, ctx context.Context, issueID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issueID, agentID,
	).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

func cancelledTaskCount(t *testing.T, ctx context.Context, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'cancelled'`,
		issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count cancelled tasks: %v", err)
	}
	return n
}

func TestReconcileAfterUpdate_AssignBranchCancelsAndEnqueues(t *testing.T) {
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "assign-reconcile-agent", nil)

	issueID := createTestIssue(t, "Assign Reconcile Test", "todo", "medium")
	prev := loadIssueRow(t, ctx, issueID)

	agentUUID := parseUUID(agentID)
	params := db.UpdateIssueParams{
		ID:           prev.ID,
		Title:        pgtype.Text{String: prev.Title, Valid: true},
		Status:       pgtype.Text{String: prev.Status, Valid: true},
		Priority:     pgtype.Text{String: prev.Priority, Valid: true},
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentUUID,
	}

	result, err := testHandler.IssueService.Update(ctx, prev, params, nil, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !result.Changes.AssigneeChanged {
		t.Fatal("expected AssigneeChanged=true")
	}

	testHandler.IssueService.ReconcileAfterUpdate(ctx, result, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})

	if n := pendingTaskCount(t, ctx, issueID, agentID); n == 0 {
		t.Fatal("expected a task enqueued for the assigned agent")
	}
}

func TestReconcileAfterUpdate_BacklogPromotionEnqueues(t *testing.T) {
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "promote-reconcile-agent", nil)

	issueID := createTestIssue(t, "Promote Reconcile Test", "backlog", "low")

	agentUUID := parseUUID(agentID)
	if _, err := testPool.Exec(ctx,
		`UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2`,
		agentUUID, issueID,
	); err != nil {
		t.Fatalf("assign agent via SQL: %v", err)
	}

	prev := loadIssueRow(t, ctx, issueID)

	params := db.UpdateIssueParams{
		ID:           prev.ID,
		Title:        pgtype.Text{String: prev.Title, Valid: true},
		Status:       pgtype.Text{String: "todo", Valid: true},
		Priority:     pgtype.Text{String: prev.Priority, Valid: true},
		AssigneeType: prev.AssigneeType,
		AssigneeID:   prev.AssigneeID,
	}

	result, err := testHandler.IssueService.Update(ctx, prev, params, nil, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !result.Changes.StatusChanged {
		t.Fatal("expected StatusChanged=true")
	}
	if result.Changes.AssigneeChanged {
		t.Fatal("expected AssigneeChanged=false (agent was pre-assigned)")
	}

	testHandler.IssueService.ReconcileAfterUpdate(ctx, result, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})

	if n := pendingTaskCount(t, ctx, issueID, agentID); n == 0 {
		t.Fatal("expected a task enqueued after backlog→todo promotion")
	}
}

func TestReconcileAfterUpdate_CancelBranchCancelsTasks(t *testing.T) {
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "cancel-reconcile-agent", nil)

	issueID := createTestIssue(t, "Cancel Reconcile Test", "todo", "medium")
	createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	prev := loadIssueRow(t, ctx, issueID)

	params := db.UpdateIssueParams{
		ID:       prev.ID,
		Title:    pgtype.Text{String: prev.Title, Valid: true},
		Status:   pgtype.Text{String: "cancelled", Valid: true},
		Priority: pgtype.Text{String: prev.Priority, Valid: true},
	}

	result, err := testHandler.IssueService.Update(ctx, prev, params, nil, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !result.Changes.StatusChanged {
		t.Fatal("expected StatusChanged=true")
	}

	testHandler.IssueService.ReconcileAfterUpdate(ctx, result, service.IssueUpdateOpts{
		ActorType: "member",
		ActorID:   testUserID,
	})

	if n := cancelledTaskCount(t, ctx, issueID); n == 0 {
		t.Fatal("expected the running task to be cancelled after issue cancelled")
	}
}
