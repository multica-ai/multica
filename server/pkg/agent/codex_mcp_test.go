package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCodexMcpConfigWritesManagedServers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("model = \"o3\"\n"), 0o644); err != nil {
		t.Fatalf("write seed config: %v", err)
	}

	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"],"env":{"TOKEN":"secret"}}}}`)
	if err := ensureCodexMcpConfig(dir, raw); err != nil {
		t.Fatalf("ensureCodexMcpConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`model = "o3"`,
		`[mcp_servers.fetch]`,
		`command = "uvx"`,
		`args = ["mcp-server-fetch"]`,
		`[mcp_servers.fetch.env]`,
		`TOKEN = "secret"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 0600", got)
	}
}
