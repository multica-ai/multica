package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparePiLeavesProjectMcpConfigAlone(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-pi-mcp",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Pi",
		Provider:       "pi",
		McpConfig:      json.RawMessage(`{"mcpServers":{"probe":{"command":"echo"}}}`),
		Task: TaskContextForEnv{
			IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := os.Stat(filepath.Join(env.WorkDir, ".pi", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("Prepare created .pi/mcp.json, stat error = %v", err)
	}
}

func TestPreparePiDoesNotOverwriteExistingProjectMcpConfig(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	userDir := t.TempDir()
	path := filepath.Join(userDir, ".pi", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"mcpServers":{"user":{"command":"echo"}}}`)
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-pi-mcp-existing",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Pi",
		Provider:       "pi",
		LocalWorkDir:   userDir,
		McpConfig:      json.RawMessage(`{"mcpServers":{"managed":{"command":"echo"}}}`),
		Task: TaskContextForEnv{
			IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(existing) {
		t.Fatalf("existing .pi/mcp.json changed to %s", data)
	}
}
