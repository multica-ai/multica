// Copyright 2026 Multica. All rights reserved.

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestMain redirects the home environment variables to one scratch directory
// for the whole binary before any test runs. Config paths resolve through
// os.UserHomeDir — USERPROFILE on Windows, HOME elsewhere — while Hermes uses
// LOCALAPPDATA on Windows. Process-wide redirection isolates tests that forget
// their own redirect; per-test t.Setenv overrides still take precedence.
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

	scratchHome, err := testutil.IsolateUserHome("multica-daemon-tests-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolate internal/daemon test home: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("LOCALAPPDATA", filepath.Join(scratchHome, "AppData", "Local")); err != nil {
		fmt.Fprintf(os.Stderr, "isolate internal/daemon LOCALAPPDATA: %v\n", err)
		_ = os.RemoveAll(scratchHome)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(scratchHome)
	os.Exit(code)
}

var redirectTestHome = testutil.RedirectUserHome
