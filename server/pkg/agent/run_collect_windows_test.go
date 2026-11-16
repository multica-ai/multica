//go:build windows

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
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Windows contract coverage for the collector, requested in review of #6275.
//
// Windows has no Unix process groups, so the collector uses a Job Object. These
// tests pin both process-tree ownership and the platform-independent contract:
//
//   - the call returns rather than parking on a CLI that will not exit;
//   - the output captured before that is complete and correct;
//   - no goroutine started by startCollector outlives the call whenever the Job
//     Object kill takes, so a daemon invoking these helpers on a timer cannot
//     accumulate parked goroutines. A process the OS refuses to terminate is out
//     of scope here, as it is on Unix: finish() logs that and still returns the
//     answer.
//
// execenv/isolation_windows_test.go separately pins the outer Job Object around
// the complete task-preparation helper.

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
				"Windows where cleanup uses a Job Object", before, got)
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

	if _, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, shim); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	before := runtime.NumGoroutine()

	out, _, _, err := RunCollectQuiet(context.Background(), nil, 0, nil, shim)
	if err != nil {
		t.Fatalf("RunCollect: %v", err)
	}
	if !strings.Contains(string(out), "fake-cli 1.2.3") {
		t.Errorf("stdout = %q, lost the output", out)
	}
	windowsAssertNoGoroutineGrowth(t, before)
}

func TestWindowsRunCollectKillsDescendant(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "fake_cli.go")
	exePath := filepath.Join(tempDir, "fake_cli.exe")
	pidPath := filepath.Join(tempDir, "descendant.pid")
	const source = `package main
import (
	"fmt"
	"os"
	"os/exec"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "descendant" {
		time.Sleep(time.Minute)
		return
	}
	child := exec.Command(os.Args[0], "descendant")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil { panic(err) }
	if err := os.WriteFile(os.Getenv("DESCENDANT_PID_FILE"), []byte(fmt.Sprint(child.Process.Pid)), 0600); err != nil { panic(err) }
	fmt.Println("fake-cli 1.2.3")
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", exePath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Windows fake CLI: %v: %s", err, output)
	}

	env := append(os.Environ(), "DESCENDANT_PID_FILE="+pidPath)
	out, _, _, err := RunCollectQuiet(context.Background(), env, 0, nil, exePath)
	if err != nil {
		t.Fatalf("RunCollect: %v", err)
	}
	if !strings.Contains(string(out), "fake-cli 1.2.3") {
		t.Fatalf("stdout = %q, lost the direct child's output", out)
	}
	rawPID, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatalf("parse descendant pid: %v", err)
	}
	descendantAlive := true
	t.Cleanup(func() {
		if descendantAlive {
			process, findErr := os.FindProcess(pid)
			if findErr != nil {
				return
			}
			_ = process.Kill()
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		process, openErr := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if errors.Is(openErr, windows.ERROR_INVALID_PARAMETER) {
			descendantAlive = false
			return
		}
		if openErr != nil {
			t.Fatalf("open descendant pid %d: %v", pid, openErr)
		}
		state, waitErr := windows.WaitForSingleObject(process, 0)
		windows.CloseHandle(process)
		if waitErr != nil {
			t.Fatalf("query descendant pid %d: %v", pid, waitErr)
		}
		if waitErr == nil && state == windows.WAIT_OBJECT_0 {
			descendantAlive = false
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant pid %d survived RunCollect", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
