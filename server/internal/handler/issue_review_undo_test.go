package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A review Undo must use the completion response, not a revision fetched after
// another edit. The original status is all it restores.
func TestReviewUndoUsesCommittedRevision(t *testing.T) {
	id := dbfx.Issue(t, "review completion", testutil.Cols{"status": "in_review"})
	var completed IssueResponse
	updateIssueForTest(t, id, map[string]any{"status": "done"}).Want(http.StatusOK).JSON(&completed)
	if completed.Revision < 1 {
		t.Fatal("completion did not return a committed revision")
	}
	var edited IssueResponse
	updateIssueForTest(t, id, map[string]any{"title": "another reviewer edited this", "expected_revision": completed.Revision}).Want(http.StatusOK).JSON(&edited)
	body := updateIssueForTest(t, id, map[string]any{
		"status": "in_review", "expected_revision": completed.Revision, "suppress_run": true,
	}).Want(http.StatusConflict).Map()
	if body["code"] != "revision_conflict" {
		t.Fatalf("conflict=%v", body)
	}
	var status, title string
	var revision int64
	dbfx.QueryRow(t, "SELECT status,title,revision FROM issue WHERE id=$1", id).Scan(&status, &title, &revision)
	if status != "done" || title != edited.Title || revision != edited.Revision {
		t.Fatalf("Undo changed newer row: %s %q %d", status, title, revision)
	}
}

func TestReviewUndoSuppressesRunButKeepsWorkflowHandoff(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Review Undo "+workflowTestSuffix(), "in_review")
	setWorkflowHandoff(t, wf.ID, "in_review", handToAgent(agentID, "Review this")).Want(http.StatusOK)
	projectID := createWorkflowTestProject(t, "review undo handoff")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	for _, suppress := range []bool{true, false} {
		id := dbfx.Issue(t, "completed review", testutil.Cols{"project_id": projectID, "status": "done"})
		var revision int64
		dbfx.QueryRow(t, "SELECT revision FROM issue WHERE id=$1", id).Scan(&revision)
		var restored IssueResponse
		updateIssueForTest(t, id, map[string]any{"status": "in_review", "expected_revision": revision, "suppress_run": suppress}).Want(http.StatusOK).JSON(&restored)
		if restored.Status != "in_review" || restored.AssigneeID == nil || *restored.AssigneeID != agentID {
			t.Fatalf("workflow handoff lost: %+v", restored)
		}
		tasks := taskCountFor(t, id, agentID)
		if suppress && tasks != 0 {
			t.Fatalf("Undo started %d tasks", tasks)
		}
		if !suppress && tasks == 0 {
			t.Fatal("unsuppressed control did not start a task")
		}
	}
}

func TestReviewUndoCannotBypassWorkflowValidation(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	wf := createTestWorkflow(t, "No review step "+workflowTestSuffix())
	projectID := createWorkflowTestProject(t, "review undo rejected")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	id := dbfx.Issue(t, "completed before workflow changed", testutil.Cols{"project_id": projectID, "status": "done"})
	var before int64
	dbfx.QueryRow(t, "SELECT revision FROM issue WHERE id=$1", id).Scan(&before)
	body := updateIssueForTest(t, id, map[string]any{"status": "in_review", "expected_revision": before, "suppress_run": true}).Want(http.StatusBadRequest).Map()
	if body["code"] != "status_not_in_workflow" {
		t.Fatalf("workflow rejection=%v", body)
	}
	var status string
	var after int64
	dbfx.QueryRow(t, "SELECT status,revision FROM issue WHERE id=$1", id).Scan(&status, &after)
	if status != "done" || after != before {
		t.Fatalf("rejected Undo changed issue: %s revision %d", status, after)
	}
}
