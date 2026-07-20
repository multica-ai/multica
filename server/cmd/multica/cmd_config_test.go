package main

import (
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
	_ = stderr.read()

	cfg, err := cli.LoadCLIConfigForProfile("dev")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:8080" || cfg.AppURL != "http://127.0.0.1:3000" || cfg.WorkspaceID != "ws-123" {
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
		"max_concurrent_tasks:",
		"poll_interval:",
		"heartbeat_interval:",
		"agent_timeout:",
		"codex_semantic_inactivity_timeout:",
		"codex_handshake_timeout:",
		"disable_auto_update:",
		"auto_update_check_interval:",
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
	pairs := []struct{ key, val string }{
		{"device_name", "vm-1-custom-name"},
		{"runtime_name", "worker-a"},
		{"max_concurrent_tasks", "4"},
		{"poll_interval", "10s"},
		{"heartbeat_interval", "5s"},
		{"codex_semantic_inactivity_timeout", "15m"},
		{"codex_handshake_timeout", "45s"},
		{"disable_auto_update", "true"},
		{"auto_update_check_interval", "12h"},
	}
	for _, p := range pairs {
		if err := applyConfigSet(&cfg, p.key, p.val); err != nil {
			t.Fatalf("applyConfigSet(%s=%s): %v", p.key, p.val, err)
		}
	}
	if cfg.DeviceName != "vm-1-custom-name" ||
		cfg.RuntimeName != "worker-a" ||
		cfg.MaxConcurrentTasks != 4 ||
		cfg.PollInterval != "10s" ||
		cfg.HeartbeatInterval != "5s" ||
		cfg.CodexSemanticInactivityTimeout != "15m" ||
		cfg.CodexHandshakeTimeout != "45s" ||
		cfg.DisableAutoUpdate != true ||
		cfg.AutoUpdateCheckInterval != "12h" {
		t.Fatalf("cfg after set = %+v", cfg)
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
// "set server_url ''" clearing behavior. For int and duration keys, ""
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
