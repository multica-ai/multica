package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWorkflowSquadHandoffReplayDoesNotDuplicate(t *testing.T) {
	seedTestCatalog(t)
	fx := newSquadBriefingClaimFixture(t, t.Context(), "Idempotent squad handoff")
	projectID := createWorkflowTestProject(t, "Idempotent squad project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": createTestWorkflow(t, "Idempotent flow "+workflowTestSuffix(), "in_progress").ID}).Want(http.StatusOK)
	setColumnHandoff(t, projectID, "in_progress", map[string]any{"handler": map[string]any{"type": "squad", "id": fx.SquadID}}).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE issue SET project_id=$2,status='todo' WHERE id=$1`, fx.IssueID, projectID)
	updateIssueForTest(t, fx.IssueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(fx.IssueID))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := testHandler.TaskService.EnqueueTaskForWorkflowSquadLeader(t.Context(), issue, parseUUID(fx.AgentID), parseUUID(fx.SquadID), "", parseUUID(testUserID))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, service.ErrDuplicatePendingTask) {
			t.Fatalf("same handoff replay = %v, want coalesced", err)
		}
	}
	tasks, err := testHandler.Queries.ListTasksByIssue(t.Context(), issue.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("handoff replay created duplicate tasks: %+v, %v", tasks, err)
	}
	// A run may advance its binding itself; the original handoff identity
	// still refers to the source status write, not the mutable run binding.
	dbfx.Exec(t, `UPDATE agent_task_queue SET workflow_step='todo' WHERE id=$1`, tasks[0].ID)
	if _, err := testHandler.TaskService.EnqueueTaskForWorkflowSquadLeader(t.Context(), issue, parseUUID(fx.AgentID), parseUUID(fx.SquadID), "", parseUUID(testUserID)); !errors.Is(err, service.ErrDuplicatePendingTask) {
		t.Fatalf("advanced handoff replay = %v, want coalesced", err)
	}

	// Re-entering a stage is new work even if an earlier visit is still pending.
	updateIssueForTest(t, fx.IssueID, map[string]any{"status": "todo"}).Want(http.StatusOK)
	updateIssueForTest(t, fx.IssueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	tasks, err = testHandler.Queries.ListTasksByIssue(t.Context(), issue.ID)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("a new visit to the same stage was coalesced: %+v, %v", tasks, err)
	}
}

func TestWorkflowSquadHandoffPreservesPendingStages(t *testing.T) {
	seedTestCatalog(t)
	for _, sharedLeader := range []bool{false, true} {
		for _, oldStatus := range []string{"queued", "dispatched", "running"} {
			t.Run(fmt.Sprintf("sharedLeader=%v/old=%s", sharedLeader, oldStatus), func(t *testing.T) {
				fx := newSquadBriefingClaimFixture(t, t.Context(), "Pending stage handoff")
				nextSquad := fx.SquadID
				if sharedLeader {
					nextSquad = dbfx.Squad(t, "Next stage squad", fx.AgentID)
				}
				wf := createTestWorkflow(t, "Pending squad flow "+workflowTestSuffix(), "in_progress", "in_review")
				projectID := createWorkflowTestProject(t, "Pending squad project")
				setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
				for status, squad := range map[string]string{"in_progress": fx.SquadID, "in_review": nextSquad} {
					setWorkflowHandoff(t, wf.ID, status, map[string]any{
						"handler": map[string]any{"type": "squad", "id": squad}, "instructions": "Handle only " + status,
					}).Want(http.StatusOK)
				}
				dbfx.Exec(t, `UPDATE issue SET project_id=$2,status='todo' WHERE id=$1`, fx.IssueID, projectID)
				updateIssueForTest(t, fx.IssueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
				old, err := testHandler.Queries.ListTasksByIssue(t.Context(), parseUUID(fx.IssueID))
				if err != nil || len(old) != 1 {
					t.Fatalf("first handoff tasks = %+v, %v", old, err)
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET status=$2 WHERE id=$1`, old[0].ID, oldStatus)

				// Enter the next stage while the old run still occupies the queue.
				updateIssueForTest(t, fx.IssueID, map[string]any{"status": "in_review"}).Want(http.StatusOK)
				tasks, err := testHandler.Queries.ListTasksByIssue(t.Context(), parseUUID(fx.IssueID))
				if err != nil || len(tasks) != 2 {
					t.Fatalf("second handoff tasks = %+v, %v; want both stages persisted", tasks, err)
				}
				var nextID string
				for _, task := range tasks {
					if task.ID == old[0].ID {
						if task.Status != oldStatus || task.WorkflowStep.String != "in_progress" || task.SquadID != parseUUID(fx.SquadID) {
							t.Fatalf("handoff changed the existing run: %+v", task)
						}
						continue
					}
					if task.WorkflowStep.String != "in_review" || task.SquadID != parseUUID(nextSquad) || !task.IsLeaderTask {
						t.Fatalf("new stage lost its execution context: %+v", task)
					}
					nextID = uuidToString(task.ID)
				}

				// Releasing the old run must make the new stage claimable without
				// another status update, mention or manually enqueued task.
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, old[0].ID)
				claim := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+fx.RuntimeID+"/tasks/claim", nil), "runtimeId", fx.RuntimeID)
				claim.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityWorkflowStatusRulesV1)
				var resp struct {
					Task *AgentTaskResponse `json:"task"`
				}
				testutil.Call(t, testHandler.ClaimTaskByRuntime, claim).Want(http.StatusOK).JSON(&resp)
				if resp.Task == nil || resp.Task.ID != nextID || !resp.Task.IsLeaderTask || resp.Task.ProjectWorkflow == nil || resp.Task.Agent == nil {
					t.Fatalf("new stage was not delivered: %+v", resp.Task)
				}
				if !strings.Contains(resp.Task.ProjectWorkflow.Brief, "Handle only in_review") || strings.Contains(resp.Task.ProjectWorkflow.Brief, "This run remains bound") || !strings.Contains(resp.Task.Agent.Instructions, "Own the parent issue status — through its project workflow") {
					t.Fatalf("new stage received stale or guest instructions: %+v", resp.Task)
				}
			})
		}
	}
}

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
