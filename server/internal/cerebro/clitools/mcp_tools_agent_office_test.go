package clitools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// FIR-3212: the agent_context_propose MCP tool must forward the two brief-layer
// modes to the change-request endpoint the same way it forwards model /
// thinking_level / persona_sandbox, so an agent author can set them the same way
// as every other agent-config field. This is Tine's wiring gap — the backend
// handler already accepts the fields; this asserts the tool actually sends them.
func TestAgentContextProposeForwardsBriefLayerModes(t *testing.T) {
	var gotBody map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Fatalf("decode proposed body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cr-1","status":"pending"}`))
	}))
	defer remote.Close()

	client := cli.NewAPIClient(remote.URL, "workspace-1", "mat_task-token")
	server := mcp.NewServer("test", "1")
	registerAgentOfficeTools(server, client)

	result, err := server.Call(context.Background(), "agent_context_propose", map[string]any{
		"agent_id":             "agent-1",
		"title":                "Free triage agent from the shared brief",
		"proposed_version":     "1.1.0",
		"workspace_brief_mode": "off",
		"tools_brief_mode":     "summary",
	})
	if err != nil {
		t.Fatalf("agent_context_propose returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("agent_context_propose returned tool error: %+v", result.Content)
	}
	if got := gotBody["workspace_brief_mode"]; got != "off" {
		t.Fatalf("workspace_brief_mode forwarded = %v, want \"off\"", got)
	}
	if got := gotBody["tools_brief_mode"]; got != "summary" {
		t.Fatalf("tools_brief_mode forwarded = %v, want \"summary\"", got)
	}
}

// An empty-string mode is a real value — "reset this agent back to the full
// default" — and must be forwarded, not dropped as if the field were absent.
// Absent fields, on the other hand, must never appear in the body.
func TestAgentContextProposeBriefModesOmittedWhenAbsentForwardedWhenEmpty(t *testing.T) {
	var gotBody map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cr-1","status":"pending"}`))
	}))
	defer remote.Close()

	client := cli.NewAPIClient(remote.URL, "workspace-1", "mat_task-token")
	server := mcp.NewServer("test", "1")
	registerAgentOfficeTools(server, client)

	// workspace_brief_mode present but empty (reset); tools_brief_mode absent.
	if _, err := server.Call(context.Background(), "agent_context_propose", map[string]any{
		"agent_id":             "agent-1",
		"title":                "Reset triage agent to the full brief",
		"proposed_version":     "1.2.0",
		"workspace_brief_mode": "",
	}); err != nil {
		t.Fatalf("agent_context_propose returned error: %v", err)
	}

	got, ok := gotBody["workspace_brief_mode"]
	if !ok || got != "" {
		t.Fatalf("workspace_brief_mode = %v (present=%v), want present and empty", got, ok)
	}
	if _, ok := gotBody["tools_brief_mode"]; ok {
		t.Fatalf("tools_brief_mode should be absent when not supplied, got %v", gotBody["tools_brief_mode"])
	}
}
