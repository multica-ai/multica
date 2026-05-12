//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
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

// setupGoroutineDump registers a SIGQUIT handler that writes a full goroutine
// stack trace to a timestamped file. Run `kill -QUIT <pid>` against a hung
// daemon to capture the stack without restarting it.
func setupGoroutineDump(logDir string) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGQUIT)
	go func() {
		for range ch {
			buf := make([]byte, 8<<20)
			n := runtime.Stack(buf, true)
			ts := time.Now().Format("20060102-150405")
			fname := filepath.Join(logDir, "multica-goroutines-"+ts+".txt")
			if err := os.WriteFile(fname, buf[:n], 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "goroutine dump: write failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "=== GOROUTINE DUMP (fallback) ===\n%s\n", buf[:n])
			} else {
				fmt.Fprintf(os.Stderr, "goroutine dump written to %s\n", fname)
			}
		}
	}()
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
