package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentWorkspaceGrantAgentCreationCLI(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	var called int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/agents/agent-1/workspace-grants/target-1/agent-creation" || r.Header.Get("X-Workspace-ID") != "home-1" {
			t.Errorf("unexpected request: %s %s workspace=%s", r.Method, r.URL.Path, r.Header.Get("X-Workspace-ID"))
		}
		var body struct {
			CanCreateAgents bool `json:"can_create_agents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !body.CanCreateAgents {
			t.Error("grant was not enabled")
		}
		called++
		_ = json.NewEncoder(w).Encode(map[string]any{"workspace_id": "target-1", "can_create_agents": true})
	}))
	defer srv.Close()
	cmd := newRuntimeDeleteTestCmd(srv.URL)
	_ = cmd.Flags().Set("workspace-id", "home-1")
	if err := runAgentWorkspaceGrantAgentCreation(cmd, []string{"agent-1", "target-1", "allow"}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("calls = %d", called)
	}
	if err := runAgentWorkspaceGrantAgentCreation(cmd, []string{"agent-1", "target-1", "invalid"}); err == nil || !strings.Contains(err.Error(), "allow or deny") {
		t.Fatalf("invalid choice: %v", err)
	}
}

func TestTaskCLITargetRuntimeAndAgentCreate(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "mat_test_task")
	t.Setenv("MULTICA_AGENT_ID", "source-agent")
	t.Setenv("MULTICA_TASK_ID", "source-task")
	var listed, created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Workspace-ID") != "target-workspace" {
			t.Errorf("workspace selector = %q", r.Header.Get("X-Workspace-ID"))
		}
		if r.Header.Get("Authorization") != "Bearer mat_test_task" {
			t.Error("task token not used")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/runtimes":
			listed = true
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "target-runtime", "name": "Target", "provider": "codex", "status": "online"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents":
			created = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["runtime_id"] != "target-runtime" {
				t.Errorf("runtime = %v", body["runtime_id"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "target-agent", "name": "Target agent"})
		default:
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	cmd := newRuntimeDeleteTestCmd(srv.URL)
	_ = cmd.Flags().Set("workspace-id", "target-workspace")
	if err := runRuntimeList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	cmd.Flags().String("name", "Target agent", "")
	cmd.Flags().String("runtime-id", "target-runtime", "")
	if err := runAgentCreate(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !listed || !created {
		t.Fatalf("list=%v create=%v", listed, created)
	}
}
