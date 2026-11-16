//go:build windows

package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestCodeArtsWindowsCancellationTerminatesDescendants proves the CodeArts
// backend opts into the owned Windows process-tree path. Killing only the
// direct CLI process would leave the helper's descendant running.
func TestCodeArtsWindowsCancellationTerminatesDescendants(t *testing.T) {
	exePath, pidPath := buildDescendantSpawner(t)
	codeartsTerminateGraceNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codeartsTerminateGraceNanos.Store(0) })

	backend, err := New("codearts", Config{
		ExecutablePath: exePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DESCENDANT_PID_FILE": pidPath},
	})
	if err != nil {
		t.Fatalf("new CodeArts backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("execute CodeArts: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	descendantPID := waitForDescendantPid(t, pidPath)
	if !processStillRunning(descendantPID) {
		t.Fatalf("descendant %d was not running; the test cannot prove tree cleanup", descendantPID)
	}

	cancel()
	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("status = %q, want aborted", result.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CodeArts did not return after cancellation")
	}

	deadline := time.Now().Add(5 * time.Second)
	for processStillRunning(descendantPID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processStillRunning(descendantPID) {
		t.Fatalf("descendant %d survived CodeArts cancellation", descendantPID)
	}
}
