package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/claudemanagedruntime"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestClaudeConfigurationRetainsPendingCleanup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, queue := range []string{"unresolved", "cleanup"} {
		for _, action := range []string{"save", "resume"} {
			t.Run(queue+"/"+action, func(t *testing.T) {
				withVCSBox(t)
				cleanupClaudeConnection(t)
				input := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "fixture-token"}
				testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)
				state := map[string]any{
					"binding":    claudemanagedruntime.Binding("", testWorkspaceID, claudemanagedruntime.DefaultAnthropicURL, "env_test"),
					"runtime_id": "existing-runtime",
				}
				if queue == "unresolved" {
					state[queue] = []map[string]any{{"task_id": "11111111-1111-4111-8111-111111111111", "agent_id": "agent_1", "create_attempt_at": time.Now().UTC()}}
				} else {
					state[queue] = []string{"sess_1"}
				}
				raw, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				sealed, err := testHandler.sealVCSSecret(string(raw))
				if err != nil {
					t.Fatal(err)
				}
				dbfx.Exec(t, `UPDATE cloud_agent_connection SET journal_encrypted=$2,enabled=false WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID, sealed)
				call := func() *testutil.Response {
					if action == "resume" {
						return testutil.Call(t, testHandler.ResumeClaudeConnection, vcsHandlerRequest("POST", "/claude-managed/resume", input, ""))
					}
					return testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, ""))
				}
				input["environment_id"] = "env_other"
				call().Want(http.StatusConflict)
				input["environment_id"] = "env_test"
				input["anthropic_token"] = "replacement-token"
				input["name"] = "Renamed Claude"
				call().Want(http.StatusOK)
				var remaining string
				dbfx.QueryRow(t, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&remaining)
				if remaining != sealed {
					t.Fatal("configuration update changed the pending cleanup journal")
				}
				cfg, enabled, _, _, err := testHandler.readClaudeSettings(context.Background(), testWorkspaceID)
				if err != nil || cfg.AnthropicToken != "replacement-token" || cfg.Name != "Renamed Claude" || enabled != (action == "resume") {
					t.Fatalf("configuration did not recover: enabled=%v err=%v", enabled, err)
				}
			})
		}
	}
}

func TestInactiveClaudeCleanupRecoversAfterCredentialReplacement(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			withVCSBox(t)
			cleanupClaudeConnection(t)
			input := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "revoked-token"}
			testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)
			const taskID = "11111111-1111-4111-8111-111111111111"
			created := time.Now().UTC()
			raw, err := json.Marshal(map[string]any{"unresolved": []map[string]any{{"task_id": taskID, "agent_id": "agent_1", "create_attempt_at": created.Add(-time.Minute)}}})
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := testHandler.sealVCSSecret(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Exec(t, `UPDATE cloud_agent_connection SET journal_encrypted=$2,enabled=false WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID, sealed)
			stopped := false
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer replacement-token" {
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/sessions":
					json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "sess_1", "agent": map[string]string{"id": "agent_1"}, "created_at": created.Format(time.RFC3339), "metadata": map[string]string{"multica_task_id": taskID, "multica_workspace_id": testWorkspaceID}}}})
				case strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodPost:
					stopped = true
					json.NewEncoder(w).Encode(map[string]any{})
				case r.URL.Path == "/sessions/sess_1":
					remoteStatus := "running"
					if stopped {
						remoteStatus = "idle"
					}
					json.NewEncoder(w).Encode(map[string]string{"status": remoteStatus})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(api.Close)
			previousURL := claudeAnthropicURL
			claudeAnthropicURL = api.URL
			t.Cleanup(func() { claudeAnthropicURL = previousURL })
			testHandler.cleanupInactiveClaude(context.Background())
			var remaining string
			var view claudeSettingsView
			testutil.Call(t, testHandler.GetClaudeConnection, vcsHandlerRequest("GET", "/claude-managed", nil, "")).Want(200).JSON(&view)
			dbfx.QueryRow(t, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&remaining)
			if remaining != sealed || stopped || view.Status != "error" || !strings.Contains(view.LastError, "API key") {
				t.Fatalf("authorization failure was not retained and exposed: stopped=%v status=%s error=%s", stopped, view.Status, view.LastError)
			}
			input["anthropic_token"] = "replacement-token"
			testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)
			testHandler.cleanupInactiveClaude(context.Background())
			dbfx.QueryRow(t, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&remaining)
			testutil.Call(t, testHandler.GetClaudeConnection, vcsHandlerRequest("GET", "/claude-managed", nil, "")).Want(200).JSON(&view)
			if remaining != "" || !stopped || view.Status != "stopped" || view.LastError != "" {
				t.Fatalf("cleanup did not recover after credential replacement: stopped=%v status=%s error=%s", stopped, view.Status, view.LastError)
			}
		})
	}
}
