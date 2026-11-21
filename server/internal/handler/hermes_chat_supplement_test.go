package handler

import (
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"net/http"
	"testing"
)

func hermesChatFixture(t *testing.T) (string, string) {
	t.Helper()
	session, task, _ := newChatSupplementFixture(t, false)
	dbfx.Exec(t, `UPDATE agent_runtime SET provider='hermes' WHERE id=(SELECT runtime_id FROM agent_task_queue WHERE id=$1)`, task)
	return session, task
}

func activateHermesForTest(t *testing.T, task, contract string) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", map[string]string{"active_turn_guidance": contract}), "taskId", task)
	return testutil.Call(t, testHandler.ClaimTaskSupplement, req)
}

func TestHermesChatSupplementNegotiationAndFallback(t *testing.T) {
	for _, delivered := range []bool{true, false} {
		t.Run(map[bool]string{true: "incorporated", false: "not_proven"}[delivered], func(t *testing.T) {
			session, task := hermesChatFixture(t)
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, task); n != 0 {
				t.Fatal("unnegotiated Hermes enabled")
			}
			activateHermesForTest(t, task, "unsupported").Want(400)
			for range 2 {
				var response map[string]string
				activateHermesForTest(t, task, "hermes-acp-steer-v1").Want(200).JSON(&response)
				if response["supplement_capability"] != protocol.DaemonCapabilityTaskSupplementV1 {
					t.Fatal(response)
				}
			}
			sent := sendChatGuidance(t, session, map[string]any{"steer": true, "content": "Hermes guidance"})
			if !sent.Steered || sent.TaskID != task {
				t.Fatalf("guidance not bound to active turn: %+v", sent)
			}
			activateHermesForTest(t, task, "hermes-acp-steer-v1").Want(200)
			if n := dbfx.Count(t, `SELECT count(*) FROM chat_task_supplement WHERE task_id=$1 AND status='pending'`, task); n != 1 {
				t.Fatal("activation claimed guidance")
			}
			if _, err := testHandler.Queries.ClaimNextChatTaskSupplement(t.Context(), parseUUID(task)); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				req := withURLParams(newOwnerDaemonRequest(http.MethodPost, "/ack", map[string]any{"delivered": delivered}), "taskId", task, "commentId", sent.MessageID)
				testutil.Call(t, testHandler.AckTaskSupplement, req).Want(200)
			}
			want := 1
			if !delivered {
				want = 2
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, session); n != want {
				t.Fatalf("tasks=%d want=%d", n, want)
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, task)
			activateHermesForTest(t, task, "hermes-acp-steer-v1").Want(409)
		})
	}
}

func TestHermesChatSupplementStockQueues(t *testing.T) {
	session, task := hermesChatFixture(t)
	sent := sendChatGuidance(t, session, map[string]any{"steer": true, "content": "ordinary Hermes input"})
	if sent.Steered || !sent.Queued || sent.TaskID == task {
		t.Fatalf("stock peer did not queue: %+v", sent)
	}
}

func TestHermesChatSupplementActivationGuards(t *testing.T) {
	for _, kind := range []string{"other_provider", "quick_actions", "issue", "channel"} {
		t.Run(kind, func(t *testing.T) {
			session, task := hermesChatFixture(t)
			switch kind {
			case "other_provider":
				dbfx.Exec(t, `UPDATE agent_runtime SET provider='codex' WHERE id=(SELECT runtime_id FROM agent_task_queue WHERE id=$1)`, task)
			case "quick_actions":
				dbfx.Exec(t, `UPDATE agent_task_queue SET regenerate_quick_actions_for=$2 WHERE id=$1`, task, task)
			case "channel":
				var agentID string
				dbfx.QueryRow(t, `SELECT agent_id::text FROM agent_task_queue WHERE id=$1`, task).Scan(&agentID)
				installation := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": dbfx.WorkspaceID, "channel_type": "feishu", "installer_user_id": testUserID, "agent_id": agentID})
				dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{"chat_session_id": session, "installation_id": installation, "channel_type": "feishu", "channel_chat_id": task, "chat_type": "group"})
			case "issue":
				issue := dbfx.Issue(t, "Hermes activation guard")
				dbfx.Exec(t, `UPDATE agent_task_queue SET issue_id=$2 WHERE id=$1`, task, issue)
			}
			// Issue supplement claims do not interpret the direct-Chat activation extension.
			want := 409
			if kind == "issue" {
				want = 200
			}
			activateHermesForTest(t, task, "hermes-acp-steer-v1").Want(want)
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, task); n != 0 {
				t.Fatal("ineligible run activated")
			}
		})
	}
}

func TestChatSupplementStartEcho(t *testing.T) {
	_, task, _ := newChatSupplementFixture(t, false)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, task)
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/start", map[string]any{"capabilities": []string{protocol.DaemonCapabilityTaskSupplementV1}}), "taskId", task)
	var response AgentTaskResponse
	testutil.Call(t, testHandler.StartTask, req).Want(200).JSON(&response)
	if response.SupplementCapability != protocol.DaemonCapabilityTaskSupplementV1 {
		t.Fatalf("Chat start lost negotiated capability: %+v", response)
	}
}
