package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// TestResolveWorkspaceID_AgentContextSkipsConfig is a regression test for
// the cross-workspace contamination bug (#1235). Inside a daemon-spawned
// agent task (MULTICA_AGENT_ID / MULTICA_TASK_ID set), the CLI must NOT
// silently read the user-global ~/.multica/config.json to recover a missing
// workspace — that fallback is how agent operations leaked into an
// unrelated workspace when the daemon failed to inject the right value.
//
// Outside agent context, the three-level fallback (flag → env → config) is
// unchanged.
func TestResolveWorkspaceID_AgentContextSkipsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Seed the global CLI config with a workspace_id that must NOT be
	// picked up while running inside an agent task.
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: "config-file-ws"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	t.Run("outside agent context falls back to config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		got := resolveWorkspaceID(testCmd())
		if got != "config-file-ws" {
			t.Fatalf("resolveWorkspaceID() = %q, want %q (config fallback)", got, "config-file-ws")
		}
	})

	t.Run("agent context with explicit env uses env", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_WORKSPACE_ID", "env-ws")

		got := resolveWorkspaceID(testCmd())
		if got != "env-ws" {
			t.Fatalf("resolveWorkspaceID() = %q, want %q (env)", got, "env-ws")
		}
	})

	t.Run("agent context without env returns empty, never config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		got := resolveWorkspaceID(testCmd())
		if got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty (no silent config fallback in agent context)", got)
		}
	})

	t.Run("task marker alone also counts as agent context", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		if got := resolveWorkspaceID(testCmd()); got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty", got)
		}
	})

	t.Run("requireWorkspaceID surfaces agent-context error", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		_, err := requireWorkspaceID(testCmd())
		if err == nil {
			t.Fatal("requireWorkspaceID(): expected error inside agent context with empty env, got nil")
		}
		if !strings.Contains(err.Error(), "agent execution context") {
			t.Fatalf("requireWorkspaceID() error = %q, want it to mention agent execution context", err.Error())
		}
	})
}

// newUpdateCmd returns a fresh cobra.Command with all flags that runAgentUpdate
// reads. The serverURL env var must be set by the caller via t.Setenv.
func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.PersistentFlags().String("profile", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("instructions", "", "")
	cmd.Flags().String("instructions-file", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("runtime-config", "", "")
	cmd.Flags().String("model", "", "")
	cmd.Flags().String("custom-args", "", "")
	cmd.Flags().String("visibility", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Int32("max-concurrent-tasks", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func TestAgentUpdate_InstructionsFile(t *testing.T) {
	wantContent := "You are a helpful orchestrator agent.\nHandle all incoming tasks carefully."

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-1", "name": "Orchestrator"})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-test")

	// Write instructions to a temp file.
	dir := t.TempDir()
	instrFile := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(instrFile, []byte(wantContent), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cmd := newUpdateCmd()
	if err := cmd.Flags().Set("instructions-file", instrFile); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	if err := runAgentUpdate(cmd, []string{"agent-1"}); err != nil {
		t.Fatalf("runAgentUpdate: %v", err)
	}

	if got, ok := gotBody["instructions"].(string); !ok || got != wantContent {
		t.Errorf("instructions = %q, want %q", gotBody["instructions"], wantContent)
	}
}

func TestAgentUpdate_InstructionsFileMutuallyExclusive(t *testing.T) {
	cmd := newUpdateCmd()
	if err := cmd.Flags().Set("instructions", "inline text"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := cmd.Flags().Set("instructions-file", "/tmp/some-file.md"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	err := runAgentUpdate(cmd, []string{"agent-1"})
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want it to mention mutually exclusive", err.Error())
	}
}

func TestAgentUpdate_InstructionsFileMissing(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://localhost:9999")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-test")

	cmd := newUpdateCmd()
	if err := cmd.Flags().Set("instructions-file", "/nonexistent/path/to/file.md"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	err := runAgentUpdate(cmd, []string{"agent-1"})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "--instructions-file") {
		t.Errorf("error = %q, want it to mention --instructions-file", err.Error())
	}
}

