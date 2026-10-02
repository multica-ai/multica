//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// primeDescendantHoldsPipesFakeScript returns a fake `prime-agent` that runs a
// turn to completion and then exits normally on stdin EOF, but leaves behind a
// descendant that inherited its stdio and keeps the named pipes open.
//
// This is the shape prime-agent really produces: its package-manager
// subprocesses are spawned with stdio ["ignore", 2, 2] whenever ACP has taken
// over stdout, so they inherit fd 2 and can outlive the turn that started them.
// The leader exiting is therefore not enough to bring the pipe to EOF.
//
// redirect selects which pipes the descendant keeps: "" holds both, ">/dev/null"
// detaches stdout and holds only stderr, "2>/dev/null" holds only stdout.
func primeDescendantHoldsPipesFakeScript(redirect string) string {
	return "#!/bin/sh\n" +
		"( sleep 300 ) " + redirect + " &\n" +
		"child=$!\n" +
		"if [ -n \"$PRIME_PID_FILE\" ]; then printf '%s %s\\n' \"$$\" \"$child\" > \"$PRIME_PID_FILE\"; fi\n" +
		fakePrimeACPScriptBody()
}

// TestPrimeCompletesWhenDescendantHoldsPipes is the success-path teardown
// guard.
//
// The reader/stderr receives run BEFORE the deferred cmd.Wait(), so
// cmd.WaitDelay cannot backstop them — it only applies once Wait has been
// entered. Waiting on them unboundedly means a turn that already succeeded
// never publishes its Result: the daemon sees no messages, its idle watchdog
// force-stops the task ~30 minutes later, and a successful run is reported as a
// failure. Nothing about cancellation is involved, which is why the existing
// cancellation coverage does not catch it.
//
// Each case asserts the run still reports "completed", returns far inside the
// watchdog window, and leaves no orphan behind.
func TestPrimeCompletesWhenDescendantHoldsPipes(t *testing.T) {
	cases := []struct {
		name     string
		redirect string
	}{
		{"descendant holds stderr", ">/dev/null"},
		{"descendant holds stdout", "2>/dev/null"},
		{"descendant holds both", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: the grace windows are process-wide atomics.
			primeTeardownGraceNanos.Store(int64(300 * time.Millisecond))
			primeGracefulExitGraceNanos.Store(int64(300 * time.Millisecond))
			primeTerminateGraceNanos.Store(int64(300 * time.Millisecond))
			t.Cleanup(func() {
				primeTeardownGraceNanos.Store(0)
				primeGracefulExitGraceNanos.Store(0)
				primeTerminateGraceNanos.Store(0)
			})

			tempDir := t.TempDir()
			pidFile := filepath.Join(tempDir, "pids")
			fakePath := filepath.Join(tempDir, "prime-agent")
			writeTestExecutable(t, fakePath, []byte(primeDescendantHoldsPipesFakeScript(tc.redirect)))

			backend, err := New("prime", Config{
				ExecutablePath: fakePath,
				Logger:         slog.Default(),
				Env:            map[string]string{"PRIME_PID_FILE": pidFile},
			})
			if err != nil {
				t.Fatalf("new prime backend: %v", err)
			}

			session, err := backend.Execute(context.Background(), "hello", ExecOptions{Cwd: tempDir})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()

			pids := waitForPids(t, pidFile)

			select {
			case res := <-session.Result:
				if res.Status != "completed" {
					t.Errorf("status = %q, want %q — the turn succeeded; only the pipe drain was obstructed (error: %q)",
						res.Status, "completed", res.Error)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("Execute never published a Result: a descendant holding an inherited pipe " +
					"parked the success path, which cmd.WaitDelay cannot backstop from there")
			}

			// Forcing the drain must also reap the tree, not just unblock us.
			for _, pid := range pids {
				waitProcessGone(t, pid)
			}
		})
	}
}
