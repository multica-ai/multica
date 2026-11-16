package handler

import (
	"encoding/json"
	"testing"
)

// TestMergeMCPOverlayAgentNilOverlayNil covers the "no managed MCP anywhere"
// branch: both inputs absent must return nil so the daemon's "managed
// mcp_config?" short-circuit treats the task as no-config — exactly the
// behavior tasks had before Stage 3 introduced the overlay column.
func TestMergeMCPOverlayAgentNilOverlayNil(t *testing.T) {
	cases := []struct {
		name    string
		agent   json.RawMessage
		overlay json.RawMessage
	}{
		{"both_nil", nil, nil},
		{"agent_null_overlay_nil", json.RawMessage("null"), nil},
		{"agent_nil_overlay_null", nil, json.RawMessage("null")},
		{"agent_empty_overlay_empty", json.RawMessage(""), json.RawMessage("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeMCPOverlay(tc.agent, tc.overlay)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil result, got %s", string(got))
			}
		})
	}
}

// TestMergeMCPOverlayAgentOnly covers the "no Composio for this task" path:
// every existing agent.mcp_config must be passed through unchanged so
// Stage 3 cannot break MCP setup for tasks where the initiator has no
// connections.
func TestMergeMCPOverlayAgentOnly(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)

	got, err := mergeMCPOverlay(agent, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Pass-through preserves the exact bytes (no re-marshal) so the
	// existing trust-byte-identity tests upstream are unaffected.
	if string(got) != string(agent) {
		t.Errorf("expected pass-through, got %s", string(got))
	}
}

// TestMergeMCPOverlayOverlayOnly covers the "agent has no mcp_config" path:
// the overlay is canonicalized through json.Marshal so the daemon receives
// a deterministic shape regardless of how Postgres' JSONB serialized the
// stored value.
func TestMergeMCPOverlayOverlayOnly(t *testing.T) {
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc","headers":{"Authorization":"Bearer mcp_xyz"}}}}`)

	got, err := mergeMCPOverlay(nil, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcpServers, got %s", string(got))
	}
	if _, ok := servers["composio"]; !ok {
		t.Errorf("expected composio server, got %s", string(got))
	}
}

// TestMergeMCPOverlayMergesBothSides — the headline case: agent's saved
// servers must survive, and the overlay's composio entry must appear alongside.
func TestMergeMCPOverlayMergesBothSides(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"},"github":{"command":"npx"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("missing mcpServers, got %s", string(got))
	}
	for _, want := range []string{"fetch", "github", "composio"} {
		if _, ok := servers[want]; !ok {
			t.Errorf("missing server %q in merged result %s", want, string(got))
		}
	}
}

// TestMergeMCPOverlayCollisionOverlayWins — the contract the comment on
// mergeMCPOverlay calls out explicitly. The overlay carries the live, user-
// scoped session URL; on a name collision it must win so the daemon's
// sidecar generator emits the live URL, not whatever placeholder the agent
// had saved.
func TestMergeMCPOverlayCollisionOverlayWins(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://placeholder.example/old"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/new"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cfg map[string]map[string]map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	gotURL, _ := cfg["mcpServers"]["composio"]["url"].(string)
	if gotURL != "https://mcp.composio.dev/s/new" {
		t.Errorf("collision: expected overlay URL to win, got %q", gotURL)
	}
}

// TestMergeMCPOverlayPreservesAgentTopLevelKeys — anything outside
// `mcpServers` on the agent side must be preserved. The overlay only
// carries server entries; it must not silently strip non-server top-level
// keys the agent admin saved.
func TestMergeMCPOverlayPreservesAgentTopLevelKeys(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}},"experimental":{"foo":"bar"}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := cfg["experimental"]; !ok {
		t.Errorf("expected experimental key preserved, got %s", string(got))
	}
}

// TestMergeMCPOverlayBadOverlayFallsBackToAgent — a malformed overlay must
// not surprise-disable the agent's saved servers. The merge returns the
// agent config unchanged plus an error so the caller can log.
func TestMergeMCPOverlayBadOverlayFallsBackToAgent(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)
	overlay := json.RawMessage(`{ this is not json`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if string(got) != string(agent) {
		t.Errorf("expected agent config preserved on overlay parse failure, got %s", string(got))
	}
}

// TestMergeMCPOverlayBadAgentReturnsBytesAndError — symmetric guard: a
// malformed agent.mcp_config must not panic the merger. We return the
// original bytes and surface the parse error so the daemon's downstream
// sidecar generator can give the existing error path (instead of a
// half-merged config).
func TestMergeMCPOverlayBadAgentReturnsBytesAndError(t *testing.T) {
	agent := json.RawMessage(`{ this is not json`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.composio.dev/s/abc"}}}`)

	got, err := mergeMCPOverlay(agent, overlay)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if string(got) != string(agent) {
		t.Errorf("expected agent bytes returned unchanged, got %s", string(got))
	}
}

// TestMergeMCPOverlayRejectsNonObjectServer pins the type guard: an
// mcpServers map whose value is a primitive (or array) is rejected, so a
// future bug that wrote `mcpServers: {composio: "https://..."}` (a string
// instead of an object) doesn't quietly travel through the merge and blow
// up in execenv's sidecar generator. Same behavior as
// parseCursorManagedMcpServers.
func TestMergeMCPOverlayRejectsNonObjectServer(t *testing.T) {
	agent := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)
	overlay := json.RawMessage(`{"mcpServers":{"composio":"not-an-object"}}`)

	if _, err := mergeMCPOverlay(agent, overlay); err == nil {
		t.Fatalf("expected error for non-object server, got nil")
	}
}
