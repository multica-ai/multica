package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCancelQueuedAgentTasks(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "queue cancellation runtime")
	agentID := dbfx.Agent(t, "queue cancellation agent", runtimeID)
	otherAgentID := dbfx.Agent(t, "another agent", runtimeID)
	queued := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	dispatched := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "dispatched"})
	running := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	newlyQueued := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	otherAgentQueued := dbfx.Task(t, otherAgentID, testutil.Cols{"runtime_id": runtimeID})
	chatSessionID := dbfx.ChatSession(t, agentID)
	queuedChat := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": chatSessionID})

	cancel := func(taskIDs []string, wantStatus int) cancelQueuedAgentTasksResponse {
		t.Helper()
		path := "/api/agents/" + agentID + "/cancel-queued-tasks"
		req := withURLParam(newRequest(http.MethodPost, path, map[string]any{"task_ids": taskIDs}), "id", agentID)
		call := testutil.Call(t, testHandler.CancelQueuedAgentTasks, req).Want(wantStatus)
		var result cancelQueuedAgentTasksResponse
		if wantStatus == http.StatusOK {
			call.JSON(&result)
		}
		return result
	}
	cancel([]string{queued, "invalid"}, http.StatusBadRequest)

	taskIDs := []string{queued, dispatched, running, otherAgentQueued, queuedChat, queued}
	if result := cancel(taskIDs, http.StatusOK); result.Cancelled != 1 || result.Skipped != 4 {
		t.Fatalf("first cancellation = %+v, want 1 cancelled and 4 skipped", result)
	}
	if result := cancel(taskIDs, http.StatusOK); result.Cancelled != 0 || result.Skipped != 5 {
		t.Fatalf("repeated cancellation = %+v, want 0 cancelled and 5 skipped", result)
	}

	for id, want := range map[string]string{
		dispatched:       "dispatched",
		running:          "running",
		newlyQueued:      "queued",
		otherAgentQueued: "queued",
		queuedChat:       "queued",
	} {
		task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(id))
		if err != nil || task.Status != want {
			t.Errorf("task %s: status = %q, err = %v, want %q", id, task.Status, err, want)
		}
	}

	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil), "id", agentID)
	var history []AgentTaskResponse
	testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&history)
	for _, task := range history {
		if task.ID == queued && task.Status == "cancelled" && task.CompletedAt != nil {
			return
		}
	}
	t.Fatal("the cancelled run did not appear as finished in agent history")
}
