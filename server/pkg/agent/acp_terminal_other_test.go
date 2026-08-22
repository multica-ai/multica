//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestACPManagedTerminalKillTerminatesProcessTree(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	c := &hermesClient{
		terminalCtx: context.Background(),
		terminalCwd: t.TempDir(),
		terminalEnv: os.Environ(),
		terminals:   make(map[string]*acpTerminal),
	}
	params, err := json.Marshal(map[string]any{
		"sessionId": "s",
		"command":   "/bin/sh",
		"args":      []string{"-c", fmt.Sprintf("sleep 30 & child=$!; echo $child > %q; wait $child", pidFile)},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.acpTerminalCreate(params)
	if err != nil {
		t.Fatal(err)
	}
	id := created["terminalId"].(string)
	defer c.acpTerminalRelease(id)

	childPID := waitForACPChildPID(t, pidFile, 2*time.Second)
	defer syscall.Kill(childPID, syscall.SIGKILL)
	request := json.RawMessage(`{"sessionId":"s","terminalId":"` + id + `"}`)
	if _, err := c.acpTerminalResponse("terminal/kill", request); err != nil {
		t.Fatal(err)
	}
	status, err := c.acpTerminalResponse("terminal/wait_for_exit", request)
	if err != nil {
		t.Fatal(err)
	}
	if status["exitCode"] != nil {
		t.Fatalf("exitCode = %#v, want null for signal exit", status["exitCode"])
	}
	if signal, ok := status["signal"].(string); !ok || signal == "" {
		t.Fatalf("signal = %#v, want signal name", status["signal"])
	}

	deadline := time.Now().Add(2 * time.Second)
	for processExists(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processExists(childPID) {
		t.Fatalf("terminal child process %d survived terminal/kill", childPID)
	}
}

func waitForACPChildPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("terminal child process did not start")
	return 0
}

func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
