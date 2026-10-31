package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func reclaimDispatchedTask(t *testing.T, taskID string) time.Time {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&runtimeID)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET status = 'online', last_seen_at = now() WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("refresh runtime for reclaim: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET dispatched_at = now() - interval '10 minutes', prepare_lease_expires_at = now() - interval '1 minute' WHERE id = $1`, taskID); err != nil {
		t.Fatalf("age dispatch for reclaim: %v", err)
	}
	reclaimed, err := testHandler.Queries.ReclaimStaleDispatchedTaskForRuntime(ctx, db.ReclaimStaleDispatchedTaskForRuntimeParams{
		RuntimeID:         parseUUID(runtimeID),
		ClaimRecoverySecs: 30,
		PrepareLeaseSecs:  60,
		RuntimeStaleSecs:  service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		t.Fatalf("reclaim stale dispatch: %v", err)
	}
	if uuidToString(reclaimed.ID) != taskID {
		t.Fatalf("reclaimed task = %s, want %s", uuidToString(reclaimed.ID), taskID)
	}
	return reclaimed.DispatchedAt.Time
}

func TestTerminalGenerationFence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, endpoint := range []string{"complete", "fail"} {
		t.Run(endpoint, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "terminal fence")
			agentID := dbfx.Agent(t, "terminal fence", runtimeID)
			taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": dbfx.Issue(t, "terminal fence"), "status": "dispatched", "dispatched_at": testutil.Raw("now()")})
			var generation time.Time
			dbfx.QueryRow(t, "SELECT dispatched_at FROM agent_task_queue WHERE id = $1", taskID).Scan(&generation)
			reclaimed := reclaimDispatchedTask(t, taskID)
			if endpoint == "complete" {
				if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
			}
			call := func(expected string) *testutil.Response {
				req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/"+endpoint, map[string]any{"output": "done", "error": "cancelled", "failure_reason": "cancelled", "expected_dispatched_at": expected}), "taskId", taskID)
				if endpoint == "complete" {
					return testutil.Call(t, testHandler.CompleteTask, req)
				}
				return testutil.Call(t, testHandler.FailTask, req)
			}
			call("yesterday").Want(http.StatusBadRequest)
			stale := call(generation.UTC().Format(time.RFC3339Nano)).Want(http.StatusConflict)
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(stale.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != protocol.DaemonTaskClaimGenerationMismatchCode {
				t.Fatalf("code = %s", body.Code)
			}
			row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			if !row.DispatchedAt.Time.Equal(reclaimed) || (row.Status != "dispatched" && row.Status != "running") || row.CompletedAt.Valid {
				t.Fatalf("stale report changed reclaim: %+v", row)
			}
			matching := reclaimed.In(time.FixedZone("KST", 9*60*60)).Format(time.RFC3339Nano)
			call(matching).Want(http.StatusOK)
			call(matching).Want(http.StatusOK)
			row, err = testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if endpoint == "fail" {
				want = "failed"
			}
			if row.Status != want {
				t.Fatalf("status = %s, want %s", row.Status, want)
			}
			call(generation.UTC().Format(time.RFC3339Nano)).Want(http.StatusConflict)
			legacyID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": dbfx.Issue(t, "legacy terminal"), "status": "running", "dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()")})
			taskID = legacyID
			call("").Want(http.StatusOK)
		})
	}
}

func TestTerminalGenerationParsing(t *testing.T) {
	for _, raw := range []string{"yesterday", "0001-01-01T00:00:00Z", " "} {
		if _, err := parseExpectedDispatchedAt(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if value, err := parseExpectedDispatchedAt(""); err != nil || !value.IsZero() {
		t.Fatalf("legacy value = %v, %v", value, err)
	}
	for _, raw := range []string{"2026-10-27T00:00:00.123456Z", "2026-10-27T09:00:00.123456+09:00"} {
		value, err := parseExpectedDispatchedAt(raw)
		if err != nil || value.UTC().Format(time.RFC3339Nano) != "2026-10-27T00:00:00.123456Z" {
			t.Fatalf("parsed %q = %v, %v", raw, value, err)
		}
	}
}

func TestTerminalGenerationCancelledCleanup(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "cleanup fence")
	agentID := dbfx.Agent(t, "cleanup fence", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": dbfx.Issue(t, "cleanup fence"), "status": "cancelled", "dispatched_at": testutil.Raw("now()"), "started_at": testutil.Raw("now()")})
	var generation time.Time
	dbfx.QueryRow(t, "SELECT dispatched_at FROM agent_task_queue WHERE id = $1", taskID).Scan(&generation)
	call := func(expected time.Time) *testutil.Response {
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/fail", map[string]any{"error": "cleanup unconfirmed", "failure_reason": "cleanup_unconfirmed", "expected_dispatched_at": expected.Format(time.RFC3339Nano)}), "taskId", taskID)
		return testutil.Call(t, testHandler.FailTask, req)
	}
	call(generation.Add(-time.Second)).Want(http.StatusConflict)
	row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil || row.Status != "cancelled" {
		t.Fatalf("stale cleanup changed row: %s, %v", row.Status, err)
	}
	call(generation).Want(http.StatusOK)
	row, err = testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil || row.Status != "failed" {
		t.Fatalf("matching cleanup did not fail row: %s, %v", row.Status, err)
	}
}
