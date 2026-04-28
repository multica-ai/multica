//go:build !windows

package procgroup

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCleanupReapsGrandchildrenUnix is the core regression test:
// spawn a shell that itself spawns a long-running grandchild, then
// verify procgroup.Start's cleanup kills the grandchild via the
// process group SIGKILL.
//
// This test mirrors the production scenario where the daemon
// spawns claude.exe which spawns pwsh which spawns kd.exe — only
// procgroup.Start's cleanup can reap the grandchild because Go's
// exec.CommandContext only kills the immediate child. Without
// procgroup, this test would fail (the inner sleep would survive
// the outer shell being killed).
func TestCleanupReapsGrandchildrenUnix(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()

	// Outer shell forks an inner sleep, prints inner PID to fd 3,
	// then sleeps so the test can verify both are alive before
	// triggering cleanup.
	cmd := exec.Command("/bin/sh", "-c",
		"sleep 9999 & echo $! >&3; sleep 9999")
	cmd.ExtraFiles = []*os.File{w}

	cleanup, err := Start(cmd)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(cleanup)

	// Close the parent end of the writer; reads from r will see
	// EOF when the shell-side w is closed (or on EOF after read).
	_ = w.Close()

	buf := make([]byte, 32)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("read grandchild PID: %v", err)
	}
	gcPID, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		t.Fatalf("parse PID %q: %v", buf[:n], err)
	}

	// Sanity: grandchild is alive right now.
	if err := syscall.Kill(gcPID, 0); err != nil {
		t.Fatalf("grandchild %d not alive after spawn: %v", gcPID, err)
	}

	// Trigger cleanup, which SIGKILLs the entire pgroup.
	cleanup()

	// Wait briefly for the kernel to deliver SIGKILL.
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		err := syscall.Kill(gcPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return // success
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive 2s after cleanup: kill(0) = %v",
		gcPID, lastErr)
}
