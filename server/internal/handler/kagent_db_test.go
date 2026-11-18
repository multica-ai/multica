package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Keep these method/path pairs aligned with cmd/server/router.go.
func kagentTestRouter() http.Handler {
	r := chi.NewRouter()
	r.Post("/api/agents", testHandler.CreateAgent)
	r.Put("/api/agents/{id}", testHandler.UpdateAgent)
	r.Put("/api/agents/{id}/env", testHandler.UpdateAgentEnv)
	r.Put("/api/agents/{id}/skills", testHandler.SetAgentSkills)
	r.Post("/api/daemon/runtimes/{runtimeId}/tasks/claim", testHandler.ClaimTaskByRuntime)
	r.Post("/api/daemon/tasks/claim", testHandler.ClaimTasksByRuntime)
	r.Post("/api/daemon/tasks/{taskId}/start", testHandler.StartTask)
	r.Post("/api/daemon/tasks/{taskId}/cancel-ack", testHandler.AckTaskCancelled)
	r.Post("/api/daemon/runtimes/{runtimeId}/recover-orphans", testHandler.RecoverOrphanedTasks)
	return r
}

func TestKagentDBAgentCreateUpdateValidation(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	runtimeID := dbfx.Runtime(t, "Kagent API", testutil.Cols{"provider": "kagent", "runtime_mode": "local"})
	skillID := insertHandlerTestSkill(t, "kagent-db-skill", "Unsupported native skill")
	router := kagentTestRouter()
	base := func() map[string]any {
		return map[string]any{"name": "Kagent native", "runtime_id": runtimeID, "runtime_config": map[string]any{"mode": "native-text"}}
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"missing opt-in", "runtime_config", map[string]any{}},
		{"connection credentials", "runtime_config", map[string]any{"mode": "native-text", "token": "secret"}},
		{"model", "model", "override"},
		{"arguments", "custom_args", []string{"--unsafe"}},
		{"environment", "custom_env", map[string]string{"SECRET": "value"}},
		{"MCP", "mcp_config", map[string]any{"mcpServers": map[string]any{"tool": map[string]string{"url": "https://example.test"}}}},
		{"skills", "skill_ids", []string{skillID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := base()
			body[tc.field] = tc.value
			testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents", body)).Want(http.StatusBadRequest)
		})
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents", base())).Want(http.StatusBadRequest)
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	var created AgentResponse
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents", base())).Want(http.StatusCreated).JSON(&created)
	if created.ID == "" {
		t.Fatal("create returned no agent")
	}
	dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE agent_id=$1`, created.ID)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE id=$1`, created.ID)
	for _, body := range []map[string]any{
		{"runtime_config": map[string]any{}},
		{"runtime_config": map[string]any{"mode": "native-text", "endpoint": "https://untrusted.test"}},
		{"model": "override"},
		{"custom_args": []string{"--unsafe"}},
		{"mcp_config": map[string]any{"mcpServers": map[string]any{"tool": map[string]string{"command": "tool"}}}},
	} {
		testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+created.ID, body)).Want(http.StatusBadRequest)
	}
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+created.ID+"/env", map[string]any{"custom_env": map[string]string{"SECRET": "value"}})).Want(http.StatusBadRequest)
	skillsResponse := testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+created.ID+"/skills", map[string]any{"skill_ids": []string{skillID}})).Want(http.StatusBadRequest)
	if !strings.Contains(skillsResponse.Text(), "kagent") {
		t.Fatalf("skill rejection did not reach provider validation: %s", skillsResponse.Text())
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+created.ID, map[string]any{"runtime_config": map[string]string{"mode": "native-text"}})).Want(http.StatusBadRequest)
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+created.ID, map[string]any{"name": "Renamed native", "runtime_config": map[string]string{"mode": "native-text"}})).Want(http.StatusOK)
	var name string
	var config []byte
	dbfx.QueryRow(t, `SELECT name, runtime_config FROM agent WHERE id=$1`, created.ID).Scan(&name, &config)
	if name != "Renamed native" || !strings.Contains(string(config), `"native-text"`) {
		t.Fatalf("saved agent name=%q config=%s", name, config)
	}
	// Switching a configured existing provider must validate stored overrides.
	otherRuntime := dbfx.Runtime(t, "Claude API", testutil.Cols{"provider": "claude"})
	other := dbfx.Agent(t, "Claude configured", otherRuntime, testutil.Cols{"model": "existing-model"})
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+other, map[string]any{"runtime_id": runtimeID, "runtime_config": map[string]string{"mode": "native-text"}})).Want(http.StatusBadRequest)
	var persistedRuntime string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id=$1`, other).Scan(&persistedRuntime)
	if persistedRuntime != otherRuntime {
		t.Fatal("rejected update changed runtime")
	}
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+other, map[string]any{"model": "updated-model"})).Want(http.StatusOK)
}

func TestKagentDBClaimNativeTextAndExistingProvider(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	for _, provider := range []string{"kagent", "claude"} {
		for _, batch := range []bool{false, true} {
			name := provider + "/single"
			if batch {
				name = provider + "/batch"
			}
			t.Run(name, func(t *testing.T) {
				runtimeID := dbfx.Runtime(t, "claim native", testutil.Cols{"provider": provider, "runtime_mode": "local"})
				agentID := dbfx.Agent(t, "claim native", runtimeID, testutil.Cols{"runtime_mode": "local", "runtime_config": `{"mode":"native-text"}`, "instructions": "Be concise."})
				session := dbfx.ChatSession(t, agentID)
				taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": session})
				dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": session, "role": "user", "content": "hello native"})
				path := "/api/daemon/runtimes/" + runtimeID + "/tasks/claim"
				var body any
				if batch {
					path = "/api/daemon/tasks/claim"
					body = map[string]any{"runtime_ids": []string{runtimeID}, "daemon_id": "kagent-db-claim", "max_tasks": 1}
				}
				response := testutil.Call(t, kagentTestRouter().ServeHTTP, newOwnerDaemonRequest("POST", path, body)).Want(http.StatusOK)
				var wire struct {
					Task  *AgentTaskResponse  `json:"task"`
					Tasks []AgentTaskResponse `json:"tasks"`
				}
				response.JSON(&wire)
				claim := wire.Task
				if batch && len(wire.Tasks) == 1 {
					claim = &wire.Tasks[0]
				}
				if claim == nil || claim.ID != taskID || claim.Agent == nil || claim.Agent.ID != agentID || claim.RuntimeID != runtimeID || claim.ChatMessage != "hello native" {
					t.Fatalf("wrong claim: %s", response.Text())
				}
				if provider == "kagent" {
					if claim.AuthToken != "" || len(claim.Agent.Skills) != 0 || len(claim.Agent.SkillRefs) != 0 {
						t.Fatalf("native claim received credential or skills: %s", response.Text())
					}
					var rc map[string]string
					if err := json.Unmarshal(claim.Agent.RuntimeConfig, &rc); err != nil || rc["mode"] != "native-text" {
						t.Fatalf("native config=%s err=%v", claim.Agent.RuntimeConfig, err)
					}
				} else if !strings.HasPrefix(claim.AuthToken, "mat_") || len(claim.Agent.Skills)+len(claim.Agent.SkillRefs) == 0 {
					t.Fatalf("existing provider lost credential or built-in skills: %s", response.Text())
				}
				var status string
				var dispatched time.Time
				dbfx.QueryRow(t, `SELECT status, dispatched_at FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &dispatched)
				if status != "dispatched" || dispatched.IsZero() || claim.DispatchedAt == nil || *claim.DispatchedAt == "" {
					t.Fatalf("claim not committed: %s %v", status, dispatched)
				}
			})
		}
	}
}

func TestKagentDBBulkCancelPersistsRemoteFence(t *testing.T) {
	for _, mode := range []string{"agent", "archive"} {
		t.Run(mode, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Kagent bulk", testutil.Cols{"provider": "kagent", "runtime_mode": "local"})
			agentID := dbfx.Agent(t, "Kagent bulk", runtimeID)
			issueID := dbfx.Issue(t, "Kagent bulk")
			taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "started_at": testutil.Raw("now()"), "result": testutil.Raw(`'{"process_cleanup_capability":"kagent-process-cleanup-v1"}'::jsonb`)})
			var err error
			if mode == "archive" {
				_, err = testHandler.TaskService.CancelTasksForArchivedAgent(t.Context(), parseUUID(agentID))
			} else {
				_, err = testHandler.TaskService.CancelTasksForAgent(t.Context(), parseUUID(agentID))
			}
			if err != nil {
				t.Fatal(err)
			}
			var status string
			var pending bool
			dbfx.QueryRow(t, `SELECT status, COALESCE(result->>'process_cleanup_pending'='true',false) FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &pending)
			if status != "cancelled" || !pending {
				t.Fatalf("bulk cancellation lost fence: status=%s pending=%v", status, pending)
			}
			next := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched"})
			blocked, err := testHandler.Queries.TaskBlockedByProcessCleanup(t.Context(), parseUUID(next))
			if err != nil || !blocked {
				t.Fatalf("successor not fenced: blocked=%v err=%v", blocked, err)
			}
		})
	}
}

func TestKagentDBCleanupAndRecoveryFences(t *testing.T) {
	for _, path := range []string{"cancel-confirmed", "cancel-unconfirmed", "restart", "timeout", "offline"} {
		t.Run(path, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Kagent fences", testutil.Cols{"provider": "kagent", "runtime_mode": "local"})
			agentID := dbfx.Agent(t, "Kagent fences", runtimeID)
			issueID := dbfx.Issue(t, "Kagent fences")
			taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched", "dispatched_at": testutil.Raw("now() - interval '4 hours'"), "attempt": 1, "max_attempts": 2})
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID)
			var generation time.Time
			dbfx.QueryRow(t, `SELECT dispatched_at FROM agent_task_queue WHERE id=$1`, taskID).Scan(&generation)
			router := kagentTestRouter()
			start := func(id string, at time.Time) *http.Request {
				return newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+id+"/start", map[string]any{"runtime_id": runtimeID, "dispatched_at": at.Format(time.RFC3339Nano), "capabilities": []string{protocol.DaemonCapabilityKagentProcessCleanupV1}})
			}
			testutil.Call(t, router.ServeHTTP, start(taskID, generation.Add(-time.Microsecond))).Want(http.StatusConflict)
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
			if status != "dispatched" {
				t.Fatalf("stale start mutated task: %s", status)
			}
			var started map[string]any
			testutil.Call(t, router.ServeHTTP, start(taskID, generation)).Want(http.StatusOK).JSON(&started)
			if started["process_cleanup_capability"] != protocol.DaemonCapabilityKagentProcessCleanupV1 {
				t.Fatalf("start=%v", started)
			}
			if strings.HasPrefix(path, "cancel-") {
				if _, err := testHandler.TaskService.CancelTask(t.Context(), parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
				var pending bool
				dbfx.QueryRow(t, `SELECT COALESCE(result->>'process_cleanup_pending'='true',false) FROM agent_task_queue WHERE id=$1`, taskID).Scan(&pending)
				if !pending {
					t.Fatal("cancel did not persist cleanup fence")
				}
				ack := map[string]any{}
				if path == "cancel-confirmed" {
					ack["process_cleanup_confirmed"] = true
				}
				for range 2 {
					testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+taskID+"/cancel-ack", ack)).Want(http.StatusOK)
				}
			} else if path == "restart" {
				testutil.Call(t, router.ServeHTTP, newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/recover-orphans", nil)).Want(http.StatusOK)
			} else {
				dbfx.Exec(t, `UPDATE agent_task_queue SET started_at=now()-interval '4 hours' WHERE id=$1`, taskID)
				dbfx.Exec(t, `UPDATE agent_runtime SET last_seen_at=now()-interval '4 hours' WHERE id=$1`, runtimeID)
				var rows []db.AgentTaskQueue
				var err error
				if path == "offline" {
					dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID)
					rows, err = testHandler.TaskService.FailTasksForOfflineRuntimes(t.Context(), db.FailTasksForOfflineRuntimesParams{ReconnectGraceSecs: 60, MaxPerTick: 10000})
				} else {
					rows, err = testHandler.TaskService.FailStaleTasks(t.Context(), db.FailStaleTasksParams{DispatchTimeoutSecs: 60, RunningTimeoutSecs: 60, RuntimeStaleSecs: 60, RuntimeReconnectGraceSecs: 60})
				}
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, row := range rows {
					if row.ID == parseUUID(taskID) {
						found = true
						testHandler.TaskService.HandleFailedTasks(t.Context(), []db.AgentTaskQueue{row})
					}
				}
				if !found {
					t.Fatal("fixture was not recovered")
				}
			}
			var reason string
			dbfx.QueryRow(t, `SELECT status, COALESCE(failure_reason,'') FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status, &reason)
			protected := path != "cancel-confirmed"
			if protected && (status != "failed" || reason != "cleanup_unconfirmed") {
				t.Fatalf("recovered task=%s/%s", status, reason)
			}
			if !protected && status != "cancelled" {
				t.Fatalf("confirmed cancellation=%s", status)
			}
			var retries int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, taskID).Scan(&retries)
			if retries != 0 {
				t.Fatalf("cleanup created %d automatic retries", retries)
			}
			successor := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "waiting_local_directory"})
			blocked, err := testHandler.Queries.TaskBlockedByProcessCleanup(t.Context(), parseUUID(successor))
			if err != nil || blocked != protected {
				t.Fatalf("successor blocked=%v want=%v err=%v", blocked, protected, err)
			}
			if protected {
				manual := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched", "rerun_of_task_id": taskID, "initiator_user_id": testUserID})
				blocked, err = testHandler.Queries.TaskBlockedByProcessCleanup(t.Context(), parseUUID(manual))
				if err != nil || !blocked {
					t.Fatalf("manual rerun must not bypass unresolved remote cleanup: blocked=%v err=%v", blocked, err)
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, manual)
				blocked, err = testHandler.Queries.TaskBlockedByProcessCleanup(t.Context(), parseUUID(successor))
				if err != nil || !blocked {
					t.Fatalf("later success must not clear unresolved remote cleanup: blocked=%v err=%v", blocked, err)
				}
			}
		})
	}
}
