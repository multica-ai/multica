//go:build windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Windows contract coverage for the collector, requested in review of #6275.
//
// Windows is where the process-group half of MUL-5467 does not apply:
// configureProcessGroup is a no-op and signalProcessGroup can only kill the
// direct child, so a descendant holding the pipe cannot be reaped. What must
// still hold there — and what these tests pin — is the local half:
//
//   - the call returns rather than parking on a CLI that will not exit;
//   - the output captured before that is complete and correct;
//   - no goroutine started by startCollector outlives the call, so a daemon
//     invoking these helpers on a timer cannot accumulate parked goroutines.
//
// Owning descendants on Windows needs a Job Object and is deliberately out of
// scope for this change; these tests are the contract for what is in scope.

const windowsCollectJSON = `{"agents":[{"id":"main"}]}`

// writeWindowsPrintThenHangShim creates a batch shim that prints payload and then
// stays alive, mirroring `openclaw config file` on the affected build. `ping` is
// used as the sleep because it is present on every Windows image and its own
// output is redirected away so it cannot pollute the captured stdout.
func writeWindowsPrintThenHangShim(t *testing.T, payload string) string {
	t.Helper()
	shim := filepath.Join(t.TempDir(), "fake-cli.cmd")
	body := "@ECHO off\r\n" +
		"ECHO " + payload + "\r\n" +
		"ping -n 20 127.0.0.1 >NUL\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return shim
}

func windowsAssertNoGoroutineGrowth(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := runtime.NumGoroutine()
		if got <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutines: %d before, %d after — the collector must join "+
				"its reader and wait goroutines before returning, including on "+
				"Windows where there is no process group to signal", before, got)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestWindowsRunCollectQuietCutsShortAndLeavesNoGoroutines is the main Windows
// contract: a CLI that prints a complete answer and then refuses to exit must
// still yield that answer promptly, and must not leave anything behind.
func TestWindowsRunCollectQuietCutsShortAndLeavesNoGoroutines(t *testing.T) {
	shim := writeWindowsPrintThenHangShim(t, windowsCollectJSON)

	// A long ctx on purpose: returning must come from the completeness rule
	// plus the idle grace, not from the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, shim); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	start := time.Now()
	out, _, quiet, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, shim)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunCollectQuiet: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != windowsCollectJSON {
		t.Errorf("stdout = %q, want the printed answer", got)
	}
	if !quiet {
		t.Error("quiet = false, want true — the shim never exits")
	}
	// Loose bound: only has to sit far below the 90s ctx and the shim's own 60s
	// ping, either of which a broken mechanism would take.
	if elapsed > 30*time.Second {
		t.Errorf("took %v — waited for an exit that never comes", elapsed)
	}
	windowsAssertNoGoroutineGrowth(t, before)
}

// TestWindowsRunCollectQuietDoesNotSalvagePartialOutput pins that the deadline is
// not success on Windows either: a shim still streaming when the deadline lands
// must not have its truncated output reported as a finished answer.
func TestWindowsRunCollectQuietDoesNotSalvagePartialOutput(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "streaming.cmd")
	// Plain ECHO rather than `<NUL SET /P` for the no-newline trick: the quoting
	// rules around SET /P are subtle and this file cannot be executed outside a
	// real Windows runner, so a fragile shim would fail in CI for reasons that
	// have nothing to do with the code under test. Newlines between the fragments
	// are irrelevant here — the buffer is never valid JSON either way, which is
	// the whole point.
	body := "@ECHO off\r\n" +
		"ECHO {\"agents\":[\r\n" +
		":loop\r\n" +
		"ECHO {\"id\":\"a\"},\r\n" +
		"ping -n 2 127.0.0.1 >NUL\r\n" +
		"GOTO loop\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, _, _, err := RunCollectQuiet(ctx, nil, 0, JSONOutputComplete, shim)
	if err == nil {
		t.Fatalf("partial output reported as success (%d bytes) — an interrupted "+
			"response must never be handed to a caller as a finished one", len(out))
	}
}

// TestWindowsRunCollectReturnsAndLeavesNoGoroutines pins the wait-for-exit helper
// on Windows: a normal command is captured correctly and joins everything.
func TestWindowsRunCollectReturnsAndLeavesNoGoroutines(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "quick.cmd")
	body := "@ECHO off\r\nECHO fake-cli 1.2.3\r\nEXIT /B 0\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o644); err != nil {
		t.Fatalf("write shim: %v", err)
	}

	if _, _, err := RunCollect(context.Background(), nil, shim); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	out, _, err := RunCollect(context.Background(), nil, shim)
	if err != nil {
		t.Fatalf("RunCollect: %v", err)
	}
	if !strings.Contains(string(out), "fake-cli 1.2.3") {
		t.Errorf("stdout = %q, lost the output", out)
	}
	windowsAssertNoGoroutineGrowth(t, before)
}
