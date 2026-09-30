package handler

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A run_only run is marked "running" the moment its task is enqueued, and it
// never carries the issue its agent goes on to work. These tests pin the two
// read-side signals that make the run history truthful: task_status (so a run
// still waiting behind the agent's concurrency cap reads as queued) and
// work_issue_id (set by the run's own agent through the work-issue endpoint).

// runOnlyRunFixture seeds a run_only autopilot with one "running" run whose
// task has the given status, and returns the ids.
func runOnlyRunFixture(t *testing.T, taskStatus string) (autopilotID, runID, taskID, agentID string) {
	t.Helper()
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	autopilotID = dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "run visibility",
		"assignee_id":     agentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	runID = dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "schedule",
		"status":       "running",
	})
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":        testRuntimeID,
		"status":            taskStatus,
		"autopilot_run_id":  runID,
		"originator_source": "unattributed",
	})
	dbfx.Exec(t, `UPDATE autopilot_run SET task_id = $1 WHERE id = $2`, taskID, runID)
	return autopilotID, runID, taskID, agentID
}

func listRuns(t *testing.T, autopilotID string) []AutopilotRunResponse {
	t.Helper()
	req := newRequest("GET", "/api/autopilots/"+autopilotID+"/runs?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", autopilotID)
	var out struct {
		Runs []AutopilotRunResponse `json:"runs"`
	}
	testutil.Call(t, testHandler.ListAutopilotRuns, req).Want(http.StatusOK).JSON(&out)
	return out.Runs
}

func TestListAutopilotRuns_ReportsTaskStatus(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			autopilotID, runID, _, _ := runOnlyRunFixture(t, status)
			runs := listRuns(t, autopilotID)
			if len(runs) != 1 || runs[0].ID != runID {
				t.Fatalf("runs = %+v, want the one seeded run", runs)
			}
			if runs[0].Status != "running" {
				t.Fatalf("run status = %q; the stored run status must not change", runs[0].Status)
			}
			if runs[0].TaskStatus == nil || *runs[0].TaskStatus != status {
				t.Fatalf("task_status = %v, want %q", runs[0].TaskStatus, status)
			}
		})
	}
}

func setWorkIssueRequest(t *testing.T, runID, issue string) *http.Request {
	t.Helper()
	req := newRequest("PUT", "/api/autopilot-runs/"+runID+"/work-issue?workspace_id="+testWorkspaceID, map[string]any{"issue_id": issue})
	return withURLParam(req, "runId", runID)
}

func asTask(req *http.Request, agentID, taskID string) *http.Request {
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func TestSetAutopilotRunWorkIssue_RunOwnTaskLinksIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	autopilotID, runID, taskID, agentID := runOnlyRunFixture(t, "running")
	issueID := dbfx.Issue(t, "picked by the lead")
	var number int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, issueID).Scan(&number)

	// The agent names the issue by its human identifier, the way the CLI is used.
	identifier := "HAN-" + strconv.Itoa(number)
	var resp AutopilotRunResponse
	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, asTask(setWorkIssueRequest(t, runID, identifier), agentID, taskID)).
		Want(http.StatusOK).JSON(&resp)
	if resp.WorkIssueID == nil || *resp.WorkIssueID != issueID {
		t.Fatalf("work_issue_id = %v, want %s", resp.WorkIssueID, issueID)
	}
	if resp.IssueID != nil {
		t.Fatalf("issue_id = %v; linking must not touch the lifecycle issue link", *resp.IssueID)
	}

	runs := listRuns(t, autopilotID)
	if len(runs) != 1 || runs[0].WorkIssueID == nil || *runs[0].WorkIssueID != issueID {
		t.Fatalf("listed run does not carry the work issue: %+v", runs)
	}
}

func TestSetAutopilotRunWorkIssue_RejectsOtherRunsTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, runID, _, agentID := runOnlyRunFixture(t, "running")
	_, _, otherTaskID, _ := runOnlyRunFixture(t, "running")
	issueID := dbfx.Issue(t, "not yours")

	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, asTask(setWorkIssueRequest(t, runID, issueID), agentID, otherTaskID)).
		Want(http.StatusForbidden)
}

func TestSetAutopilotRunWorkIssue_RejectsFinishedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, runID, taskID, agentID := runOnlyRunFixture(t, "completed")
	issueID := dbfx.Issue(t, "too late")

	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, asTask(setWorkIssueRequest(t, runID, issueID), agentID, taskID)).
		Want(http.StatusForbidden)
}

func TestSetAutopilotRunWorkIssue_MemberWithWriteAccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, runID, _, _ := runOnlyRunFixture(t, "queued")
	issueID := dbfx.Issue(t, "set by a human")

	var resp AutopilotRunResponse
	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, setWorkIssueRequest(t, runID, issueID)).
		Want(http.StatusOK).JSON(&resp)
	if resp.WorkIssueID == nil || *resp.WorkIssueID != issueID {
		t.Fatalf("work_issue_id = %v, want %s", resp.WorkIssueID, issueID)
	}
	if resp.TaskStatus == nil || *resp.TaskStatus != "queued" {
		t.Fatalf("task_status = %v, want queued", resp.TaskStatus)
	}
}

func TestSetAutopilotRunWorkIssue_RejectsRunThatOwnsAnIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, runID, _, _ := runOnlyRunFixture(t, "running")
	ownIssue := dbfx.Issue(t, "created by the run")
	dbfx.Exec(t, `UPDATE autopilot_run SET issue_id = $1 WHERE id = $2`, ownIssue, runID)
	other := dbfx.Issue(t, "another")

	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, setWorkIssueRequest(t, runID, other)).
		Want(http.StatusConflict)
}

func TestSetAutopilotRunWorkIssue_UnknownIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	_, runID, _, _ := runOnlyRunFixture(t, "running")

	testutil.Call(t, testHandler.SetAutopilotRunWorkIssue, setWorkIssueRequest(t, runID, "HAN-999999")).
		Want(http.StatusNotFound)
}
