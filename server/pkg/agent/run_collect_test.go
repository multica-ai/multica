//go:build !windows

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests reproduce the shape behind MUL-5467: invoking a CLI that forks a
// long-lived helper which inherits stdout/stderr.
//
// Observed on an OpenClaw host — `openclaw --version` returns promptly but
// leaves an `openclaw-config` helper holding the pipe's write end. With a bare
// cmd.Output() the call never returns (os/exec waits for pipe EOF, and killing
// the direct child on ctx cancellation does not unblock that), and the helper
// is reparented to init. A daemon probing on a timer therefore accumulated one
// parked goroutine and one orphan per cycle.

// writeForkingCLI creates a shell script with that behaviour: print a version
// line, fork a helper that holds the inherited stdout/stderr for far longer
// than any test would wait, then exit 0. The helper records its own pid so the
// test can assert it was reaped.
func writeForkingCLI(t *testing.T, pidFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cli")
	body := `#!/bin/sh
# The helper keeps the inherited stdout/stderr open — the exact shape that
# makes cmd.Output() wait for a process it never spawned.
( echo $$ > "` + pidFile + `"; sleep 300 ) &
# Block until the helper has recorded its pid. Without this the parent can exit
# (and the group be reaped) before the helper ever runs, leaving the test with
# no pid to assert on — the reaping is fast enough for that race to fire.
while [ ! -s "` + pidFile + `" ]; do sleep 0.01; done
echo "fake-cli 1.2.3"
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return script
}

// waitForPidFile waits for the forked helper to record its pid.
func waitForPidFile(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper never wrote its pid to %s", pidFile)
	return 0
}

// processAlive reports whether pid still exists. Signal 0 only performs the
// permission/existence check.
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// waitForProcessGone polls until pid is gone, since SIGKILL delivery and
// reaping are asynchronous.
func waitForProcessGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestRunCollectReturnsDespitePipeHoldingGrandchild pins guarantee #1: the call
// completes even though a helper still holds stdout. With cmd.Output() this
// blocked until the helper's `sleep 300` finished.
func TestRunCollectReturnsDespitePipeHoldingGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	start := time.Now()
	out, _, err := RunCollect(context.Background(), nil, cli)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunCollect returned an error: %v", err)
	}
	if !strings.Contains(string(out), "fake-cli 1.2.3") {
		t.Errorf("stdout did not survive: %q", out)
	}
	// Generous bound: the point is "does not wait for the 300s helper".
	if elapsed > 15*time.Second {
		t.Errorf("RunCollect took %v — it waited on the helper instead of "+
			"returning once the direct child exited", elapsed)
	}
}

// TestRunCollectReapsForkedHelper pins guarantee #2: the helper is killed
// before RunCollect returns, so invoking a CLI on a timer cannot accumulate
// orphans. This is the assertion that would have caught the production leak.
func TestRunCollectReapsForkedHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	if _, _, err := RunCollect(context.Background(), nil, cli); err != nil {
		t.Fatalf("RunCollect returned an error: %v", err)
	}

	pid := waitForPidFile(t, pidFile)
	if !waitForProcessGone(pid, 5*time.Second) {
		// Don't leave a stray `sleep 300` behind if the assertion fails.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("forked helper (pid %d) survived RunCollect — the process "+
			"group was not reaped and the orphan leak is back", pid)
	}
}

// TestRunCollectSurfacesStderrAndExitStatus guards the diagnostic behaviour:
// owning the pipes must not cost us the CLI's stderr or its exit status.
func TestRunCollectSurfacesStderrAndExitStatus(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "failing-cli")
	body := "#!/bin/sh\necho 'boom' >&2\nexit 7\n"
	if err := os.WriteFile(cli, []byte(body), 0o755); err != nil {
		t.Fatalf("write cli: %v", err)
	}

	_, stderr, err := RunCollect(context.Background(), nil, cli)
	if err == nil {
		t.Fatal("expected an error for exit status 7")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %T (%v), want *exec.ExitError — callers such as "+
			"openclawShimDiagnostic type-switch on it", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Errorf("exit code = %d, want 7", exitErr.ExitCode())
	}
	if !strings.Contains(stderr, "boom") {
		t.Errorf("stderr = %q, lost the CLI's diagnostics", stderr)
	}
}

// TestRunCollectRespectsProcessGroupPrecondition pins the invariant the group
// kill depends on: the child must lead its own group. If configureProcessGroup
// ever stopped setting Setpgid, reapProcessTree would silently degrade to
// signalling only the direct child and the orphan leak would return.
func TestRunCollectRespectsProcessGroupPrecondition(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	configureProcessGroup(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("configureProcessGroup did not set Setpgid — reapProcessTree " +
			"can no longer reach descendants")
	}
}

// TestDetectCLIVersionReapsForkedHelper covers the real caller: version
// detection runs inside the daemon's blocking preflight for every registered
// provider, and it was one of the paths leaking `openclaw-config`.
func TestDetectCLIVersionReapsForkedHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	version, err := detectCLIVersion(context.Background(), cli)
	if err != nil {
		t.Fatalf("detectCLIVersion: %v", err)
	}
	if !strings.Contains(version, "1.2.3") {
		t.Errorf("version = %q, want it to contain 1.2.3", version)
	}

	pid := waitForPidFile(t, pidFile)
	if !waitForProcessGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("detectCLIVersion left helper pid %d running", pid)
	}
}

// assertNoGoroutineGrowth pins the contract that finish() joins everything
// startCollector spawned. Polls because goroutine teardown is observed
// asynchronously, but a correct implementation satisfies it on the first look.
func assertNoGoroutineGrowth(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := runtime.NumGoroutine()
		if got <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutines: %d before, %d after — the collector must join "+
				"its reader and wait goroutines before returning, on every exit "+
				"path", before, got)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunCollectLeavesNoGoroutines pins review item 3: the call must not park a
// goroutine on a CLI whose descendant holds the pipe. Previously the wait
// goroutine could sit in cmd.Wait indefinitely, which on Windows (no process
// group to signal) had nothing to release it.
func TestRunCollectLeavesNoGoroutines(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	// Warm up so lazily-created runtime goroutines exist before the baseline.
	if _, _, err := RunCollect(context.Background(), nil, cli); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	for i := 0; i < 2; i++ {
		if _, _, err := RunCollect(context.Background(), nil, cli); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertNoGoroutineGrowth(t, before)
}

// TestRunCollectQuietLeavesNoGoroutines pins the same contract on the path that
// returns while the CLI is still alive — the one that has to kill the child
// itself rather than waiting for it.
func TestRunCollectQuietLeavesNoGoroutines(t *testing.T) {
	cli := writePrintThenHangCLI(t, quietTestJSON, "")

	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	for i := 0; i < 2; i++ {
		if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertNoGoroutineGrowth(t, before)
}

// TestOutputBufferAbsorbStopsWhenReadEndClosed pins the mechanism finish() relies
// on when a descendant will not release the pipe: closing the read end has to
// terminate the in-flight Read, otherwise the join would hang and the previous
// revision's alternative — returning while io.Copy was still appending — is a
// data race on the buffer the caller is about to read.
func TestOutputBufferAbsorbStopsWhenReadEndClosed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer w.Close()

	var buf outputBuffer
	done := make(chan struct{})
	go func() { defer close(done); _ = buf.absorb(r) }()

	if _, err := w.WriteString("partial"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Let the reader pick it up, then pull the rug out while it is blocked in
	// Read with the write end still open (i.e. no EOF is coming).
	time.Sleep(50 * time.Millisecond)
	r.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("absorb did not return after the read end was closed — finish() " +
			"would block forever waiting to join it")
	}
	if got := string(buf.snapshot()); got != "partial" {
		t.Errorf("snapshot = %q, want the bytes that arrived before the close", got)
	}
}

// TestOutputBufferPublishesBytesAndTimestampTogether pins review item 2. The
// buffer and the last-write timestamp are updated in one critical section, so a
// reader can never see new bytes carrying a stale timestamp and conclude the
// stream has gone quiet while it is in fact producing.
//
// Run under -race this also covers the concurrent access itself.
func TestOutputBufferPublishesBytesAndTimestampTogether(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	var buf outputBuffer
	absorbed := make(chan struct{})
	go func() { defer close(absorbed); _ = buf.absorb(r) }()

	writing := make(chan struct{})
	go func() {
		defer close(writing)
		for i := 0; i < 60; i++ {
			_, _ = w.WriteString("x")
			time.Sleep(3 * time.Millisecond)
		}
		w.Close()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-writing:
			// Writer finished; idleness is legitimate from here on.
			<-absorbed
			return
		default:
		}
		n := len(buf.snapshot())
		idle, produced := buf.idleFor()
		// While the writer is active at 1ms intervals, any observed byte must
		// come with a recent timestamp. A generous bound keeps this from being a
		// scheduling flake while still failing if the timestamp lagged the bytes
		// by a whole grace period.
		if produced && n > 0 && idle > 300*time.Millisecond {
			t.Fatalf("saw %d bytes with idle=%v while the writer was still "+
				"producing — a stale timestamp lets RunCollectQuiet truncate an "+
				"answer mid-write", n, idle)
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-writing
	<-absorbed
}
