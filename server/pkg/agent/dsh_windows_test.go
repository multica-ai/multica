//go:build windows

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDshWindowsStartupFailureKillsProcessTree pins the Windows half of the
// review-5004702473 regression: a DSH runtime that spawns a descendant and
// dies before the protocol `ready` frame must take the whole owned process
// tree with it. The fake runtime mimics the Unix fixture — spawn a
// long-lived child whose stdio is redirected, record its pid, then exit 3 —
// so the direct `resCh` return path in the handshake select runs while the
// descendant is still alive. Without startOwnedProcessTree ownership,
// reapDshStartupTree can only kill the already-exited direct child and the
// descendant survives as an orphan.
func TestDshWindowsStartupFailureKillsProcessTree(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "fake_dsh.go")
	exePath := filepath.Join(tempDir, "fake_dsh.exe")
	pidPath := filepath.Join(tempDir, "descendant.pid")
	const source = `package main
import (
	"fmt"
	"os"
	"os/exec"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "descendant" {
		devnull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		child := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 > "+os.DevNull)
		child.Stdin, child.Stdout, child.Stderr = devnull, devnull, devnull
		_ = child.Start()
		_ = os.WriteFile(os.Getenv("DESCENDANT_PID_FILE"), []byte(fmt.Sprint(child.Process.Pid)), 0600)
		time.Sleep(time.Hour)
		return
	}
	// Runtime role: spawn the detached descendant, wait until its pid is
	// recorded, then die before ever emitting a "ready" frame.
	child := exec.Command(os.Args[0], "descendant")
	if err := child.Start(); err != nil {
		panic(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(os.Getenv("DESCENDANT_PID_FILE")); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	os.Exit(3)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", exePath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake dsh runtime: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if raw, err := os.ReadFile(pidPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				killProcessTreeByPid(pid)
			}
		}
	})

	b, err := New("dsh", Config{
		ExecutablePath: exePath,
		TaskID:         "task-win-startup-failure",
		Logger:         slog.Default(),
		Env:            map[string]string{"DESCENDANT_PID_FILE": pidPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Execute(context.Background(), "run", ExecOptions{
		Cwd:              t.TempDir(),
		Timeout:          5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("expected the real exit-status diagnostic, got %v", err)
	}

	// The detached grandchild must have been reaped with the owned tree.
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("descendant pid was never recorded: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse descendant pid %q: %v", raw, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processStillRunning(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d survived the dsh startup-failure cleanup", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// killProcessTreeByPid is a test-only cleanup helper: taskkill /T /F forces
// the whole tree of a pid that leaked past a failed assertion.
func killProcessTreeByPid(pid int) {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}
