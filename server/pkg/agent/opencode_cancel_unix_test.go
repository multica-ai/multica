//go:build unix

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// opencodeCancelFakeScript returns a POSIX-sh script that impersonates a
// long-running `opencode`: it spawns a background grandchild, records both its
// own (process-group-leader) pid and the grandchild pid, then streams stdout in
// a tight loop forever. This is the shape that orphans and spins on EPIPE when
// the daemon closes stdout while the process is still alive. When ignoreTerm is
// true the whole group ignores SIGTERM, forcing the SIGKILL escalation path.
func opencodeCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
if [ -n "$OPENCODE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$OPENCODE_PID_FILE"
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
while true; do
  printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"tick"}}\n'
  sleep 0.1
done
`
}

// TestOpencodeCancellationTerminatesProcessGroupGraceful verifies that
// cancelling a run terminates a SIGTERM-respecting opencode and its whole
// process group, returns an "aborted" result without hanging, and leaves no
// orphaned descendant.
func TestOpencodeCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	runOpencodeCancellationTest(t, opencodeCancelFakeScript(false))
}

// TestOpencodeCancellationEscalatesToSIGKILL verifies the worst case from
// #4533: opencode (and its children) ignore SIGTERM and keep writing to stdout.
// Cancellation must escalate to a group SIGKILL, still return promptly, and
// still reap the whole group — without deadlocking on the stdout scanner or
// closing the pipe under a live writer.
func TestOpencodeCancellationEscalatesToSIGKILL(t *testing.T) {
	opencodeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { opencodeTerminateGraceNanos.Store(0) })
	runOpencodeCancellationTest(t, opencodeCancelFakeScript(true))
}

func runOpencodeCancellationTest(t *testing.T, script string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain streamed messages so processEvents never blocks on a full channel.
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)

	cancel() // user cancels the task

	select {
	case res := <-session.Result:
		if res.Status != "aborted" {
			t.Errorf("status = %q, want aborted", res.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation (possible scanner deadlock or unkilled process)")
	}

	// The leader and the grandchild must both be gone — cancellation reaped the
	// whole group, leaving no orphan spinning.
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

// waitForPids polls pidFile until it contains the space-separated pids the fake
// recorded, then returns them.
func waitForPids(t *testing.T, pidFile string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			fields := strings.Fields(string(raw))
			if len(fields) >= 2 {
				pids := make([]int, 0, len(fields))
				ok := true
				for _, f := range fields {
					n, perr := strconv.Atoi(f)
					if perr != nil || n <= 0 {
						ok = false
						break
					}
					pids = append(pids, n)
				}
				if ok {
					return pids
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fake opencode never recorded its pids in %s", pidFile)
	return nil
}

// waitProcessGone polls until signal 0 to pid reports the process no longer
// exists (ESRCH), failing if it is still alive after the deadline.
func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d still alive after cancellation — orphaned/leaked", pid)
}
