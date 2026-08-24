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

// waitForPidFile polls pidFile until it holds a pid, returning the parsed pid.
func waitForPidFile(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture never recorded its child pid in %s: %v", pidFile, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertProcessGone polls signal-0 until pid no longer exists, failing if it
// is still alive after the deadline.
func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return // gone: the process tree did not survive
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphaned pid %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestDshBackendStartupCancellationKillsProcessTree pins the review regression:
// cancelling Execute while it is still waiting for the protocol `ready` frame
// must terminate the whole process group — fixture plus its spawned `sleep 30`
// child — leaving no orphan behind. The fixture never emits `ready` on stdout,
// so the handshake is guaranteed still pending when the context cancels. The
// cancel fires only after the fixture has recorded its child pid, so the test
// genuinely exercises external context cancellation (not the Timeout bound).
// Unix-only: the shell fixture and signal-0 liveness probing need POSIX.
func TestDshBackendStartupCancellationKillsProcessTree(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	// The fixture starts a long-lived child, records its pid, and never
	// emits a protocol `ready` frame on stdout — Execute must still be
	// waiting for the handshake when the context is cancelled. The pid file
	// alone is the spawn signal: no separate marker write, so there is no
	// ordering window between "child spawned" and "observable" for the test
	// to race against (review 5004702473).
	bin := writeDshFixture(t, fmt.Sprintf(`
sleep 30 &
echo $! > %q
wait
`, pidFile))
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-startup-cancel", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Timeout is a backstop only; the handshake window is wide enough that
	// the external cancel below is what reaches runCtx.Done() first.
	resultCh := make(chan error, 1)
	go func() {
		_, execErr := b.Execute(ctx, "run", ExecOptions{
			Cwd:              t.TempDir(),
			Timeout:          30 * time.Second,
			HandshakeTimeout: 30 * time.Second,
		})
		resultCh <- execErr
	}()
	// Wait until the fixture is actually running with its child spawned,
	// then cancel externally.
	pid := waitForPidFile(t, pidFile)
	cancel()
	select {
	case err := <-resultCh:
		if err == nil || !strings.Contains(err.Error(), "cancelled before the runtime protocol became ready") {
			t.Fatalf("expected cancelled-before-ready error, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return after external cancellation")
	}
	assertProcessGone(t, pid)
}

// TestDshBackendEarlyExitReapsDetachedChild pins the other review regression:
// when the DSH parent exits before `ready` after spawning a background child
// whose stdio is fully redirected, the direct `resCh` return path must still
// reap the remaining process group before surfacing the exit diagnostic.
// Without the fix, Execute returned `exit status 3` while the detached sleep
// kept running as an orphan.
func TestDshBackendEarlyExitReapsDetachedChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	bin := writeDshFixture(t, fmt.Sprintf(`
sleep 30 </dev/null >/dev/null 2>&1 &
echo $! > %q
exit 3
`, pidFile))
	b, err := New("dsh", Config{ExecutablePath: bin, TaskID: "task-early-exit", Logger: slog.Default()})
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
	// The detached child must have been reaped along with the group despite
	// the parent having already exited.
	assertProcessGone(t, waitForPidFile(t, pidFile))
}
