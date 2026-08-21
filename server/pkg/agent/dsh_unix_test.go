//go:build unix

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDshBackendStartupCancellationKillsProcessTree pins the review regression:
// cancelling Execute while it is still waiting for the protocol `ready` frame
// must terminate the whole process group — fixture plus its spawned `sleep 30`
// child — leaving no orphan behind. The fixture never emits `ready` on stdout,
// so the handshake is guaranteed still pending when the context cancels.
// Unix-only: the shell fixture and signal-0 liveness probing need POSIX.
func TestDshBackendStartupCancellationKillsProcessTree(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "alive")
	pidFile := filepath.Join(dir, "pid")
	bin := writeDshFixture(t, fmt.Sprintf(`
sleep 30 &
echo $! > %q
printf '%%s' alive > %q
wait
`, pidFile, marker))
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-startup-cancel", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = b.Execute(ctx, "run", ExecOptions{
		Cwd:              t.TempDir(),
		Timeout:          5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "cancelled before the runtime protocol became ready") {
		t.Fatalf("expected cancelled-before-ready error, got %v", err)
	}
	// Wait for the fixture to write its child pid, then verify the whole
	// process group (fixture + `sleep 30` child) is gone after cancellation.
	deadline := time.Now().Add(5 * time.Second)
	var childPID []byte
	for {
		childPID, err = os.ReadFile(pidFile)
		if err == nil && len(strings.TrimSpace(string(childPID))) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture never reported its child pid: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(childPID)))
	if convErr != nil {
		t.Fatalf("parse child pid %q: %v", childPID, convErr)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return // child is gone: the process tree did not survive
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphaned child pid %d still alive after startup cancellation", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
