package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestClaudeDisconnectRemovesConnectionAndUnbindsAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	cleanupClaudeConnection(t)
	input := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)
	runtimeID := dbfx.Runtime(t, "Claude", testutil.Cols{"provider": "claude_managed", "runtime_mode": "cloud", "daemon_id": claudeManagedDaemonPrefix + testWorkspaceID})
	agentID := dbfx.Agent(t, "Claude agent", runtimeID)

	deleteReq := newRequest("DELETE", "/api/runtimes/"+runtimeID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	deleteReq = deleteReq.WithContext(context.WithValue(deleteReq.Context(), chi.RouteCtxKey, rctx))
	var managed map[string]any
	testutil.Call(t, testHandler.DeleteAgentRuntime, deleteReq).Want(409).JSON(&managed)
	if managed["code"] != "runtime_managed_by_connection" {
		t.Fatalf("generic delete of a Claude runtime: %v", managed)
	}

	var changed map[string]any
	testutil.Call(t, testHandler.DisconnectClaudeConnection, vcsHandlerRequest("DELETE", "/claude-managed", map[string]any{"expected_active_agent_ids": []string{}}, "")).Want(409).JSON(&changed)
	if changed["code"] != "runtime_delete_plan_changed" {
		t.Fatalf("unconfirmed agents should refuse with the latest plan: %v", changed)
	}
	testutil.Call(t, testHandler.DisconnectClaudeConnection, vcsHandlerRequest("DELETE", "/claude-managed", map[string]any{"expected_active_agent_ids": []string{"not-a-uuid"}}, "")).Want(400)

	body := map[string]any{"expected_active_agent_ids": []string{agentID}}
	testutil.Call(t, testHandler.DisconnectClaudeConnection, vcsHandlerRequest("DELETE", "/claude-managed", body, "")).Want(200)

	var connections, runtimes, agents int
	var bound bool
	dbfx.QueryRow(t, `SELECT count(*) FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&connections)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime WHERE id=$1`, runtimeID).Scan(&runtimes)
	dbfx.QueryRow(t, `SELECT count(*), bool_or(runtime_id IS NOT NULL) FROM agent WHERE id=$1`, agentID).Scan(&agents, &bound)
	if connections != 0 || runtimes != 0 || agents != 1 || bound {
		t.Fatalf("disconnect: connections=%d runtimes=%d agents=%d bound=%v", connections, runtimes, agents, bound)
	}
	testutil.Call(t, testHandler.DisconnectClaudeConnection, vcsHandlerRequest("DELETE", "/claude-managed", body, "")).Want(404)
}

func TestClaudeCatalogHandlersRefuseWithoutACredential(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	cleanupClaudeConnection(t)

	testutil.Call(t, testHandler.ListClaudeAgents, vcsHandlerRequest(http.MethodGet, "/claude-managed/agents", nil, "")).Want(409)
	testutil.Call(t, testHandler.ListClaudeEnvironments, vcsHandlerRequest(http.MethodPost, "/claude-managed/environments", map[string]any{}, "")).Want(400)
	testutil.Call(t, testHandler.ListClaudeEnvironments, vcsHandlerRequest(http.MethodPost, "/claude-managed/environments", map[string]any{"unexpected": "field"}, "")).Want(400)
}

func TestStandaloneClaudeBridgeRuntimeIsNotConnectionManaged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	managedID := dbfx.Runtime(t, "Managed Claude", testutil.Cols{"provider": "claude_managed", "runtime_mode": "cloud", "daemon_id": claudeManagedDaemonPrefix + testWorkspaceID})
	standaloneID := dbfx.Runtime(t, "Standalone Claude", testutil.Cols{"provider": "claude_managed", "runtime_mode": "cloud", "daemon_id": "claude-bridge-on-my-server"})
	localID := dbfx.Runtime(t, "Local Claude Code", testutil.Cols{"provider": "claude"})
	for runtimeID, want := range map[string]bool{managedID: true, standaloneID: false, localID: false} {
		rt, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(runtimeID))
		if err != nil {
			t.Fatal(err)
		}
		got, err := testHandler.connectionManagesRuntime(ctx, rt)
		if err != nil {
			t.Fatal(err)
		}
		if want && !got {
			// Without a saved connection the managed runtime is not owned by one.
			continue
		}
		if got != want {
			t.Errorf("connectionManagesRuntime(%s) = %v, want %v", rt.Name, got, want)
		}
	}
}

func TestClaudeDisconnectStopsSessionsOfUnconfirmedCreates(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	cleanupClaudeConnection(t)
	input := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)

	created := time.Now().UTC()
	stopped := map[string]bool{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/sessions" && r.Method == http.MethodGet:
			var data []map[string]any
			for i, task := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
				data = append(data, map[string]any{"id": fmt.Sprintf("sess_%d", i), "agent": map[string]string{"id": "agent_1"}, "created_at": created.Format(time.RFC3339), "metadata": map[string]string{"multica_task_id": task, "multica_workspace_id": testWorkspaceID}})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data, "next_page": nil})
		case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodPost:
			stopped[strings.Split(r.URL.Path, "/")[2]] = true
			io.WriteString(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/sessions/") && r.Method == http.MethodGet:
			status := "running"
			if stopped[strings.TrimPrefix(r.URL.Path, "/sessions/")] {
				status = "idle"
			}
			json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/sessions/"), "status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	prevURL := claudeAnthropicURL
	claudeAnthropicURL = api.URL
	defer func() { claudeAnthropicURL = prevURL }()

	attempt := created.Add(-time.Minute).Format(time.RFC3339)
	journal := `{"binding":"b","runs":[{"task_id":"11111111-1111-4111-8111-111111111111","agent_id":"agent_1","phase":"creating","create_attempt_at":"` + attempt + `"}],"unresolved":[{"task_id":"22222222-2222-4222-8222-222222222222","agent_id":"agent_1","create_attempt_at":"` + attempt + `"}]}`
	sealed, err := testHandler.sealVCSSecret(journal)
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `UPDATE cloud_agent_connection SET journal_encrypted=$2 WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID, sealed)

	if err := testHandler.cleanupClaudeJournal(context.Background(), testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if !stopped["sess_0"] || !stopped["sess_1"] {
		t.Fatalf("sessions of unconfirmed creates were not stopped: %v", stopped)
	}
	var remaining string
	dbfx.QueryRow(t, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&remaining)
	if remaining != "" {
		t.Fatal("journal kept after cleanup")
	}
}
