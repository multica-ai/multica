package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
)

// effectiveServerNames runs the full chain a claimed task actually goes
// through — the server's workspace/agent resolution followed by this daemon's
// runtime merge — and returns the server set the provider would end up with.
//
// Testing the two halves separately is not enough: they agree on the wire
// shape only if the resolver emits its result in the container this merge
// reads, and that coupling is exactly what regressed OpenCode agents.
func effectiveServerNames(t *testing.T, provider, workspaceCfg, agentCfg string) map[string]any {
	t.Helper()

	resolved, err := handler.ResolveWorkspaceMcpConfig(json.RawMessage(workspaceCfg), json.RawMessage(agentCfg))
	if err != nil {
		t.Fatalf("ResolveWorkspaceMcpConfig: %v", err)
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
// container must keep them once the workspace starts sharing servers. The
// daemon's merge only falls back to `mcp` when `mcpServers` is ABSENT, so a
// resolver that wrote shared servers into `mcpServers` and left the agent's
// entries beside them would silently drop every private server.
func TestWorkspaceResolveThenRuntimeMergeKeepsLegacyOpencodeServers(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode",
		`{"mcpServers":{"shared":{"url":"https://shared.example"}}}`,
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

// The same agent, before the workspace shares anything, must be untouched:
// with no workspace document the resolver passes the agent config through
// verbatim, so the daemon still takes its legacy-container fallback.
func TestWorkspaceResolveWithoutSharedServersLeavesLegacyOpencodeAgentAlone(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode", "", `{"mcp":{"private":{"command":"private-server"}}}`)

	if len(servers) != 2 || servers["private"] == nil || servers["runtime-local"] == nil {
		t.Fatalf("effective set = %v, want runtime-local + private", servers)
	}
}

// An agent that opted out (managed but empty) keeps running with only its
// runtime's own servers — the shared ones must not reach it through the
// daemon merge either.
func TestWorkspaceResolveThenRuntimeMergeHonorsAgentOptOut(t *testing.T) {
	writeOpencodeRuntimeConfig(t, `{"runtime-local":{"command":"runtime-server"}}`)

	servers := effectiveServerNames(t, "opencode",
		`{"mcpServers":{"shared":{"url":"https://shared.example"}}}`, `{}`)

	if _, leaked := servers["shared"]; leaked {
		t.Fatalf("opted-out agent received the shared server: %v", servers)
	}
	if len(servers) != 1 || servers["runtime-local"] == nil {
		t.Fatalf("effective set = %v, want only the runtime's own server", servers)
	}
}
