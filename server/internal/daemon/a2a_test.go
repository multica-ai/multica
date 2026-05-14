package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.yaml.in/yaml/v2"
)

// ---------------------------------------------------------------------------
// Config loading tests
// ---------------------------------------------------------------------------

func TestLoadA2AConfig_Nonexistent(t *testing.T) {
	cfg, err := loadA2AConfig("nonexistent-profile-" + t.Name())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config for missing file")
	}
}

func TestLoadA2AConfigFromDir_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, a2aConfigFilename)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadA2AConfigFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config for empty file")
	}
}

func TestLoadA2AConfigFromDir_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `agents:
  - name: "test-agent"
    url: "http://localhost:8900"
  - name: "auth-agent"
    url: "https://agents.example.com/research"
    token_env: "MY_TOKEN"
`
	path := filepath.Join(dir, a2aConfigFilename)
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadA2AConfigFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}
	if cfg.Agents[0].Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", cfg.Agents[0].Name)
	}
	if cfg.Agents[0].URL != "http://localhost:8900" {
		t.Errorf("expected url 'http://localhost:8900', got %q", cfg.Agents[0].URL)
	}
	if cfg.Agents[1].TokenEnv != "MY_TOKEN" {
		t.Errorf("expected token_env 'MY_TOKEN', got %q", cfg.Agents[1].TokenEnv)
	}
}

func TestLoadA2AConfigFromDir_EmptyAgentsList(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `agents: []
`
	path := filepath.Join(dir, a2aConfigFilename)
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadA2AConfigFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(cfg.Agents))
	}
}

func TestLoadA2AConfigFromDir_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, a2aConfigFilename)
	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadA2AConfigFromDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// loadA2AConfigFromDir is a test helper that reads the A2A config from a
// specific directory, bypassing cli.ProfileDir.
func loadA2AConfigFromDir(dir string) (*a2aConfigFile, error) {
	path := filepath.Join(dir, a2aConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var cfg a2aConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Agent Card parsing tests
// ---------------------------------------------------------------------------

func TestFetchAgentCard_Valid(t *testing.T) {
	cardJSON := `{
		"name": "Test Agent",
		"version": "1.0.0",
		"description": "A test agent",
		"defaultInputModes": ["text/plain"],
		"defaultOutputModes": ["text/plain"]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			t.Errorf("expected path /.well-known/agent-card.json, got %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, cardJSON)
	}))
	defer srv.Close()

	card, err := fetchAgentCard(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.Name != "Test Agent" {
		t.Errorf("expected name 'Test Agent', got %q", card.Name)
	}
	if card.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", card.Version)
	}
}

func TestFetchAgentCard_MissingName(t *testing.T) {
	cardJSON := `{"version": "1.0.0"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, cardJSON)
	}))
	defer srv.Close()

	_, err := fetchAgentCard(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestFetchAgentCard_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := fetchAgentCard(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestFetchAgentCard_Unreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := fetchAgentCard(ctx, "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// ---------------------------------------------------------------------------
// State mapping tests
// ---------------------------------------------------------------------------

func TestMapA2AResult_Completed(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID: "task-1",
			Status: a2aStatus{
				State:   "TASK_STATE_COMPLETED",
				Message: &a2aMessage{Parts: []a2aPart{{Text: "done"}}},
			},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected 'completed', got %q", result.Status)
	}
	if result.Comment != "done" {
		t.Errorf("expected 'done', got %q", result.Comment)
	}
}

func TestMapA2AResult_Failed(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID: "task-1",
			Status: a2aStatus{
				State:   "TASK_STATE_FAILED",
				Message: &a2aMessage{Parts: []a2aPart{{Text: "something went wrong"}}},
			},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

func TestMapA2AResult_Cancelled(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID:     "task-1",
			Status: a2aStatus{State: "TASK_STATE_CANCELED"},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "cancelled" {
		t.Errorf("expected 'cancelled', got %q", result.Status)
	}
}

func TestMapA2AResult_InputRequired(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID: "task-1",
			Status: a2aStatus{
				State:   "TASK_STATE_INPUT_REQUIRED",
				Message: &a2aMessage{Parts: []a2aPart{{Text: "need more info"}}},
			},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked' for INPUT_REQUIRED, got %q", result.Status)
	}
}

func TestMapA2AResult_Rejected(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID:     "task-1",
			Status: a2aStatus{State: "TASK_STATE_REJECTED"},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked' for REJECTED, got %q", result.Status)
	}
}

func TestMapA2AResult_UnknownState(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID:     "task-1",
			Status: a2aStatus{State: "TASK_STATE_FUTURE_STATE"},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked' for unknown state, got %q", result.Status)
	}
}

func TestMapA2AResult_MessageOnly(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Message: &a2aMessage{
			Role:  "ROLE_AGENT",
			Parts: []a2aPart{{Text: "Can you clarify?"}},
		},
	})

	result, err := mapA2AResult(raw, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked' for message-only response, got %q", result.Status)
	}
}

func TestMapA2AResult_EmptyResult(t *testing.T) {
	result, err := mapA2AResult(nil, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked' for empty result, got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC dispatch test
// ---------------------------------------------------------------------------

func TestDispatchA2ATask_Completed(t *testing.T) {
	card := &A2AAgentCard{Name: "test-agent", Version: "1.0.0"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aJSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if req.Method != "SendMessage" {
			t.Errorf("expected method 'SendMessage', got %q", req.Method)
		}

		result := a2aSendResult{
			Task: &a2aTaskResponse{
				ID: "a2a-task-1",
				Status: a2aStatus{
					State:   "TASK_STATE_COMPLETED",
					Message: &a2aMessage{Parts: []a2aPart{{Text: "Task output here"}}},
				},
			},
		}
		resultBytes, _ := json.Marshal(result)
		resp := a2aJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  resultBytes,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{
		ID:          "task-123",
		WorkspaceID: "ws-1",
		IssueID:     "issue-1",
		Agent:       &AgentData{Name: "test-agent"},
	}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected 'completed', got %q", result.Status)
	}
	if result.Comment != "Task output here" {
		t.Errorf("expected 'Task output here', got %q", result.Comment)
	}
}

func TestDispatchA2ATask_Failed(t *testing.T) {
	card := &A2AAgentCard{Name: "fail-agent", Version: "1.0.0"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := a2aSendResult{
			Task: &a2aTaskResponse{
				ID: "a2a-task-1",
				Status: a2aStatus{
					State:   "TASK_STATE_FAILED",
					Message: &a2aMessage{Parts: []a2aPart{{Text: "error: bad input"}}},
				},
			},
		}
		resultBytes, _ := json.Marshal(result)
		resp := a2aJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      "task-456",
			Result:  resultBytes,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{ID: "task-456", WorkspaceID: "ws-1", IssueID: "issue-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

func TestDispatchA2ATask_RPCError(t *testing.T) {
	card := &A2AAgentCard{Name: "err-agent", Version: "1.0.0"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := a2aJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      "task-789",
			Error:   &a2aJSONRPCError{Code: -32600, Message: "Invalid Request"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{ID: "task-789", WorkspaceID: "ws-1", IssueID: "issue-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

func TestDispatchA2ATask_NoWorkspace(t *testing.T) {
	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: "http://example.com", Card: &A2AAgentCard{Name: "test"}}
	task := Task{ID: "task-no-ws"}

	_, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err == nil {
		t.Fatal("expected error for missing workspace_id")
	}
}

func TestDispatchA2ATask_UnreachableAgent(t *testing.T) {
	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: "http://127.0.0.1:1", Card: &A2AAgentCard{Name: "test"}}
	task := Task{ID: "task-unreachable", WorkspaceID: "ws-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return blocked result, not an error.
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return json.RawMessage(b)
}
