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
