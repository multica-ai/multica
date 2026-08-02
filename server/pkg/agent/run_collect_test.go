//go:build !windows

package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
