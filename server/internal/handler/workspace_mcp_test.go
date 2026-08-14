package handler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// decodeServers pulls the `mcpServers` map out of a resolved document so
// assertions can compare server sets without depending on key order.
func decodeServers(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal resolved document %s: %v", raw, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	return servers
}

func serverNames(servers map[string]any) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	return names
}

func TestResolveWorkspaceMcpConfig(t *testing.T) {
	const wsDoc = `{"mcpServers":{"shared":{"url":"https://shared.example"},"linear":{"url":"https://ws-linear.example"}}}`

	tests := []struct {
		name        string
		workspace   string
		agent       string
		wantServers map[string]string // server name -> expected url
		wantNil     bool
	}{
		{
			name:      "agent absent inherits the whole workspace document",
			workspace: wsDoc,
			agent:     "",
			wantServers: map[string]string{
				"shared": "https://shared.example",
				"linear": "https://ws-linear.example",
			},
		},
		{
			name:      "agent json null inherits too",
			workspace: wsDoc,
			agent:     "null",
			wantServers: map[string]string{
				"shared": "https://shared.example",
				"linear": "https://ws-linear.example",
			},
		},
		{
			name:      "agent servers union with the workspace's",
			workspace: wsDoc,
			agent:     `{"mcpServers":{"private":{"url":"https://private.example"}}}`,
			wantServers: map[string]string{
				"shared":  "https://shared.example",
				"linear":  "https://ws-linear.example",
				"private": "https://private.example",
			},
		},
		{
			name:      "agent wins on a server-name collision",
			workspace: wsDoc,
			agent:     `{"mcpServers":{"linear":{"url":"https://agent-linear.example"}}}`,
			wantServers: map[string]string{
				"shared": "https://shared.example",
				"linear": "https://agent-linear.example",
			},
		},
		{
			// The legacy entry both shadows the shared one AND survives, now
			// folded into the canonical container the daemon actually reads.
			name:      "an agent name in the legacy container shadows the shared one and survives",
			workspace: wsDoc,
			agent:     `{"mcp":{"linear":{"url":"https://legacy-linear.example"}}}`,
			wantServers: map[string]string{
				"shared": "https://shared.example",
				"linear": "https://legacy-linear.example",
			},
		},
		{
			name:      "no workspace document leaves the agent config alone",
			workspace: "",
			agent:     `{"mcpServers":{"private":{"url":"https://private.example"}}}`,
			wantServers: map[string]string{
				"private": "https://private.example",
			},
		},
		{
			name:      "an empty workspace document shares nothing",
			workspace: `{"mcpServers":{}}`,
			agent:     "",
			wantNil:   true,
		},
		{
			name:      "nothing configured anywhere stays nil",
			workspace: "",
			agent:     "",
			wantNil:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveWorkspaceMcpConfig(json.RawMessage(tc.workspace), json.RawMessage(tc.agent))
			if err != nil {
				t.Fatalf("ResolveWorkspaceMcpConfig: unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil result, got %s", got)
				}
				return
			}
			servers := decodeServers(t, got)
			if len(servers) != len(tc.wantServers) {
				t.Fatalf("server set = %v, want %v", serverNames(servers), tc.wantServers)
			}
			for name, wantURL := range tc.wantServers {
				entry, ok := servers[name].(map[string]any)
				if !ok {
					t.Fatalf("missing server %q in %v", name, serverNames(servers))
				}
				if entry["url"] != wantURL {
					t.Errorf("server %q url = %v, want %v", name, entry["url"], wantURL)
				}
			}
		})
	}
}

// An agent whose saved config declares no servers is the explicit "this agent
// runs with zero MCP" state from #3545. Sharing a workspace document must not
// hand it credentials it was deliberately not given — that silent propagation
// is the main risk of an inheritance layer, so it gets its own test.
func TestResolveWorkspaceMcpConfig_ZeroServerAgentOptsOut(t *testing.T) {
	const wsDoc = `{"mcpServers":{"shared":{"url":"https://shared.example"}}}`

	for _, agentCfg := range []string{`{}`, `{"mcpServers":{}}`, `{"mcp":{}}`} {
		t.Run(agentCfg, func(t *testing.T) {
			got, err := ResolveWorkspaceMcpConfig(json.RawMessage(wsDoc), json.RawMessage(agentCfg))
			if err != nil {
				t.Fatalf("ResolveWorkspaceMcpConfig: unexpected error: %v", err)
			}
			if servers := decodeServers(t, got); len(servers) != 0 {
				t.Fatalf("opted-out agent inherited %v", serverNames(servers))
			}
			// The agent document must also survive byte-for-byte: the
			// three-state contract keys strict mode off "managed but empty",
			// which a nil result would silently turn back into "inherit".
			if string(got) != agentCfg {
				t.Fatalf("agent document = %s, want it returned unchanged (%s)", got, agentCfg)
			}
		})
	}
}

// The agent's servers must survive whichever container they were stored in,
// and the result must be normalized onto `mcpServers`. This is the case that
// regressed OpenCode agents: the daemon's runtime merge only falls back to the
// legacy `mcp` container when `mcpServers` is absent, so leaving the agent's
// entries behind in `mcp` while writing shared ones into `mcpServers` made the
// daemon read the shared set and drop the agent's own servers entirely.
func TestResolveWorkspaceMcpConfig_FoldsLegacyContainerIntoCanonical(t *testing.T) {
	const wsDoc = `{"mcpServers":{"shared":{"url":"https://shared.example"}}}`
	const agentCfg = `{"mcp":{"legacy":{"command":"legacy-server"}},"inputs":[{"id":"token"}]}`

	got, err := ResolveWorkspaceMcpConfig(json.RawMessage(wsDoc), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveWorkspaceMcpConfig: unexpected error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal merged document: %v", err)
	}
	if _, ok := doc["mcp"]; ok {
		t.Errorf("legacy container must be consumed, not left beside mcpServers: %s", got)
	}
	if _, ok := doc["inputs"]; !ok {
		t.Errorf("merge dropped the agent's non-server top-level key: %s", got)
	}
	servers := decodeServers(t, got)
	if len(servers) != 2 || servers["shared"] == nil || servers["legacy"] == nil {
		t.Fatalf("effective server set = %v, want shared + legacy", serverNames(servers))
	}
}

// A name present in BOTH containers resolves to the canonical entry — the
// precedence the agent settings UI already displays.
func TestResolveWorkspaceMcpConfig_CanonicalContainerWinsOverLegacy(t *testing.T) {
	const wsDoc = `{"mcpServers":{"shared":{"url":"https://shared.example"}}}`
	const agentCfg = `{"mcpServers":{"dup":{"url":"https://canonical.example"}},"mcp":{"dup":{"url":"https://legacy.example"}}}`

	got, err := ResolveWorkspaceMcpConfig(json.RawMessage(wsDoc), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveWorkspaceMcpConfig: unexpected error: %v", err)
	}
	servers := decodeServers(t, got)
	dup, _ := servers["dup"].(map[string]any)
	if dup["url"] != "https://canonical.example" {
		t.Fatalf("dup url = %v, want the canonical container to win", dup["url"])
	}
}

// Fail-soft: a malformed shared document must never take away servers the
// agent runs with today. It reports the error and returns the agent config.
func TestResolveWorkspaceMcpConfig_MalformedWorkspaceKeepsAgentConfig(t *testing.T) {
	const agentCfg = `{"mcpServers":{"private":{"url":"https://private.example"}}}`

	for _, wsDoc := range []string{`{"mcpServers":[]}`, `not json`, `{"mcpServers":{"broken":"not-an-object"}}`} {
		t.Run(wsDoc, func(t *testing.T) {
			got, err := ResolveWorkspaceMcpConfig(json.RawMessage(wsDoc), json.RawMessage(agentCfg))
			if err == nil {
				t.Fatalf("expected an error for workspace document %s", wsDoc)
			}
			if string(got) != agentCfg {
				t.Fatalf("agent config = %s, want it returned unchanged (%s)", got, agentCfg)
			}
		})
	}
}

// The claim path applies the workspace layer before the per-task overlay, so
// the two merges have to compose into workspace < agent < overlay.
func TestResolveWorkspaceMcpConfig_ComposesWithTaskOverlay(t *testing.T) {
	const wsDoc = `{"mcpServers":{"shared":{"url":"https://shared.example"},"composio":{"url":"https://ws-composio.example"}}}`
	const agentCfg = `{"mcpServers":{"private":{"url":"https://private.example"}}}`
	const overlay = `{"mcpServers":{"composio":{"url":"https://session-composio.example"}}}`

	resolved, err := ResolveWorkspaceMcpConfig(json.RawMessage(wsDoc), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveWorkspaceMcpConfig: unexpected error: %v", err)
	}
	merged, err := mergeMCPOverlay(resolved, json.RawMessage(overlay))
	if err != nil {
		t.Fatalf("mergeMCPOverlay: unexpected error: %v", err)
	}
	servers := decodeServers(t, merged)
	if len(servers) != 3 {
		t.Fatalf("server set = %v, want shared/composio/private", serverNames(servers))
	}
	composio, _ := servers["composio"].(map[string]any)
	if composio["url"] != "https://session-composio.example" {
		t.Errorf("overlay must win over the workspace entry, got %v", composio["url"])
	}
}

func TestValidateWorkspaceMcpConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "servers", raw: `{"mcpServers":{"a":{"url":"https://a.example"}}}`},
		{name: "empty object", raw: `{}`},
		{name: "empty server map", raw: `{"mcpServers":{}}`},
		{name: "array", raw: `[]`, wantErr: true},
		{name: "string", raw: `"nope"`, wantErr: true},
		{name: "servers not an object", raw: `{"mcpServers":[]}`, wantErr: true},
		{name: "server entry not an object", raw: `{"mcpServers":{"a":"https://a.example"}}`, wantErr: true},
		{name: "empty server name", raw: `{"mcpServers":{"":{"url":"https://a.example"}}}`, wantErr: true},
		{name: "legacy container", raw: `{"mcp":{"a":{"url":"https://a.example"}}}`, wantErr: true},
		{name: "no servers key", raw: `{"inputs":[]}`, wantErr: true},
		{name: "empty input", raw: ``, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWorkspaceMcpConfig(json.RawMessage(tc.raw))
			if tc.wantErr && err == nil {
				t.Fatalf("validateWorkspaceMcpConfig(%s): expected an error", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateWorkspaceMcpConfig(%s): unexpected error: %v", tc.raw, err)
			}
		})
	}
}

// Validation errors are surfaced to the caller verbatim, and a shared document
// is exactly where a token would sit — so the message must never quote input.
func TestValidateWorkspaceMcpConfig_ErrorNeverEchoesInput(t *testing.T) {
	secret := `{"mcpServers":{"a":{"headers":{"Authorization":"Bearer sk-live-should-not-leak"` // deliberately truncated

	err := validateWorkspaceMcpConfig(json.RawMessage(secret))
	if err == nil {
		t.Fatal("expected an error for a truncated document")
	}
	if strings.Contains(err.Error(), "sk-live-should-not-leak") {
		t.Fatalf("validation error leaked input: %q", err.Error())
	}
}

func TestParseMcpDocument_NamesSpanBothContainers(t *testing.T) {
	_, names, err := parseMcpDocument(json.RawMessage(`{"mcpServers":{"a":{}},"mcp":{"b":{}}}`))
	if err != nil {
		t.Fatalf("parseMcpDocument: unexpected error: %v", err)
	}
	want := map[string]struct{}{"a": {}, "b": {}}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}
