//go:build !windows

package execenv

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Regression tests for the task-critical half of MUL-5467. prepareOpenclawConfig
// shells out to `openclaw config file` / `openclaw config get ...` while
// preparing a task's execution environment, so both OpenClaw misbehaviours land
// on the path between "task claimed" and "agent started":
//
//   - openclaw forks a long-lived `openclaw-config` helper that inherits
//     stdout/stderr. With cmd.Output(), os/exec waits for those pipes to reach
//     EOF, which never comes while the helper lives — and cancelling the context
//     kills the direct child without unblocking that wait, so openclawCLITimeout
//     could not bound the call.
//   - `openclaw config file` and `openclaw agents list` print the correct answer
//     in ~250ms and then do not exit, so waiting for exit turned two working
//     commands into a task-fatal error.

// writeHelperForkingOpenclaw creates a fake openclaw with the first shape: emit
// JSON on stdout, fork a helper that keeps the inherited stdout/stderr open far
// longer than the test would wait, then exit 0.
func writeHelperForkingOpenclaw(t *testing.T, pidFile string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := `#!/bin/sh
( echo $$ > "` + pidFile + `"; sleep 300 ) &
# Make the helper's registration deterministic: without this the parent can
# exit and the group be reaped before the helper runs, leaving the test with
# no pid to assert on.
while [ ! -s "` + pidFile + `" ]; do sleep 0.01; done
echo '{}'
exit 0
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}
	return bin
}

func readHelperPid(t *testing.T, pidFile string) int {
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

func helperGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestExecOpenclawCLIReturnsDespitePipeHoldingHelper is the assertion that
// makes openclawCLITimeout meaningful: the call must come back on the direct
// child's exit, not on the helper's lifetime and not on the deadline.
func TestExecOpenclawCLIReturnsDespitePipeHoldingHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	bin := writeHelperForkingOpenclaw(t, pidFile)

	// A deliberately generous deadline: before the fix this hung past it, so a
	// tight ctx would have hidden the bug behind a plausible-looking timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, err := execOpenclawCLI(ctx, bin, "config", "get", "--json")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	if strings.TrimSpace(out) != "{}" {
		t.Errorf("stdout = %q, want {}", out)
	}
	if elapsed > 15*time.Second {
		t.Errorf("execOpenclawCLI took %v — it waited on the helper instead "+
			"of returning once openclaw itself exited", elapsed)
	}
}

// TestExecOpenclawCLIReapsForkedHelper pins the cleanup half. Task preparation
// runs per task, and this is where the orphan `openclaw-config` processes came
// from. It is also what the reverted cmd.WaitDelay backstop could not do.
func TestExecOpenclawCLIReapsForkedHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	bin := writeHelperForkingOpenclaw(t, pidFile)

	if _, err := execOpenclawCLI(context.Background(), bin, "config", "file"); err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}

	pid := readHelperPid(t, pidFile)
	if !helperGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("forked helper (pid %d) survived execOpenclawCLI — the "+
			"orphan leak is back", pid)
	}
}

// TestExecOpenclawCLIToleratesNonExitingCLI covers the second failure mode.
// Measured on the host, `openclaw config file` printed the path in ~250ms and
// then hung until killed, which reached the user as
//
//	agent_error.process_failure (prepare execution environment: execenv:
//	prepare openclaw config: locate openclaw active config:
//	openclaw config file: context deadline exceeded (process: signal: killed))
//
// while the answer had been on stdout the whole time.
func TestExecOpenclawCLIToleratesNonExitingCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	body := `#!/bin/sh
printf '%s\n' '/root/.openclaw/openclaw.json'
sleep 300
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, err := execOpenclawCLI(ctx, bin, "config", "file")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("execOpenclawCLI: %v", err)
	}
	if strings.TrimSpace(out) != "/root/.openclaw/openclaw.json" {
		t.Errorf("stdout = %q, want the printed path", out)
	}
	// Loose on purpose: only has to sit far below the 60s ctx and the stub's 300s
	// sleep, either of which a broken mechanism would take.
	if elapsed > 10*time.Second {
		t.Errorf("took %v — waited for an exit that never comes", elapsed)
	}
}
