package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// secretaryCall makes a request as the Secretary's run in f: the agent and the
// task it is running, whose originator is the boss.
func secretaryCall(f directiveRunFixture, method, path string, body map[string]any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Agent-ID", f.secretaryID)
	req.Header.Set("X-Task-ID", f.taskID)
	return req
}

// The contract of a Secretary request (spec 13.3.1), end to end on the server:
// the Secretary files a parent request in the Secretary desk, files a child per
// addressee, and files a one-shot deadline wakeup on the request. When the child
// closes, the rule wakes the Secretary with the request's own instruction, and
// that run is attributed to the boss, the originator of the Secretary's run.
func TestSecretaryRequestContract_DeadlineAndChildDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := seedDirectiveRun(t, "Is the spring launch on track, and what could delay it?")
	desk := dbfx.Project(t, "Secretary desk", testutil.Cols{"workspace_id": testWorkspaceID})
	leader := createHandlerTestAgent(t, "request contract leader", nil)

	var request IssueResponse
	testutil.Call(t, testHandler.CreateIssue, secretaryCall(f, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Spring launch assessment",
		"project_id":    desk,
		"assignee_type": "agent",
		"assignee_id":   f.secretaryID,
	})).Want(http.StatusCreated).JSON(&request)

	var child IssueResponse
	testutil.Call(t, testHandler.CreateIssue, secretaryCall(f, "POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "Growth: what could delay the launch",
		"status":          "in_progress",
		"parent_issue_id": request.ID,
		"assignee_type":   "agent",
		"assignee_id":     leader,
	})).Want(http.StatusCreated).JSON(&child)

	deadline := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	wake := secretaryCall(f, "POST", "/api/issues/"+request.ID+"/wakeups", map[string]any{
		"kind":        "at",
		"at":          deadline,
		"mode":        "once",
		"instruction": "The deadline has passed: read the answers and assess what is in.",
	})
	wake = withURLParam(wake, "id", request.ID)
	testutil.Call(t, testHandler.CreateIssueWakeup, wake).Want(http.StatusCreated)

	var kind, mode, instruction string
	dbfx.QueryRow(t, `SELECT kind, mode, instruction FROM issue_wakeup
		WHERE issue_id = $1 AND agent_id = $2 AND kind = 'at'`, request.ID, f.secretaryID).Scan(&kind, &mode, &instruction)
	if kind != "at" || mode != "once" || !strings.Contains(instruction, "deadline has passed") {
		t.Fatalf("deadline wakeup = kind %q mode %q instruction %q", kind, mode, instruction)
	}

	updateChildStatus(t, child.ID, "done")

	runs := childDoneRuns(t, request.ID)
	if len(runs) != 1 || runs[0].AgentID != f.secretaryID {
		t.Fatalf("child_done runs = %+v, want one run for the Secretary", runs)
	}
	if !strings.Contains(runs[0].Note, service.SecretaryRequestChildDoneInstruction) {
		t.Errorf("child_done note lacks the request's instruction:\n%s", runs[0].Note)
	}
	if strings.Contains(runs[0].Note, service.ChildDoneDefaultInstruction) {
		t.Errorf("child_done note carries the generic delivery instruction:\n%s", runs[0].Note)
	}

	var bossOfSecretary, bossOfRun string
	dbfx.QueryRow(t, `SELECT COALESCE(originator_user_id::text, '') FROM agent_task_queue WHERE id = $1`, f.taskID).Scan(&bossOfSecretary)
	dbfx.QueryRow(t, `SELECT COALESCE(originator_user_id::text, '') FROM agent_task_queue WHERE id = $1`, runs[0].ID).Scan(&bossOfRun)
	if bossOfSecretary == "" || bossOfRun != bossOfSecretary {
		t.Errorf("child_done run originator = %q, want the boss %q", bossOfRun, bossOfSecretary)
	}
}
