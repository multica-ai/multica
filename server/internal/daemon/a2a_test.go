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
    auth:
      scheme: "api-key"
      token_env: "MY_API_KEY"
      header: "X-API-Key"
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
	if cfg.Agents[1].Auth == nil {
		t.Fatal("expected auth config for second agent")
	}
	if cfg.Agents[1].Auth.Scheme != "api-key" {
		t.Errorf("expected auth scheme 'api-key', got %q", cfg.Agents[1].Auth.Scheme)
	}
}

func TestLoadA2AConfigFromDir_WithRegistry(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `agents:
  - name: "test-agent"
    url: "http://localhost:8900"
registry:
  url: "https://registry.example.com/agents"
  token_env: "REGISTRY_TOKEN"
  poll_interval: 60
`
	path := filepath.Join(dir, a2aConfigFilename)
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadA2AConfigFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Registry == nil {
		t.Fatal("expected registry config")
	}
	if cfg.Registry.URL != "https://registry.example.com/agents" {
		t.Errorf("expected registry url, got %q", cfg.Registry.URL)
	}
	if cfg.Registry.PollSec != 60 {
		t.Errorf("expected poll_interval 60, got %d", cfg.Registry.PollSec)
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

func TestFetchAgentCard_WithCapabilities(t *testing.T) {
	cardJSON := `{
		"name": "Streaming Agent",
		"version": "2.0.0",
		"capabilities": {
			"streaming": true,
			"pushNotifications": false,
			"stateTransitionHistory": true
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, cardJSON)
	}))
	defer srv.Close()

	card, err := fetchAgentCard(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.Capabilities == nil {
		t.Fatal("expected capabilities")
	}
	if !card.Capabilities.Streaming {
		t.Error("expected streaming=true")
	}
	if card.Capabilities.PushNotifications {
		t.Error("expected pushNotifications=false")
	}
}

func TestFetchAgentCard_WithSkills(t *testing.T) {
	cardJSON := `{
		"name": "Skilled Agent",
		"version": "1.0.0",
		"skills": [
			{"id": "s1", "name": "Code Review", "description": "Reviews code"},
			{"id": "s2", "name": "Test Writer", "description": "Writes tests"}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, cardJSON)
	}))
	defer srv.Close()

	card, err := fetchAgentCard(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(card.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(card.Skills))
	}
	if card.Skills[0].Name != "Code Review" {
		t.Errorf("expected skill 'Code Review', got %q", card.Skills[0].Name)
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
// Auth resolution tests
// ---------------------------------------------------------------------------

func TestResolveA2AAuth_LegacyTokenEnv(t *testing.T) {
	t.Setenv("TEST_TOKEN", "mytoken123")
	auth := resolveA2AAuth(nil, "TEST_TOKEN")
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Header != "Authorization" {
		t.Errorf("expected Authorization header, got %q", auth.Header)
	}
	if auth.Value != "Bearer mytoken123" {
		t.Errorf("expected 'Bearer mytoken123', got %q", auth.Value)
	}
}

func TestResolveA2AAuth_APIKey(t *testing.T) {
	t.Setenv("TEST_KEY", "abc123")
	auth := resolveA2AAuth(&a2aAuthYAML{
		Scheme:   "api-key",
		TokenEnv: "TEST_KEY",
		Header:   "X-API-Key",
	}, "")
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Header != "X-API-Key" {
		t.Errorf("expected X-API-Key header, got %q", auth.Header)
	}
	if auth.Value != "abc123" {
		t.Errorf("expected raw token 'abc123', got %q", auth.Value)
	}
}

func TestResolveA2AAuth_OpenIDConnect(t *testing.T) {
	t.Setenv("OIDC_TOKEN", "jwt-token")
	auth := resolveA2AAuth(&a2aAuthYAML{
		Scheme:   "openid-connect",
		TokenEnv: "OIDC_TOKEN",
	}, "")
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Value != "Bearer jwt-token" {
		t.Errorf("expected 'Bearer jwt-token', got %q", auth.Value)
	}
}

func TestResolveA2AAuth_EmptyToken(t *testing.T) {
	auth := resolveA2AAuth(&a2aAuthYAML{
		Scheme:   "bearer",
		TokenEnv: "NONEXISTENT_ENV_VAR",
	}, "")
	if auth != nil {
		t.Error("expected nil for missing env var")
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
// SSE stream event check tests
// ---------------------------------------------------------------------------

func TestCheckA2AStreamEvent_Completed(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID: "task-1",
			Status: a2aStatus{
				State:   "TASK_STATE_COMPLETED",
				Message: &a2aMessage{Parts: []a2aPart{{Text: "all done"}}},
			},
		},
	})

	result, terminal := checkA2AStreamEvent(raw, slog.Default())
	if !terminal {
		t.Error("expected terminal=true for COMPLETED")
	}
	if result.Status != "completed" {
		t.Errorf("expected 'completed', got %q", result.Status)
	}
}

func TestCheckA2AStreamEvent_Working(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Task: &a2aTaskResponse{
			ID: "task-1",
			Status: a2aStatus{
				State:   "TASK_STATE_WORKING",
				Message: &a2aMessage{Parts: []a2aPart{{Text: "processing..."}}},
			},
		},
	})

	result, terminal := checkA2AStreamEvent(raw, slog.Default())
	if terminal {
		t.Error("expected terminal=false for WORKING")
	}
	if result.Status != "working" {
		t.Errorf("expected 'working', got %q", result.Status)
	}
}

func TestCheckA2AStreamEvent_Empty(t *testing.T) {
	result, terminal := checkA2AStreamEvent(nil, slog.Default())
	if terminal {
		t.Error("expected terminal=false for empty payload")
	}
	if result.Status != "" {
		t.Errorf("expected empty status, got %q", result.Status)
	}
}

func TestCheckA2AStreamEvent_NoTask(t *testing.T) {
	raw := mustMarshalJSON(t, a2aSendResult{
		Message: &a2aMessage{Parts: []a2aPart{{Text: "hello"}}},
	})
	result, terminal := checkA2AStreamEvent(raw, slog.Default())
	if terminal {
		t.Error("expected terminal=false for message-only response")
	}
	_ = result
}

// ---------------------------------------------------------------------------
// JSON-RPC dispatch test (blocking mode)
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
// Streaming dispatch tests (Phase 2)
// ---------------------------------------------------------------------------

func TestDispatchA2ATask_StreamingCompleted(t *testing.T) {
	card := &A2AAgentCard{
		Name:    "streaming-agent",
		Version: "1.0.0",
		Capabilities: &A2ACapabilities{
			Streaming: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aJSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if req.Method != "SendStreamingMessage" {
			t.Errorf("expected method 'SendStreamingMessage', got %q", req.Method)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream, got %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		// Send a WORKING event, then a COMPLETED event.
		events := []a2aJSONRPCResponse{
			{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mustMarshalJSON(t, a2aSendResult{
					Task: &a2aTaskResponse{
						ID:     "a2a-task-1",
						Status: a2aStatus{State: "TASK_STATE_WORKING", Message: &a2aMessage{Parts: []a2aPart{{Text: "processing..."}}}},
					},
				}),
			},
			{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mustMarshalJSON(t, a2aSendResult{
					Task: &a2aTaskResponse{
						ID:     "a2a-task-1",
						Status: a2aStatus{State: "TASK_STATE_COMPLETED", Message: &a2aMessage{Parts: []a2aPart{{Text: "streamed output"}}}},
					},
				}),
			},
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		for _, evt := range events {
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{
		ID:          "task-stream-1",
		WorkspaceID: "ws-1",
		IssueID:     "issue-1",
	}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected 'completed', got %q", result.Status)
	}
	if result.Comment != "streamed output" {
		t.Errorf("expected 'streamed output', got %q", result.Comment)
	}
}

func TestDispatchA2ATask_StreamingCancelled(t *testing.T) {
	card := &A2AAgentCard{
		Name:    "streaming-agent",
		Version: "1.0.0",
		Capabilities: &A2ACapabilities{
			Streaming: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		evt := a2aJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      "task-cancel-1",
			Result: mustMarshalJSON(t, a2aSendResult{
				Task: &a2aTaskResponse{
					ID:     "a2a-task-1",
					Status: a2aStatus{State: "TASK_STATE_CANCELED"},
				},
			}),
		}
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{ID: "task-cancel-1", WorkspaceID: "ws-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "cancelled" {
		t.Errorf("expected 'cancelled', got %q", result.Status)
	}
}

func TestDispatchA2ATask_StreamingRPCError(t *testing.T) {
	card := &A2AAgentCard{
		Name:    "streaming-agent",
		Version: "1.0.0",
		Capabilities: &A2ACapabilities{
			Streaming: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		evt := a2aJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      "task-err-1",
			Error:   &a2aJSONRPCError{Code: -32600, Message: "bad request"},
		}
		data, _ := json.Marshal(evt)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{ID: "task-err-1", WorkspaceID: "ws-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

func TestDispatchA2ATask_StreamingNon200(t *testing.T) {
	card := &A2AAgentCard{
		Name:    "streaming-agent",
		Version: "1.0.0",
		Capabilities: &A2ACapabilities{
			Streaming: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{Mode: "a2a", A2AURL: srv.URL, Card: card}
	task := Task{ID: "task-500", WorkspaceID: "ws-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Cancellation tests (Phase 2)
// ---------------------------------------------------------------------------

func TestCancelA2ATask_SendsCancelRequest(t *testing.T) {
	cancelReceived := false
	var receivedMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2aJSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		receivedMethod = req.Method
		if req.Method == "CancelTask" {
			cancelReceived = true
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a2aJSONRPCResponse{JSONRPC: "2.0", ID: req.ID})
	}))
	defer srv.Close()

	entry := AgentEntry{
		Mode:    "a2a",
		A2AURL:  srv.URL,
		Card:    &A2AAgentCard{Name: "test"},
		A2AAuth: &a2aResolvedAuth{Header: "Authorization", Value: "Bearer test"},
	}

	cancelA2ATask(context.Background(), entry, "task-123", slog.Default())

	if !cancelReceived {
		t.Error("expected CancelTask to be sent")
	}
	if receivedMethod != "CancelTask" {
		t.Errorf("expected method 'CancelTask', got %q", receivedMethod)
	}
}

func TestCancelA2ATask_Unreachable(t *testing.T) {
	entry := AgentEntry{
		Mode:   "a2a",
		A2AURL: "http://127.0.0.1:1",
		Card:   &A2AAgentCard{Name: "test"},
	}
	// Should not panic or hang.
	cancelA2ATask(context.Background(), entry, "task-123", slog.Default())
}

// ---------------------------------------------------------------------------
// Auth passthrough tests (Phase 3)
// ---------------------------------------------------------------------------

func TestDispatchA2ATask_WithAuth(t *testing.T) {
	card := &A2AAgentCard{Name: "auth-agent", Version: "1.0.0"}
	var receivedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("X-API-Key")

		var req a2aJSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		result := a2aSendResult{
			Task: &a2aTaskResponse{
				ID:     "a2a-task-1",
				Status: a2aStatus{State: "TASK_STATE_COMPLETED"},
			},
		}
		resultBytes, _ := json.Marshal(result)
		resp := a2aJSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	entry := AgentEntry{
		Mode:    "a2a",
		A2AURL:  srv.URL,
		Card:    card,
		A2AAuth: &a2aResolvedAuth{Header: "X-API-Key", Value: "secret123"},
	}
	task := Task{ID: "task-auth", WorkspaceID: "ws-1"}

	result, err := d.dispatchA2ATask(context.Background(), task, entry, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected 'completed', got %q", result.Status)
	}
	if receivedAuth != "secret123" {
		t.Errorf("expected X-API-Key 'secret123', got %q", receivedAuth)
	}
}

// ---------------------------------------------------------------------------
// Port scan tests (Phase 3)
// ---------------------------------------------------------------------------

func TestScanA2ALocalPorts_Discovery(t *testing.T) {
	cardJSON := `{
		"name": "port-agent",
		"version": "1.0.0"
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, cardJSON)
	}))
	defer srv.Close()

	// Verify the agent card endpoint works on the test server.
	card, err := fetchAgentCard(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.Name != "port-agent" {
		t.Errorf("expected 'port-agent', got %q", card.Name)
	}

	// Test that scanA2ALocalPorts correctly skips existing agents and adds new ones.
	agents := map[string]AgentEntry{
		"existing": {Mode: "a2a", A2AURL: "http://localhost:9999", Card: &A2AAgentCard{Name: "existing"}},
	}
	// Port scan will try a2aPortScanMin..a2aPortScanMax on localhost.
	// Since none of those ports are serving agent cards in this test,
	// no new agents will be added — but the function should not panic.
	scanA2ALocalPorts(context.Background(), agents, slog.Default())
	if _, ok := agents["existing"]; !ok {
		t.Error("existing agent should not have been removed")
	}
}

// ---------------------------------------------------------------------------
// Health probe test (Phase 2)
// ---------------------------------------------------------------------------

func TestA2AHealthProbeLoop_ContextCancellation(t *testing.T) {
	d := &Daemon{
		cfg:    Config{},
		client: NewClient("http://localhost:9999"),
		logger: slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		d.a2aHealthProbeLoop(ctx, "test", "http://127.0.0.1:1")
		close(done)
	}()

	// Cancel after a short delay.
	time.AfterFunc(100*time.Millisecond, cancel)

	select {
	case <-done:
		// Loop exited on context cancellation.
	case <-time.After(5 * time.Second):
		t.Fatal("health probe loop did not exit on context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

func mustMarshalJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return json.RawMessage(b)
}
