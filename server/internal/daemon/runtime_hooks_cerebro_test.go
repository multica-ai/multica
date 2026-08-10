package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
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

// FIR-4797: the run-killing error must name the hook and its reason. Reading
// only "workflow hook blocked before.session.start" is what made a single
// policy look like an unexplained platform failure.
func TestBlockingRuntimeHookErrorNamesTheHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"decision":"block","warning":"Hook \"Comment quality gate\" stopped this because one of its actions failed: judge gateway unreachable","blocked_by":{"id":"hook-1","name":"Comment quality gate"}}`))
	}))
	defer server.Close()

	daemon := &Daemon{client: NewClient(server.URL)}
	_, err := daemon.evaluateBlockingRuntimeHook(context.Background(), "task-1", RuntimeHookEvent{EventType: "before.session.start"})
	if err == nil {
		t.Fatal("blocking runtime hook was allowed")
	}
	if !strings.Contains(err.Error(), `workflow hook "Comment quality gate"`) {
		t.Fatalf("error = %q, want the hook name", err)
	}
	if !strings.Contains(err.Error(), "before.session.start") || !strings.Contains(err.Error(), "judge gateway unreachable") {
		t.Fatalf("error = %q, want the event and the reason", err)
	}
}

// The producing and consuming structs are declared in different packages and
// are held together only by their JSON tags. Without this the runtime can go
// back to "an unnamed workflow hook" while every other test stays green.
func TestRuntimeHookResultReadsTheServerBlockedByShape(t *testing.T) {
	raw, err := json.Marshal(cerebroworkflows.HookResult{
		Decision:  cerebroworkflows.HookBlock,
		BlockedBy: &cerebroworkflows.HookRef{ID: "hook-1", Name: "Comment quality gate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got RuntimeHookResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.BlockedBy == nil || got.BlockedBy.Name != "Comment quality gate" || got.BlockedBy.ID != "hook-1" {
		t.Fatalf("server payload %s did not survive into %#v", raw, got.BlockedBy)
	}
}

// Without an identity the message must say so rather than imply a named hook.
func TestBlockingRuntimeHookErrorMarksAnUnnamedHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(RuntimeHookResult{Decision: "block"})
	}))
	defer server.Close()

	daemon := &Daemon{client: NewClient(server.URL)}
	_, err := daemon.evaluateBlockingRuntimeHook(context.Background(), "task-1", RuntimeHookEvent{EventType: "before.session.start"})
	if err == nil || !strings.Contains(err.Error(), "an unnamed workflow hook blocked before.session.start") {
		t.Fatalf("error = %v", err)
	}
}
