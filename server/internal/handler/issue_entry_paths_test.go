package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueStatusEntryPathsApplyPolicyAtomically(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "Entry paths runtime")
	executorID := fx.Agent(t, "Entry executor", runtimeID)
	assigneeID := fx.Agent(t, "Independent assignee", runtimeID)
	projectID := fx.Project(t, "Entry paths")
	workflowID := fx.Insert(t, "issue_workflow", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": projectID, "name": "Entry paths",
	})
	initialID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": workflowID, "spec_key": "build", "name": "Build", "phase": "started", "position": 0, "color": "#123456",
	})
	policy := `{"executor":{"type":"agent","id":"` + executorID + `"},"instructions":"Review this entry."}`
	reviewID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": workflowID, "spec_key": "review", "name": "Review", "phase": "started", "position": 1, "color": "#123456", "entry_policy": policy,
	})
	doneID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": workflowID, "spec_key": "done", "name": "Done", "phase": "done", "outcome": "completed", "position": 2, "color": "#123456", "legacy_status_key": "done", "entry_policy": policy,
	})
	fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, initialID, workflowID)
	fx.Exec(t, `UPDATE project SET default_issue_workflow_id=$1 WHERE id=$2`, workflowID, projectID)
	create := func(t *testing.T) IssueResponse {
		t.Helper()
		var issue IssueResponse
		testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": t.Name(), "project_id": projectID, "assignee_type": "agent", "assignee_id": assigneeID,
		})).Want(http.StatusCreated).JSON(&issue)
		t.Cleanup(func() {
			testutil.Call(t, testHandler.DeleteIssue, withURLParam(newRequest(http.MethodDelete, "/api/issues/"+issue.ID, nil), "id", issue.ID)).Want(http.StatusNoContent)
		})
		return issue
	}
	for _, path := range []string{"native", "generic node", "legacy key", "batch key", "github"} {
		t.Run(path, func(t *testing.T) {
			issue := create(t)
			targetID := doneID
			if path == "native" || path == "generic node" {
				targetID = reviewID
			}
			change := func() {
				t.Helper()
				switch path {
				case "native":
					testutil.Call(t, testHandler.TransitionIssueStatusNode, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issue.ID+"/transitions", map[string]any{"workflow_status_id": targetID}), "id", issue.ID)).Want(http.StatusOK)
				case "generic node", "legacy key":
					body := map[string]any{"status": "done", "suppress_run": true}
					if path == "generic node" {
						body = map[string]any{"workflow_status_id": targetID}
					}
					testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, body), "id", issue.ID)).Want(http.StatusOK)
				case "batch key":
					var out struct {
						Updated int `json:"updated"`
					}
					testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{"issue_ids": []string{issue.ID}, "updates": map[string]any{"status": "done"}})).Want(http.StatusOK).JSON(&out)
					if out.Updated != 1 {
						t.Fatalf("updated = %d", out.Updated)
					}
				case "github":
					current, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issue.ID))
					if err != nil {
						t.Fatal(err)
					}
					testHandler.advanceIssueToDone(context.Background(), current, testWorkspaceID)
				}
			}
			change()
			change() // Repeating the same state must not create another entry or task.
			var statusID, owner string
			var revision int64
			fx.QueryRow(t, `SELECT workflow_status_id, assignee_id, revision FROM issue WHERE id=$1`, issue.ID).Scan(&statusID, &owner, &revision)
			if statusID != targetID || owner != assigneeID || revision <= issue.Revision {
				t.Fatalf("status=%s assignee=%s revision=%d", statusID, owner, revision)
			}
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue.ID) != 1 ||
				fx.Count(t, `SELECT count(*) FROM agent_task_queue t JOIN automation_execution e ON e.id=t.automation_execution_id WHERE t.issue_id=$1 AND t.agent_id=$2 AND t.handoff_note='Review this entry.' AND e.status_id=$3 AND e.status='queued'`, issue.ID, executorID, targetID) != 1 {
				t.Fatal("entry did not create exactly one configured run")
			}
			// Returning to the manual initial node supersedes the active entry.
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{"workflow_status_id": initialID}), "id", issue.ID)).Want(http.StatusOK)
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='cancelled'`, issue.ID) != 1 {
				t.Fatal("old entry was not cancelled")
			}
			change()
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue.ID) != 2 {
				t.Fatal("re-entry did not create a new run")
			}
		})
	}
	for _, path := range []string{"native", "generic node", "legacy key", "batch key", "project move"} {
		t.Run("triage proposal has no entry executor/"+path, func(t *testing.T) {
			issue := create(t)
			fx.Exec(t, `UPDATE issue SET triage_state='pending' WHERE id=$1`, issue.ID)
			targetID := doneID
			switch path {
			case "native":
				targetID = reviewID
				testutil.Call(t, testHandler.TransitionIssueStatusNode, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issue.ID+"/transitions", map[string]any{"workflow_status_id": targetID}), "id", issue.ID)).Want(http.StatusOK)
			case "generic node", "legacy key":
				body := map[string]any{"status": "done"}
				if path == "generic node" {
					targetID = reviewID
					body = map[string]any{"workflow_status_id": targetID}
				}
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, body), "id", issue.ID)).Want(http.StatusOK)
			case "batch key":
				testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{"issue_ids": []string{issue.ID}, "updates": map[string]any{"status": "done"}})).Want(http.StatusOK)
			case "project move":
				targetID = reviewID
				fx.Exec(t, `UPDATE issue SET project_id=NULL WHERE id=$1`, issue.ID)
				fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, reviewID, workflowID)
				defer fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, initialID, workflowID)
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{"project_id": projectID}), "id", issue.ID)).Want(http.StatusOK)
			}
			current, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issue.ID))
			if err != nil {
				t.Fatal(err)
			}
			if current.WorkflowStatusID != parseUUID(targetID) || current.TriageState.String != "pending" {
				t.Fatalf("proposal status=%v triage=%v", current.WorkflowStatusID, current.TriageState)
			}
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue.ID) != 0 ||
				fx.Count(t, `SELECT count(*) FROM automation_execution WHERE issue_id=$1 AND executor_type IS NOT NULL`, issue.ID) != 0 {
				t.Fatal("triage proposal started a workflow executor")
			}
		})
	}
	t.Run("unavailable policy rolls back the entire generic patch", func(t *testing.T) {
		issue := create(t)
		fx.Exec(t, `UPDATE agent SET archived_at=NOW() WHERE id=$1`, executorID)
		defer fx.Exec(t, `UPDATE agent SET archived_at=NULL WHERE id=$1`, executorID)
		testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{"workflow_status_id": reviewID, "title": "must roll back"}), "id", issue.ID)).Want(http.StatusConflict)
		current, err := testHandler.Queries.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{ID: parseUUID(issue.ID), WorkspaceID: parseUUID(testWorkspaceID)})
		if err != nil {
			t.Fatal(err)
		}
		if current.Title != issue.Title || current.Revision != issue.Revision || current.WorkflowStatusID != parseUUID(initialID) {
			t.Fatalf("failed entry changed issue: %#v", current)
		}
		if fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1`, issue.ID) != 1 {
			t.Fatal("failed entry left transition history")
		}
	})
}
