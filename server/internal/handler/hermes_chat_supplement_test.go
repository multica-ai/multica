package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestHermesChatSupplementDeferredNegotiation(t *testing.T) {
	f := newChatSupplementFixture(t, "hermes", true)
	// The old version/name offer cannot enable Hermes at StartTask.
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "literal").Want(409)
	activate := func(contract string) *testutil.Response {
		return testutil.Call(t, testHandler.ClaimTaskSupplement, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", map[string]string{"active_turn_guidance": contract}), "taskId", f.task))
	}
	activate("end_turn").Want(400)
	activate("hermes-active-turn-guidance-v1").Want(200)
	activate("hermes-active-turn-guidance-v1").Want(200)
	var pending PendingChatTaskResponse
	testutil.Call(t, testHandler.GetPendingChatTask, withURLParam(chatSupplementAPIRequest(t, "GET", "/pending-task", nil), "sessionId", f.session)).Want(200).JSON(&pending)
	if !pending.CanSupplement {
		t.Fatal("live negotiated contract not exposed")
	}
	chatSupplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-111111111111", "literal").Want(201)
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, f.task); n != 1 {
		t.Fatalf("activation duplicated: %d", n)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, f.task)
	activate("hermes-active-turn-guidance-v1").Want(409)
}

func TestHermesChatSupplementActivationExcludesOtherProviders(t *testing.T) {
	f := newChatSupplementFixture(t, "claude", false)
	testutil.Call(t, testHandler.ClaimTaskSupplement, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/claim", map[string]string{"active_turn_guidance": "hermes-active-turn-guidance-v1"}), "taskId", f.task)).Want(409)
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement_capability WHERE task_id=$1`, f.task); n != 0 {
		t.Fatal("ineligible provider enabled")
	}
}
