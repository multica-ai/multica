//go:build !windows

package agent

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

// Reproduces the second OpenClaw failure mode behind MUL-5467: the CLI prints
// the correct answer and then never exits. Measured on the host with openclaw
// 2026.5.27:
//
//	openclaw --version    258ms  exits cleanly
//	openclaw config file    60s  correct path printed, then killed by the caller
//	openclaw agents list    60s  correct list printed, then killed by the caller
//
// Waiting for exit turned working commands into task-fatal errors
// ("context deadline exceeded (process: signal: killed)"), so a host with this
// CLI build could not prepare a single task's execution environment.

// writePrintThenHangCLI creates a CLI that prints payload and then hangs
// forever, optionally forking a helper that also holds the pipes.
func writePrintThenHangCLI(t *testing.T, payload, helperPidFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cli")
	fork := ""
	if helperPidFile != "" {
		fork = `( echo $$ > "` + helperPidFile + `"; sleep 300 ) &
while [ ! -s "` + helperPidFile + `" ]; do sleep 0.01; done
`
	}
	body := "#!/bin/sh\n" + fork + `printf '%s\n' '` + payload + `'
# The defining behaviour: answer delivered, process refuses to exit.
sleep 300
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return script
}

// TestRunCollectQuietReturnsOnceOutputGoesIdle is the core contract: flushed
// output plus silence is enough, and returning must not depend on the deadline.
func TestRunCollectQuietReturnsOnceOutputGoesIdle(t *testing.T) {
	cli := writePrintThenHangCLI(t, "/root/.openclaw/openclaw.json", "")

	// A long ctx on purpose.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, _, quiet, err := RunCollectQuiet(ctx, nil, 0, cli)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunCollectQuiet returned an error: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "/root/.openclaw/openclaw.json" {
		t.Errorf("stdout = %q, want the printed path", got)
	}
	if !quiet {
		t.Error("quiet = false, want true — callers should be able to log the " +
			"CLI's failure to exit without failing on it")
	}
	// Loose on purpose: the bound only has to sit far below the 60s ctx and the
	// stub's 300s sleep, either of which a broken mechanism would take. Tying it
	// to the 400ms grace would make it a CI flake, since spawning the stub costs
	// the same order of magnitude.
	if elapsed > 10*time.Second {
		t.Errorf("took %v — it waited for an exit that never comes instead of "+
			"accepting the flushed output", elapsed)
	}
}

// TestRunCollectQuietPrefersCleanExit pins that a well-behaved CLI is not
// mislabelled as misbehaving: quiet must be false, which is also what proves it
// returned through the exit path rather than the idle shortcut. (Asserting on
// elapsed time instead would be unreliable — spawning the script is itself of
// the same order as the idle grace.)
func TestRunCollectQuietPrefersCleanExit(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "fast-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\necho ok\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write cli: %v", err)
	}

	out, _, quiet, err := RunCollectQuiet(context.Background(), nil, 0, cli)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("stdout = %q, want ok", out)
	}
	if quiet {
		t.Error("quiet = true for a CLI that exited cleanly — the flag must " +
			"distinguish real misbehaviour from normal operation, and a clean " +
			"exit must not be reported through the idle path")
	}
}

// TestRunCollectQuietPropagatesExitFailure pins that "output is enough" never
// becomes "everything is fine": a genuinely broken CLI must still fail, with
// its stderr intact for openclawShimDiagnostic and the daemon log.
func TestRunCollectQuietPropagatesExitFailure(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "failing-cli")
	body := "#!/bin/sh\necho 'run openclaw doctor' >&2\nexit 4\n"
	if err := os.WriteFile(cli, []byte(body), 0o755); err != nil {
		t.Fatalf("write cli: %v", err)
	}

	_, stderr, _, err := RunCollectQuiet(context.Background(), nil, 0, cli)
	if err == nil {
		t.Fatal("expected an error for exit status 4 — a genuinely broken CLI " +
			"must not be silently treated as success")
	}
	if !strings.Contains(stderr, "openclaw doctor") {
		t.Errorf("stderr = %q, lost the CLI's diagnostics", stderr)
	}
}

// TestRunCollectQuietReapsHelperOnIdleReturn pins that the idle shortcut still
// cleans up: this path runs per task, so a helper left behind each time is how
// a host accumulates orphan `openclaw-config` processes.
func TestRunCollectQuietReapsHelperOnIdleReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writePrintThenHangCLI(t, "{}", pidFile)

	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, cli); err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}

	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("helper pid file unreadable: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("bad pid %q: %v", data, convErr)
	}

	if !waitForProcessGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("helper pid %d survived the idle-path return — the orphan "+
			"leak is back on this code path", pid)
	}
}

// TestRunCollectQuietWithNoOutputHonorsContext pins that the idle shortcut
// cannot mask a CLI that produces nothing: with no output there is nothing to
// salvage, so the deadline must still govern and the call must still fail.
func TestRunCollectQuietWithNoOutputHonorsContext(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "silent-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nsleep 300\n"), 0o755); err != nil {
		t.Fatalf("write cli: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, _, _, err := RunCollectQuiet(ctx, nil, 0, cli)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected an error when the CLI produced no output at all")
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("stdout = %q, want empty", out)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %v — the context deadline was not honored", elapsed)
	}
}
