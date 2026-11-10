package handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A daemon restart cannot stop an independent Muse worker. No worker-stop
// acknowledgement is supplied during recovery, so automatic work stays fenced.
func TestMuseAppRecoveryBlocksAutomaticSuccessor(t *testing.T) {
	if testHandler == nil {
		t.Fatal("PostgreSQL is required")
	}
	runtimeID := dbfx.Runtime(t, "Muse recovery", testutil.Cols{"provider": "museapp"})
	agentID := dbfx.Agent(t, "Muse recovery", runtimeID)
	issueID := dbfx.Issue(t, "Muse recovery")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID,
		"status": "dispatched", "dispatched_at": time.Now().UTC(),
		"attempt": 1, "max_attempts": 2,
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID)
	start := withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+taskID+"/start", map[string]any{
		"capabilities": []string{"museapp-process-cleanup-v1"},
	}), "taskId", taskID)
	testutil.Call(t, testHandler.StartTask, start).Want(http.StatusOK)
	recoverReq := withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/recover-orphans", nil), "runtimeId", runtimeID)
	var recovered map[string]any
	testutil.Call(t, testHandler.RecoverOrphanedTasks, recoverReq).Want(http.StatusOK).JSON(&recovered)
	var failure, capability string
	dbfx.QueryRow(t, `SELECT failure_reason, result->>'process_cleanup_capability' FROM agent_task_queue WHERE id=$1`, taskID).Scan(&failure, &capability)
	if failure != "cleanup_unconfirmed" || capability != "museapp-process-cleanup-v1" {
		t.Fatalf("recovery=%v failure=%s capability=%s", recovered, failure, capability)
	}
	var retries int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID).Scan(&retries)
	if retries != 0 {
		t.Fatalf("recovery created %d automatic retries", retries)
	}
	successor := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched", "dispatched_at": time.Now().UTC()})
	start = withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+successor+"/start", nil), "taskId", successor)
	testutil.Call(t, testHandler.StartTask, start).Want(http.StatusConflict)
	manual := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched", "rerun_of_task_id": taskID, "initiator_user_id": testUserID})
	start = withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+manual+"/start", map[string]any{"capabilities": []string{"museapp-process-cleanup-v1"}}), "taskId", manual)
	testutil.Call(t, testHandler.StartTask, start).Want(http.StatusOK)
}

func TestMuseAppInfrastructureRecoveryPolicies(t *testing.T) {
	if testHandler == nil {
		t.Fatal("PostgreSQL is required")
	}
	for _, path := range []string{"restart", "offline", "timeout"} {
		for _, tc := range []struct {
			name, provider, status, capability string
			rebound                            bool
			protected                          bool
		}{
			{"started", "museapp", "running", "museapp-process-cleanup-v1", false, true},
			{"legacy daemon", "museapp", "running", "", false, true},
			{"runtime changed", "museapp", "running", "museapp-process-cleanup-v1", true, true},
			{"not started", "museapp", "dispatched", "", false, false},
			{"waiting for workdir", "museapp", "waiting_local_directory", "", false, false},
			{"Codex", "codex", "running", "", false, false},
			{"Pi", "pi", "running", "pi-process-cleanup-v1", false, false},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				runtimeID := dbfx.Runtime(t, "recovery policy", testutil.Cols{"provider": tc.provider})
				agentID := dbfx.Agent(t, "recovery policy", runtimeID)
				issueID := dbfx.Issue(t, "recovery policy")
				cols := testutil.Cols{
					"runtime_id": runtimeID, "issue_id": issueID, "status": tc.status,
					"dispatched_at": testutil.Raw("now() - interval '4 hours'"),
					"attempt":       1, "max_attempts": 2,
				}
				if tc.status == "running" {
					cols["started_at"] = testutil.Raw("now() - interval '4 hours'")
				}
				if tc.capability != "" {
					cols["result"] = `{"process_cleanup_capability":"` + tc.capability + `"}`
				}
				taskID := dbfx.Task(t, agentID, cols)
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID)
				if tc.rebound {
					dbfx.Exec(t, `UPDATE agent_runtime SET provider='codex' WHERE id=$1`, runtimeID)
				}
				if path != "restart" {
					dbfx.Exec(t, `UPDATE agent_runtime SET last_seen_at=now()-interval '4 hours' WHERE id=$1`, runtimeID)
					if path == "offline" {
						dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID)
					}
				}
				var rows []db.AgentTaskQueue
				var err error
				wantReason := "runtime_recovery"
				switch path {
				case "restart":
					rows, err = testHandler.TaskService.RecoverOrphanedTasksForRuntime(t.Context(), parseUUID(runtimeID))
				case "offline":
					wantReason = "runtime_offline"
					rows, err = testHandler.TaskService.FailTasksForOfflineRuntimes(t.Context(), db.FailTasksForOfflineRuntimesParams{ReconnectGraceSecs: 60, MaxPerTick: 10000})
				case "timeout":
					wantReason = "timeout"
					rows, err = testHandler.TaskService.FailStaleTasks(t.Context(), db.FailStaleTasksParams{DispatchTimeoutSecs: 60, RunningTimeoutSecs: 60, RuntimeStaleSecs: 60, RuntimeReconnectGraceSecs: 60})
				}
				if err != nil {
					t.Fatal(err)
				}
				var recovered *db.AgentTaskQueue
				for i := range rows {
					if rows[i].ID == parseUUID(taskID) {
						recovered = &rows[i]
						break
					}
				}
				if path == "timeout" && tc.status == "waiting_local_directory" {
					if recovered != nil {
						t.Fatal("timeout reclaimed a legitimate workdir waiter")
					}
					return
				}
				if recovered == nil {
					t.Fatal("fixture task was not recovered")
				}
				if tc.protected {
					wantReason = "cleanup_unconfirmed"
				}
				if recovered.FailureReason.String != wantReason {
					t.Fatalf("failure = %s, want %s", recovered.FailureReason.String, wantReason)
				}
				var persistedReason string
				dbfx.QueryRow(t, `SELECT failure_reason FROM agent_task_queue WHERE id=$1`, taskID).Scan(&persistedReason)
				if persistedReason != wantReason {
					t.Fatalf("persisted reason = %s, want %s", persistedReason, wantReason)
				}
				if tc.protected && !strings.Contains(recovered.Error.String, "worker may still be running") {
					t.Fatalf("missing recovery guidance: %s", recovered.Error.String)
				}
				testHandler.TaskService.HandleFailedTasks(t.Context(), []db.AgentTaskQueue{*recovered})
				var retries int
				dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID).Scan(&retries)
				wantRetries := 1
				if tc.protected {
					wantRetries = 0
				}
				if retries != wantRetries {
					t.Fatalf("retries = %d, want %d", retries, wantRetries)
				}
				var successor string
				if tc.protected {
					successor = dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched"})
				} else {
					var state string
					dbfx.QueryRow(t, `SELECT id::text, status FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID).Scan(&successor, &state)
					wantState := "queued"
					if path == "offline" {
						wantState = "deferred"
					}
					if state != wantState {
						t.Fatalf("retry state = %s, want %s", state, wantState)
					}
					dbfx.Exec(t, `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, successor)
				}
				blocked, err := testHandler.Queries.TaskBlockedByProcessCleanup(t.Context(), parseUUID(successor))
				if err != nil || blocked != tc.protected {
					t.Fatalf("successor blocked=%v, want %v, err=%v", blocked, tc.protected, err)
				}
			})
		}
	}
}
