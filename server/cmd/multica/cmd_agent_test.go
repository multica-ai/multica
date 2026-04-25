package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// freshAgentUpdateCmd returns a standalone cobra.Command with the three
// --custom-env* flags registered identically to agentUpdateCmd, so tests
// can mutate flag state without leaking across subtests (the package-level
// agentUpdateCmd has no Reset).
func freshAgentUpdateCmd() *cobra.Command {
	c := &cobra.Command{Use: "update"}
	c.Flags().String("custom-env", "", "")
	c.Flags().Bool("custom-env-stdin", false, "")
	c.Flags().String("custom-env-file", "", "")
	return c
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
// string keys and values; the only clear signal is the explicit "{}"
// (server treats a non-nil empty map on update as a clear). Empty or
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

// TestAgentUpdateNoFieldsErrorMentionsAllCustomEnvFlags actually invokes
// runAgentUpdate with no flags set and asserts the resulting "no fields"
// error mentions all three --custom-env channels by name. This guards
// against the discoverability regression we'd see if a future edit
// dropped one of the flag names from the hint.
func TestAgentUpdateNoFieldsErrorMentionsAllCustomEnvFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "test-ws")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")

	// Build a fresh command with the same flag surface as agentUpdateCmd
	// but without the package-level state, so cmd.Flags().Changed(...)
	// returns false for every field and runAgentUpdate falls into the
	// "no fields to update" branch.
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("instructions", "", "")
	cmd.Flags().String("runtime-id", "", "")
	cmd.Flags().String("runtime-config", "", "")
	cmd.Flags().String("model", "", "")
	cmd.Flags().String("custom-args", "", "")
	cmd.Flags().String("custom-env", "", "")
	cmd.Flags().Bool("custom-env-stdin", false, "")
	cmd.Flags().String("custom-env-file", "", "")
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
	// "--custom-env (" matches the bare flag specifically, not its -stdin /
	// -file siblings, so we can prove all three names are present.
	for _, want := range []string{"--custom-env (", "--custom-env-stdin", "--custom-env-file"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("no-fields error must mention %q; got: %q", want, msg)
		}
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

// TestAgentCreateAndUpdateExposeSecretSafeFlags guarantees the
// --custom-env-stdin and --custom-env-file alternatives stay wired
// up on both commands. They exist specifically so callers can keep
// secret material out of shell history / 'ps'; regressing either
// surface reopens the foot-gun.
func TestAgentCreateAndUpdateExposeSecretSafeFlags(t *testing.T) {
	for _, flag := range []string{"custom-env-stdin", "custom-env-file"} {
		if agentCreateCmd.Flag(flag) == nil {
			t.Fatalf("agent create must expose --%s", flag)
		}
		if agentUpdateCmd.Flag(flag) == nil {
			t.Fatalf("agent update must expose --%s", flag)
		}
	}
	// The --custom-env help text must warn users that argv is visible
	// to shell history / 'ps' — "never logged" alone is misleading.
	for _, c := range []struct {
		name  string
		usage string
	}{
		{"agent create", agentCreateCmd.Flag("custom-env").Usage},
		{"agent update", agentUpdateCmd.Flag("custom-env").Usage},
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
		cmd := freshAgentUpdateCmd()
		got, ok, err := resolveCustomEnv(cmd)
		if err != nil || ok || got != nil {
			t.Fatalf("unset flags: got=%v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("inline flag", func(t *testing.T) {
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		_ = cmd.Flags().Set("custom-env-file", path)
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutual-exclusion error, got %v", err)
		}
	})

	t.Run("file: missing path surfaces filesystem error", func(t *testing.T) {
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		cmd.SetIn(bytes.NewBufferString(""))
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-stdin") || !strings.Contains(err.Error(), "{}") {
			t.Fatalf("expected --custom-env-stdin empty-input error mentioning '{}', got %v", err)
		}
	})

	t.Run("stdin: whitespace-only input errors", func(t *testing.T) {
		cmd := freshAgentUpdateCmd()
		_ = cmd.Flags().Set("custom-env-stdin", "true")
		cmd.SetIn(bytes.NewBufferString("   \n\t "))
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-stdin") {
			t.Fatalf("expected --custom-env-stdin empty-input error, got %v", err)
		}
	})

	t.Run("stdin: explicit {} still clears", func(t *testing.T) {
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
		_ = cmd.Flags().Set("custom-env-file", path)
		_, _, err := resolveCustomEnv(cmd)
		if err == nil || !strings.Contains(err.Error(), "--custom-env-file") || !strings.Contains(err.Error(), "{}") {
			t.Fatalf("expected --custom-env-file empty-contents error mentioning '{}', got %v", err)
		}
	})

	t.Run("file: empty path errors instead of being silently swallowed", func(t *testing.T) {
		cmd := freshAgentUpdateCmd()
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
		cmd := freshAgentUpdateCmd()
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
