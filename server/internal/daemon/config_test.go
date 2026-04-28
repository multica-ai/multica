package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLoadConfigWorkdirSharing exercises the MULTICA_WORKDIR_SHARING flag.
// LoadConfig requires a discoverable agent CLI on PATH, so the test seeds a
// fake `claude` binary in a temp dir and points MULTICA_CLAUDE_PATH at it.
func TestLoadConfigWorkdirSharing(t *testing.T) {
	cases := []struct {
		envValue string
		want     string
	}{
		{"", WorkdirSharingTask},
		{"task", WorkdirSharingTask},
		{"issue", WorkdirSharingIssue},
		{"ISSUE", WorkdirSharingIssue},
		{" issue ", WorkdirSharingIssue},
		{"bogus", WorkdirSharingTask}, // unknown values fall back to safe default
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.envValue+"-want-"+tc.want, func(t *testing.T) {
			fakeBin := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write fake binary: %v", err)
			}
			if _, err := exec.LookPath(fakeBin); err != nil {
				t.Skipf("cannot exec fake binary in this environment: %v", err)
			}
			t.Setenv("MULTICA_CLAUDE_PATH", fakeBin)
			t.Setenv("MULTICA_WORKDIR_SHARING", tc.envValue)
			t.Setenv("MULTICA_WORKSPACES_ROOT", t.TempDir())
			t.Setenv("MULTICA_DAEMON_ID", "test-daemon")

			cfg, err := LoadConfig(Overrides{})
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.WorkdirSharing != tc.want {
				t.Errorf("WorkdirSharing = %q, want %q", cfg.WorkdirSharing, tc.want)
			}
		})
	}
}
