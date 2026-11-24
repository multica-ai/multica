package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestIssueBlockerBacklogPromotionIsConsumedOnce(t *testing.T) {
	seedTestCatalog(t)
	agent := seededReadyAgentID(t)
	prerequisite := blockerTestIssue(t, map[string]any{"status": "todo"})
	issue := blockerTestIssue(t, map[string]any{"status": "backlog", "blocked_by": []string{prerequisite.ID}, "assignee_type": "agent", "assignee_id": agent})
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	tick := func() {
		t.Helper()
		if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
			t.Fatal(err)
		}
	}
	updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "done"}).Want(200)
	tick()
	updateIssueForTest(t, issue.ID, map[string]any{"status": "todo"}).Want(200)
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue.ID); n != 1 {
		t.Fatalf("promotion queued %d runs, want one", n)
	}
	var taskID string
	dbfx.QueryRow(t, "SELECT id::text FROM agent_task_queue WHERE issue_id=$1", issue.ID).Scan(&taskID)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='dispatched' WHERE id=$1", taskID)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.JoinWaitingWakeups(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='running',started_at=now() WHERE id=$1", taskID)
	updateIssueForTest(t, issue.ID, map[string]any{"status": "in_review"}).Want(200)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE issue_id=$1", issue.ID)
	tick()
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue.ID); n != 1 {
		t.Fatalf("completed promotion was dispatched again: got %d runs, want one", n)
	}
}
