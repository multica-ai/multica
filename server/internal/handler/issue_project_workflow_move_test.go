package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectMoveRequiresMappingAndDoesNotEnterAutomation(t *testing.T) {
	seedTestCatalog(t)
	base, err := issueworkflow.EnsureDefault(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtime := fx.Runtime(t, "Move fixture")
	agent := fx.Agent(t, "Destination executor", runtime)
	project := fx.Project(t, "Move destination")
	workflow := fx.Insert(t, "issue_workflow", testutil.Cols{"workspace_id": testWorkspaceID, "scope_type": "project", "scope_id": project, "name": "Destination"})
	initial := fx.Insert(t, "issue_workflow_status", testutil.Cols{"workspace_id": testWorkspaceID, "workflow_id": workflow, "spec_key": "intake", "name": "Intake", "color": "#123456", "position": 0, "phase": "unstarted"})
	target := fx.Insert(t, "issue_workflow_status", testutil.Cols{"workspace_id": testWorkspaceID, "workflow_id": workflow, "spec_key": "building", "name": "Building", "color": "#123456", "position": 1, "phase": "started", "entry_policy": `{"executor":{"type":"agent","id":"` + agent + `"},"instructions":"Run on real entry"}`})
	fx.Exec(t, `UPDATE issue_workflow SET initial_status_id=$1 WHERE id=$2`, initial, workflow)
	fx.Exec(t, `UPDATE project SET default_issue_workflow_id=$1 WHERE id=$2`, workflow, project)
	for _, batch := range []bool{false, true} {
		name := "single"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			id := fx.Issue(t, "Move", testutil.Cols{"workflow_id": base.ID, "workflow_status_id": base.InitialStatusID})
			fx.Cleanup(t, `DELETE FROM issue_transition WHERE issue_id=$1`, id)
			if !batch {
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": project}), "id", id)).Want(409)
			}
			if batch {
				var out struct {
					Updated int `json:"updated"`
				}
				testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPost, "/", map[string]any{"issue_ids": []string{id}, "updates": map[string]any{"project_id": project, "workflow_status_id": target}})).Want(200).JSON(&out)
				if out.Updated != 1 {
					t.Fatalf("updated=%d", out.Updated)
				}
			} else {
				testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": project, "workflow_status_id": target}), "id", id)).Want(200)
			}
			if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND project_id=$2 AND workflow_id=$3 AND workflow_status_id=$4`, id, project, workflow, target) != 1 {
				t.Fatal("move ignored selected target")
			}
			if fx.Count(t, `SELECT count(*) FROM issue_transition WHERE issue_id=$1 AND cause='project_moved'`, id) != 1 {
				t.Fatal("missing move history")
			}
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, id) != 0 || fx.Count(t, `SELECT count(*) FROM automation_execution WHERE issue_id=$1`, id) != 0 {
				t.Fatal("move started automation")
			}
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": project}), "id", id)).Want(200)
			if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND workflow_status_id=$2`, id, target) != 1 {
				t.Fatal("reselecting project reset progress")
			}
			task := fx.Task(t, agent, testutil.Cols{"issue_id": id, "runtime_id": runtime, "status": "queued"})
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": nil, "workflow_status_id": uuidToString(base.InitialStatusID)}), "id", id)).Want(409)
			if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='queued'`, task) != 1 {
				t.Fatal("blocked move cancelled work")
			}
			fx.Exec(t, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, task)
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": nil, "workflow_status_id": uuidToString(base.InitialStatusID), "expected_workflow_revision": 999}), "id", id)).Want(409)
			if fx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND project_id=$2`, id, project) != 1 {
				t.Fatal("stale move partially committed")
			}
			testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest(http.MethodPut, "/", map[string]any{"project_id": nil, "workflow_status_id": uuidToString(base.InitialStatusID)}), "id", id)).Want(200)
		})
	}
}
