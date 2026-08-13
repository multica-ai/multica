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

// zcodeCancelFakeScript returns a POSIX-sh script that impersonates a
// long-running streaming-enabled `zcode`: it advertises --stream-json in
// --help (so the capability gate takes the streaming path), spawns a
// background grandchild, records both its own (process-group-leader) pid and
// the grandchild pid, then streams qwen-schema NDJSON on stdout forever. This
// is the shape that orphans a descendant when only the direct child is killed
// on cancellation. When ignoreTerm is true the whole group ignores SIGTERM,
// forcing the SIGKILL escalation path.
func zcodeCancelFakeScript(ignoreTerm bool) string {
	trap := "trap 'exit 0' TERM\n"
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return "#!/bin/sh\n" + trap +
		`# Capability probe must take the streaming path.
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
  printf 'Usage: zcode --prompt <text> [--stream-json]\n'
  exit 0
fi
# Background grandchild so the test can assert the *whole* group is
# terminated on cancellation, not just the direct child.
( sleep 300 ) &
child=$!
if [ -n "$ZCODE_PID_FILE" ]; then
  printf '%s %s\n' "$$" "$child" > "$ZCODE_PID_FILE"
fi
printf '{"type":"system","subtype":"init","session_id":"ses_fake"}\n'
while true; do
  printf '{"type":"assistant","session_id":"ses_fake","message":{"content":[{"type":"text","text":"tick"}]}}\n'
  sleep 0.1
done
`
}

// TestZcodeCancellationTerminatesProcessGroupGraceful verifies that cancelling
// a run terminates a SIGTERM-respecting zcode and its whole process group
// (including the grandchild), returns without hanging, and leaves no orphaned
// descendant.
func TestZcodeCancellationTerminatesProcessGroupGraceful(t *testing.T) {
	runZcodeCancellationTest(t, zcodeCancelFakeScript(false))
}

// TestZcodeCancellationEscalatesToSIGKILL verifies the worst case: zcode (and
// its children) ignore SIGTERM and keep writing to stdout. Cancellation must
// escalate to a group SIGKILL, still return promptly, and still reap the whole
// group — without deadlocking on the stdout scanner or closing the pipe under
// a live writer.
func TestZcodeCancellationEscalatesToSIGKILL(t *testing.T) {
	zcodeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
	t.Cleanup(func() { zcodeTerminateGraceNanos.Store(0) })
	runZcodeCancellationTest(t, zcodeCancelFakeScript(true))
}

func runZcodeCancellationTest(t *testing.T, script string) {
	t.Helper()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "zcode")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("zcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"ZCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Cwd: tempDir, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain streamed messages so processEvents never blocks on a full channel.
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForZcodePids(t, pidFile)

	cancel() // user cancels the task

	select {
	case <-session.Result:
		// Result delivered — process tree torn down.
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return after cancellation (possible scanner deadlock or unkilled process)")
	}

	// The leader and the grandchild must both be gone — cancellation reaped the
	// whole group, leaving no orphan spinning.
	for _, pid := range pids {
		waitZcodeProcessGone(t, pid)
	}
}

// waitForZcodePids polls pidFile until it contains the space-separated pids the
// fake recorded, then returns them.
func waitForZcodePids(t *testing.T, pidFile string) []int {
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
	t.Fatalf("fake zcode never recorded its pids in %s", pidFile)
	return nil
}

// waitZcodeProcessGone polls until signal 0 to pid reports the process no longer
// exists (ESRCH), failing if it is still alive after the deadline.
func waitZcodeProcessGone(t *testing.T, pid int) {
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
