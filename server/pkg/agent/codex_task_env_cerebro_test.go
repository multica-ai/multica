package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CEREBRO-PATCH(codex-self-mcp-task-env-test): pin task identity in the generated Codex MCP entry.
func TestCodexExecuteInjectsTaskEnvIntoSelfMCPConfig(t *testing.T) {
	fakePath := writeFakeCodexAppServer(t, "exit 0\n")
	codexHome := t.TempDir()
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"CODEX_HOME":           codexHome,
			"MULTICA_TOKEN":        "task-token",
			"MULTICA_SERVER_URL":   "https://multica.example.test",
			"MULTICA_WORKSPACE_ID": "workspace-id",
			"MULTICA_AGENT_ID":     "agent-id",
			"MULTICA_TASK_ID":      "task-id",
			"OPENAI_API_KEY":       "must-not-be-forwarded",
		},
	})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = backend.Execute(ctx, "prompt", ExecOptions{Timeout: 2 * time.Second})

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read generated Codex config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`MULTICA_TOKEN = "task-token"`,
		`MULTICA_SERVER_URL = "https://multica.example.test"`,
		`MULTICA_WORKSPACE_ID = "workspace-id"`,
		`MULTICA_AGENT_ID = "agent-id"`,
		`MULTICA_TASK_ID = "task-id"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated Codex config missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "OPENAI_API_KEY") || strings.Contains(got, "must-not-be-forwarded") {
		t.Fatalf("generated Codex config forwarded unrelated provider secrets:\n%s", got)
	}
}

// CEREBRO-PATCH(codex-self-mcp-task-env-test): a stale user-global Multica entry must not override task identity.
func TestCodexExecuteReplacesUserGlobalSelfMCPWithTaskBoundEntry(t *testing.T) {
	fakePath := writeFakeCodexAppServer(t, "exit 0\n")
	codexHome := t.TempDir()
	initial := `[mcp_servers.multica]
command = "/old/multica"
env = { MULTICA_WORKSPACE_ID = "wrong-workspace", MULTICA_TOKEN = "wrong-token" }

[mcp_servers.user_global]
command = "keep"
`
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(initial), 0o600); err != nil {
		t.Fatalf("seed Codex config: %v", err)
	}
	backend, err := New("codex", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"CODEX_HOME":           codexHome,
			"MULTICA_TOKEN":        "task-token",
			"MULTICA_SERVER_URL":   "https://multica.example.test",
			"MULTICA_WORKSPACE_ID": "task-workspace",
			"MULTICA_AGENT_ID":     "task-agent",
			"MULTICA_TASK_ID":      "task-id",
		},
	})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = backend.Execute(ctx, "prompt", ExecOptions{Timeout: 2 * time.Second})

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read generated Codex config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"[mcp_servers.multica]",
		`MULTICA_WORKSPACE_ID = "task-workspace"`,
		`MULTICA_AGENT_ID = "task-agent"`,
		"[mcp_servers.user_global]",
		`command = "keep"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated Codex config missing %q:\n%s", want, got)
		}
	}
	for _, stale := range []string{"wrong-workspace", "wrong-token", "/old/multica"} {
		if strings.Contains(got, stale) {
			t.Fatalf("generated Codex config retained stale value %q:\n%s", stale, got)
		}
	}
}
