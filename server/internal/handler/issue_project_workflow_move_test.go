package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectMoveEntersInitialWorkflowStatus(t *testing.T) {
	seedTestCatalog(t)
	if _, err := issueworkflow.EnsureDefault(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "Project move runtime")
	agentID := fx.Agent(t, "Project move executor", runtimeID)
	projectID := fx.Project(t, "Project move destination")
	workflowID := fx.Insert(t, "issue_workflow", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": projectID, "name": "Destination workflow",
	})
	initialID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": workflowID, "spec_key": "intake", "name": "Intake", "color": "#123456",
		"position": 1, "phase": "started",
		"entry_policy": `{"executor":{"type":"agent","id":"` + agentID + `"},"instructions":"Review the incoming task."}`,
	})
	otherID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": workflowID, "spec_key": "later", "name": "Later", "color": "#123456",
		"position": 0, "phase": "started",
	})
	fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, initialID, workflowID)
	fx.Exec(t, `UPDATE project SET default_issue_workflow_id=$1 WHERE id=$2`, workflowID, projectID)

	sourceProjectID := fx.Project(t, "Source project")
	sourceWorkflowID := fx.Insert(t, "issue_workflow", testutil.Cols{
		"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": sourceProjectID, "name": "Source workflow",
	})
	sourceStatusID := fx.Insert(t, "issue_workflow_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "workflow_id": sourceWorkflowID, "spec_key": "done", "name": "Done", "color": "#123456", "position": 0, "phase": "done", "outcome": "completed",
	})

	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			issueID := fx.Issue(t, "Incoming task", testutil.Cols{"status": "done", "assignee_type": "member", "assignee_id": testUserID})
			if batch {
				fx.Exec(t, `UPDATE issue SET project_id=$1, workflow_id=$2, workflow_status_id=$3 WHERE id=$4`, sourceProjectID, sourceWorkflowID, sourceStatusID, issueID)
			}

			fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id=$1`, issueID)
			fx.Cleanup(t, `DELETE FROM automation_execution WHERE issue_id=$1`, issueID)
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			if batch {
				var result struct {
					Updated int `json:"updated"`
				}
				testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/api/issues/batch", map[string]any{
					"issue_ids": []string{issueID}, "updates": map[string]any{"project_id": projectID},
				})).Want(http.StatusOK).JSON(&result)
				if result.Updated != 1 {
					t.Fatalf("updated %d issues, want 1", result.Updated)
				}
			} else {
				var result IssueResponse
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
					"project_id": projectID,
				}), "id", issueID)).Want(http.StatusOK).JSON(&result)
				if result.WorkflowStatusID == nil || *result.WorkflowStatusID != initialID {
					t.Fatalf("move response status = %v, want initial %s", result.WorkflowStatusID, initialID)
				}
			}
			var actualProject, actualWorkflow, actualStatus, assignee string
			fx.QueryRow(t, `SELECT project_id, workflow_id, workflow_status_id, assignee_id FROM issue WHERE id=$1`, issueID).
				Scan(&actualProject, &actualWorkflow, &actualStatus, &assignee)
			if actualProject != projectID || actualWorkflow != workflowID || actualStatus != initialID || assignee != testUserID {
				t.Fatalf("move binding = %s/%s/%s, assignee=%s", actualProject, actualWorkflow, actualStatus, assignee)
			}
			if count := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND handoff_note='Review the incoming task.' AND automation_execution_id IS NOT NULL`, issueID, agentID); count != 1 {
				t.Fatalf("initial entry runs = %d, want 1", count)
			}
			// Re-selecting the current project preserves progress and does not run again.
			fx.Exec(t, `UPDATE issue SET workflow_status_id=$1 WHERE id=$2`, otherID, issueID)
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
				"project_id": projectID,
			}), "id", issueID)).Want(http.StatusOK)
			fx.QueryRow(t, `SELECT workflow_status_id FROM issue WHERE id=$1`, issueID).Scan(&actualStatus)
			if actualStatus != otherID || fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issueID) != 1 {
				t.Fatal("selecting the same project reset status or duplicated its run")
			}
		})
	}
	t.Run("active entry is superseded only after a successful move", func(t *testing.T) {
		issueID := fx.Issue(t, "Active incoming task")
		fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id=$1`, issueID)
		fx.Cleanup(t, `DELETE FROM automation_execution WHERE issue_id=$1`, issueID)
		fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
		move := func(project any, revision *int64, want int) {
			body := map[string]any{"project_id": project}
			if revision != nil {
				body["expected_workflow_revision"] = *revision
			}
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID)).Want(want)
		}
		move(projectID, nil, http.StatusOK)
		var oldTask, oldExecution, oldTransition string
		fx.QueryRow(t, `SELECT t.id, t.automation_execution_id, e.trigger_transition_id FROM agent_task_queue t JOIN automation_execution e ON e.id=t.automation_execution_id WHERE t.issue_id=$1`, issueID).Scan(&oldTask, &oldExecution, &oldTransition)
		// Different projects may share a workflow; the move still starts a fresh entry.
		nextProject := fx.Project(t, "Shared workflow destination")
		fx.Exec(t, `UPDATE project SET default_issue_workflow_id=$1 WHERE id=$2`, workflowID, nextProject)
		assertUnchanged := func() {
			t.Helper()
			if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND project_id=$2 AND last_transition_id=$3`, issueID, projectID, oldTransition) != 1 ||
				fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='queued'`, oldTask) != 1 ||
				fx.Count(t, `SELECT count(*) FROM automation_execution WHERE id=$1 AND status='queued'`, oldExecution) != 1 ||
				fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1`, issueID) != 1 {
				t.Fatal("failed move changed the project, transition, execution, or task")
			}
		}
		staleRevision := int64(999)
		move(nextProject, &staleRevision, http.StatusConflict)
		assertUnchanged()
		fx.Exec(t, `UPDATE agent SET archived_at=NOW() WHERE id=$1`, agentID)
		move(nextProject, nil, http.StatusConflict)
		assertUnchanged()
		fx.Exec(t, `UPDATE agent SET archived_at=NULL WHERE id=$1`, agentID)
		move(nextProject, nil, http.StatusOK)
		if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='cancelled'`, oldTask) != 1 ||
			fx.Count(t, `SELECT count(*) FROM automation_execution WHERE id=$1 AND status='superseded'`, oldExecution) != 1 ||
			fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='queued'`, issueID) != 1 {
			t.Fatal("successful move did not replace the old workflow entry run")
		}
		// Removing the project enters the workspace default workflow's initial node.
		move(nil, nil, http.StatusOK)
		if fx.Count(t, `SELECT count(*) FROM issue i JOIN issue_workflow w ON w.id=i.workflow_id WHERE i.id=$1 AND i.project_id IS NULL AND w.scope_type='workspace' AND i.workflow_status_id=w.initial_status_id`, issueID) != 1 {
			t.Fatal("removing the project did not enter the workspace initial status")
		}
	})

}
