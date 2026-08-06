package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientEmitRuntimeHookEventUsesTaskScopedChannel(t *testing.T) {
	var received RuntimeHookEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/task-1/hook-events" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(RuntimeHookResult{
			Decision:      "modify",
			Modifications: map[string]any{"prompt": "updated"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	result, err := client.EmitRuntimeHookEvent(context.Background(), "task-1", RuntimeHookEvent{
		EventType:      "before.prompt.assemble",
		ProposedAction: map[string]any{"prompt": "original"},
		MutableFields:  []string{"prompt"},
	})
	if err != nil {
		t.Fatalf("EmitRuntimeHookEvent: %v", err)
	}
	if received.EventType != "before.prompt.assemble" {
		t.Fatalf("event_type = %q", received.EventType)
	}
	if result.Decision != "modify" || result.Modifications["prompt"] != "updated" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyRuntimePromptHookUsesModification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(RuntimeHookResult{
			Decision:      "modify",
			Modifications: map[string]any{"prompt": "updated"},
		})
	}))
	defer server.Close()

	daemon := &Daemon{client: NewClient(server.URL)}
	got, err := daemon.applyRuntimePromptHook(context.Background(), Task{ID: "task-1"}, "codex", "original")
	if err != nil {
		t.Fatalf("applyRuntimePromptHook: %v", err)
	}
	if got != "updated" {
		t.Fatalf("prompt = %q, want updated", got)
	}
}

func TestBlockingRuntimeHookStopsLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(RuntimeHookResult{Decision: "block"})
	}))
	defer server.Close()

	daemon := &Daemon{client: NewClient(server.URL)}
	_, err := daemon.evaluateBlockingRuntimeHook(context.Background(), "task-1", RuntimeHookEvent{EventType: "before.session.start"})
	if err == nil {
		t.Fatal("blocking runtime hook was allowed")
	}
}
