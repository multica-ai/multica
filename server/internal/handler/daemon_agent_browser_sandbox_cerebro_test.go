package handler

// CEREBRO-PATCH(agent-browser-sandbox-gate): FIR-1428 — unit coverage for the
// pure JSON merge that stamps allow_agent_browser into the runtime sandbox
// policy forwarded to the daemon at task-claim time. The resolve path itself is
// DB-backed and covered by the tool-policy chain tests; this pins the merge
// contract so a future refactor cannot silently drop the flag or clobber an
// existing policy.

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

// TestAgentBrowserRegistersAsCapability proves the registration end to end through
// the real mirror: the static claude capability set → AsMap snapshot →
// capabilityItemsFromSnapshot must yield a "tools" item named "agent-browser",
// which persistRuntimeCapabilitySnapshot keys as "tools:agent-browser" — exactly
// the key the tool-policy table reads and the sandbox gate resolves (FIR-1428).
func TestAgentBrowserRegistersAsCapability(t *testing.T) {
	snapshotJSON, err := json.Marshal(capabilities.AsMap(capabilities.For("claude")))
	if err != nil {
		t.Fatalf("marshal claude capability snapshot: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(snapshotJSON, &raw); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	found := false
	for _, item := range capabilityItemsFromSnapshot(raw) {
		if item.Kind == "tools" && item.Name == "agent-browser" {
			found = true
			if key := item.Kind + ":" + item.Name; key != agentBrowserToolKey {
				t.Errorf("mirror key %q != gate key %q", key, agentBrowserToolKey)
			}
		}
	}
	if !found {
		t.Error("agent-browser is not registered as a claude tool capability")
	}
}

func TestWithAgentBrowserSandbox(t *testing.T) {
	t.Run("empty policy yields just the flag", func(t *testing.T) {
		got := withAgentBrowserSandbox(nil)
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if obj["allow_agent_browser"] != true {
			t.Errorf("expected allow_agent_browser=true, got %v", obj["allow_agent_browser"])
		}
	})

	t.Run("preserves existing keys", func(t *testing.T) {
		in := json.RawMessage(`{"deny_shell":true,"allowed_hosts":["a:1"]}`)
		got := withAgentBrowserSandbox(in)
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("result is not valid JSON: %v", err)
		}
		if obj["allow_agent_browser"] != true {
			t.Errorf("expected allow_agent_browser=true, got %v", obj["allow_agent_browser"])
		}
		if obj["deny_shell"] != true {
			t.Errorf("existing deny_shell dropped: %v", obj)
		}
		if _, ok := obj["allowed_hosts"]; !ok {
			t.Errorf("existing allowed_hosts dropped: %v", obj)
		}
	})

	t.Run("non-object policy is left untouched (fail closed)", func(t *testing.T) {
		in := json.RawMessage(`["not","an","object"]`)
		got := withAgentBrowserSandbox(in)
		if string(got) != string(in) {
			t.Errorf("expected untouched policy on non-object input, got %s", got)
		}
	})
}
