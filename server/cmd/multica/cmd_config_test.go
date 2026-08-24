package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newConfigTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config"}
	cmd.Flags().String("profile", "", "")
	return cmd
}

func TestRunConfigSetPersistsSupportedKeysInProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspacesRoot := filepath.Join(t.TempDir(), "multica-dev")

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "dev")

	stderr := captureStderr(t)
	defer stderr.restore()
	if err := runConfigSet(cmd, []string{"server_url", "http://127.0.0.1:8080"}); err != nil {
		t.Fatalf("runConfigSet server_url: %v", err)
	}
	if err := runConfigSet(cmd, []string{"app_url", "http://127.0.0.1:3000"}); err != nil {
		t.Fatalf("runConfigSet app_url: %v", err)
	}
	if err := runConfigSet(cmd, []string{"workspace_id", "ws-123"}); err != nil {
		t.Fatalf("runConfigSet workspace_id: %v", err)
	}
	if err := runConfigSet(cmd, []string{"workspaces_root", workspacesRoot}); err != nil {
		t.Fatalf("runConfigSet workspaces_root: %v", err)
	}
	_ = stderr.read()

	cfg, err := cli.LoadCLIConfigForProfile("dev")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:8080" || cfg.AppURL != "http://127.0.0.1:3000" || cfg.WorkspaceID != "ws-123" || cfg.WorkspacesRoot != workspacesRoot {
		t.Fatalf("config = %#v, want persisted supported keys", cfg)
	}
}

func TestRunConfigShowIncludesProfileAndDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "empty")

	out, err := captureStdout(t, func() error { return runConfigShow(cmd, nil) })
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	// Match on "key:" + eventual "(not set)" — the column width is a
	// formatting detail, not something worth pinning byte-for-byte.
	for _, key := range []string{
		"server_url:",
		"app_url:",
		"workspace_id:",
		"device_name:",
		"runtime_name:",
		"workspaces_root:",
		"max_concurrent_tasks:",
		"poll_interval:",
		"heartbeat_interval:",
		"agent_timeout:",
		"codex_semantic_inactivity_timeout:",
		"codex_handshake_timeout:",
		"disable_auto_update:",
		"auto_update_check_interval:",
		"disable_auto_reload:",
	} {
		if !strings.Contains(out, key) {
			t.Fatalf("runConfigShow output missing %q:\n%s", key, out)
		}
	}
	if !strings.Contains(out, "(not set)") {
		t.Fatalf("runConfigShow output missing (not set) placeholder:\n%s", out)
	}
	if !strings.Contains(out, "disable_auto_update:") || !strings.Contains(out, "false") {
		t.Fatalf("runConfigShow disable_auto_update default should print false:\n%s", out)
	}
	if !strings.Contains(out, "Profile:      empty") {
		t.Fatalf("runConfigShow missing profile header:\n%s", out)
	}
}

func TestRunConfigCommandsUseTaskLocalConfigWithoutTouchingOwner(t *testing.T) {
	ownerHome := t.TempDir()
	taskRoot := filepath.Join(t.TempDir(), "task-multica")
	t.Setenv("HOME", ownerHome)
	t.Setenv("MULTICA_AGENT_ID", "agent-test")
	t.Setenv("MULTICA_TASK_ID", "task-test")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", taskRoot)

	ownerPath := filepath.Join(ownerHome, ".multica", "config.json")
	if err := os.MkdirAll(filepath.Dir(ownerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	ownerBytes := []byte("{\n  \"server_url\": \"https://owner.invalid\",\n  \"workspace_id\": \"owner-workspace-sentinel\",\n  \"token\": \"mul_owner_sentinel\"\n}\n")
	if err := os.WriteFile(ownerPath, ownerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ownerBefore, err := os.Stat(ownerPath)
	if err != nil {
		t.Fatal(err)
	}

	cmd := newConfigTestCmd()
	if err := runConfigSet(cmd, []string{"server_url", "https://task.invalid"}); err != nil {
		t.Fatalf("runConfigSet: %v", err)
	}
	out, err := captureStdout(t, func() error { return runConfigShow(cmd, nil) })
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	for _, forbidden := range []string{ownerHome, "https://owner.invalid", "owner-workspace-sentinel", "mul_owner_sentinel"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("task config output exposed owner sentinel %q:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, filepath.Join(taskRoot, "config.json")) || !strings.Contains(out, "https://task.invalid") {
		t.Fatalf("task config output missing task-local state:\n%s", out)
	}

	after, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	ownerAfter, err := os.Stat(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(ownerBytes) {
		t.Fatalf("owner config content changed: got %q", after)
	}
	if !ownerAfter.ModTime().Equal(ownerBefore.ModTime()) {
		t.Fatalf("owner config mtime changed: before %v after %v", ownerBefore.ModTime(), ownerAfter.ModTime())
	}
}

func TestRunConfigCommandsFailClosedWithoutTaskRoot(t *testing.T) {
	ownerHome := t.TempDir()
	t.Setenv("HOME", ownerHome)
	t.Setenv("MULTICA_AGENT_ID", "agent-test")
	t.Setenv("MULTICA_TASK_ID", "task-test")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", "")

	ownerPath := filepath.Join(ownerHome, ".multica", "config.json")
	if err := os.MkdirAll(filepath.Dir(ownerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	ownerBytes := []byte("{\n  \"server_url\": \"https://owner.invalid\",\n  \"token\": \"mul_owner_sentinel\"\n}\n")
	if err := os.WriteFile(ownerPath, ownerBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newConfigTestCmd()
	if _, err := captureStdout(t, func() error { return runConfigShow(cmd, nil) }); err == nil || !strings.Contains(err.Error(), "task-local") {
		t.Fatalf("runConfigShow error = %v, want missing task-local config root", err)
	}
	if err := runConfigSet(cmd, []string{"server_url", "https://task.invalid"}); err == nil || !strings.Contains(err.Error(), "task-local") {
		t.Fatalf("runConfigSet error = %v, want missing task-local config root", err)
	}
	after, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(ownerBytes) {
		t.Fatalf("owner config content changed: got %q", after)
	}
}

func TestRunConfigSetRejectsUnknownKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigTestCmd()
	err := runConfigSet(cmd, []string{"token", "secret"})
	if err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("runConfigSet error = %v, want unknown key", err)
	}
}

// TestApplyConfigSetSupportsDaemonKeys locks in the daemon keys added
// for issue #3824 (device_name, runtime_name, max_concurrent_tasks,
// poll_interval) plus the follow-up knobs that use the same shape
// (heartbeat_interval, codex_*, disable_auto_update,
// auto_update_check_interval). applyConfigSet is the split-out validator
// so tests don't have to touch disk on every case.
func TestApplyConfigSetSupportsDaemonKeys(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{}
	workspacesRoot := filepath.Join(t.TempDir(), "multica")
	pairs := []struct{ key, val string }{
		{"device_name", "vm-1-custom-name"},
		{"runtime_name", "worker-a"},
		{"workspaces_root", workspacesRoot},
		{"max_concurrent_tasks", "4"},
		{"poll_interval", "10s"},
		{"heartbeat_interval", "5s"},
		{"codex_semantic_inactivity_timeout", "15m"},
		{"codex_handshake_timeout", "45s"},
		{"disable_auto_update", "true"},
		{"auto_update_check_interval", "12h"},
		{"disable_auto_reload", "true"},
	}
	for _, p := range pairs {
		if err := applyConfigSet(&cfg, p.key, p.val); err != nil {
			t.Fatalf("applyConfigSet(%s=%s): %v", p.key, p.val, err)
		}
	}
	if cfg.DeviceName != "vm-1-custom-name" ||
		cfg.RuntimeName != "worker-a" ||
		cfg.WorkspacesRoot != workspacesRoot ||
		cfg.MaxConcurrentTasks != 4 ||
		cfg.PollInterval != "10s" ||
		cfg.HeartbeatInterval != "5s" ||
		cfg.CodexSemanticInactivityTimeout != "15m" ||
		cfg.CodexHandshakeTimeout != "45s" ||
		cfg.DisableAutoUpdate != true ||
		cfg.AutoUpdateCheckInterval != "12h" ||
		cfg.DisableAutoReload != true {
		t.Fatalf("cfg after set = %+v", cfg)
	}
}

func TestApplyConfigSetNormalizesWorkspacesRoot(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	cfg := cli.CLIConfig{}
	if err := applyConfigSet(&cfg, "workspaces_root", filepath.Join("data", "multica")); err != nil {
		t.Fatalf("applyConfigSet: %v", err)
	}
	want := filepath.Join(cwd, "data", "multica")
	if cfg.WorkspacesRoot != want {
		t.Fatalf("WorkspacesRoot = %q, want absolute path %q", cfg.WorkspacesRoot, want)
	}
	if err := applyConfigSet(&cfg, "workspaces_root", ""); err != nil {
		t.Fatalf("clear workspaces_root: %v", err)
	}
	if cfg.WorkspacesRoot != "" {
		t.Fatalf("WorkspacesRoot = %q, want empty after clear", cfg.WorkspacesRoot)
	}
}

func TestApplyConfigSetPositiveDurationRoundTripsToDaemonResolver(t *testing.T) {
	const envName = "TEST_MULTICA_PERSISTED_DURATION"
	t.Setenv(envName, "")

	cases := []struct {
		key  string
		read func(cli.CLIConfig) string
	}{
		{"heartbeat_interval", func(cfg cli.CLIConfig) string { return cfg.HeartbeatInterval }},
		{"codex_semantic_inactivity_timeout", func(cfg cli.CLIConfig) string { return cfg.CodexSemanticInactivityTimeout }},
		{"codex_handshake_timeout", func(cfg cli.CLIConfig) string { return cfg.CodexHandshakeTimeout }},
		{"auto_update_check_interval", func(cfg cli.CLIConfig) string { return cfg.AutoUpdateCheckInterval }},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			cfg := cli.CLIConfig{}
			if err := applyConfigSet(&cfg, tc.key, " 5s "); err != nil {
				t.Fatalf("applyConfigSet: %v", err)
			}

			stored := tc.read(cfg)
			if stored != "5s" {
				t.Fatalf("stored value = %q, want canonical %q", stored, "5s")
			}
			got, err := resolveDaemonDurationOverride(0, envName, stored)
			if err != nil {
				t.Fatalf("resolve persisted value: %v", err)
			}
			if got != 5*time.Second {
				t.Fatalf("resolved duration = %v, want %v", got, 5*time.Second)
			}
		})
	}
}

// TestApplyConfigSetAgentTimeoutTriState pins the agent_timeout
// pointer-based semantics: "" clears, "0s" persists as an explicit
// "disabled" sentinel, positive durations persist as-is. Negative
// values are rejected up front so the daemon doesn't fall back to the
// default and quietly lose the operator's intent.
func TestApplyConfigSetAgentTimeoutTriState(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{}
	if err := applyConfigSet(&cfg, "agent_timeout", "30m"); err != nil {
		t.Fatalf("set 30m: %v", err)
	}
	if cfg.AgentTimeout == nil || *cfg.AgentTimeout != "30m" {
		t.Fatalf("AgentTimeout = %v, want &\"30m\"", cfg.AgentTimeout)
	}
	if err := applyConfigSet(&cfg, "agent_timeout", "0s"); err != nil {
		t.Fatalf("set 0s: %v", err)
	}
	if cfg.AgentTimeout == nil || *cfg.AgentTimeout != "0s" {
		t.Fatalf("AgentTimeout = %v, want explicit &\"0s\"", cfg.AgentTimeout)
	}
	if err := applyConfigSet(&cfg, "agent_timeout", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cfg.AgentTimeout != nil {
		t.Fatalf("AgentTimeout = %v, want nil after clear", cfg.AgentTimeout)
	}
	if err := applyConfigSet(&cfg, "agent_timeout", "-1s"); err == nil {
		t.Fatalf("expected error for negative agent_timeout")
	}
}

// TestApplyConfigSetRejectsBadValues covers the typed keys — ints must
// be non-negative, most durations must be strictly positive, booleans
// must parse. Catching bad values at write time keeps the error next to
// the user's typo instead of surfacing later at daemon start.
//
// The "poll zero" case is the regression from #3824's review: `config
// set poll_interval 0s` used to be accepted and persisted, then
// silently ignored at daemon start because the resolver only
// substitutes strictly positive durations. Reject it up front so
// `config show` and daemon behavior agree.
func TestApplyConfigSetRejectsBadValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"max non-int", "max_concurrent_tasks", "many", "integer"},
		{"max negative", "max_concurrent_tasks", "-1", ">= 0"},
		{"poll bad duration", "poll_interval", "10", "duration"},
		{"poll zero", "poll_interval", "0s", "positive"},
		{"poll negative", "poll_interval", "-5s", "positive"},
		{"heartbeat bad duration", "heartbeat_interval", "abc", "duration"},
		{"heartbeat zero", "heartbeat_interval", "0s", "positive"},
		{"codex semantic zero", "codex_semantic_inactivity_timeout", "0s", "positive"},
		{"codex handshake bad", "codex_handshake_timeout", "10", "duration"},
		{"agent_timeout bad duration", "agent_timeout", "abc", "duration"},
		{"agent_timeout negative", "agent_timeout", "-1s", ">= 0"},
		{"disable_auto_update bad bool", "disable_auto_update", "maybe", "true"},
		{"auto_update_check_interval zero", "auto_update_check_interval", "0s", "positive"},
		{"disable_auto_reload bad bool", "disable_auto_reload", "maybe", "true"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := cli.CLIConfig{}
			err := applyConfigSet(&cfg, tc.key, tc.value)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q; want to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestApplyConfigSetPollIntervalZeroDoesNotOverwrite ensures a rejected
// "0s" write leaves any previously persisted value intact — the caller
// only saves when applyConfigSet returns nil, but pin the invariant so
// a future refactor can't quietly drop it.
func TestApplyConfigSetPollIntervalZeroDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{PollInterval: "10s"}
	if err := applyConfigSet(&cfg, "poll_interval", "0s"); err == nil {
		t.Fatalf("expected error for poll_interval=0s, got nil")
	}
	if cfg.PollInterval != "10s" {
		t.Fatalf("PollInterval mutated on rejected write: got %q, want %q", cfg.PollInterval, "10s")
	}
}

// TestApplyConfigSetEmptyStringClearsTypedKeys — parity with the existing
// "set server_url ”" clearing behavior. For int and duration keys, ""
// resets to the zero value rather than surfacing an Atoi/ParseDuration
// error the user didn't ask for.
func TestApplyConfigSetEmptyStringClearsTypedKeys(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{MaxConcurrentTasks: 8, PollInterval: "10s"}
	if err := applyConfigSet(&cfg, "max_concurrent_tasks", ""); err != nil {
		t.Fatalf("clear max_concurrent_tasks: %v", err)
	}
	if err := applyConfigSet(&cfg, "poll_interval", ""); err != nil {
		t.Fatalf("clear poll_interval: %v", err)
	}
	if cfg.MaxConcurrentTasks != 0 || cfg.PollInterval != "" {
		t.Fatalf("cfg after clear = %+v", cfg)
	}
}

// TestPollIntervalRoundTripThroughDuration ensures the string persisted
// by applyConfigSet parses back to the same Go duration the daemon will
// consume at start-up. Cheap sanity check — the daemon calls
// time.ParseDuration on the same string.
func TestPollIntervalRoundTripThroughDuration(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{}
	if err := applyConfigSet(&cfg, "poll_interval", "1m30s"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		t.Fatalf("re-parse %q: %v", cfg.PollInterval, err)
	}
	if want := time.Minute + 30*time.Second; got != want {
		t.Fatalf("parsed = %v, want %v", got, want)
	}
}

// TestApplyConfigSetExtraHeadersRoundTrip (TIM-142 PR 1) pins the
// accepted shape for `multica config set extra_headers '<json>'`. We
// store map[string]string verbatim in cli.CLIConfig; the daemon's
// resolveExtraHeaders helper turns that into http.Header at startup.
// Multi-entry JSON must survive as-is (entries preserved, values
// preserved byte-for-byte).
func TestApplyConfigSetExtraHeadersRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{}
	spec := `{"X-Auth": "bearer xyz", "X-Region": "eu-west-1"}`
	if err := applyConfigSet(&cfg, "extra_headers", spec); err != nil {
		t.Fatalf("applyConfigSet extra_headers: %v", err)
	}
	want := map[string]string{
		"X-Auth":   "bearer xyz",
		"X-Region": "eu-west-1",
	}
	if len(cfg.ExtraHeaders) != len(want) {
		t.Fatalf("ExtraHeaders length: got %d entries, want %d (got %v)", len(cfg.ExtraHeaders), len(want), cfg.ExtraHeaders)
	}
	for name, val := range want {
		if got := cfg.ExtraHeaders[name]; got != val {
			t.Errorf("ExtraHeaders[%q]: got %q, want %q", name, got, val)
		}
	}
}

// TestApplyConfigSetExtraHeadersEmptyClears — `multica config set
// extra_headers ""` (or `null`) clears a previously persisted map.
// Parity with `config set poll_interval ""` and friends.
func TestApplyConfigSetExtraHeadersEmptyClears(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{ExtraHeaders: map[string]string{"X-Auth": "bearer xyz"}}
	if err := applyConfigSet(&cfg, "extra_headers", ""); err != nil {
		t.Fatalf("clear with empty string: %v", err)
	}
	if cfg.ExtraHeaders != nil {
		t.Fatalf("ExtraHeaders should be nil after empty clear, got %v", cfg.ExtraHeaders)
	}
	// Setting then clearing should also leave the field nil, even
	// after re-populating. Pins the contract that "no headers" and
	// "explicitly unset" are indistinguishable on disk.
	cfg.ExtraHeaders = map[string]string{"X-Auth": "bearer xyz"}
	if err := applyConfigSet(&cfg, "extra_headers", "null"); err != nil {
		t.Fatalf("clear with null: %v", err)
	}
	if cfg.ExtraHeaders != nil {
		t.Fatalf("ExtraHeaders should be nil after null clear, got %v", cfg.ExtraHeaders)
	}
}

// TestApplyConfigSetExtraHeadersRejectsCRLFInjection pins the
// header-injection boundary at the cmd level. A value that smuggles a
// CRLF must surface as a hard error so the on-disk config never
// contains a header that would let an attacker forge `Set-Cookie:` or
// similar downstream.
func TestApplyConfigSetExtraHeadersRejectsCRLFInjection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec string
		want string // substring of expected error
	}{
		{
			name: "CR in value",
			spec: `{"X-Bad": "foo\rSet-Cookie: bad"}`,
			want: "carriage return",
		},
		{
			name: "LF in value",
			spec: `{"X-Bad": "foo\nSet-Cookie: bad"}`,
			want: "line feed",
		},
		// The NUL case is covered at the parser level by
		// TestExtraHeadersFromSpec and TestValidateHeaderNameValue in
		// internal/daemon/headers_test.go. Constructing a JSON-encoded
		// NUL inside a Go raw string is awkward, and the cmd-layer
		// wrapper would only re-assert the same ValidateHeaderNameValue
		// call without exercising new behaviour.
		{
			name: "colon in name",
			spec: `{"X:Inj": "ok"}`,
			want: "colon",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := cli.CLIConfig{}
			err := applyConfigSet(&cfg, "extra_headers", tc.spec)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.want)
			}
			if cfg.ExtraHeaders != nil {
				t.Fatalf("ExtraHeaders mutated on rejected write: got %v", cfg.ExtraHeaders)
			}
		})
	}
}

// TestApplyConfigSetExtraHeadersRejectsMalformedJSON pins the JSON
// boundary so a typo or paste-error surfaces as a parse error rather
// than persisting an unintended empty object.
func TestApplyConfigSetExtraHeadersRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	cfg := cli.CLIConfig{}
	err := applyConfigSet(&cfg, "extra_headers", `{not json`)
	if err == nil {
		t.Fatalf("expected malformed-JSON error, got nil")
	}
	if !strings.Contains(err.Error(), "JSON object literal") {
		t.Fatalf("err = %q, want JSON parse error message", err.Error())
	}
	if cfg.ExtraHeaders != nil {
		t.Fatalf("ExtraHeaders mutated on rejected write: got %v", cfg.ExtraHeaders)
	}
}

// TestRunConfigShowIncludesExtraHeaders verifies the new key shows up
// in `config show` output, since the issue spec calls for in-code
// help / show output to ship with the code.
func TestRunConfigShowIncludesExtraHeaders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The workdir contains a daemon_task_context.json marker; setting
	// MULTICA_TASK_CONFIG_ROOT to a private temp dir makes
	// requireTaskLocalConfigRoot pass and isolates any state we'd
	// write away from the user's repo.
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "dev")

	out, err := captureStdout(t, func() error { return runConfigShow(cmd, nil) })
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	if !strings.Contains(out, "extra_headers:") {
		t.Fatalf("runConfigShow missing extra_headers line:\n%s", out)
	}
	// Empty config should render the "<none>" placeholder so the
	// line is still useful (operator can confirm the field exists and
	// is unconfigured) rather than the usual "(not set)" filler.
	// Values are deliberately omitted from show output (see
	// extraHeadersDisplay) because extra_headers is the primary
	// vehicle for Authorization-style secrets.
	if !strings.Contains(out, " <none>") {
		t.Fatalf("runConfigShow missing empty-extra_headers placeholder:\n%s", out)
	}
}

// TestRunConfigSetExtraHeadersEndToEnd wires the cmd through
// SaveCLIConfigForProfile and reads it back, confirming the on-disk
// shape survives the full cmd path.
func TestRunConfigSetExtraHeadersEndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// See TestRunConfigShowIncludesExtraHeaders for the rationale.
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", t.TempDir())

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "dev")
	stderr := captureStderr(t)
	defer stderr.restore()

	spec := `{"X-Auth": "bearer prod-token", "X-Region": "us-west-2"}`
	if err := runConfigSet(cmd, []string{"extra_headers", spec}); err != nil {
		t.Fatalf("runConfigSet extra_headers: %v", err)
	}
	_ = stderr.read()

	loaded, err := cli.LoadCLIConfigForProfile("dev")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if got, want := loaded.ExtraHeaders["X-Auth"], "bearer prod-token"; got != want {
		t.Errorf("ExtraHeaders[X-Auth]: got %q, want %q", got, want)
	}
	if got, want := loaded.ExtraHeaders["X-Region"], "us-west-2"; got != want {
		t.Errorf("ExtraHeaders[X-Region]: got %q, want %q", got, want)
	}

	// Round-tripping once more with an empty string clears it.
	if err := runConfigSet(cmd, []string{"extra_headers", ""}); err != nil {
		t.Fatalf("runConfigSet clear: %v", err)
	}
	loaded, err = cli.LoadCLIConfigForProfile("dev")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if loaded.ExtraHeaders != nil {
		t.Fatalf("ExtraHeaders should be nil after clear, got %v", loaded.ExtraHeaders)
	}
}
