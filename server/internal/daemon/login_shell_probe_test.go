package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoginShellProbeEnabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"false", false},
		{"0", false},
		{"", true},
		{"true", true},
		{"1", true},
		{"no", true},
	}
	for _, c := range cases {
		t.Run(c.val, func(t *testing.T) {
			if c.val == "" {
				// unset: need to clear env
				t.Setenv("MULTICA_LOGIN_SHELL_PROBE", "")
				os.Unsetenv("MULTICA_LOGIN_SHELL_PROBE")
			} else {
				t.Setenv("MULTICA_LOGIN_SHELL_PROBE", c.val)
			}
			if got := loginShellProbeEnabled(); got != c.want {
				t.Fatalf("val %q got %v want %v", c.val, got, c.want)
			}
		})
	}
}

func TestResolveAgentsViaLoginShell_DisabledSkipsFork(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel")
	script := filepath.Join(dir, "zsh")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch \""+sentinel+"\"\n"), 0o755)
	t.Setenv("SHELL", script)

	// enabled: should fork and touch sentinel
	os.Unsetenv("MULTICA_LOGIN_SHELL_PROBE")
	os.Remove(sentinel)
	resolveAgentsViaLoginShell([]string{"claude"})
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("expected sentinel to exist when probe enabled, err %v", err)
	}

	// disabled with 0
	t.Setenv("MULTICA_LOGIN_SHELL_PROBE", "0")
	os.Remove(sentinel)
	m := resolveAgentsViaLoginShell([]string{"claude"})
	if len(m) != 0 {
		t.Fatalf("expected empty map when disabled, got %v", m)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("sentinel should not exist when disabled (0)")
	}

	// disabled with false
	t.Setenv("MULTICA_LOGIN_SHELL_PROBE", "false")
	os.Remove(sentinel)
	m = resolveAgentsViaLoginShell([]string{"claude"})
	if len(m) != 0 {
		t.Fatalf("expected empty map when disabled false, got %v", m)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("sentinel should not exist when disabled (false)")
	}
}
