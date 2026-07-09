package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// freshAgentEnvSetCmd returns a standalone cobra.Command with the three
// --custom-env* flags registered identically to agentEnvSetCmd, so
// resolveCustomEnv-shaped tests can mutate flag state without leaking
// across subtests. After MUL-2600 the same three flags are registered
// on `agent create` and `agent env set` (NOT on `agent update`), but
// the parser they drive is shared, so a single fresh-command helper
// covers both call sites.
func freshAgentEnvSetCmd() *cobra.Command {
	c := &cobra.Command{Use: "set"}
	c.Flags().String("custom-env", "", "")
	c.Flags().Bool("custom-env-stdin", false, "")
	c.Flags().String("custom-env-file", "", "")
	return c
}

func chdirWithDaemonTaskMarker(t *testing.T) {
	t.Helper()

	workDir := t.TempDir()
	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("create marker dir: %v", err)
	}
	data := []byte(`{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `"}`)
	if err := os.WriteFile(markerPath, data, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	nested := filepath.Join(workDir, "repo", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested cwd: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

// TestNewAPIClient_WorkdirParentEscapeFailsClosed reproduces the confirmed
// impersonation escape: a sandbox fault strips every MULTICA_* env var from
// an agent subprocess, which then runs `multica` from the *parent* directory
// of its workdir. The per-workdir marker sits below cwd, so the upward walk
// used to find no daemon signal and silently fell back to the user's config
// PAT, posting agent writes as the workspace owner (author_type=member).
//
// The daemon now writes a persistent marker at the workspaces root
// (execenv.EnsureWorkspacesRootMarker), so every cwd inside the
// daemon-owned tree — task dir, workspace dir, sibling task dirs, the root
// itself — carries the fail-closed signal. This test builds that tree shape
// and asserts the CLI refuses the config-PAT fallback from the escaped cwd.
func TestNewAPIClient_WorkdirParentEscapeFailsClosed(t *testing.T) {
	// Seed a user config with a mul_ PAT that must never be picked up.
	t.Setenv("HOME", t.TempDir())
	if err := cli.SaveCLIConfig(cli.CLIConfig{Token: "mul_owner_pat"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Daemon-owned tree: {root}/.multica marker + {root}/{ws}/{task}/workdir
	// with its own per-workdir marker, exactly as the daemon lays it out.
	root := t.TempDir()
	if err := execenv.EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatalf("write root marker: %v", err)
	}
	taskDir := filepath.Join(root, "ws-1", "task-1")
	workDir := filepath.Join(taskDir, "workdir")
	workdirMarker := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(workdirMarker), 0o755); err != nil {
		t.Fatalf("create workdir marker dir: %v", err)
	}
	data := []byte(`{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `"}`)
	if err := os.WriteFile(workdirMarker, data, 0o644); err != nil {
		t.Fatalf("write workdir marker: %v", err)
	}

	// The escape: cwd is the workdir's parent, all daemon env vars gone.
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(taskDir); err != nil {
		t.Fatalf("chdir task dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")

	if got := resolveToken(testCmd()); got != "" {
		t.Fatalf("resolveToken() = %q, want empty (config PAT must not leak into an escaped daemon subprocess)", got)
	}
	if _, err := newAPIClient(testCmd()); err == nil {
		t.Fatal("newAPIClient(): expected fail-closed error from workdir-parent escape, got nil")
	} else if !strings.Contains(err.Error(), "mat_") {
		t.Fatalf("error should demand a task-scoped mat_ token; got %q", err.Error())
	}
}

// TestNewAPIClient_LeftoverMarkerActionableError verifies that a stale
// daemon-task marker with no daemon env (the local_directory crash-leftover
// case) fails closed with an actionable message that names the marker file,
// rather than an opaque "requires mat_ token" error.
func TestNewAPIClient_LeftoverMarkerActionableError(t *testing.T) {
	chdirWithDaemonTaskMarker(t)
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")

	if _, err := newAPIClient(testCmd()); err == nil {
		t.Fatal("newAPIClient(): expected error for leftover daemon-task marker, got nil")
	} else if !strings.Contains(err.Error(), execenv.TaskContextMarkerRelPath) {
		t.Fatalf("error should name the marker path; got %q", err.Error())
	} else if !strings.Contains(err.Error(), "leftover") {
		t.Fatalf("error should hint it may be a leftover; got %q", err.Error())
	}
}

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
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		got := resolveWorkspaceID(testCmd())
		if got != "config-file-ws" {
			t.Fatalf("resolveWorkspaceID() = %q, want %q (config fallback)", got, "config-file-ws")
		}
	})

	t.Run("agent context with explicit env uses env", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "env-ws")

		got := resolveWorkspaceID(testCmd())
		if got != "env-ws" {
			t.Fatalf("resolveWorkspaceID() = %q, want %q (env)", got, "env-ws")
		}
	})

	t.Run("agent context without env returns empty, never config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		got := resolveWorkspaceID(testCmd())
		if got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty (no silent config fallback in agent context)", got)
		}
	})

	t.Run("task marker alone also counts as agent context", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		if got := resolveWorkspaceID(testCmd()); got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty", got)
		}
	})

	t.Run("daemon port marker also skips config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "27182")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		if got := resolveWorkspaceID(testCmd()); got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty", got)
		}
	})

	t.Run("workdir marker also skips config when env is stripped", func(t *testing.T) {
		chdirWithDaemonTaskMarker(t)
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")

		if got := resolveWorkspaceID(testCmd()); got != "" {
			t.Fatalf("resolveWorkspaceID() = %q, want empty", got)
		}
	})

	t.Run("requireWorkspaceID surfaces agent-context error", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
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

func TestResolveToken_AgentContextSkipsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := cli.SaveCLIConfig(cli.CLIConfig{Token: "mul_profile_token"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	t.Run("outside agent context falls back to config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_TOKEN", "")

		if got := resolveToken(testCmd()); got != "mul_profile_token" {
			t.Fatalf("resolveToken() = %q, want profile token", got)
		}
	})

	t.Run("explicit server URL alone still allows normal config token fallback", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
		t.Setenv("MULTICA_TOKEN", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")

		if got := resolveToken(testCmd()); got != "mul_profile_token" {
			t.Fatalf("resolveToken() = %q, want profile token", got)
		}
	})

	t.Run("agent context without env never reads config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_TOKEN", "")

		if got := resolveToken(testCmd()); got != "" {
			t.Fatalf("resolveToken() = %q, want empty in agent context without MULTICA_TOKEN", got)
		}
	})

	t.Run("daemon port marker without env never reads config", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "27182")
		t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
		t.Setenv("MULTICA_TOKEN", "")

		if got := resolveToken(testCmd()); got != "" {
			t.Fatalf("resolveToken() = %q, want empty in daemon-managed context without MULTICA_TOKEN", got)
		}
	})

	t.Run("workdir marker without env never reads config", func(t *testing.T) {
		chdirWithDaemonTaskMarker(t)
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
		t.Setenv("MULTICA_TOKEN", "")

		if got := resolveToken(testCmd()); got != "" {
			t.Fatalf("resolveToken() = %q, want empty in daemon-managed context without MULTICA_TOKEN", got)
		}
	})

	// A non-regular file at the marker path (here a directory) makes
	// os.ReadFile fail with a non-IsNotExist error. That must NOT be read as a
	// daemon-task signal: a normal user whose ancestor tree happens to contain
	// such a path (or any unreadable one) must still reach their config token,
	// rather than be locked out by a fail-closed guard on an unrelated error.
	t.Run("unreadable marker path does not fail closed", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(workDir, execenv.TaskContextMarkerRelPath), 0o755); err != nil {
			t.Fatalf("create marker-as-dir: %v", err)
		}
		nested := filepath.Join(workDir, "repo")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("create nested cwd: %v", err)
		}
		prev, err := os.Getwd()
		if err != nil {
			t.Fatalf("get cwd: %v", err)
		}
		if err := os.Chdir(nested); err != nil {
			t.Fatalf("chdir nested: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Chdir(prev); err != nil {
				t.Fatalf("restore cwd: %v", err)
			}
		})

		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_SERVER_URL", "")
		t.Setenv("MULTICA_TOKEN", "")

		if got := resolveToken(testCmd()); got != "mul_profile_token" {
			t.Fatalf("resolveToken() = %q, want profile token (unreadable marker path must not fail closed)", got)
		}
	})

	t.Run("agent context uses explicit task token env", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "agent-123")
		t.Setenv("MULTICA_TASK_ID", "task-456")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_TOKEN", "mat_task_token")

		if got := resolveToken(testCmd()); got != "mat_task_token" {
			t.Fatalf("resolveToken() = %q, want MULTICA_TOKEN", got)
		}
	})

	t.Run("daemon port set without agent context avoids config fallback", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_TOKEN", "")
		t.Setenv("MULTICA_DAEMON_PORT", "19514")

		if got := resolveToken(testCmd()); got != "" {
			t.Fatalf("resolveToken() = %q, want empty (daemon port set, fail closed)", got)
		}
	})

	t.Run("daemon port set with explicit task token uses task token", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_TOKEN", "mat_task_token")
		t.Setenv("MULTICA_DAEMON_PORT", "19514")

		if got := resolveToken(testCmd()); got != "mat_task_token" {
			t.Fatalf("resolveToken() = %q, want MULTICA_TOKEN (task token wins over daemon signal)", got)
		}
	})

	// MULTICA_SERVER_URL is a user-facing env var that may be set in a
	// normal shell. It is NOT a daemon identity signal — only
	// MULTICA_DAEMON_PORT is. The config fallback must still work when
	// SERVER_URL is set but no daemon signal is present.
	t.Run("MULTICA_SERVER_URL alone does not block config fallback", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_TOKEN", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_SERVER_URL", "https://api.multica.ai")

		if got := resolveToken(testCmd()); got != "mul_profile_token" {
			t.Fatalf("resolveToken() = %q, want profile token (SERVER_URL is not a daemon identity signal)", got)
		}
	})

	// Normal CLI usage: no daemon signals whatsoever. The user-global
	// config token must be reachable. This is the most basic path and
	// must not be broken by any daemon-signal guard expansion.
	t.Run("no daemon signals, normal CLI reads config token", func(t *testing.T) {
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		t.Setenv("MULTICA_TOKEN", "")
		t.Setenv("MULTICA_DAEMON_PORT", "")
		t.Setenv("MULTICA_SERVER_URL", "")

		if got := resolveToken(testCmd()); got != "mul_profile_token" {
			t.Fatalf("resolveToken() = %q, want profile token (normal CLI flow)", got)
		}
	})
}

func TestNewAPIClient_AgentContextRequiresTaskToken(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	t.Setenv("MULTICA_AGENT_ID", "agent-123")
	t.Setenv("MULTICA_TASK_ID", "task-456")

	t.Run("missing token fails closed", func(t *testing.T) {
		t.Setenv("MULTICA_TOKEN", "")

		_, err := newAPIClient(testCmd())
		if err == nil {
			t.Fatal("newAPIClient(): expected error without task token")
		}
		if !strings.Contains(err.Error(), "mat_ token") {
			t.Fatalf("newAPIClient() error = %q, want mat_ token guidance", err.Error())
		}
	})

	t.Run("member token fails closed", func(t *testing.T) {
		t.Setenv("MULTICA_TOKEN", "mul_member_token")

		_, err := newAPIClient(testCmd())
		if err == nil {
			t.Fatal("newAPIClient(): expected error with member token")
		}
		if !strings.Contains(err.Error(), "mat_ token") {
			t.Fatalf("newAPIClient() error = %q, want mat_ token guidance", err.Error())
		}
	})

	t.Run("task token succeeds", func(t *testing.T) {
		t.Setenv("MULTICA_TOKEN", "mat_task_token")

		client, err := newAPIClient(testCmd())
		if err != nil {
			t.Fatalf("newAPIClient(): %v", err)
		}
		if client.Token != "mat_task_token" {
			t.Fatalf("client token = %q, want task token", client.Token)
		}
	})
}

func TestNewAPIClient_DaemonPortRequiresTaskToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "27182")
	t.Setenv("MULTICA_TOKEN", "")

	if err := cli.SaveCLIConfig(cli.CLIConfig{Token: "mul_profile_token", WorkspaceID: "config-file-ws"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := newAPIClient(testCmd())
	if err == nil {
		t.Fatal("newAPIClient(): expected error without task token")
	}
	if !strings.Contains(err.Error(), "mat_ token") {
		t.Fatalf("newAPIClient() error = %q, want mat_ token guidance", err.Error())
	}
}

func TestNewAPIClient_WorkdirMarkerRequiresTaskToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv("MULTICA_TOKEN", "")
	chdirWithDaemonTaskMarker(t)

	if err := cli.SaveCLIConfig(cli.CLIConfig{Token: "mul_profile_token", WorkspaceID: "config-file-ws"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := newAPIClient(testCmd())
	if err == nil {
		t.Fatal("newAPIClient(): expected error without task token")
	}
	if !strings.Contains(err.Error(), "mat_ token") {
		t.Fatalf("newAPIClient() error = %q, want mat_ token guidance", err.Error())
	}
}

// TestParseCustomEnv covers the --custom-env flag parser used by
// `agent create` and `agent env set`. The flag accepts a JSON object
// of string keys and values; the only clear signal is the explicit
// "{}" (server treats a non-nil empty map as a clear). Empty or
// whitespace-only input must error — that path nearly always means an
// upstream failure rather than a deliberate clear, especially via the
// stdin/file channels.
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
			name: "explicit empty object clears",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name:    "empty string errors",
			raw:     ``,
			wantErr: true,
		},
		{
			name:    "whitespace only errors",
			raw:     `   `,
			wantErr: true,
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

// TestAgentUpdateNoFieldsErrorPointsAtEnvCommand invokes runAgentUpdate
// with no flags set and asserts the resulting "no fields" error
// directs the user toward the new env subcommand. After MUL-2600 the
// --custom-env* flags are gone from `agent update`; the hint must
// surface their replacement so users discover the new audited path.
func TestAgentUpdateNoFieldsErrorPointsAtEnvCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "test-ws")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("instructions", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("runtime-config", "", "")
	cmd.Flags().String("model", "", "")
	cmd.Flags().String("custom-args", "", "")
	cmd.Flags().String("visibility", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Int32("max-concurrent-tasks", 0, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")

	err := runAgentUpdate(cmd, []string{"agent-id-placeholder"})
	if err == nil {
		t.Fatal("runAgentUpdate with no flags: expected 'no fields' error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "multica agent env set") {
		t.Fatalf("no-fields error must direct users to `multica agent env set`; got: %q", msg)
	}
}

// TestAgentUpdateDoesNotExposeCustomEnvFlags is the inverse guarantee
// for the above test: if someone re-adds the --custom-env* flags to
// `agent update`, this fails loudly. The /env path is the only
// audited surface and we don't want a silent regression.
func TestAgentUpdateDoesNotExposeCustomEnvFlags(t *testing.T) {
	for _, flag := range []string{"custom-env", "custom-env-stdin", "custom-env-file"} {
		if agentUpdateCmd.Flag(flag) != nil {
			t.Errorf("agent update must NOT expose --%s after MUL-2600; use `multica agent env set` instead", flag)
		}
	}
}

// TestAgentCreateDoesNotExposeFromTemplate guards against re-adding the
// `--from-template` flag. It was an untaught, immature CLI surface that
// short-circuited before body assembly — silently dropping sibling create
// flags like --mcp-config / --custom-env — and was removed. The agent-template
// backend API still exists but has no CLI surface; manual `agent create` is the
// only supported CLI creation path.
func TestAgentCreateDoesNotExposeFromTemplate(t *testing.T) {
	if agentCreateCmd.Flag("from-template") != nil {
		t.Error("agent create must NOT expose --from-template; it was removed as an untaught CLI surface that silently dropped sibling flags")
	}
}

// TestParseCustomEnvErrorSanitization guards against future changes
// re-introducing %w wrapping of json.Unmarshal errors. Those errors
// can surface short fragments of the input, which — for a flag that
// carries secret material — must not appear in user-visible error
// messages.
func TestParseCustomEnvErrorSanitization(t *testing.T) {
	// Pick a string that, if echoed, would be obvious. The key is
	// that the error must not contain any substring of the raw input.
	secretish := `{"SECRET_TOKEN":verySensitiveValue}` // invalid JSON, unquoted value
	_, err := parseCustomEnv(secretish)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	msg := err.Error()
	for _, leak := range []string{"SECRET_TOKEN", "verySensitiveValue"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("parseCustomEnv error leaked input fragment %q: %q", leak, msg)
		}
	}
}

// TestParseCustomArgsErrorSanitization mirrors the parseCustomEnv check
// for --custom-args. custom_args is not a dedicated secret channel, but
// callers regularly stuff sensitive values (e.g. "--api-key=…") into the
// list, so json.Unmarshal errors must never echo input fragments here
// either.
func TestParseCustomArgsErrorSanitization(t *testing.T) {
	secretish := `["--api-key=verySensitiveValue", oops]` // invalid JSON, bare oops
	_, err := parseCustomArgs(secretish)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	msg := err.Error()
	for _, leak := range []string{"--api-key", "verySensitiveValue", "oops"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("parseCustomArgs error leaked input fragment %q: %q", leak, msg)
		}
	}
}

// TestAgentCreateAndEnvSetExposeSecretSafeFlags guarantees the
// --custom-env-stdin and --custom-env-file alternatives stay wired
// up on both commands that accept env input (`agent create` and the
// new `agent env set`). They exist specifically so callers can keep
// secret material out of shell history / 'ps'; regressing either
// surface reopens the foot-gun.
func TestAgentCreateAndEnvSetExposeSecretSafeFlags(t *testing.T) {
	for _, flag := range []string{"custom-env-stdin", "custom-env-file"} {
		if agentCreateCmd.Flag(flag) == nil {
			t.Fatalf("agent create must expose --%s", flag)
		}
		if agentEnvSetCmd.Flag(flag) == nil {
			t.Fatalf("agent env set must expose --%s", flag)
		}
	}
	// The --custom-env help text must warn users that argv is visible
	// to shell history / 'ps' — "never logged" alone is misleading.
	for _, c := range []struct {
		name  string
		usage string
	}{
		{"agent create", agentCreateCmd.Flag("custom-env").Usage},
		{"agent env set", agentEnvSetCmd.Flag("custom-env").Usage},
	} {
		low := strings.ToLower(c.usage)
		if !strings.Contains(low, "shell history") || !strings.Contains(low, "'ps'") {
			t.Fatalf("%s --custom-env usage must warn about shell history and 'ps' exposure; got: %q", c.name, c.usage)
		}
	}
}

// TestResolveCustomEnv exercises the input-channel resolver: inline
// flag, stdin, file, mutual exclusion, and the "not supplied" path.
func TestResolveCustomEnv(t *testing.T) {
	t.Run("not supplied", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || ok || got != nil {
			t.Fatalf("unset flags: got=%v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("inline flag", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		if err := cmd.Flags().Set("custom-env", `{"A":"1"}`); err != nil {
			t.Fatal(err)
		}
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || !ok {
			t.Fatalf("inline: ok=%v err=%v", ok, err)
		}
		if !reflect.DeepEqual(got, map[string]string{"A": "1"}) {
			t.Fatalf("inline: got %v", got)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		if err := cmd.Flags().Set("custom-env-stdin", "true"); err != nil {
			t.Fatal(err)
		}
		cmd.SetIn(bytes.NewBufferString(`{"B":"2"}`))
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || !ok {
			t.Fatalf("stdin: ok=%v err=%v", ok, err)
		}
		if !reflect.DeepEqual(got, map[string]string{"B": "2"}) {
			t.Fatalf("stdin: got %v", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "env.json")
		if err := os.WriteFile(path, []byte(`{"C":"3"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshAgentEnvSetCmd()
		if err := cmd.Flags().Set("custom-env-file", path); err != nil {
			t.Fatal(err)
		}
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || !ok {
			t.Fatalf("file: ok=%v err=%v", ok, err)
		}
		if !reflect.DeepEqual(got, map[string]string{"C": "3"}) {
			t.Fatalf("file: got %v", got)
		}
	})

	t.Run("mutually exclusive: inline + stdin", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env", `{"A":"1"}`)
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutual-exclusion error, got %v", err)
		}
	})

	t.Run("mutually exclusive: inline + file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "env.json")
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env", `{}`)
		_ = cmd.Flags().Set("custom-env-file", path)
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutual-exclusion error, got %v", err)
		}
	})

	t.Run("mutually exclusive: stdin + file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "env.json")
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		_ = cmd.Flags().Set("custom-env-file", path)
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutual-exclusion error, got %v", err)
		}
	})

	t.Run("file: missing path surfaces filesystem error", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-file", filepath.Join(t.TempDir(), "does-not-exist.json"))
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-file") {
			t.Fatalf("expected --custom-env-file error, got %v", err)
		}
	})

	// Empty input on stdin/file almost always means an upstream failure
	// (missing file, set -o pipefail off, etc.), not a deliberate clear.
	// The resolver must reject it with a channel-specific error so the
	// secret map is never silently wiped.
	t.Run("stdin: empty input errors", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		cmd.SetIn(bytes.NewBufferString(""))
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-stdin") || !strings.Contains(err.Error(), "{}") {
			t.Fatalf("expected --custom-env-stdin empty-input error mentioning '{}', got %v", err)
		}
	})

	t.Run("stdin: whitespace-only input errors", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		cmd.SetIn(bytes.NewBufferString("   \n\t "))
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-stdin") {
			t.Fatalf("expected --custom-env-stdin empty-input error, got %v", err)
		}
	})

	t.Run("stdin: explicit {} still clears", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		cmd.SetIn(bytes.NewBufferString("{}"))
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || !ok {
			t.Fatalf("stdin {}: ok=%v err=%v", ok, err)
		}
		if !reflect.DeepEqual(got, map[string]string{}) {
			t.Fatalf("stdin {}: got %v, want empty map", got)
		}
	})

	t.Run("file: empty contents errors", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-file", path)
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-file") || !strings.Contains(err.Error(), "{}") {
			t.Fatalf("expected --custom-env-file empty-contents error mentioning '{}', got %v", err)
		}
	})

	t.Run("file: empty path errors instead of being silently swallowed", func(t *testing.T) {
		cmd := freshAgentEnvSetCmd()
		// Mark the flag as Changed with an empty value — previously this
		// was swallowed by the && filePath != "" guard.
		_ = cmd.Flags().Set("custom-env-file", "")
		if !cmd.Flags().Changed("custom-env-file") {
			t.Fatal("setup: expected custom-env-file flag to be marked Changed")
		}
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-file") {
			t.Fatalf("expected --custom-env-file empty-path error, got %v", err)
		}
	})

	t.Run("file: explicit {} still clears", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "clear.json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshAgentEnvSetCmd()
		_ = cmd.Flags().Set("custom-env-file", path)
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || !ok {
			t.Fatalf("file {}: ok=%v err=%v", ok, err)
		}
		if !reflect.DeepEqual(got, map[string]string{}) {
			t.Fatalf("file {}: got %v, want empty map", got)
		}
	})
}

// freshMcpConfigCmd returns a standalone cobra.Command with the three
// --mcp-config* flags registered identically to `agent create` / `agent
// update`, so resolveMcpConfig-shaped tests can mutate flag state without
// leaking across subtests.
func freshMcpConfigCmd() *cobra.Command {
	c := &cobra.Command{Use: "x"}
	c.Flags().String("mcp-config", "", "")
	c.Flags().Bool("mcp-config-stdin", false, "")
	c.Flags().String("mcp-config-file", "", "")
	return c
}

func TestParseMcpConfig(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string // expected raw JSON; ignored when wantErr
		wantErr bool
	}{
		{name: "object with servers", raw: `{"mcpServers":{"shortcut":{"command":"npx"}}}`, want: `{"mcpServers":{"shortcut":{"command":"npx"}}}`},
		{name: "explicit empty object is a valid empty set", raw: `{}`, want: `{}`},
		{name: "null clears", raw: `null`, want: `null`},
		{name: "null with surrounding whitespace clears", raw: "  null\n", want: `null`},
		{name: "empty string errors", raw: ``, wantErr: true},
		{name: "whitespace only errors", raw: `   `, wantErr: true},
		{name: "not JSON", raw: `command=npx`, wantErr: true},
		{name: "top-level array rejected", raw: `[{"a":1}]`, wantErr: true},
		{name: "top-level string rejected", raw: `"oops"`, wantErr: true},
		{name: "top-level number rejected", raw: `42`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMcpConfig(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMcpConfig(%q): expected error, got nil (result=%s)", tc.raw, got)
				}
				if !strings.Contains(err.Error(), "--mcp-config") {
					t.Fatalf("parseMcpConfig(%q): error should mention --mcp-config, got %v", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMcpConfig(%q): unexpected error: %v", tc.raw, err)
			}
			if string(got) != tc.want {
				t.Fatalf("parseMcpConfig(%q) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseMcpConfigErrorSanitization mirrors the parseCustomEnv check:
// mcp_config carries secret material (MCP entries embed API tokens), so a
// json.Unmarshal failure must never echo fragments of the input.
func TestParseMcpConfigErrorSanitization(t *testing.T) {
	secretish := `{"mcpServers":{"x":{"env":{"TOKEN":verySensitiveValue}}}}` // invalid JSON, unquoted value
	_, err := parseMcpConfig(secretish)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	msg := err.Error()
	for _, leak := range []string{"TOKEN", "verySensitiveValue", "mcpServers"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("parseMcpConfig error leaked input fragment %q: %q", leak, msg)
		}
	}
}

// TestResolveMcpConfig exercises the input-channel resolver: inline flag,
// stdin, file, the `null` clear sentinel, mutual exclusion, and the
// "not supplied" path.
func TestResolveMcpConfig(t *testing.T) {
	t.Run("not supplied", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		got, ok, err := resolveMcpConfig(cmd)
		if err != nil || ok || got != nil {
			t.Fatalf("unset flags: got=%s ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("inline object", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		if err := cmd.Flags().Set("mcp-config", `{"mcpServers":{}}`); err != nil {
			t.Fatal(err)
		}
		got, ok, err := resolveMcpConfig(cmd)
		if err != nil || !ok {
			t.Fatalf("inline: ok=%v err=%v", ok, err)
		}
		if string(got) != `{"mcpServers":{}}` {
			t.Fatalf("inline: got %s", got)
		}
	})

	t.Run("inline null clears", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config", `null`)
		got, ok, err := resolveMcpConfig(cmd)
		if err != nil || !ok {
			t.Fatalf("null: ok=%v err=%v", ok, err)
		}
		if string(got) != `null` {
			t.Fatalf("null: got %s, want null", got)
		}
	})

	t.Run("stdin", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config-stdin", "true")
		cmd.SetIn(bytes.NewBufferString(`{"mcpServers":{"a":{}}}`))
		got, ok, err := resolveMcpConfig(cmd)
		if err != nil || !ok {
			t.Fatalf("stdin: ok=%v err=%v", ok, err)
		}
		if string(got) != `{"mcpServers":{"a":{}}}` {
			t.Fatalf("stdin: got %s", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "mcp.json")
		if err := os.WriteFile(path, []byte(`{"mcpServers":{"b":{}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config-file", path)
		got, ok, err := resolveMcpConfig(cmd)
		if err != nil || !ok {
			t.Fatalf("file: ok=%v err=%v", ok, err)
		}
		if string(got) != `{"mcpServers":{"b":{}}}` {
			t.Fatalf("file: got %s", got)
		}
	})

	t.Run("mutually exclusive: inline + stdin", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config", `{}`)
		_ = cmd.Flags().Set("mcp-config-stdin", "true")
		_, _, err := resolveMcpConfig(cmd)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutual-exclusion error, got %v", err)
		}
	})

	// Empty stdin almost always means an upstream failure, not a deliberate
	// clear — it must error rather than silently wipe a secret-bearing field.
	t.Run("stdin: empty input errors", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config-stdin", "true")
		cmd.SetIn(bytes.NewBufferString(""))
		_, _, err := resolveMcpConfig(cmd)
		if err == nil || !strings.Contains(err.Error(), "--mcp-config-stdin") || !strings.Contains(err.Error(), "null") {
			t.Fatalf("expected --mcp-config-stdin empty-input error mentioning 'null', got %v", err)
		}
	})

	t.Run("file: missing path surfaces filesystem error", func(t *testing.T) {
		cmd := freshMcpConfigCmd()
		_ = cmd.Flags().Set("mcp-config-file", filepath.Join(t.TempDir(), "nope.json"))
		_, _, err := resolveMcpConfig(cmd)
		if err == nil || !strings.Contains(err.Error(), "--mcp-config-file") {
			t.Fatalf("expected --mcp-config-file error, got %v", err)
		}
	})
}

// TestAgentCreateAndUpdateExposeMcpConfigFlags guarantees the secret-safe
// --mcp-config-stdin / --mcp-config-file alternatives stay wired up on both
// commands that accept MCP input. Unlike custom_env, mcp_config IS updatable
// via `agent update` (it has no dedicated audited endpoint), so both surfaces
// must expose all three channels.
func TestAgentCreateAndUpdateExposeMcpConfigFlags(t *testing.T) {
	for _, flag := range []string{"mcp-config", "mcp-config-stdin", "mcp-config-file"} {
		if agentCreateCmd.Flag(flag) == nil {
			t.Fatalf("agent create must expose --%s", flag)
		}
		if agentUpdateCmd.Flag(flag) == nil {
			t.Fatalf("agent update must expose --%s", flag)
		}
	}
	// The --mcp-config help text must warn that argv is visible to shell
	// history / 'ps' — the same foot-gun the custom-env flags warn about.
	for _, c := range []struct {
		name  string
		usage string
	}{
		{"agent create", agentCreateCmd.Flag("mcp-config").Usage},
		{"agent update", agentUpdateCmd.Flag("mcp-config").Usage},
	} {
		low := strings.ToLower(c.usage)
		if !strings.Contains(low, "shell history") || !strings.Contains(low, "'ps'") {
			t.Fatalf("%s --mcp-config usage must warn about shell history and 'ps' exposure; got: %q", c.name, c.usage)
		}
	}
}

func TestAgentSkillsAddCallsAdditiveEndpoint(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "skill-a", "name": "Skill A", "description": ""},
			{"id": "skill-b", "name": "Skill B", "description": ""},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().StringSlice("skill-ids", nil, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("skill-ids", "skill-a,skill-b"); err != nil {
		t.Fatal(err)
	}

	if err := runAgentSkillsAdd(cmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentSkillsAdd: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/agents/agent-123/skills/add" {
		t.Fatalf("path = %q, want additive endpoint", gotPath)
	}
	if !reflect.DeepEqual(gotBody["skill_ids"], []string{"skill-a", "skill-b"}) {
		t.Fatalf("skill_ids body = %v", gotBody["skill_ids"])
	}
}

func TestAgentSkillsAddRequiresSkillIDs(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().StringSlice("skill-ids", nil, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")

	err := runAgentSkillsAdd(cmd, []string{"agent-123"})
	if err == nil || !strings.Contains(err.Error(), "--skill-ids is required") {
		t.Fatalf("expected required --skill-ids error, got %v", err)
	}
}

// TestAgentAvatarHappyPath verifies the full flow: agent pre-check, file upload,
// and avatar update all succeed.
func TestAgentAvatarHappyPath(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "avatar.png")
	if err := os.WriteFile(pngPath, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/agents/agent-123":
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode(map[string]any{
					"id":   "agent-123",
					"name": "TestAgent",
				})
			} else if r.Method == http.MethodPut {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["avatar_url"] != "https://cdn.example.com/avatars/agent-123.png" {
					t.Errorf("unexpected avatar_url: %v", body["avatar_url"])
				}
				json.NewEncoder(w).Encode(map[string]any{
					"id":         "agent-123",
					"name":       "TestAgent",
					"avatar_url": "https://cdn.example.com/avatars/agent-123.png",
				})
			} else {
				t.Errorf("unexpected method: %s", r.Method)
			}
		case "/api/upload-file":
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"id":  "att-456",
				"url": "https://cdn.example.com/avatars/agent-123.png",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", pngPath); err != nil {
		t.Fatal(err)
	}

	if err := runAgentAvatar(cmd, []string{"agent-123"}); err != nil {
		t.Fatalf("runAgentAvatar: %v", err)
	}

	if len(gotPaths) != 3 {
		t.Fatalf("expected 3 API calls, got %d: %v", len(gotPaths), gotPaths)
	}
}

// TestAgentAvatarUnsupportedFormat rejects files with unsupported extensions.
func TestAgentAvatarUnsupportedFormat(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	dir := t.TempDir()
	txtPath := filepath.Join(dir, "avatar.txt")
	if err := os.WriteFile(txtPath, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write test txt: %v", err)
	}

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", txtPath); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported file format") {
		t.Fatalf("expected 'unsupported file format' error, got: %v", err)
	}
}

// TestAgentAvatarOversizedFile rejects files larger than 5MB.
func TestAgentAvatarOversizedFile(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	dir := t.TempDir()
	bigPath := filepath.Join(dir, "big.png")
	// Write slightly more than 5MB.
	if err := os.WriteFile(bigPath, make([]byte, 5<<20+1), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", bigPath); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("expected 'file too large' error, got: %v", err)
	}
}

// TestAgentAvatarMissingAgent returns 404 when the agent does not exist.
func TestAgentAvatarMissingAgent(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "avatar.png")
	if err := os.WriteFile(pngPath, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agents/missing-agent" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "agent not found")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", pngPath); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"missing-agent"})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	if !strings.Contains(err.Error(), "get agent") {
		t.Fatalf("expected 'get agent' error, got: %v", err)
	}
}

// TestAgentAvatarUploadFailure handles upload endpoint returning an error.
func TestAgentAvatarUploadFailure(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "avatar.png")
	if err := os.WriteFile(pngPath, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/agent-123":
			json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "TestAgent"})
		case "/api/upload-file":
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "upload failed")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", pngPath); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error for upload failure, got nil")
	}
	if !strings.Contains(err.Error(), "upload avatar") {
		t.Fatalf("expected 'upload avatar' error, got: %v", err)
	}
}

// TestAgentAvatarUpdateFailure handles the PUT update endpoint returning an error.
func TestAgentAvatarUpdateFailure(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "avatar.png")
	if err := os.WriteFile(pngPath, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/agent-123":
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, "forbidden")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "TestAgent"})
		case "/api/upload-file":
			json.NewEncoder(w).Encode(map[string]any{
				"id":  "att-456",
				"url": "https://cdn.example.com/avatars/agent-123.png",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", pngPath); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error for update failure, got nil")
	}
	if !strings.Contains(err.Error(), "update agent avatar") {
		t.Fatalf("expected 'update agent avatar' error, got: %v", err)
	}
}

// TestAgentAvatarMissingFileFlag rejects when --file is not provided.
func TestAgentAvatarMissingFileFlag(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error when --file is missing, got nil")
	}
	if !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("expected '--file is required' error, got: %v", err)
	}
}

// TestAgentAvatarNonexistentFile rejects when the file path does not exist.
func TestAgentAvatarNonexistentFile(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "avatar"}
	cmd.Flags().String("file", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("file", filepath.Join(t.TempDir(), "does-not-exist.png")); err != nil {
		t.Fatal(err)
	}

	err := runAgentAvatar(cmd, []string{"agent-123"})
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("expected 'file not found' error, got: %v", err)
	}
}

// TestAgentAvatarSizeBoundary verifies that exactly 5MB passes and 5MB+1 fails.
func TestAgentAvatarSizeBoundary(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	t.Run("exactly 5MB passes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ok.png")
		if err := os.WriteFile(path, make([]byte, 5<<20), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		// The command will fail later because no server is running, but
		// the size validation itself should pass.
		cmd := &cobra.Command{Use: "avatar"}
		cmd.Flags().String("file", "", "")
		cmd.Flags().String("output", "json", "")
		cmd.Flags().String("profile", "", "")
		if err := cmd.Flags().Set("file", path); err != nil {
			t.Fatal(err)
		}

		err := runAgentAvatar(cmd, []string{"agent-123"})
		// We expect an error from the network call, not from size validation.
		if err != nil && strings.Contains(err.Error(), "file too large") {
			t.Fatalf("size validation should pass for exactly-5MB file, got: %v", err)
		}
	})

	t.Run("5MB plus one byte is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.png")
		if err := os.WriteFile(path, make([]byte, 5<<20+1), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		cmd := &cobra.Command{Use: "avatar"}
		cmd.Flags().String("file", "", "")
		cmd.Flags().String("output", "json", "")
		cmd.Flags().String("profile", "", "")
		if err := cmd.Flags().Set("file", path); err != nil {
			t.Fatal(err)
		}

		err := runAgentAvatar(cmd, []string{"agent-123"})
		if err == nil {
			t.Fatal("expected error for 5MB+1 file, got nil")
		}
		if !strings.Contains(err.Error(), "file too large") {
			t.Fatalf("expected 'file too large' error, got: %v", err)
		}
	})
}

// TestAgentAvatarCaseInsensitiveExtension verifies uppercase extensions are accepted.
func TestAgentAvatarCaseInsensitiveExtension(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	for _, ext := range []string{"avatar.PNG", "avatar.JPG", "avatar.JPEG", "avatar.GIF", "avatar.WEBP"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ext)
			if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			cmd := &cobra.Command{Use: "avatar"}
			cmd.Flags().String("file", "", "")
			cmd.Flags().String("output", "json", "")
			cmd.Flags().String("profile", "", "")
			if err := cmd.Flags().Set("file", path); err != nil {
				t.Fatal(err)
			}

			err := runAgentAvatar(cmd, []string{"agent-123"})
			// We expect an error from the network call, not from extension validation.
			if err != nil && strings.Contains(err.Error(), "unsupported file format") {
				t.Fatalf("extension validation should pass for %s, got: %v", ext, err)
			}
		})
	}
}

// TestAgentGetTableIncludesAvatarURL verifies the table output includes AVATAR_URL.
func TestAgentGetTableIncludesAvatarURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents/agent-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "agent-123",
			"name":         "TestAgent",
			"status":       "active",
			"runtime_mode": "cloud",
			"visibility":   "workspace",
			"avatar_url":   "https://cdn.example.com/avatar.png",
			"description":  "A test agent",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "table", "")
	cmd.Flags().String("profile", "", "")

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAgentGet(cmd, []string{"agent-123"})

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("runAgentGet: %v", err)
	}
	if !strings.Contains(string(out), "AVATAR_URL") {
		t.Fatalf("table output missing AVATAR_URL header: %s", string(out))
	}
	if !strings.Contains(string(out), "https://cdn.example.com/avatar.png") {
		t.Fatalf("table output missing avatar_url value: %s", string(out))
	}
}

// TestAgentCreateSendsThinkingLevel verifies `agent create --thinking-level`
// puts the value on the top-level `thinking_level` key of the POST body —
// the same field the web inspector and HTTP API already accept. The value is
// passed through verbatim; provider-level validation is the server's job
// (IsKnownThinkingValue), exactly as `--model` defers model validation.
func TestAgentCreateSendsThinkingLevel(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "TestAgent", "thinking_level": "high"})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("instructions", "", "")
	cmd.Flags().String("thinking-level", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	_ = cmd.Flags().Set("name", "TestAgent")
	_ = cmd.Flags().Set("runtime-id", "runtime-1")
	_ = cmd.Flags().Set("thinking-level", "high")

	if err := runAgentCreate(cmd, nil); err != nil {
		t.Fatalf("runAgentCreate: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/agents" {
		t.Fatalf("path = %q, want /api/agents", gotPath)
	}
	if gotBody["thinking_level"] != "high" {
		t.Fatalf("thinking_level body = %v, want high", gotBody["thinking_level"])
	}
}

// TestAgentCreateOmitsThinkingLevelWhenUnset guards the Changed-gated send:
// an unset --thinking-level must not appear in the body at all, so the server
// applies its default instead of receiving an explicit empty string.
func TestAgentCreateOmitsThinkingLevelWhenUnset(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "TestAgent"})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("thinking-level", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	_ = cmd.Flags().Set("name", "TestAgent")
	_ = cmd.Flags().Set("runtime-id", "runtime-1")

	if err := runAgentCreate(cmd, nil); err != nil {
		t.Fatalf("runAgentCreate: %v", err)
	}
	if _, ok := gotBody["thinking_level"]; ok {
		t.Fatalf("unset --thinking-level must be omitted from the body; got %v", gotBody)
	}
}

// TestAgentUpdateSendsThinkingLevel covers both update modes that mirror
// --model: setting an explicit level, and passing an empty string to clear
// back to the runtime default. In both cases the key must be present in the
// PUT body — the server reads it as a tri-state pointer (omitted = no change,
// "" = clear, value = set), so the CLI's only job is to send the key when the
// flag was provided.
func TestAgentUpdateSendsThinkingLevel(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"set explicit level", "xhigh"},
		{"empty string clears", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Errorf("decode request body: %v", err)
				}
				json.NewEncoder(w).Encode(map[string]any{"id": "agent-123", "name": "TestAgent", "thinking_level": tc.value})
			}))
			defer srv.Close()

			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
			t.Setenv("MULTICA_TOKEN", "test-token")
			t.Setenv("MULTICA_AGENT_ID", "")
			t.Setenv("MULTICA_TASK_ID", "")

			cmd := &cobra.Command{Use: "update"}
			cmd.Flags().String("thinking-level", "", "")
			cmd.Flags().String("output", "json", "")
			cmd.Flags().String("profile", "", "")
			if err := cmd.Flags().Set("thinking-level", tc.value); err != nil {
				t.Fatal(err)
			}

			if err := runAgentUpdate(cmd, []string{"agent-123"}); err != nil {
				t.Fatalf("runAgentUpdate: %v", err)
			}
			if gotMethod != http.MethodPut {
				t.Fatalf("method = %s, want PUT", gotMethod)
			}
			if gotPath != "/api/agents/agent-123" {
				t.Fatalf("path = %q, want /api/agents/agent-123", gotPath)
			}
			v, ok := gotBody["thinking_level"]
			if !ok {
				t.Fatalf("body missing thinking_level key; got %v", gotBody)
			}
			if v != tc.value {
				t.Fatalf("thinking_level body = %v, want %q", v, tc.value)
			}
		})
	}
}

// TestAgentCreateAndUpdateExposeThinkingLevelFlag guarantees the flag stays
// wired on both write surfaces. The read side (`agent get`) already exposes
// thinking_level; this is the matching write surface (#4170).
func TestAgentCreateAndUpdateExposeThinkingLevelFlag(t *testing.T) {
	if agentCreateCmd.Flag("thinking-level") == nil {
		t.Error("agent create must expose --thinking-level")
	}
	if agentUpdateCmd.Flag("thinking-level") == nil {
		t.Error("agent update must expose --thinking-level")
	}
}

// TestAgentCreateThinkingLevelServerRejectionSurfaces proves the CLI does not
// own thinking-level validation: a runtime whose provider has no thinking
// concept (or an unknown literal) is rejected server-side with a 400, and that
// message must reach the user rather than being swallowed. This is why the CLI
// can stay a thin pass-through — the server already owns the (provider, model)
// catalog (server/pkg/agent/thinking.go).
func TestAgentCreateThinkingLevelServerRejectionSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"thinking_level \"max\" is not a recognised value for runtime \"gemini\""}`)
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("thinking-level", "", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	_ = cmd.Flags().Set("name", "TestAgent")
	_ = cmd.Flags().Set("runtime-id", "runtime-gemini")
	_ = cmd.Flags().Set("thinking-level", "max")

	err := runAgentCreate(cmd, nil)
	if err == nil {
		t.Fatal("expected error when server rejects thinking_level, got nil")
	}
	if !strings.Contains(err.Error(), "not a recognised value for runtime") {
		t.Fatalf("server thinking_level rejection should surface to the user; got: %v", err)
	}
}
