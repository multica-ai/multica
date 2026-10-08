//go:build windows

package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDimBackendUsesOwnedProcessTree verifies that the Dim backend starts its
// child process through startOwnedProcessTree (Job Object on Windows) rather
// than a plain cmd.Start, so tool descendants are captured and terminated by
// the cleanup. This is a Dim-specific companion to
// TestStartOwnedProcessTreeCapturesImmediateDescendants (review round 4 #1).
func TestDimBackendUsesOwnedProcessTree(t *testing.T) {
	spawnerExe, pidPath := buildDescendantSpawner(t)

	// Create a fake dim binary: a batch file that spawns the descendant
	// spawner (which writes its PID to pidPath) and then hangs, mimicking a
	// real dim acp process that spawns tool subprocesses.
	dir := t.TempDir()
	fakeDim := filepath.Join(dir, "dim.bat")
	script := "@echo off\r\n\"" + spawnerExe + "\" spawn\r\n:loop\r\nping -n 60 127.0.0.1 >nul\r\ngoto loop\r\n"
	if err := os.WriteFile(fakeDim, []byte(script), 0o644); err != nil {
		t.Fatalf("write fake dim: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b, err := New("dim", Config{
		ExecutablePath: fakeDim,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(dim): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Wait for the descendant to be spawned by the fake dim.
	descendantPid := waitForDescendantPid(t, pidPath)
	if !processStillRunning(descendantPid) {
		t.Fatalf("descendant %d was not running; the test cannot prove anything", descendantPid)
	}

	// Drain the result — the deferred cleanup should terminate the whole tree
	// via the Job Object attached by startOwnedProcessTree.
	<-session.Result

	// The descendant must be gone — the Job Object captured and killed it.
	// Give the OS a moment to reap.
	goneDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(goneDeadline) {
		if !processStillRunning(descendantPid) {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("descendant PID %d survived Dim cleanup; the process tree was not owned by a Job Object", descendantPid)
}
