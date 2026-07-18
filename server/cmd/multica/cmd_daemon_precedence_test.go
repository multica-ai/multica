package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestResolveDaemonStringOverridePrecedence pins the three-tier order:
// flag > env > config.json. The daemon.LoadConfig layer applies the env
// value itself via envOrDefault, so when only env is set we return "" —
// the "don't touch, let the runtime read it" signal.
func TestResolveDaemonStringOverridePrecedence(t *testing.T) {
	const envName = "TEST_MULTICA_STR_OVERRIDE"

	cases := []struct {
		name   string
		flag   string
		env    string // "" means unset
		cfg    string
		want   string
	}{
		{"flag wins over env and cfg", "flag-val", "env-val", "cfg-val", "flag-val"},
		{"env suppresses cfg", "", "env-val", "cfg-val", ""},
		{"cfg used when flag and env unset", "", "", "cfg-val", "cfg-val"},
		{"all unset returns empty", "", "", "", ""},
		{"whitespace env counts as unset", "", "   ", "cfg-val", "cfg-val"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(envName, tc.env)
			} else {
				t.Setenv(envName, "")
			}
			got := resolveDaemonStringOverride(tc.flag, envName, tc.cfg)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveDaemonDurationOverridePrecedence covers the numeric path:
// flag>0 wins, env suppresses cfg, cfg parsed on demand, invalid cfg
// surfaces as an error so the daemon doesn't silently fall back.
func TestResolveDaemonDurationOverridePrecedence(t *testing.T) {
	const envName = "TEST_MULTICA_DUR_OVERRIDE"

	cases := []struct {
		name    string
		flag    time.Duration
		env     string
		cfg     string
		want    time.Duration
		errSub  string // substring of expected error, "" = no error
	}{
		{"flag wins", 5 * time.Second, "10s", "20s", 5 * time.Second, ""},
		{"env suppresses cfg", 0, "10s", "20s", 0, ""},
		{"cfg parsed when flag and env unset", 0, "", "500ms", 500 * time.Millisecond, ""},
		{"empty cfg returns zero", 0, "", "", 0, ""},
		{"invalid cfg errors", 0, "", "not-a-duration", 0, "not a valid duration"},
		{"zero cfg errors", 0, "", "0s", 0, "must be positive"},
		{"negative cfg errors", 0, "", "-1s", 0, "must be positive"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(envName, tc.env)
			} else {
				t.Setenv(envName, "")
			}
			got, err := resolveDaemonDurationOverride(tc.flag, envName, tc.cfg)
			if tc.errSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (value=%v)", tc.errSub, got)
				}
				if !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("error = %q; want to contain %q", err.Error(), tc.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveDaemonIntOverridePrecedence — same shape as the string case
// but with the int knob (max_concurrent_tasks). flag>0 wins; env
// non-empty suppresses cfg; cfg>0 wins only when both are absent.
func TestResolveDaemonIntOverridePrecedence(t *testing.T) {
	const envName = "TEST_MULTICA_INT_OVERRIDE"

	cases := []struct {
		name string
		flag int
		env  string
		cfg  int
		want int
	}{
		{"flag wins", 8, "16", 32, 8},
		{"env suppresses cfg", 0, "16", 32, 0},
		{"cfg used when flag and env unset", 0, "", 32, 32},
		{"zero everywhere returns zero", 0, "", 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(envName, tc.env)
			} else {
				t.Setenv(envName, "")
			}
			got := resolveDaemonIntOverride(tc.flag, envName, tc.cfg)
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResolveDaemonAgentTimeoutOverridePrecedence pins the pointer-based
// tri-state for --agent-timeout: an explicit flag (even `--agent-timeout 0`)
// wins over env and cfg; env set means "let LoadConfig read the raw env"
// (nil); cfg pointer is only consulted when both flag and env are silent,
// and "0s" there is a legitimate persisted "disable the cap" sentinel.
func TestResolveDaemonAgentTimeoutOverridePrecedence(t *testing.T) {
	const envName = "TEST_MULTICA_AGENT_TIMEOUT"
	strPtr := func(s string) *string { return &s }

	newCmd := func(changed bool, flagVal time.Duration) *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().Duration("agent-timeout", 0, "")
		if changed {
			_ = cmd.Flags().Set("agent-timeout", flagVal.String())
		}
		return cmd
	}

	cases := []struct {
		name       string
		flagChgd   bool
		flagVal    time.Duration
		env        string
		cfg        *string
		wantPtr    bool
		wantVal    time.Duration
		wantErrSub string
	}{
		{"explicit zero flag wins", true, 0, "10m", strPtr("20m"), true, 0, ""},
		{"positive flag wins", true, 5 * time.Minute, "10m", strPtr("20m"), true, 5 * time.Minute, ""},
		{"env suppresses cfg", false, 0, "10m", strPtr("20m"), false, 0, ""},
		{"cfg positive applied when flag+env silent", false, 0, "", strPtr("30m"), true, 30 * time.Minute, ""},
		{"cfg zero string persists as explicit disable", false, 0, "", strPtr("0s"), true, 0, ""},
		{"cfg nil -> no override", false, 0, "", nil, false, 0, ""},
		{"cfg empty string -> no override", false, 0, "", strPtr(""), false, 0, ""},
		{"cfg unparseable errors", false, 0, "", strPtr("not-a-dur"), false, 0, "not a valid duration"},
		{"cfg negative errors", false, 0, "", strPtr("-1s"), false, 0, "must be >= 0"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(envName, tc.env)
			} else {
				t.Setenv(envName, "")
			}
			cmd := newCmd(tc.flagChgd, tc.flagVal)
			got, err := resolveDaemonAgentTimeoutOverride(cmd, envName, tc.cfg)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error = %q; want %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantPtr {
				if got == nil {
					t.Fatalf("got nil, want ptr to %v", tc.wantVal)
				}
				if *got != tc.wantVal {
					t.Fatalf("got %v, want %v", *got, tc.wantVal)
				}
				return
			}
			if got != nil {
				t.Fatalf("got %v, want nil (no override)", *got)
			}
		})
	}
}

// TestResolveDaemonDisableAutoUpdatePrecedence pins the single-direction
// disable signal: flag OR falsy env OR persisted cfg=true all disable
// auto-update; a truthy env leaves the override off so LoadConfig honors
// the raw env; missing signals return false so the default wins.
func TestResolveDaemonDisableAutoUpdatePrecedence(t *testing.T) {
	const envName = "TEST_MULTICA_DAEMON_AUTO_UPDATE"

	cases := []struct {
		name string
		flag bool
		env  string
		cfg  bool
		want bool
	}{
		{"flag true wins", true, "true", false, true},
		{"env=false disables", false, "false", false, true},
		{"env=0 disables", false, "0", false, true},
		{"env=off disables", false, "off", false, true},
		{"env=true leaves override off", false, "true", true, false},
		{"cfg=true when flag+env silent", false, "", true, true},
		{"cfg=false when flag+env silent", false, "", false, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(envName, tc.env)
			} else {
				t.Setenv(envName, "")
			}
			got := resolveDaemonDisableAutoUpdate(tc.flag, envName, tc.cfg)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
