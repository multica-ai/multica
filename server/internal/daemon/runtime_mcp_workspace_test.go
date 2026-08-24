package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
)

// effectiveServerNames runs the full chain a claimed task actually goes
// through — the server's binding resolution followed by this daemon's runtime
// merge — and returns the server set the provider would end up with.
//
// Testing the two halves separately is not enough: they agree on the wire
// shape only if the resolver emits its result in the container this merge
// reads, and that coupling is exactly what regressed OpenCode agents.
func effectiveServerNames(t *testing.T, provider string, bound []handler.WorkspaceMcpBinding, agentCfg string) map[string]any {
	t.Helper()

	resolved, err := handler.ResolveAgentMcpConfig(bound, json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveAgentMcpConfig: %v", err)
	}
	merged, err := mergeRuntimeAndAgentMcpConfig(provider, resolved)
	if err != nil {
		t.Fatalf("mergeRuntimeAndAgentMcpConfig: %v", err)
	}
	if len(merged) == 0 {
		return nil
	}
	var doc struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("unmarshal merged document %s: %v", merged, err)
	}
	return doc.McpServers
}

func writeOpencodeRuntimeConfig(t *testing.T, servers string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create opencode config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":`+servers+`}`), 0o600); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}
}

// An OpenCode agent that stored its servers under the legacy top-level `mcp`
// container must keep them once it is given a workspace server. The daemon's
// merge only falls back to `mcp` when `mcpServers` is ABSENT, so a resolver
// that wrote bound servers into `mcpServers` and left the agent's entries
// beside them would silently drop every private server.
func TestWorkspaceResolveThenRuntimeMergeKeepsLegacyOpencodeServers(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode",
		[]handler.WorkspaceMcpBinding{{Name: "shared", Config: json.RawMessage(`{"url":"https://shared.example"}`)}},
		`{"mcp":{"private":{"command":"private-server"}}}`)

	for _, name := range []string{"runtime-local", "shared", "private"} {
		if _, ok := servers[name]; !ok {
			t.Errorf("effective set is missing %q: %v", name, servers)
		}
	}
	if len(servers) != 3 {
		t.Fatalf("effective set = %v, want runtime-local + shared + private", servers)
	}
}

// The same agent with nothing assigned to it must be untouched: the resolver
// passes the agent config through verbatim, so the daemon still takes its
// legacy-container fallback.
func TestWorkspaceResolveWithoutBoundServersLeavesLegacyOpencodeAgentAlone(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode", nil, `{"mcp":{"private":{"command":"private-server"}}}`)

	if len(servers) != 2 || servers["private"] == nil || servers["runtime-local"] == nil {
		t.Fatalf("effective set = %v, want runtime-local + private", servers)
	}
}

// An agent with nothing assigned keeps running with only its runtime's own
// servers — a library entry nobody added must not reach it through the daemon
// merge either.
func TestWorkspaceResolveWithNoBindingsKeepsOnlyRuntimeServers(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode", nil, "")

	if len(servers) != 0 {
		// A nil managed config means the provider keeps its native
		// inheritance; the daemon returns it unchanged rather than building a
		// merged document.
		t.Fatalf("effective set = %v, want the native path (no managed document)", servers)
	}
}
