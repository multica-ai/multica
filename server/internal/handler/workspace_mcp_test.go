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

func bindings(pairs ...string) []WorkspaceMcpBinding {
	out := make([]WorkspaceMcpBinding, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, WorkspaceMcpBinding{Name: pairs[i], Config: json.RawMessage(pairs[i+1])})
	}
	return out
}

func TestResolveAgentMcpConfig(t *testing.T) {
	tests := []struct {
		name        string
		bound       []WorkspaceMcpBinding
		agent       string
		wantServers map[string]string // server name -> expected url
		wantNil     bool
	}{
		{
			// The whole point of the binding model: an agent runs with exactly
			// the workspace servers it was given, and nothing else.
			name:  "agent with no config of its own runs the servers it was given",
			bound: bindings("shared", `{"url":"https://shared.example"}`),
			agent: "",
			wantServers: map[string]string{
				"shared": "https://shared.example",
			},
		},
		{
			name:  "bound servers union with the agent's own",
			bound: bindings("shared", `{"url":"https://shared.example"}`),
			agent: `{"mcpServers":{"private":{"url":"https://private.example"}}}`,
			wantServers: map[string]string{
				"shared":  "https://shared.example",
				"private": "https://private.example",
			},
		},
		{
			name:  "the agent's own entry wins on a name collision",
			bound: bindings("linear", `{"url":"https://ws-linear.example"}`),
			agent: `{"mcpServers":{"linear":{"url":"https://agent-linear.example"}}}`,
			wantServers: map[string]string{
				"linear": "https://agent-linear.example",
			},
		},
		{
			// The legacy entry both shadows the bound one AND survives, folded
			// into the canonical container the daemon actually reads.
			name:  "a legacy-container name shadows the bound server and survives",
			bound: bindings("linear", `{"url":"https://ws-linear.example"}`, "shared", `{"url":"https://shared.example"}`),
			agent: `{"mcp":{"linear":{"url":"https://legacy-linear.example"}}}`,
			wantServers: map[string]string{
				"shared": "https://shared.example",
				"linear": "https://legacy-linear.example",
			},
		},
		{
			// Nothing bound: an unassigned library entry is invisible here,
			// and the agent keeps exactly what it had.
			name:  "no bindings leaves the agent config alone",
			bound: nil,
			agent: `{"mcpServers":{"private":{"url":"https://private.example"}}}`,
			wantServers: map[string]string{
				"private": "https://private.example",
			},
		},
		{
			name:    "nothing bound and nothing configured stays nil",
			bound:   nil,
			agent:   "",
			wantNil: true,
		},
		{
			// A managed-but-empty agent config keeps meaning "no servers of my
			// own"; it is not an opt-out signal any more, because there is
			// nothing to opt out of — bindings are already explicit.
			name:  "a managed-but-empty agent config still receives its bindings",
			bound: bindings("shared", `{"url":"https://shared.example"}`),
			agent: `{}`,
			wantServers: map[string]string{
				"shared": "https://shared.example",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveAgentMcpConfig(tc.bound, json.RawMessage(tc.agent))
			if err != nil {
				t.Fatalf("ResolveAgentMcpConfig: unexpected error: %v", err)
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

// The agent's servers must survive whichever container they were stored in,
// and the result must be normalized onto `mcpServers`. This is the case that
// regressed OpenCode agents: the daemon's runtime merge only falls back to the
// legacy `mcp` container when `mcpServers` is absent, so leaving the agent's
// entries behind in `mcp` while writing bound ones into `mcpServers` made the
// daemon read the bound set and drop the agent's own servers entirely.
func TestResolveAgentMcpConfig_FoldsLegacyContainerIntoCanonical(t *testing.T) {
	const agentCfg = `{"mcp":{"legacy":{"command":"legacy-server"}},"inputs":[{"id":"token"}]}`

	got, err := ResolveAgentMcpConfig(bindings("shared", `{"url":"https://shared.example"}`), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: unexpected error: %v", err)
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
func TestResolveAgentMcpConfig_CanonicalContainerWinsOverLegacy(t *testing.T) {
	const agentCfg = `{"mcpServers":{"dup":{"url":"https://canonical.example"}},"mcp":{"dup":{"url":"https://legacy.example"}}}`

	got, err := ResolveAgentMcpConfig(bindings("shared", `{"url":"https://shared.example"}`), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: unexpected error: %v", err)
	}
	servers := decodeServers(t, got)
	dup, _ := servers["dup"].(map[string]any)
	if dup["url"] != "https://canonical.example" {
		t.Fatalf("dup url = %v, want the canonical container to win", dup["url"])
	}
}

// Fail-soft: a malformed agent document must never cost the agent its servers.
// It reports the error and returns the agent config untouched.
func TestResolveAgentMcpConfig_MalformedAgentConfigIsReturnedUnchanged(t *testing.T) {
	for _, agentCfg := range []string{`{"mcpServers":[]}`, `not json`, `{"mcpServers":{"broken":"not-an-object"}}`} {
		t.Run(agentCfg, func(t *testing.T) {
			got, err := ResolveAgentMcpConfig(bindings("shared", `{"url":"https://shared.example"}`), json.RawMessage(agentCfg))
			if err == nil {
				t.Fatalf("expected an error for agent document %s", agentCfg)
			}
			if string(got) != agentCfg {
				t.Fatalf("agent config = %s, want it returned unchanged (%s)", got, agentCfg)
			}
		})
	}
}

// The claim path applies the bindings before the per-task overlay, so the two
// merges have to compose into bound < agent < overlay.
func TestResolveAgentMcpConfig_ComposesWithTaskOverlay(t *testing.T) {
	const agentCfg = `{"mcpServers":{"private":{"url":"https://private.example"}}}`
	const overlay = `{"mcpServers":{"composio":{"url":"https://session-composio.example"}}}`

	resolved, err := ResolveAgentMcpConfig(
		bindings("shared", `{"url":"https://shared.example"}`, "composio", `{"url":"https://ws-composio.example"}`),
		json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: unexpected error: %v", err)
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
		t.Errorf("overlay must win over the bound entry, got %v", composio["url"])
	}
}

func TestValidateWorkspaceMcpServerEntry(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "stdio entry", raw: `{"command":"npx","args":["-y","server"]}`},
		{name: "http entry", raw: `{"type":"http","url":"https://mcp.example"}`},
		{name: "empty object", raw: `{}`, wantErr: true},
		{name: "array", raw: `[]`, wantErr: true},
		{name: "string", raw: `"nope"`, wantErr: true},
		{name: "null", raw: `null`, wantErr: true},
		{name: "empty input", raw: ``, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWorkspaceMcpServerEntry(json.RawMessage(tc.raw))
			if tc.wantErr && err == nil {
				t.Fatalf("validateWorkspaceMcpServerEntry(%s): expected an error", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateWorkspaceMcpServerEntry(%s): unexpected error: %v", tc.raw, err)
			}
		})
	}
}

// Validation errors are surfaced to the caller verbatim, and a server entry is
// exactly where a token sits — so the message must never quote input.
func TestValidateWorkspaceMcpServerEntry_ErrorNeverEchoesInput(t *testing.T) {
	secret := `{"headers":{"Authorization":"Bearer sk-live-should-not-leak"` // deliberately truncated

	err := validateWorkspaceMcpServerEntry(json.RawMessage(secret))
	if err == nil {
		t.Fatal("expected an error for a truncated entry")
	}
	if strings.Contains(err.Error(), "sk-live-should-not-leak") {
		t.Fatalf("validation error leaked input: %q", err.Error())
	}
}

func TestValidateWorkspaceMcpServerName(t *testing.T) {
	for _, name := range []string{"linear", "local-tool", "my_server", "abc123"} {
		if err := validateWorkspaceMcpServerName(name); err != nil {
			t.Errorf("validateWorkspaceMcpServerName(%q): unexpected error: %v", name, err)
		}
	}
	// The name is what a runtime mounts the server under, so keep it to the
	// same character set the agent settings dialog enforces.
	for _, name := range []string{"", "has space", "dot.name", "slash/name", "emoji🎉"} {
		if err := validateWorkspaceMcpServerName(name); err == nil {
			t.Errorf("validateWorkspaceMcpServerName(%q): expected an error", name)
		}
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

// mcpTransportOf feeds the client's "can the guided form edit this?" guard, so
// it must never launder an unknown protocol into a known one. Reporting
// {"type":"websocket","url":"wss://…"} as http would let the settings form
// open and rewrite the entry to type:"http" on save — the exact failure the
// front-end guard exists to prevent.
func TestMcpTransportOf(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  string
	}{
		{name: "explicit stdio", entry: `{"type":"stdio","command":"x"}`, want: "stdio"},
		{name: "explicit local", entry: `{"type":"local","command":"x"}`, want: "stdio"},
		{name: "explicit http", entry: `{"type":"http","url":"https://x"}`, want: "http"},
		{name: "explicit streamable-http", entry: `{"type":"streamable-http","url":"https://x"}`, want: "http"},
		{name: "explicit sse", entry: `{"type":"sse","url":"https://x"}`, want: "sse"},
		// The regression: an unknown type with a url must NOT become http.
		{name: "unknown type with url", entry: `{"type":"websocket","url":"wss://x"}`, want: "websocket"},
		{name: "unknown type with command", entry: `{"type":"grpc","command":"x"}`, want: "grpc"},
		{name: "unknown type is normalized to lower case", entry: `{"type":"WebSocket","url":"wss://x"}`, want: "websocket"},
		// Inference only when nothing was declared — that is lossless, because
		// the form writes the same shape back.
		{name: "inferred stdio", entry: `{"command":"x"}`, want: "stdio"},
		{name: "inferred http", entry: `{"url":"https://x"}`, want: "http"},
		{name: "nothing to go on", entry: `{}`, want: "unknown"},
		{name: "malformed", entry: `not json`, want: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpTransportOf(json.RawMessage(tc.entry)); got != tc.want {
				t.Fatalf("mcpTransportOf(%s) = %q, want %q", tc.entry, got, tc.want)
			}
		})
	}
}
