package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A manual "run now" spends a human's authority. When the caller is an agent —
// the CLI running inside a task, or an A2A call — that human is the agent's run
// ORIGINATOR, not the runtime owner whose token authenticated the request.
//
// The regression these tests exist for (#8078): MUL-6951 replaced the
// trigger-independent creator gate with one that resolves the FIRING TRIGGER's
// creator whenever the actor is not a member. A manual trigger supplies no
// trigger by construction, so every agent-initiated "run now" resolved no
// principal and was refused — while the same request had already been accepted
// by requireAutopilotWrite three lines earlier.

// triggerInvokerFixture builds an active create_issue autopilot owned by
// testUserID and returns it with the agent that will do the triggering.
func triggerInvokerFixture(t *testing.T, titleSuffix string) (autopilotID, agentID string) {
	t.Helper()

	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)

	title := fmt.Sprintf("manual trigger invoker %s %d", titleSuffix, time.Now().UnixNano())
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                title,
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
	})
	var ap AutopilotResponse
	testutil.Call(t, testHandler.CreateAutopilot, req).Want(http.StatusCreated).JSON(&ap)

	// A dispatched run leaves an issue and a task behind it; both outlive the
	// autopilot row, so they are cleaned before it.
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND title = $2)`, testWorkspaceID, title)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
	dbfx.Cleanup(t, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	return ap.ID, agentID
}

// callerTask is the running task the triggering agent speaks from. An empty
// originatorUserID leaves the task carrying no human at all — the shape a legacy
// or system-rooted run has.
func callerTask(t *testing.T, agentID, originatorUserID string) string {
	t.Helper()
	if originatorUserID == "" {
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":        testRuntimeID,
			"status":            "running",
			"originator_source": "unattributed",
		})
	}
	// originator == accountable: migrations 190/197 require the pair to match.
	return dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":          testRuntimeID,
		"status":              "running",
		"originator_user_id":  originatorUserID,
		"accountable_user_id": originatorUserID,
		"originator_source":   "direct_human",
	})
}

// triggerAsAgent issues the manual trigger the way the CLI does from inside a
// task: authenticated by the runtime owner's bound user, but stamped by the auth
// middleware as a task-token actor.
func triggerAsAgent(t *testing.T, autopilotID, agentID, taskID string) *testutil.Response {
	t.Helper()
	r := newRequestAs(testUserID, "POST", "/api/autopilots/"+autopilotID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r.Header.Set("X-Actor-Source", "task_token")
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "id", autopilotID)
	return testutil.Call(t, testHandler.TriggerAutopilot, r)
}

// TestTriggerAutopilot_AgentActsForItsOriginator is the acceptance test for the
// 人 → agent → autopilot chain: an agent whose run carries a human who may
// trigger this autopilot dispatches exactly as that human clicking "Run now".
func TestTriggerAutopilot_AgentActsForItsOriginator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	autopilotID, agentID := triggerInvokerFixture(t, "admitted")
	taskID := callerTask(t, agentID, testUserID)

	var run AutopilotRunResponse
	triggerAsAgent(t, autopilotID, agentID, taskID).Want(http.StatusOK).JSON(&run)
	if run.Status != "issue_created" {
		// .Want() reports the status code; only this assertion knows that a 200
		// carrying a skipped run is the exact shape the bug produced.
		t.Fatalf("run status = %q (reason_code %v, failure_reason %v), want issue_created",
			run.Status, derefStr(run.ReasonCode), derefStr(run.FailureReason))
	}
}

// TestTriggerAutopilot_AgentWithNoOriginatorRefused pins the ruling on #8078: a
// chain reaching this gate with no human at its top is refused outright, rather
// than admitted through the workspace-broad exception invokeAgentDecision grants
// unattributed agent/system principals. A manual trigger is somebody's decision;
// no human means something upstream dropped it, and that should surface.
func TestTriggerAutopilot_AgentWithNoOriginatorRefused(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	autopilotID, agentID := triggerInvokerFixture(t, "no originator")
	taskID := callerTask(t, agentID, "")

	triggerAsAgent(t, autopilotID, agentID, taskID).Want(http.StatusForbidden)

	// Refused before dispatch, so no run row: a skipped run would report this as
	// an admission outcome on the autopilot's own failure-rate history, which the
	// auto-pause monitor reads.
	if runs := dbfx.Count(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID); runs != 0 {
		t.Errorf("autopilot_run rows = %d, want 0 for a pre-dispatch refusal", runs)
	}
}

// TestTriggerAutopilot_AgentCannotExceedItsOriginator pins the other half of the
// ruling: the agent may only press a "Run now" its ordering human could press
// themselves. The request still authenticates as the runtime owner, who DOES
// hold write here, so this can only fail because the ORIGINATOR does not —
// proving the gate reads the originator and not the token's bound user.
func TestTriggerAutopilot_AgentCannotExceedItsOriginator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	autopilotID, agentID := triggerInvokerFixture(t, "originator lacks write")

	outsiderID := dbfx.User(t, "Trigger Outsider", fmt.Sprintf("trigger-outsider-%d@multica.test", time.Now().UnixNano()))
	// A plain member: in the workspace, but neither the autopilot's creator nor
	// an admin nor a granted collaborator.
	dbfx.Member(t, testWorkspaceID, outsiderID, "member")

	taskID := callerTask(t, agentID, outsiderID)
	triggerAsAgent(t, autopilotID, agentID, taskID).Want(http.StatusForbidden)

	if runs := dbfx.Count(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID); runs != 0 {
		t.Errorf("autopilot_run rows = %d, want 0 for a pre-dispatch refusal", runs)
	}
}

// TestTriggerAutopilot_MemberPathUnchanged guards the half of the manual path
// that was never broken: a member triggering directly is judged as themselves,
// with no originator lookup in the way.
func TestTriggerAutopilot_MemberPathUnchanged(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	autopilotID, _ := triggerInvokerFixture(t, "member direct")

	r := newRequestAs(testUserID, "POST", "/api/autopilots/"+autopilotID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", autopilotID)

	var run AutopilotRunResponse
	testutil.Call(t, testHandler.TriggerAutopilot, r).Want(http.StatusOK).JSON(&run)
	if run.Status != "issue_created" {
		t.Fatalf("run status = %q (reason_code %v), want issue_created", run.Status, derefStr(run.ReasonCode))
	}
}

func derefStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
