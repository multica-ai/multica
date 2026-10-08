package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// #9105: GetTaskStatus already gets polled every ~5s for every in-flight
// daemon task (watchTaskCancellation on the daemon side) -- these tests pin
// the server-side half of the per-task liveness signal that piggybacks on
// that existing poll, without any new daemon-side call.

// TestGetTaskStatus_StampsHeartbeatWhileRunning covers the primary case: a
// running task's poll advances last_heartbeat_at.
func TestGetTaskStatus_StampsHeartbeatWhileRunning(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "#9105 heartbeat runtime")
	agentID := dbfx.Agent(t, "#9105 heartbeat agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running", "started_at": testutil.Raw("now()"),
	})

	var before *time.Time
	dbfx.QueryRow(t, `SELECT last_heartbeat_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&before)
	if before != nil {
		t.Fatalf("last_heartbeat_at = %v before any poll, want nil", before)
	}

	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, testWorkspaceID, "test-daemon")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusOK)

	var after *time.Time
	dbfx.QueryRow(t, `SELECT last_heartbeat_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&after)
	if after == nil {
		t.Fatal("last_heartbeat_at is still nil after a status poll on a running task")
	}
	if time.Since(*after) > 10*time.Second {
		t.Fatalf("last_heartbeat_at = %v, too old to have just been stamped", after)
	}
}

// TestGetTaskStatus_DoesNotStampHeartbeatWhenNotRunning covers the exclusion:
// a poll against a task that already reached a terminal state must not
// fabricate a heartbeat for it (the query's own WHERE status='running' is
// the real guard; this pins the handler-level skip too, since a terminal
// task's poll response is what makes the daemon interrupt the agent in the
// first place -- it should not also look "freshly alive" server-side).
func TestGetTaskStatus_DoesNotStampHeartbeatWhenNotRunning(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "#9105 terminal runtime")
	agentID := dbfx.Agent(t, "#9105 terminal agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "completed", "completed_at": testutil.Raw("now()"),
	})

	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, testWorkspaceID, "test-daemon")
	req = withURLParam(req, "taskId", taskID)
	var response map[string]string
	testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusOK).JSON(&response)
	if response["status"] != "completed" {
		t.Fatalf("status = %q, want completed", response["status"])
	}

	var heartbeat *time.Time
	dbfx.QueryRow(t, `SELECT last_heartbeat_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&heartbeat)
	if heartbeat != nil {
		t.Fatalf("last_heartbeat_at = %v for a completed task, want nil", heartbeat)
	}
}

// TestTaskToResponse_SurfacesLastHeartbeatAt pins that any consumer of the
// normal task response shape (ListAgentTasks, task detail, etc.) gets the
// liveness signal for free, without a bespoke staleness endpoint.
func TestTaskToResponse_SurfacesLastHeartbeatAt(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "#9105 response runtime")
	agentID := dbfx.Agent(t, "#9105 response agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running", "started_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() WHERE id = $1`, taskID)

	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	resp := taskToResponse(task, testWorkspaceID)
	if resp.LastHeartbeatAt == nil {
		t.Fatal("taskToResponse dropped last_heartbeat_at")
	}
}

// TestListStaleRunningAgentTasks pins the real query the heartbeat column
// exists to serve: a caller (Hellsing, an ops surface) decides its own
// staleness threshold and asks for exactly the running tasks older than it.
func TestListStaleRunningAgentTasks(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "#9105 stale-list runtime")
	agentID := dbfx.Agent(t, "#9105 stale-list agent", runtimeID)

	staleID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running",
		"started_at": testutil.Raw("now() - interval '1 hour'"),
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() - interval '1 hour' WHERE id = $1`, staleID)

	freshID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running",
		"started_at": testutil.Raw("now() - interval '1 hour'"),
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() WHERE id = $1`, freshID)

	// No heartbeat yet at all -- falls back to started_at, which here is old
	// enough to still count as stale (e.g. a row that predates this column,
	// or a genuinely hung task that never got its first poll).
	noHeartbeatStaleID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running",
		"started_at": testutil.Raw("now() - interval '1 hour'"),
	})

	// Stale by heartbeat age, but not running -- must be excluded regardless.
	completedID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "completed",
		"started_at": testutil.Raw("now() - interval '1 hour'"), "completed_at": testutil.Raw("now()"),
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() - interval '1 hour' WHERE id = $1`, completedID)

	staleBefore := time.Now().Add(-10 * time.Minute)
	rows, err := testHandler.Queries.ListStaleRunningAgentTasks(t.Context(), db.ListStaleRunningAgentTasksParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListStaleRunningAgentTasks: %v", err)
	}

	got := make(map[string]bool, len(rows))
	for _, r := range rows {
		got[uuidToString(r.ID)] = true
	}
	if !got[staleID] {
		t.Errorf("stale running task missing from results")
	}
	if !got[noHeartbeatStaleID] {
		t.Errorf("running task with no heartbeat but old started_at missing from results")
	}
	if got[freshID] {
		t.Errorf("fresh running task incorrectly returned as stale")
	}
	if got[completedID] {
		t.Errorf("completed task incorrectly returned despite an old heartbeat")
	}
}

// TestGetTaskStatus_HeartbeatWriteIsThrottled pins the cost bound (#9105):
// the daemon polls every ~5s, but a running task's row is rewritten at most
// once per 60s. A poll inside the window leaves the stamp untouched; a poll
// after it advances the stamp.
func TestGetTaskStatus_HeartbeatWriteIsThrottled(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "#9105 throttle runtime")
	agentID := dbfx.Agent(t, "#9105 throttle agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running", "started_at": testutil.Raw("now()"),
	})
	poll := func() {
		req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/status", nil, testWorkspaceID, "test-daemon")
		req = withURLParam(req, "taskId", taskID)
		testutil.Call(t, testHandler.GetTaskStatus, req).Want(http.StatusOK)
	}
	stamp := func() time.Time {
		var ts time.Time
		dbfx.QueryRow(t, `SELECT last_heartbeat_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&ts)
		return ts
	}

	// Inside the window: pin a stamp 10s old, poll, and expect no rewrite.
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() - interval '10 seconds' WHERE id = $1`, taskID)
	before := stamp()
	poll()
	if after := stamp(); !after.Equal(before) {
		t.Fatalf("heartbeat rewritten inside the 60s throttle window: before=%v after=%v", before, after)
	}

	// Past the window: a stamp 2 minutes old must advance on the next poll.
	dbfx.Exec(t, `UPDATE agent_task_queue SET last_heartbeat_at = now() - interval '2 minutes' WHERE id = $1`, taskID)
	old := stamp()
	poll()
	if after := stamp(); !after.After(old) || time.Since(after) > 10*time.Second {
		t.Fatalf("heartbeat not advanced after the throttle window: old=%v after=%v", old, after)
	}
}
