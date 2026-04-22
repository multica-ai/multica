package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
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
