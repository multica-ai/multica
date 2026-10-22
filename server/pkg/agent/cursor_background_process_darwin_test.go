//go:build darwin

package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCaptureCursorBackgroundDetachedSession(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids.json")
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), cursorFakeModeEnv+"=shell", "CURSOR_FAKE_DURATION=30s", "CURSOR_FAKE_PIDS="+pidFile)
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer self.Release()
	var pids []int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(pidFile)
		if json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pids) != 2 {
		t.Fatal("fixture did not publish detached shell and child")
	}
	defer syscall.Kill(pids[1], syscall.SIGKILL)
	owned, err := captureCursorBackgroundProcess(&exec.Cmd{Process: self}, child.Process.Pid)
	if err != nil {
		t.Fatalf("capture detached Cursor shell: %v", err)
	}
	defer owned.Close()
	group := owned.platform.(*cursorUnixBackgroundProcess)
	group.leaderStart++
	if err := owned.Terminate(); err == nil {
		t.Fatal("signalled a detached shell with a changed identity")
	}
	group.leaderStart--
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("identity rejection signalled the detached shell: %v", err)
	}
	if err := owned.Terminate(); err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		assertCursorTestProcessGone(t, pid)
	}
}
