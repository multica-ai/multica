package daemonmcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveCodexMCPServerTablePreservesUnrelatedServers(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	initial := `model = "gpt-5"

[mcp_servers."multica"]
command = "/old/multica"

[mcp_servers."multica".env]
MULTICA_WORKSPACE_ID = "wrong-workspace"

[mcp_servers.multica_tools]
command = "keep-similar-name"

[mcp_servers.user_global]
command = "keep"
`
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := RemoveCodexMCPServerTable(configPath, "multica"); err != nil {
		t.Fatalf("remove server table: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`model = "gpt-5"`,
		"[mcp_servers.multica_tools]",
		`command = "keep-similar-name"`,
		"[mcp_servers.user_global]",
		`command = "keep"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("updated config missing %q:\n%s", want, got)
		}
	}
	for _, stale := range []string{`[mcp_servers."multica"]`, "wrong-workspace", "/old/multica"} {
		if strings.Contains(got, stale) {
			t.Fatalf("updated config retained %q:\n%s", stale, got)
		}
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("config mode = %o, want 600", gotMode)
	}
}
