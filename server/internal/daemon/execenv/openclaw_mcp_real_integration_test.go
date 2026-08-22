//go:build agentintegration

package execenv

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Real-CLI coverage for the managed-MCP include chain. Opt-in twice over: the
// agentintegration build tag, and MULTICA_RUN_REAL_AGENT_SMOKE, because this
// executes the openclaw binary installed on the host.
//
// The unit tests above assert the JSON this package writes. That is not enough
// for a design whose correctness lives in OpenClaw's include-merge semantics —
// `{"mcp": null}` resetting the user's block, and the resolved siblings being
// restored after it. Only the real loader can confirm that, which is why this
// test asks the CLI to resolve the chain we generated rather than inspecting our
// own files.
func realOpenclawBin(t *testing.T) string {
	t.Helper()
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") == "" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run the real openclaw smoke tests")
	}
	bin, err := exec.LookPath("openclaw")
	if err != nil {
		t.Skipf("openclaw not installed: %v", err)
	}
	return bin
}

// realOpenclawConfig points the CLI at an isolated HOME holding a user config
// with both a user-only MCP server (which must not survive the reset) and a
// non-server MCP setting (which must).
func realOpenclawConfig(t *testing.T) (bin, activeConfig string) {
	t.Helper()
	bin = realOpenclawBin(t)

	home := t.TempDir()
	stateDir := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create isolated OpenClaw state: %v", err)
	}
	activeConfig = filepath.Join(stateDir, "openclaw.json")
	config := `{
		"gateway": {"mode": "local"},
		"mcp": {
			"sessionIdleTtlMs": 300000,
			"servers": {"user-only": {"command": "echo"}}
		}
	}`
	if err := os.WriteFile(activeConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write isolated OpenClaw config: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENCLAW_HOME", home)
	t.Setenv("OPENCLAW_STATE_DIR", stateDir)
	t.Setenv("OPENCLAW_CONFIG_PATH", activeConfig)
	return bin, activeConfig
}

func TestPrepareOpenclawConfigRealCLI(t *testing.T) {
	bin, activeConfig := realOpenclawConfig(t)

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: bin,
		McpConfig:   json.RawMessage(`{"mcpServers":{}}`),
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig against the real CLI: %v", err)
	}

	wrapper := mustReadJSON(t, result.ConfigPath)
	snapshotPath := filepath.Join(envRoot, openclawUserSnapshotFile)
	include, ok := wrapper["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != snapshotPath {
		t.Fatalf("wrapper $include = %#v, want [%q]", wrapper["$include"], snapshotPath)
	}
	if result.IncludeRoot != filepath.Dir(activeConfig) {
		t.Fatalf("include root = %q, want %q", result.IncludeRoot, filepath.Dir(activeConfig))
	}

	// Ask the real CLI to resolve the generated include chain. This is what
	// verifies the reset bridge rather than merely inspecting the JSON we wrote.
	t.Setenv("OPENCLAW_CONFIG_PATH", result.ConfigPath)
	t.Setenv("OPENCLAW_INCLUDE_ROOTS", result.IncludeRoot)
	resolvedMcp, err := openclawResolvedMcpConfig(bin, openclawCLITimeout)
	if err != nil {
		t.Fatalf("resolve the generated config with the real CLI: %v", err)
	}
	servers, ok := resolvedMcp["servers"].(map[string]any)
	if !ok || len(servers) != 0 {
		t.Fatalf("resolved managed servers = %#v, want empty and no user-only leak", resolvedMcp["servers"])
	}
	if resolvedMcp["sessionIdleTtlMs"] != float64(300000) {
		t.Fatalf("resolved non-server MCP settings = %#v, want sessionIdleTtlMs preserved", resolvedMcp)
	}
}
