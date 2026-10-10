// Copyright 2026 Multica. All rights reserved.

package cli

import (
	"fmt"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
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

	scratchHome, err := testutil.IsolateUserHome("multica-cli-config-tests-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolate internal/cli test home: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(scratchHome)
	os.Exit(code)
}

var redirectTestHome = testutil.RedirectUserHome
