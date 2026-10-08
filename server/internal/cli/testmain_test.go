// Copyright 2026 Multica. All rights reserved.

package cli

import (
	"os"
	"testing"
)

// TestMain redirects both home environment variables to one scratch directory
// for the whole binary before any test runs. This package owns SaveCLIConfig,
// which resolves ~/.multica through os.UserHomeDir — USERPROFILE on Windows,
// HOME elsewhere. Tests that only redirect HOME therefore split the fixture's
// write path from the code's read path on Windows: SaveCLIConfig writes test
// fixtures over a real default-profile config.json. Process-wide redirection
// isolates tests that forget their own redirect on every platform; per-test
// t.Setenv overrides still take precedence.
func TestMain(m *testing.M) {
	for _, key := range []string{
		"MULTICA_AGENT_ID",
		"MULTICA_TASK_ID",
		"MULTICA_TOKEN",
		"MULTICA_DAEMON_PORT",
		"MULTICA_WORKSPACE_ID",
		"MULTICA_SERVER_URL",
		"MULTICA_TASK_CONFIG_ROOT",
	} {
		os.Unsetenv(key)
	}

	var scratchHome string
	if home, err := os.MkdirTemp("", "multica-cli-config-tests-home-"); err == nil {
		scratchHome = home
		os.Setenv("HOME", home)
		os.Setenv("USERPROFILE", home)
	}

	code := m.Run()
	if scratchHome != "" {
		os.RemoveAll(scratchHome)
	}
	os.Exit(code)
}

// redirectTestHome points BOTH home environment variables at dir. Production
// resolves ~/.multica through os.UserHomeDir — HOME on unix and USERPROFILE on
// Windows — so redirecting only HOME splits the fixture's write path from the
// code's read path on Windows. Prefer TestMain's process-wide scratch home;
// use this when a single test needs its own directory.
func redirectTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
