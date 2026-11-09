package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// taskRuntimes reads each task's runtime_id, keyed by task id.
func taskRuntimes(t *testing.T, taskIDs ...string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(taskIDs))
	for _, id := range taskIDs {
		var runtimeID string
		if err := testPool.QueryRow(context.Background(),
			`SELECT runtime_id::text FROM agent_task_queue WHERE id = $1`, id).Scan(&runtimeID); err != nil {
			t.Fatalf("read task %s runtime: %v", id, err)
		}
		out[id] = runtimeID
	}
	return out
}

func patchAgentRuntime(t *testing.T, agentID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+agentID, map[string]any{
		"runtime_id": runtimeID,
	}), "id", agentID)
	testHandler.UpdateAgent(w, req)
	return w
}

func TestUpdateAgent_RuntimeSwitch_MovesPendingTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	oldRuntimeID := dbfx.Runtime(t, "runtime-switch-tasks-old")
	newRuntimeID := dbfx.Runtime(t, "runtime-switch-tasks-new")
	agentID := dbfx.Agent(t, "runtime-switch-tasks-agent", oldRuntimeID)
	issueID := dbfx.Issue(t, "runtime switch pending tasks")
	queuedID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": oldRuntimeID, "issue_id": issueID})
	deferredID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": oldRuntimeID, "issue_id": issueID, "status": "deferred",
	})
	runningID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": oldRuntimeID, "issue_id": issueID, "status": "running",
		"dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()"),
	})
	finishedID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": oldRuntimeID, "issue_id": issueID, "status": "completed",
		"completed_at": testutil.Raw("now()"),
	})

	w := patchAgentRuntime(t, agentID, newRuntimeID)
	if w.Code != http.StatusOK {
		t.Fatalf("switch runtime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := taskRuntimes(t, queuedID, deferredID, runningID, finishedID)
	want := map[string]string{
		queuedID:   newRuntimeID,
		deferredID: newRuntimeID,
		runningID:  oldRuntimeID,
		finishedID: oldRuntimeID,
	}
	for id, runtimeID := range want {
		if got[id] != runtimeID {
			t.Errorf("task %s runtime = %s, want %s", id, got[id], runtimeID)
		}
	}
}

func TestUpdateAgent_RuntimeSwitch_ForbiddenLeavesTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	suffix := time.Now().UnixNano()
	strangerID := dbfx.User(t,
		fmt.Sprintf("runtime-switch-stranger-%d", suffix),
		fmt.Sprintf("runtime-switch-stranger-%d@example.com", suffix),
	)
	dbfx.Member(t, testWorkspaceID, strangerID, "member")
	oldRuntimeID := dbfx.Runtime(t, "runtime-switch-forbidden-old")
	strangerRuntimeID := dbfx.Runtime(t, "runtime-switch-forbidden-private", testutil.Cols{
		"visibility": "private", "owner_id": strangerID,
	})
	agentID := dbfx.Agent(t, "runtime-switch-forbidden-agent", oldRuntimeID)
	queuedID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": oldRuntimeID, "issue_id": dbfx.Issue(t, "runtime switch forbidden"),
	})

	w := patchAgentRuntime(t, agentID, strangerRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("switch to another member's private runtime: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := taskRuntimes(t, queuedID)[queuedID]; got != oldRuntimeID {
		t.Fatalf("queued task runtime = %s, want unchanged %s", got, oldRuntimeID)
	}
}
