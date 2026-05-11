package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestClient_IdentityHeaders_PostJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		if got := r.Header.Get("X-Client-Version"); got != "9.9.9" {
			t.Errorf("expected X-Client-Version 9.9.9, got %q", got)
		}
		if got := r.Header.Get("X-Client-OS"); got != normalizeGOOS(runtime.GOOS) {
			t.Errorf("expected X-Client-OS %q, got %q", normalizeGOOS(runtime.GOOS), got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("expected Authorization Bearer tok, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"ok": "1"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	c.SetVersion("9.9.9")

	if err := c.postJSON(context.Background(), "/api/daemon/test", map[string]any{}, nil); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
}

func TestClient_IdentityHeaders_GetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		if got := r.Header.Get("X-Client-Version"); got != "1.2.3" {
			t.Errorf("expected X-Client-Version 1.2.3, got %q", got)
		}
		if got := r.Header.Get("X-Client-OS"); got == "" {
			t.Errorf("expected X-Client-OS to be set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	c.SetVersion("1.2.3")

	var out map[string]any
	if err := c.getJSON(context.Background(), "/api/daemon/test", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
}

func TestClient_VersionOmittedWhenUnset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Client-Platform"); got != "daemon" {
			t.Errorf("expected X-Client-Platform daemon, got %q", got)
		}
		// SetVersion not called → header must be omitted (not "").
		if vals := r.Header.Values("X-Client-Version"); len(vals) != 0 {
			t.Errorf("expected X-Client-Version absent, got %v", vals)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.postJSON(context.Background(), "/api/daemon/test", nil, nil); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
}

func TestClientClaimTaskPreservesAgentRuntimeConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"task": {
				"id": "task-1",
				"agent_id": "agent-1",
				"runtime_id": "runtime-1",
				"issue_id": "issue-1",
				"workspace_id": "workspace-1",
				"agent": {
					"id": "agent-1",
					"name": "Codex Agent",
					"runtime_config": {"model_reasoning_effort": "high"}
				}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.ClaimTask(context.Background(), "runtime-1")
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if task == nil || task.Agent == nil {
		t.Fatal("expected claimed task with agent data")
	}
	if !json.Valid(task.Agent.RuntimeConfig) {
		t.Fatalf("runtime_config should be preserved as JSON, got %q", task.Agent.RuntimeConfig)
	}
	if !bytes.Contains(task.Agent.RuntimeConfig, []byte(`"model_reasoning_effort"`)) {
		t.Fatalf("runtime_config missing model_reasoning_effort: %s", task.Agent.RuntimeConfig)
	}
}

func TestNormalizeGOOS(t *testing.T) {
	cases := map[string]string{
		"darwin":  "macos",
		"windows": "windows",
		"linux":   "linux",
		"freebsd": "freebsd",
	}
	for in, want := range cases {
		if got := normalizeGOOS(in); got != want {
			t.Errorf("normalizeGOOS(%q) = %q, want %q", in, got, want)
		}
	}
}
