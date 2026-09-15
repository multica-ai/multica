package handler

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type daemonTerminalFenceState struct {
	status    string
	error     string
	result    string
	completed bool
}

func daemonTerminalFenceTaskState(t *testing.T, taskID string) daemonTerminalFenceState {
	t.Helper()
	var state daemonTerminalFenceState
	dbfx.QueryRow(t, `
		SELECT status, COALESCE(error, ''), COALESCE(result, 'null'::jsonb)::text,
		       completed_at IS NOT NULL
		FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&state.status, &state.error, &state.result, &state.completed)
	return state
}

func daemonTerminalFenceCall(t *testing.T, endpoint string, taskID string, body any, handler http.HandlerFunc) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/"+endpoint,
		body, testWorkspaceID, "terminal-fence-daemon")
	req = testutil.WithURLParams(req, "taskId", taskID)
	return testutil.Call(t, handler, req)
}

func seedDaemonTerminalFenceTask(t *testing.T, agentID, issueID, status string, dispatchedAt time.Time) string {
	t.Helper()
	cols := testutil.Cols{
		"agent_id":      agentID,
		"issue_id":      issueID,
		"runtime_id":    handlerTestRuntimeID(t),
		"status":        status,
		"priority":      0,
		"dispatched_at": dispatchedAt,
		"error":         "before terminal callback",
		"result":        testutil.Raw(`'{"before":true}'::jsonb`),
	}
	if status == "running" {
		cols["started_at"] = dispatchedAt.Add(time.Second)
	}
	if status == "waiting_local_directory" {
		cols["wait_reason"] = "path is busy"
		cols["prepare_lease_expires_at"] = testutil.Raw("now() + interval '1 minute'")
	}
	return dbfx.Task(t, agentID, cols)
}

func TestDaemonTerminalCallbacksHonorExpectedDispatchedAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// Keep the value fixed so stale and matching callbacks differ by a whole
	// second; the production CAS intentionally compares at second precision.
	dispatchedAt := time.Date(2024, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	// Claims expose the legacy second-precision timestamp; the status endpoint
	// may expose nanos, but the terminal CAS accepts either representation.
	generation := dispatchedAt.Format(time.RFC3339)
	staleGeneration := dispatchedAt.Add(-time.Hour).Format(time.RFC3339)
	agentID := dbfx.Agent(t, "daemon terminal fence agent", handlerTestRuntimeID(t))
	issueID := dbfx.Issue(t, "daemon terminal fence issue", testutil.Cols{"status": "in_progress"})

	t.Run("stale generation is rejected and leaves the row untouched", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			status  string
			body    map[string]any
			handler http.HandlerFunc
			path    string
		}{
			{
				name:    "complete",
				status:  "running",
				body:    map[string]any{"output": "stale completion", "expected_dispatched_at": staleGeneration},
				handler: testHandler.CompleteTask,
				path:    "complete",
			},
			{
				name:    "fail",
				status:  "dispatched",
				body:    map[string]any{"error": "stale failure", "failure_reason": "agent_error", "expected_dispatched_at": staleGeneration},
				handler: testHandler.FailTask,
				path:    "fail",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				taskID := seedDaemonTerminalFenceTask(t, agentID, issueID, tc.status, dispatchedAt)
				before := daemonTerminalFenceTaskState(t, taskID)
				daemonTerminalFenceCall(t, tc.path, taskID, tc.body, tc.handler).Want(http.StatusConflict)
				after := daemonTerminalFenceTaskState(t, taskID)
				if after != before {
					t.Fatalf("stale callback changed task: before=%+v after=%+v", before, after)
				}
			})
		}
	})

	t.Run("matching generation settles every terminal status", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			status  string
			failed  bool
			handler http.HandlerFunc
			path    string
		}{
			{name: "complete running", status: "running", handler: testHandler.CompleteTask, path: "complete"},
			{name: "fail dispatched", status: "dispatched", failed: true, handler: testHandler.FailTask, path: "fail"},
			{name: "fail waiting", status: "waiting_local_directory", failed: true, handler: testHandler.FailTask, path: "fail"},
			{name: "fail running", status: "running", failed: true, handler: testHandler.FailTask, path: "fail"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				taskID := seedDaemonTerminalFenceTask(t, agentID, issueID, tc.status, dispatchedAt)
				body := map[string]any{"expected_dispatched_at": generation}
				if tc.failed {
					body["error"] = "matching failure"
					body["failure_reason"] = "agent_error"
				} else {
					body["output"] = "matching completion"
				}
				daemonTerminalFenceCall(t, tc.path, taskID, body, tc.handler).Want(http.StatusOK)
				state := daemonTerminalFenceTaskState(t, taskID)
				wantStatus := "completed"
				if tc.failed {
					wantStatus = "failed"
				}
				if state.status != wantStatus || !state.completed {
					t.Fatalf("settled state = %+v, want status %q with completed_at", state, wantStatus)
				}
			})
		}
	})

	t.Run("malformed generation is a bad request", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			status  string
			body    map[string]any
			handler http.HandlerFunc
			path    string
		}{
			{
				name:    "complete",
				status:  "running",
				body:    map[string]any{"output": "bad generation", "expected_dispatched_at": "not-a-timestamp"},
				handler: testHandler.CompleteTask,
				path:    "complete",
			},
			{
				name:    "fail",
				status:  "running",
				body:    map[string]any{"error": "bad generation", "expected_dispatched_at": "not-a-timestamp"},
				handler: testHandler.FailTask,
				path:    "fail",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				taskID := seedDaemonTerminalFenceTask(t, agentID, issueID, tc.status, dispatchedAt)
				before := daemonTerminalFenceTaskState(t, taskID)
				daemonTerminalFenceCall(t, tc.path, taskID, tc.body, tc.handler).Want(http.StatusBadRequest)
				if after := daemonTerminalFenceTaskState(t, taskID); after != before {
					t.Fatalf("malformed generation changed task: before=%+v after=%+v", before, after)
				}
			})
		}
	})

	t.Run("fenced terminal callback is idempotent", func(t *testing.T) {
		taskID := seedDaemonTerminalFenceTask(t, agentID, issueID, "running", dispatchedAt)
		body := map[string]any{"output": "first completion", "expected_dispatched_at": generation}
		daemonTerminalFenceCall(t, "complete", taskID, body, testHandler.CompleteTask).Want(http.StatusOK)
		beforeReplay := daemonTerminalFenceTaskState(t, taskID)
		body["output"] = "replayed completion"
		daemonTerminalFenceCall(t, "complete", taskID, body, testHandler.CompleteTask).Want(http.StatusOK)
		if afterReplay := daemonTerminalFenceTaskState(t, taskID); afterReplay != beforeReplay {
			t.Fatalf("idempotent callback changed terminal row: before=%+v after=%+v", beforeReplay, afterReplay)
		}
		if !bytes.Contains([]byte(beforeReplay.result), []byte("first completion")) {
			t.Fatalf("first completion result = %s, want original callback retained", beforeReplay.result)
		}
	})
}

func TestPrelaunchRejectionDoesNotFailReclaimedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := dbfx.Agent(t, "stale prelaunch agent", handlerTestRuntimeID(t))
	issueID := dbfx.Issue(t, "stale prelaunch issue")
	claimedAt := time.Now().Add(-time.Hour)
	taskID := seedDaemonTerminalFenceTask(t, agentID, issueID, "dispatched", claimedAt)
	claim, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at = $2 WHERE id = $1`, taskID, claimedAt.Add(time.Minute))
	before := daemonTerminalFenceTaskState(t, taskID)
	failure := testHandler.failClaimedTaskBeforeLaunch(context.Background(), &claim,
		"old claim rejected", taskfailure.ReasonAgentMissingConfig,
		"error_required_remote_mcp", http.StatusConflict, "old claim rejected")
	if failure == nil || failure.status != http.StatusConflict {
		t.Fatalf("stale claim rejection = %+v", failure)
	}
	if after := daemonTerminalFenceTaskState(t, taskID); after != before {
		t.Fatalf("stale prelaunch rejection changed reclaimed task: before=%+v after=%+v", before, after)
	}
}
