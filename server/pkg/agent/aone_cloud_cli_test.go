package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAoneCloudCLIBackendExecuteStreamsNDJSON(t *testing.T) {
	t.Parallel()

	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime/execute" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer secret" && r.Header.Get("X-Runtime-Token") == "secret" && r.Header.Get("X-API-Key") == "secret" {
			sawAuth = true
		}
		var req aoneRuntimeExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Prompt != "say hi" {
			t.Fatalf("prompt = %q, want say hi", req.Prompt)
		}
		if req.Workspace.Kind != "local_path" || req.Workspace.Path != "/tmp/work" {
			t.Fatalf("workspace = %+v", req.Workspace)
		}
		if req.AgentProfile != "smoke" {
			t.Fatalf("agentProfile = %q, want smoke", req.AgentProfile)
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"message","message":{"kind":"session_created","sessionId":"s1","newSessionId":"s1","provider":"claude"}}`)
		fmt.Fprintln(w, `{"type":"message","message":{"kind":"text","sessionId":"s1","content":"hello","provider":"claude"}}`)
		fmt.Fprintln(w, `{"type":"result","status":"completed","sessionId":"s1","output":"hello","usage":{"test-model":{"inputTokens":1,"outputTokens":2}}}`)
	}))
	defer server.Close()

	backend, err := New(AoneCloudCLIProvider, Config{
		ExecutablePath: server.URL,
		Env: map[string]string{
			"AONE_RUNTIME_TOKEN":   "secret",
			"AONE_RUNTIME_PROFILE": "smoke",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session, err := backend.Execute(context.Background(), "say hi", ExecOptions{Cwd: "/tmp/work", Timeout: time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var texts []string
	var pinnedSession string
	for msg := range session.Messages {
		switch msg.Type {
		case MessageStatus:
			pinnedSession = msg.SessionID
		case MessageText:
			texts = append(texts, msg.Content)
		}
	}
	result := <-session.Result

	if !sawAuth {
		t.Fatal("expected runtime auth headers")
	}
	if pinnedSession != "s1" {
		t.Fatalf("pinned session = %q, want s1", pinnedSession)
	}
	if strings.Join(texts, "") != "hello" {
		t.Fatalf("texts = %q, want hello", strings.Join(texts, ""))
	}
	if result.Status != "completed" || result.Output != "hello" || result.SessionID != "s1" {
		t.Fatalf("result = %+v", result)
	}
	if got := result.Usage["test-model"].OutputTokens; got != 2 {
		t.Fatalf("output tokens = %d, want 2", got)
	}
}

func TestDetectAoneRuntimeVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Runtime-Token") != "secret" {
			t.Fatalf("missing runtime token header")
		}
		_, _ = w.Write([]byte(`{"status":"ok","version":"1.2.3"}`))
	}))
	defer server.Close()

	version, err := DetectAoneRuntimeVersion(context.Background(), server.URL+"/api/runtime", "secret")
	if err != nil {
		t.Fatalf("DetectAoneRuntimeVersion: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", version)
	}
}

func TestAoneCloudCLIBackendLiveSmoke(t *testing.T) {
	if os.Getenv("MULTICA_AONE_RUNTIME_LIVE_TEST") != "1" {
		t.Skip("set MULTICA_AONE_RUNTIME_LIVE_TEST=1 to run against a local Aone Cloud CLI runtime")
	}
	endpoint := os.Getenv("MULTICA_AONE_RUNTIME_URL")
	if endpoint == "" {
		t.Fatal("MULTICA_AONE_RUNTIME_URL is required")
	}

	backend, err := New(AoneCloudCLIProvider, Config{
		ExecutablePath: endpoint,
		Env: map[string]string{
			"AONE_RUNTIME_TOKEN":   os.Getenv("MULTICA_AONE_RUNTIME_TOKEN"),
			"AONE_RUNTIME_PROFILE": os.Getenv("MULTICA_AONE_RUNTIME_PROFILE"),
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	session, err := backend.Execute(context.Background(), "hello from multica", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q error = %q output = %q", result.Status, result.Error, result.Output)
	}
	if !strings.Contains(result.Output, "Aone runtime smoke task completed") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
