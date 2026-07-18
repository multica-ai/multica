package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestListRuntimeLocalMcpServersCodexRedactsDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[mcp_servers.fetch]
command = "uvx"
args = ["mcp-server-fetch", "--token", "secret"]

[mcp_servers.docs]
url = "https://secret.example/mcp"
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("codex")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 2 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "docs" || servers[0].Transport != "http" || servers[0].Enabled {
		t.Fatalf("docs summary = %#v", servers[0])
	}
	if servers[1].Name != "fetch" || servers[1].Transport != "stdio" || !servers[1].Enabled {
		t.Fatalf("fetch summary = %#v", servers[1])
	}
}

func TestListRuntimeLocalMcpServersClaudeMissingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	servers, supported, err := listRuntimeLocalMcpServers("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 0 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

func TestListRuntimeLocalMcpServersClaudeEnabledPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	config := `{"mcpServers":{"paper":{"type":"http","url":"http://127.0.0.1:29979/mcp"}}}`
	if err := os.WriteFile(filepath.Join(installPath, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	servers, supported, err := listRuntimeLocalMcpServers("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(servers) != 1 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
	if servers[0].Name != "paper" || servers[0].Transport != "http" || !servers[0].Enabled {
		t.Fatalf("plugin MCP summary = %#v", servers[0])
	}
	if servers[0].Source != "Claude Plugin · paper-desktop" {
		t.Fatalf("plugin MCP source = %q", servers[0].Source)
	}
}

func TestListRuntimeLocalMcpServersUnknownProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	servers, supported, err := listRuntimeLocalMcpServers("future-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if supported || len(servers) != 0 {
		t.Fatalf("supported=%v servers=%#v", supported, servers)
	}
}

func TestMergeRuntimeAndAgentMcpConfigClaudeCombinesAndAgentWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runtimeConfig := `{"mcpServers":{"runtime-only":{"command":"runtime-cmd","env":{"TOKEN":"local-secret"}},"shared":{"command":"runtime-shared"}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(runtimeConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	if err := os.WriteFile(filepath.Join(installPath, "mcp.json"), []byte(`{"mcpServers":{"paper":{"type":"http","url":"http://127.0.0.1:29979/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("claude", json.RawMessage(`{"mcpServers":{"shared":{"command":"agent-shared"},"agent-only":{"url":"https://agent.example/mcp"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 4 {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
	if got := document.McpServers["shared"]["command"]; got != "agent-shared" {
		t.Fatalf("shared command = %#v, want agent-shared", got)
	}
	if got := document.McpServers["runtime-only"]["command"]; got != "runtime-cmd" {
		t.Fatalf("runtime-only command = %#v", got)
	}
	if got := document.McpServers["runtime-only"]["env"].(map[string]any)["TOKEN"]; got != "local-secret" {
		t.Fatalf("runtime secret was not preserved locally: %#v", got)
	}
	if got := document.McpServers["paper"]["url"]; got != "http://127.0.0.1:29979/mcp" {
		t.Fatalf("plugin server url = %#v", got)
	}
}

func TestMergeRuntimeAndAgentMcpConfigCodexNormalizesHeaders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[mcp_servers.docs]
url = "https://runtime.example/mcp"
http_headers = { Authorization = "Bearer local-secret" }

[mcp_servers.fetch]
command = "uvx"
args = ["mcp-server-fetch"]
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := mergeRuntimeAndAgentMcpConfig("codex", json.RawMessage(`{"mcpServers":{"agent":{"command":"node","args":["agent.js"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.McpServers) != 3 {
		t.Fatalf("merged servers = %#v", document.McpServers)
	}
	if got := document.McpServers["docs"]["type"]; got != "http" {
		t.Fatalf("docs type = %#v", got)
	}
	headers := document.McpServers["docs"]["headers"].(map[string]any)
	if got := headers["Authorization"]; got != "Bearer local-secret" {
		t.Fatalf("docs Authorization = %#v", got)
	}
}

func TestMergeRuntimeAndAgentMcpConfigNullKeepsNativeInheritance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(" null ")} {
		merged, err := mergeRuntimeAndAgentMcpConfig("claude", raw)
		if err != nil {
			t.Fatal(err)
		}
		if string(merged) != string(raw) {
			t.Fatalf("merged %q = %q", string(raw), string(merged))
		}
	}
}
