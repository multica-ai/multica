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
// the correct answer and then never exits. Measured on a host running openclaw
// 2026.5.27:
//
//	openclaw --version    258ms  exits cleanly
//	openclaw config file    60s  correct path printed, then killed by the caller
//	openclaw agents list    60s  correct list printed, then killed by the caller
//
// Waiting for exit turned working commands into task-fatal errors, so a host
// with that CLI build could not prepare a single task's execution environment.

const quietTestJSON = `{"agents":[{"id":"main"}]}`

// writePrintThenHangCLI creates a CLI that prints payload and then hangs
// forever, optionally forking a helper that also holds the pipes.
func writePrintThenHangCLI(t *testing.T, payload, helperPidFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cli")
	fork := ""
	if helperPidFile != "" {
		// $!, not $$ from inside a subshell: see writeForkingCLIOutput. Naming
		// the direct child here would make the reaping assertion vacuous.
		fork = `sleep 300 &
echo $! > "` + helperPidFile + `"
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

func writeCLI(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return script
}

// TestRunCollectQuietReturnsOnceOutputGoesIdle is the core contract: a complete
// answer plus silence is enough, and returning must not depend on the deadline.
func TestRunCollectQuietReturnsOnceOutputGoesIdle(t *testing.T) {
	cli := writePrintThenHangCLI(t, quietTestJSON, "")

	// A long ctx on purpose.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	out, _, quiet, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, cli)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunCollectQuiet returned an error: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != quietTestJSON {
		t.Errorf("stdout = %q, want the printed answer", got)
	}
	if !quiet {
		t.Error("quiet = false, want true — callers should be able to log the " +
			"CLI's failure to exit without failing on it")
	}
	// A loose bound on purpose: it only has to sit far below the 60s ctx and the
	// stub's 300s sleep, either of which a broken mechanism would take. Tying it
	// to the 400ms grace would make it a CI flake, since spawning the stub costs
	// the same order of magnitude.
	if elapsed > 10*time.Second {
		t.Errorf("took %v — it waited for an exit that never comes instead of "+
			"accepting the flushed output", elapsed)
	}
}

// TestRunCollectQuietDoesNotSalvagePartialOutputAtDeadline is the regression for
// the review finding on #6275. An earlier revision returned success from the
// deadline branch whenever stdout was non-empty, so a CLI still streaming when
// the deadline arrived had its truncated output reported as success — measured
// 9 runs in 10. The deadline is never success.
func TestRunCollectQuietDoesNotSalvagePartialOutputAtDeadline(t *testing.T) {
	// Emits a JSON document forever: always non-empty, never complete.
	// 120ms between writes, not 30ms: the stub forks a `sleep` per iteration and
	// this package also holds timing-tight tests, so a tighter loop steals CPU
	// from them. Still far below the 250ms deadline, so the document is always
	// caught mid-write.
	cli := writeCLI(t, "#!/bin/sh\nprintf '{\"agents\":['\n"+
		"while :; do printf '{\"id\":\"a\"},'; sleep 0.12; done\n")

	// Four runs is decisive: the reverted implementation salvaged 9 of 10.
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, cli)
		cancel()

		if err == nil {
			t.Fatalf("run %d: partial output reported as success (%d bytes: %q) — "+
				"an interrupted response must never be handed to a caller as a "+
				"finished one", i, len(out), truncateForLog(out))
		}
		// The bytes are still returned so a caller with its own rule can look,
		// but they arrive with the error attached.
		if JSONOutputComplete(out) {
			t.Fatalf("run %d: the stub is not supposed to be able to emit a "+
				"complete document; test is not exercising the intended path", i)
		}
	}
}

// TestRunCollectQuietWaitsForTheAnswerAfterAPrompt is the second regression the
// review asked for. Output arriving and then going quiet is NOT sufficient: the
// CLI may be pausing between a banner and the real answer. `openclaw config file`
// does exactly this — it prints Doctor warning UI first (MUL-3136) — and cutting
// off there would return the banner as the answer.
func TestRunCollectQuietWaitsForTheAnswerAfterAPrompt(t *testing.T) {
	// Banner, a pause well past the idle grace, then the real answer, then hang.
	cli := writeCLI(t, "#!/bin/sh\n"+
		"echo 'warning: run openclaw doctor to inspect config'\n"+
		"sleep 1\n"+
		"printf '%s\\n' '"+quietTestJSON+"'\n"+
		"sleep 300\n")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The rule mirrors what execenv uses for `config file`: the answer is the
	// last non-empty line, and it has to look like an answer.
	lastLineIsAnswer := func(out []byte) bool {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return strings.TrimSpace(lines[len(lines)-1]) == quietTestJSON
	}

	out, _, quiet, err := RunCollectQuiet(ctx, nil, 0, lastLineIsAnswer, cli)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if !quiet {
		t.Error("quiet = false, want true — the stub never exits")
	}
	if !strings.Contains(string(out), quietTestJSON) {
		t.Errorf("stdout = %q — returned before the real answer arrived, so the "+
			"banner would have been mistaken for it", out)
	}
}

// TestRunCollectQuietReportsLateNonZeroExit is the third regression the review
// asked for. A CLI that prints a complete answer and then fails must be reported
// as the failure it is, as long as it fails within the idle grace — which is
// exactly what the grace is for. The stub exits at 150ms against a 400ms grace.
func TestRunCollectQuietReportsLateNonZeroExit(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\nprintf '%s\\n' '"+quietTestJSON+"'\n"+
		"echo 'openclaw doctor found a problem' >&2\nsleep 0.15\nexit 5\n")

	out, stderr, quiet, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli)
	if err == nil {
		t.Fatalf("a complete answer followed by exit 5 was reported as success "+
			"(out=%q) — the answer does not excuse the failure", truncateForLog(out))
	}
	if quiet {
		t.Error("quiet = true, want false — this returned through the exit path")
	}
	if !strings.Contains(stderr, "openclaw doctor") {
		t.Errorf("stderr = %q, lost the CLI's diagnostics", stderr)
	}
}

// TestRunCollectQuietWithoutCompletenessRuleWaitsForExit pins the conservative
// default: with no rule there is nothing to judge the output by, so the early
// return is disabled entirely rather than guessed at.
//
// Synchronized on a marker the CLI writes after its answer rather than on a
// deadline. The two assertions want opposite budgets — "returned an error" wants
// a short one, "the captured bytes survived" wants the spawn to have finished —
// and any single number that serves both is a flake waiting for a loaded host.
// Waiting for the marker also makes the check stronger than a timeout could: it
// proves the runner was still blocked at a moment when the answer was fully
// available to it.
func TestRunCollectQuietWithoutCompletenessRuleWaitsForExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "answer-delivered")
	cli := writeCLI(t, "#!/bin/sh\n"+
		`printf '%s\n' '`+quietTestJSON+"'\n"+
		"touch '"+marker+"'\n"+
		"# The defining behaviour: answer delivered, process refuses to exit.\n"+
		"sleep 300\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type collected struct {
		out []byte
		err error
	}
	done := make(chan collected, 1)
	go func() {
		out, _, _, err := RunCollectQuiet(ctx, nil, 0, nil, cli)
		done <- collected{out: out, err: err}
	}()

	waitForFile(t, marker, 30*time.Second)

	// Well past the point where a rule-driven early return would have fired.
	select {
	case got := <-done:
		t.Fatalf("returned while the process was still running (err=%v, out=%q) — "+
			"without a rule the runner must not decide the answer is finished",
			got.err, truncateForLog(got.out))
	case <-time.After(3 * DefaultQuietIdleGrace):
	}

	cancel()
	got := <-done
	// Not a context sentinel: cancelling reaps the tree, and both entry points
	// deliberately prefer the process error once they have one, because
	// "signal: killed" is the more specific account of what happened. The
	// invariant under test is only that success was never reported.
	if got.err == nil {
		t.Errorf("nil completeness rule still returned success (out=%q)", truncateForLog(got.out))
	}
	// The output is still handed back for a caller that wants to inspect it.
	if !strings.Contains(string(got.out), quietTestJSON) {
		t.Errorf("stdout = %q, want the captured bytes to survive the error", got.out)
	}
}

// TestRunCollectQuietPrefersCleanExit pins that a well-behaved CLI is not
// mislabelled as misbehaving: quiet must be false, which is also what proves it
// returned through the exit path rather than the idle shortcut.
func TestRunCollectQuietPrefersCleanExit(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\nprintf '%s\\n' '"+quietTestJSON+"'\nexit 0\n")

	out, _, quiet, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli)
	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if strings.TrimSpace(string(out)) != quietTestJSON {
		t.Errorf("stdout = %q, want the printed answer", out)
	}
	if quiet {
		t.Error("quiet = true for a CLI that exited cleanly — the flag must " +
			"distinguish real misbehaviour from normal operation, and a clean " +
			"exit must not be reported through the idle path")
	}
}

// TestRunCollectQuietPropagatesExitFailure pins that "output is enough" never
// becomes "everything is fine": a genuinely broken CLI must still fail, with its
// stderr intact for openclawShimDiagnostic and the daemon log.
func TestRunCollectQuietPropagatesExitFailure(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\necho 'run openclaw doctor' >&2\nexit 4\n")

	_, stderr, _, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli)
	if err == nil {
		t.Fatal("expected an error for exit status 4 — a genuinely broken CLI " +
			"must not be silently treated as success")
	}
	if !strings.Contains(stderr, "openclaw doctor") {
		t.Errorf("stderr = %q, lost the CLI's diagnostics", stderr)
	}
}

// TestRunCollectQuietReapsHelperOnIdleReturn pins that the idle shortcut still
// cleans up: this path runs per task, so a helper left behind each time is how a
// host accumulates orphan `openclaw-config` processes.
func TestRunCollectQuietReapsHelperOnIdleReturn(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	cli := writePrintThenHangCLI(t, quietTestJSON, pidFile)

	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, JSONOutputComplete, cli); err != nil {
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

// TestRunCollectQuietWithNoOutputHonorsContext pins that the idle shortcut cannot
// mask a CLI that produces nothing: with no output there is nothing to judge, so
// the deadline must still govern and the call must still fail.
func TestRunCollectQuietWithNoOutputHonorsContext(t *testing.T) {
	cli := writeCLI(t, "#!/bin/sh\nsleep 300\n")

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, cli)
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

func truncateForLog(b []byte) string {
	if len(b) > 80 {
		return string(b[:80]) + "..."
	}
	return string(b)
}
