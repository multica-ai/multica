//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopCodexSidecarCommandTerminatesProcessGroup(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "child.pid")
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & echo $! > \"$1\"; wait", "sh", pidFile)
	prepareCodexSidecarCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar fixture: %v", err)
	}

	childPID := waitForPIDFile(t, pidFile)
	stopCodexSidecarCommand(cmd)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if !processExists(childPID) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d stayed alive after process group shutdown", childPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr != nil {
				t.Fatalf("parse child pid: %v", convErr)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("read child pid file: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
