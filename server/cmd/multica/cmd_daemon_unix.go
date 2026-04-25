//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

// daemonSysProcAttr returns the attributes used when spawning the background
// daemon. The withBreakaway argument exists only to share a signature with
// the Windows version (where it controls CREATE_BREAKAWAY_FROM_JOB); on
// Unix Setsid alone is sufficient to detach the child from its parent's
// session and process group.
func daemonSysProcAttr(_ bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// isAccessDeniedSpawnErr is always false on Unix. The Windows version
// looks for ERROR_ACCESS_DENIED to detect "parent Job Object disallowed
// breakaway" and trigger the breakaway-disabled retry; that retry is a
// no-op on Unix.
func isAccessDeniedSpawnErr(_ error) bool { return false }

func notifyShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

func tailLogFile(logPath string, lines int, follow bool) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, logPath)

	tail := exec.Command("tail", args...)
	tail.Stdout = os.Stdout
	tail.Stderr = os.Stderr
	return tail.Run()
}
