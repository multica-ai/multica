package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWorkflowHandoffToSquadStartsLeaderAtEachStep(t *testing.T) {
	seedTestCatalog(t)
	fx := newSquadBriefingClaimFixture(t, t.Context(), "Workflow squad handoff")
	wf := createTestWorkflow(t, "Squad flow "+workflowTestSuffix(), "in_progress", "in_review")
	projectID := createWorkflowTestProject(t, "Squad handoff project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE issue SET project_id=$2,status='todo' WHERE id=$1`, fx.IssueID, projectID)

	for _, status := range []string{"in_progress", "in_review"} {
		instruction := "Coordinate only the " + status + " step."
		setWorkflowHandoff(t, wf.ID, status, map[string]any{
			"handler": map[string]any{"type": "squad", "id": fx.SquadID}, "instructions": instruction,
			"next_status_key": "done", "back_status_key": "todo",
		}).Want(http.StatusOK)

		var preview WorkflowHandoffPreviewResponse
		req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+fx.IssueID+"/workflow-handoff?status="+status, nil), "id", fx.IssueID)
		testutil.Call(t, testHandler.PreviewWorkflowHandoff, req).Want(http.StatusOK).JSON(&preview)
		if !preview.Handoff || preview.HandlerType != "squad" || preview.HandlerID != fx.SquadID || !strings.Contains(preview.Brief, instruction) {
			t.Fatalf("squad handoff preview = %+v", preview)
		}

		updateIssueForTest(t, fx.IssueID, map[string]any{"status": status}).Want(http.StatusOK)
		_, assigneeType, assigneeID := issueStatusAndAssignee(t, fx.IssueID)
		if assigneeType != "squad" || assigneeID != fx.SquadID {
			t.Fatalf("handoff assignee = %s/%s, want squad/%s", assigneeType, assigneeID, fx.SquadID)
		}
		tasks, err := testHandler.Queries.ListActiveTasksByIssue(t.Context(), parseUUID(fx.IssueID))
		if err != nil || len(tasks) != 1 {
			t.Fatalf("active tasks = %+v, %v; want one leader run", tasks, err)
		}
		task := tasks[0]
		if task.AgentID != parseUUID(fx.AgentID) || task.SquadID != parseUUID(fx.SquadID) || !task.IsLeaderTask || task.WorkflowStep.String != status {
			t.Fatalf("handoff lost leader role or workflow binding: %+v", task)
		}
		claim := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+fx.RuntimeID+"/tasks/claim", nil), "runtimeId", fx.RuntimeID)
		claim.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityWorkflowStatusRulesV1)
		var resp struct {
			Task *AgentTaskResponse `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, claim).Want(http.StatusOK).JSON(&resp)
		if resp.Task == nil || resp.Task.ID != uuidToString(task.ID) || !resp.Task.IsLeaderTask || resp.Task.Agent == nil || resp.Task.ProjectWorkflow == nil {
			t.Fatalf("missing leader workflow claim: %+v", resp.Task)
		}
		if !strings.Contains(resp.Task.Agent.Instructions, "## Squad Roster") || !strings.Contains(resp.Task.Agent.Instructions, "current step's goal") || !strings.Contains(resp.Task.ProjectWorkflow.Brief, instruction) {
			t.Fatalf("claim lost squad coordination or step instructions: %+v", resp.Task)
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, task.ID)
	}
}

func TestWorkflowSquadHandoffKeepsAssignmentGates(t *testing.T) {
	seedTestCatalog(t)
	wf := createTestWorkflow(t, "Squad permissions "+workflowTestSuffix(), "in_progress")
	leaderID := seededReadyAgentID(t)
	otherWS := dbfx.Workspace(t, "Other squad workspace", "squad-handoff-"+workflowTestSuffix())
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	cases := []struct {
		name, squadID string
		status        int
	}{
		{"another workspace", dbfx.Squad(t, "Foreign squad", leaderID, testutil.Cols{"workspace_id": otherWS}), http.StatusBadRequest},
		{"archived squad", dbfx.Squad(t, "Archived squad", leaderID, testutil.Cols{"archived_at": time.Now()}), http.StatusBadRequest},
		{"private leader", dbfx.Squad(t, "Private leader squad", privateLeaderID), http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{
				"handler": map[string]any{"type": "squad", "id": tc.squadID},
			}).Want(tc.status)
		})
	}
}
