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
			"id":         "agent-123",
			"name":       "TestAgent",
			"status":     "active",
			"runtime_mode": "cloud",
			"visibility": "workspace",
			"avatar_url": "https://cdn.example.com/avatar.png",
			"description": "A test agent",
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
