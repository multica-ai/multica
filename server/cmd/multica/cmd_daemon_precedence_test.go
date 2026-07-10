package main

import (
	"strings"
	"testing"
	"time"
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
