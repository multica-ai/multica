package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/desktopdictation"
)

func TestDesktopDictationHelperRunsBeforeNormalCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Ordinary CLI config resolution rejects this root. The private helper
	// must never reach that path, even when rejecting an invalid HWND.
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", "not-an-absolute-path")
	previousArgs := os.Args
	os.Args = []string{"multica", desktopdictation.HelperArg, "toggle", "0"}
	t.Cleanup(func() { os.Args = previousArgs })
	output, err := captureStdout(t, func() error {
		main()
		return nil
	})
	if err != nil || output != "unavailable\n" {
		t.Fatalf("helper output = %q, error = %v", output, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".multica")); !os.IsNotExist(err) {
		t.Fatal("helper created ordinary CLI state")
	}
}
