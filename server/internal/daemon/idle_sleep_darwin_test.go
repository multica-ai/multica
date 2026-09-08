package daemon

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCaffeinateAssertionRestartsAndReleasesSynchronously(t *testing.T) {
	assertion := newIdleSleepAssertion(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := assertion.Acquire(); err != nil {
		t.Fatalf("acquire assertion: %v", err)
	}
	t.Cleanup(assertion.Release)

	firstPID := waitForCaffeinateChild(t, 0)
	if err := syscall.Kill(firstPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill first caffeinate process: %v", err)
	}
	_ = waitForCaffeinateChild(t, firstPID)

	assertion.Release()
	if children := caffeinateChildPIDs(t); len(children) != 0 {
		t.Fatalf("caffeinate children after release = %v, want none", children)
	}
}

func waitForCaffeinateChild(t *testing.T, excluding int) int {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for _, pid := range caffeinateChildPIDs(t) {
			if pid != excluding {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for caffeinate child other than %d", excluding)
	return 0
}

func caffeinateChildPIDs(t *testing.T) []int {
	t.Helper()
	out, err := exec.Command("/usr/bin/pgrep", "-P", strconv.Itoa(os.Getpid()), "-x", "caffeinate").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("list caffeinate children: %v", err)
	}

	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("parse caffeinate pid %q: %v", field, err)
		}
		pids = append(pids, pid)
	}
	return pids
}
