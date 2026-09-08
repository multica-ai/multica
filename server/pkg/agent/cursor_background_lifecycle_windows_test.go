//go:build windows

package agent

import (
	"errors"
	"golang.org/x/sys/windows"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func configureCursorTestBackgroundProcess(cmd *exec.Cmd) { configureProcessGroup(cmd) }

func TestCaptureCursorBackgroundProcessRejectsForeignJob(t *testing.T) {
	spawn := func() *exec.Cmd {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), cursorFakeModeEnv+"=leaf", "CURSOR_FAKE_DURATION=30s")
		hideAgentWindow(cmd)
		if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { signalProcessGroup(cmd, syscall.SIGKILL); _ = cmd.Wait(); releaseProcessGroup(cmd) })
		return cmd
	}
	root, other := spawn(), spawn()
	for _, pid := range []int{root.Process.Pid, other.Process.Pid, os.Getpid(), -1} {
		if p, err := captureCursorBackgroundProcess(root, pid); err == nil {
			p.Close()
			t.Fatalf("accepted non-background PID %d", pid)
		}
	}
}

func assertCursorTestProcessGone(t *testing.T, pid int) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 2000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("background process %d survived: wait=%d err=%v", pid, status, err)
	}
}
