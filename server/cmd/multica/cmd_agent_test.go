package main

import (
	"reflect"
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

// TestParseCustomEnv covers the --custom-env flag parser used by both
// `agent create` and `agent update`. The flag accepts a JSON object of
// string keys and values; both "" and "{}" mean "clear the map"
// (server treats a non-nil empty map on update as a clear), and any
// other input must be a valid JSON object.
func TestParseCustomEnv(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "single pair",
			raw:  `{"SECOND_BRAIN_TOKEN":"abc123"}`,
			want: map[string]string{"SECOND_BRAIN_TOKEN": "abc123"},
		},
		{
			name: "multiple pairs",
			raw:  `{"A":"1","B":"2"}`,
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "empty object clears",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name: "empty string clears",
			raw:  ``,
			want: map[string]string{},
		},
		{
			name: "whitespace only clears",
			raw:  `   `,
			want: map[string]string{},
		},
		{
			name:    "not JSON",
			raw:     `KEY=value`,
			wantErr: true,
		},
		{
			name:    "JSON array not object",
			raw:     `["A","B"]`,
			wantErr: true,
		},
		{
			name:    "non-string value",
			raw:     `{"A":1}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCustomEnv(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCustomEnv(%q): expected error, got nil (result=%v)", tc.raw, got)
				}
				if !strings.Contains(err.Error(), "--custom-env") {
					t.Fatalf("parseCustomEnv(%q): error should mention --custom-env, got %v", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCustomEnv(%q): unexpected error: %v", tc.raw, err)
			}
			if got == nil {
				t.Fatalf("parseCustomEnv(%q): result must be non-nil (empty map, not nil) so the server treats it as clear", tc.raw)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseCustomEnv(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestAgentUpdateNoFieldsMentionsCustomEnv makes sure the "no fields"
// usage hint lists --custom-env so discoverability does not silently
// regress the next time someone touches this error message.
func TestAgentUpdateNoFieldsMentionsCustomEnv(t *testing.T) {
	if !strings.Contains(agentUpdateCmd.Flag("custom-env").Usage, "secret") {
		t.Fatalf("custom-env usage must flag it as secret material; got: %q", agentUpdateCmd.Flag("custom-env").Usage)
	}
	if agentCreateCmd.Flag("custom-env") == nil {
		t.Fatal("agent create must expose --custom-env")
	}
}
