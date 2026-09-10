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
	"sync/atomic"
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
	return writeForkingCLIOutput(t, pidFile, "fake-cli 1.2.3")
}

func writeForkingCLIOutput(t *testing.T, pidFile, output string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cli")
	body := `#!/bin/sh
# The helper keeps the inherited stdout/stderr open — the exact shape that
# makes cmd.Output() wait for a process it never spawned.
sleep 300 &
# $!, not $$ from inside a subshell: POSIX keeps $$ at the invoking shell's pid
# even inside ( ), so recording it would name the direct child — which cmd.Wait
# reaps regardless of whether the tree kill works, making every assertion built
# on this file vacuous.
echo $! > "` + pidFile + `"
echo "` + output + `"
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return script
}

// TestCheckOpenclawVersionReapsPipeHoldingGrandchild covers the task-start
// version gate, which is separate from the daemon registration probe. It must
// use RunCollect too: Execute calls this synchronously before it returns a
// Session, so a pipe-holding descendant here would bypass both the provider
// timeout and the daemon's inactivity watchdog.
func TestCheckOpenclawVersionReapsPipeHoldingGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLIOutput(t, pidFile, "OpenClaw 2026.7.1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checkOpenclawVersion(ctx, Command{Path: cli}); err != nil {
		t.Fatalf("checkOpenclawVersion: %v", err)
	}

	pid := waitForPidFile(t, pidFile)
	if !waitForProcessGone(pid, 2*time.Second) {
		t.Errorf("openclaw version helper pid %d still alive after version check", pid)
	}
}

// TestCheckOpenclawVersionAcceptsStderrOnlyVersion — the gate replaced
// combinedOutputOwned, so stderr has to stay in the parse. A build that prints
// its banner there would otherwise fail the minimum-version check with an empty
// output, which skips the runtime entirely.
func TestCheckOpenclawVersionAcceptsStderrOnlyVersion(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\necho 'OpenClaw 2026.7.1' >&2\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checkOpenclawVersion(ctx, Command{Path: cli}); err != nil {
		t.Fatalf("checkOpenclawVersion with the version on stderr: %v", err)
	}
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

// waitForFile polls until path exists, so a test can synchronize on something
// the fake CLI did rather than on how long it took to get there.
func waitForFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake CLI never created %s within %v", path, within)
}

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
// completes even though a helper still holds stdout. With any buffer-based form —
// cmd.Output(), or launch.go's outputOwned — this blocks until the helper's
// `sleep 300` finishes, or until probeWaitDelay converts it into
// exec.ErrWaitDelay.
func TestRunCollectReturnsDespitePipeHoldingGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	start := time.Now()
	out, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli)
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

	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli); err != nil {
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

	_, stderr, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli)
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

// TestRunCollectRetriesTheTreeKill pins the retry, which is not decoration: a
// single pass loses a descendant whose fork completes between the kill's
// enumeration of the process group and the signal's delivery. Measured 3 misses
// in 10 runs of the forking stub, each leaving a `sleep 300` reparented to init
// and the call stalled until the settle grace expired.
//
// The race cannot be forced from a stub — it lives inside one syscall — so the
// missed pass is injected here instead. Same var-for-tests pattern as
// detectVersionTimeout and openclawExec.
func TestRunCollectRetriesTheTreeKill(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	original := reapKill
	var passes int32
	reapKill = func(cmd *exec.Cmd) {
		// The first pass misses exactly the way the enumeration race does: the
		// signal is issued and reaches nothing.
		if atomic.AddInt32(&passes, 1) > 1 {
			original(cmd)
		}
	}
	t.Cleanup(func() { reapKill = original })

	out, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli)
	if err != nil {
		t.Fatalf("RunCollect returned an error: %v", err)
	}
	if !strings.Contains(string(out), "fake-cli 1.2.3") {
		t.Errorf("stdout did not survive: %q", out)
	}

	pid := waitForPidFile(t, pidFile)
	if !waitForProcessGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("helper (pid %d) survived a missed first kill — one pass is "+
			"all the tree gets, so a descendant that was mid-fork stays an orphan", pid)
	}
	if got := atomic.LoadInt32(&passes); got < 2 {
		t.Errorf("kill passes = %d, want at least 2 — the retry never ran, so "+
			"this test passed for the wrong reason", got)
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

// TestRunCollectCmdHonorsContextWithoutACommandContext — RunCollectCmd takes a
// command the caller built, so "the ctx is enforced" must not quietly depend on
// that caller having used exec.CommandContext. A plain exec.Command with a
// hanging CLI is the shape that would otherwise block for as long as the CLI
// felt like running, with the ctx argument doing nothing at all.
func TestRunCollectCmdHonorsContextWithoutACommandContext(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\nsleep 300\n")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _, err := RunCollectQuietCmd(ctx, exec.Command(cli), nil, 0, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected an error once the context expired")
	}
	if elapsed > 10*time.Second {
		t.Errorf("took %v — the context did not bound a command built without one", elapsed)
	}
}

// TestDetectCLIVersionReapsForkedHelper covers the real caller: version
// detection runs inside the daemon's blocking preflight for every registered
// provider, and it was one of the paths leaking `openclaw-config`.
func TestDetectCLIVersionReapsForkedHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	version, err := detectCLIVersion(context.Background(), Command{Path: cli})
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
// goroutine could sit in cmd.Wait indefinitely when no owned process-tree
// boundary released the inherited pipe.
//
// Scope: this proves convergence for a tree the kill reaches, which is the case
// the fix is about. It does not — and cannot — prove anything about a process
// the OS refuses to terminate; finish() logs that case and still returns the
// answer, rather than reporting it as a failed call.
func TestRunCollectLeavesNoGoroutines(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writeForkingCLI(t, pidFile)

	// Warm up so lazily-created runtime goroutines exist before the baseline.
	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	for i := 0; i < 2; i++ {
		if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, cli); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertNoGoroutineGrowth(t, before)
}

// TestRunCollectQuietLeavesNoGoroutines pins the same contract on the path that
// returns while the CLI is still alive — the one that has to kill the child
// itself rather than waiting for it. Same scope caveat as above: a successful
// reap is what is being verified.
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
