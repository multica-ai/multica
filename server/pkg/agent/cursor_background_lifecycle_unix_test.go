//go:build linux || darwin

package agent

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func configureCursorTestBackgroundProcess(cmd *exec.Cmd) {
	if os.Getenv("CURSOR_FAKE_SETSID") == "1" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return
	}
	configureProcessGroup(cmd)
}

func assertCursorTestProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := readCursorUnixProcessInfo(pid)
		if err != nil || info.zombie {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background process %d survived", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
