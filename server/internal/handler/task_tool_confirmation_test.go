package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func registerToolConfirmations(t *testing.T, taskID string, calls ...map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/tool-confirmations", map[string]any{"confirmations": calls}), "taskId", taskID)
	return testutil.Call(t, testHandler.RegisterTaskToolConfirmations, req)
}

func decideToolConfirmation(t *testing.T, f supplementFixture, callID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/tasks/"+f.taskID+"/tool-confirmations/"+callID, body)
	return testutil.Call(t, testHandler.DecideTaskToolConfirmation, withURLParams(req, "id", f.issueID, "taskId", f.taskID, "callId", callID))
}

type toolConfirmationList struct {
	Confirmations []TaskToolConfirmationResponse `json:"confirmations"`
}

func TestTaskToolConfirmationLifecycle(t *testing.T) {
	f := newSupplementFixture(t, "qoder", "running", false)
	dbfx.Cleanup(t, `DELETE FROM task_tool_confirmation WHERE task_id=$1`, f.taskID)
	var published []events.Event
	testHandler.Bus.Subscribe(protocol.EventTaskToolConfirmation, func(e events.Event) { published = append(published, e) })

	call := map[string]any{"call_id": "evt_1", "tool_name": "Bash", "input": map[string]any{"command": "rm -rf build"}}
	var got toolConfirmationList
	registerToolConfirmations(t, f.taskID, call).Want(http.StatusOK).JSON(&got)
	if len(got.Confirmations) != 1 || got.Confirmations[0].Status != "pending" || string(got.Confirmations[0].Input) == "" {
		t.Fatalf("registered = %+v", got.Confirmations)
	}
	registerToolConfirmations(t, f.taskID, call).Want(http.StatusOK).JSON(&got)
	if len(got.Confirmations) != 1 || len(published) != 1 {
		t.Fatalf("retry was not idempotent: rows=%d events=%d", len(got.Confirmations), len(published))
	}

	var tasks []AgentTaskResponse
	listReq := withURLParam(newRequest(http.MethodGet, "/api/issues/"+f.issueID+"/tasks", nil), "id", f.issueID)
	testutil.Call(t, testHandler.ListTasksByIssue, listReq).Want(http.StatusOK).JSON(&tasks)
	if len(tasks) != 1 || len(tasks[0].PendingToolConfirmations) != 1 || tasks[0].PendingToolConfirmations[0].ToolName != "Bash" {
		t.Fatalf("pending confirmations not hydrated: %+v", tasks)
	}

	decideToolConfirmation(t, f, "evt_1", map[string]any{"decision": "maybe"}).Want(http.StatusBadRequest)
	var decided TaskToolConfirmationResponse
	decideToolConfirmation(t, f, "evt_1", map[string]any{"decision": "deny", "message": "use make clean"}).Want(http.StatusOK).JSON(&decided)
	if decided.Status != "denied" || decided.DenyMessage != "use make clean" || len(published) != 2 {
		t.Fatalf("decided = %+v events=%d", decided, len(published))
	}
	decideToolConfirmation(t, f, "evt_1", map[string]any{"decision": "allow"}).Want(http.StatusConflict)

	listDaemon := withURLParam(newOwnerDaemonRequest(http.MethodGet, "/tool-confirmations", nil), "taskId", f.taskID)
	testutil.Call(t, testHandler.ListTaskToolConfirmationsForDaemon, listDaemon).Want(http.StatusOK).JSON(&got)
	if len(got.Confirmations) != 1 || got.Confirmations[0].Status != "denied" {
		t.Fatalf("daemon view = %+v", got.Confirmations)
	}
}

func TestTaskToolConfirmationRequiresRunningIssueTask(t *testing.T) {
	f := newSupplementFixture(t, "qoder", "running", false)
	dbfx.Cleanup(t, `DELETE FROM task_tool_confirmation WHERE task_id=$1`, f.taskID)
	registerToolConfirmations(t, f.taskID, map[string]any{"call_id": "evt_1", "tool_name": "Bash"}).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='failed', completed_at=now() WHERE id=$1`, f.taskID)
	decideToolConfirmation(t, f, "evt_1", map[string]any{"decision": "allow"}).Want(http.StatusConflict)
	registerToolConfirmations(t, f.taskID, map[string]any{"call_id": "evt_2", "tool_name": "Bash"}).Want(http.StatusConflict)
}
