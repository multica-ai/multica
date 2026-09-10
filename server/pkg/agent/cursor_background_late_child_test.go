//go:build linux || darwin || windows

package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCaptureCursorBackgroundLateDescendant(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "fork")
	root := exec.Command(os.Args[0])
	root.Env = append(os.Environ(), cursorFakeModeEnv+"=late", "CURSOR_FAKE_DIR="+dir,
		"CURSOR_FAKE_FORK_GATE="+gate, "CURSOR_FAKE_DURATION=30s")
	configureProcessGroup(root)
	hideAgentWindow(root)
	if err := startOwnedProcessTree(root, slog.Default()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { signalProcessGroup(root, syscall.SIGKILL); _ = root.Wait(); releaseProcessGroup(root) })
	readPIDs := func(count int) []int {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var pids []int
			data, _ := os.ReadFile(filepath.Join(dir, "0.json"))
			if json.Unmarshal(data, &pids) == nil && len(pids) == count {
				return pids
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("fixture did not publish %d PIDs", count)
		return nil
	}
	leaderPID := readPIDs(1)[0]
	// The fork gate is still closed: the shell has no child to snapshot yet.
	owned, err := captureCursorBackgroundProcess(root, leaderPID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owned.Terminate(); owned.Close() })
	unrelated := exec.Command(os.Args[0])
	unrelated.Env = append(os.Environ(), cursorFakeModeEnv+"=leaf", "CURSOR_FAKE_DURATION=30s")
	configureProcessGroup(unrelated)
	hideAgentWindow(unrelated)
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	unrelatedDone := make(chan struct{})
	go func() { _ = unrelated.Wait(); close(unrelatedDone) }()
	t.Cleanup(func() { _ = unrelated.Process.Kill(); <-unrelatedDone })
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	pids := readPIDs(2)
	// Test-only failure cleanup uses the fixture's known process identities.
	// Production gets no refresh between capture, fork and leader exit.
	late, err := os.FindProcess(pids[1])
	if err != nil {
		t.Fatal(err)
	}
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = late.Kill()
		}
		_ = late.Release()
	})
	leader, err := os.FindProcess(leaderPID)
	if err != nil {
		t.Fatal(err)
	}
	defer leader.Release()
	if err := leader.Kill(); err != nil {
		t.Fatal(err)
	}
	assertCursorTestProcessGone(t, leaderPID)
	if err := owned.Terminate(); err != nil {
		t.Fatalf("late-descendant cleanup failed: %v", err)
	}
	assertCursorTestProcessGone(t, pids[1])
	cleaned = true
	if alive, err := owned.Alive(); alive || err != nil {
		t.Fatalf("late descendant remains: alive=%v err=%v", alive, err)
	}
	select {
	case <-unrelatedDone:
		t.Fatal("unrelated process was signalled")
	default:
	}
	owned.Close()
	owned.Close()
}
