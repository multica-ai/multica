//go:build !windows && !linux && !darwin

package agent

import (
	"os/exec"
	"testing"
)

func configureCursorTestBackgroundProcess(cmd *exec.Cmd) { configureProcessGroup(cmd) }

func assertCursorTestProcessGone(t *testing.T, _ int) {
	t.Helper()
	t.Skip("background process ownership is unavailable on this platform")
}
