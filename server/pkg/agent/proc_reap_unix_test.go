//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// TestNormalExitReapsDescendantProcessGroup is the #8153 regression, exercised
// through a real backend's normal-exit path.
//
// #7531 put the runtime CLI in its own process group and signals the whole group
// on cancellation/timeout. A backend that neuters cmd.Cancel to drive its own
// shutdown (claude, deveco, opencode, dsh) never signals the group when the
// leader exits on its own, and releaseProcessGroup is a Unix no-op — so a
// descendant that inherited the pgid but does not hold the CLI's stdout kept
// running after the task completed successfully (observed: an orphaned headless
// Chrome burning cores for hours, reparented to PID 1 in the now-leaderless
// group). The fix reaps the group right after cmd.Wait, mirroring runOwned.
//
// The fake opencode spawns such a descendant — it stays in the group but
// redirects its own stdio away from the inherited pipes, so the leader reaches
// EOF and exits 0 normally while it keeps running — then completes. After the
// run returns, the descendant must be gone. Fails before the fix (it survives
// its full sleep); passes once the normal-exit path reaps the group.
//
// Environment note (from the issue): in a bare container with no init to reap
// the SIGKILLed descendant it lingers as a zombie and kill(pid, 0) still
// succeeds; run with `docker run --init`. On a normal host init reaps it.
func TestNormalExitReapsDescendantProcessGroup(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "opencode")

	script := "#!/bin/sh\n" +
		"# Descendant stays in the process group but does not hold the leader's\n" +
		"# stdout, so the leader can reach EOF and exit normally while it runs on.\n" +
		"( sleep 300 >/dev/null 2>&1 </dev/null ) &\n" +
		"child=$!\n" +
		`printf '%s %s\n' "$$" "$child" > "$OPENCODE_PID_FILE"` + "\n" +
		`printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'` + "\n" +
		`printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"done"}}\n'` + "\n" +
		"exit 0\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"OPENCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile) // [leaderPID, descendantPID]

	// The run must complete on its own (no cancellation) so the normal-exit path
	// runs and reaps the group.
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after the fake exited normally")
	}

	waitProcessGone(t, pids[1])
}
