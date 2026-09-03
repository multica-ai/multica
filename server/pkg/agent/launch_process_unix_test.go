//go:build unix

package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRuntimeCommandsGetTheirOwnProcessGroup pins the default that GH #7522
// showed was missing. Before, each backend had to remember to ask for a process
// group and most did not, so a group-wide signal could not reach their CLI at
// all. The group now comes from the one place a runtime process is built, which
// means a backend cannot be launched without it.
func TestRuntimeCommandsGetTheirOwnProcessGroup(t *testing.T) {
	t.Parallel()

	cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", "true")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("a runtime command must be built with Setpgid so cancellation can signal the whole tree")
	}
}

// TestRuntimeCommandCancellationKillsDescendants is the behaviour the default
// buys, on a command with no backend-specific cancellation logic at all: the
// tool subprocesses an agent spawned must die with it.
//
// os/exec's own Cancel kills the leader alone, which is what left a cancelled
// agent's descendants running. The fake here spawns a grandchild that outlives
// its parent, so killing the leader is not enough to pass.
func TestRuntimeCommandCancellationKillsDescendants(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "runtime")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		`( sleep 300 ) </dev/null >/dev/null 2>&1 &
printf '%s %s\n' "$$" "$!" > "$1"
sleep 300
`))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewCommand(fakePath, nil).exec(ctx, pidFile)
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer releaseProcessGroup(cmd)

	pids := waitForPids(t, pidFile)
	cancel()

	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the cancelled process was never reaped")
	}
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

// TestProbeOutputCancellationKillsDescendants is the probe half of GH #7522.
//
// outputOwned exists because cmd.Output() calls Start itself and so can never
// reach the ownership boundary. detectCLIVersion's own comment describes the
// shape this reproduces: a CLI shim that leaves a grandchild behind. Cancelling
// the probe has to reap both, not just the shim.
func TestProbeOutputCancellationKillsDescendants(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "probe")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		`( sleep 300 ) </dev/null >/dev/null 2>&1 &
printf '%s %s\n' "$$" "$!" > "$1"
sleep 300
`))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewCommand(fakePath, nil).exec(ctx, pidFile)
	cmd.WaitDelay = 5 * time.Second

	done := make(chan error, 1)
	go func() {
		_, err := outputOwned(cmd, slog.Default())
		done <- err
	}()

	pids := waitForPids(t, pidFile)
	cancel()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("outputOwned never returned after the probe was cancelled")
	}
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

// TestDetectVersionTimeoutOnlyKillsItsProbeGroup guards the daemon safety
// boundary: timing out one version probe must reap that probe's descendants
// without signalling an unrelated task in the caller's process group.
func TestDetectVersionTimeoutOnlyKillsItsProbeGroup(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "probe-child.pid")
	probePath := filepath.Join(tempDir, "probe")
	writeTestExecutable(t, probePath, []byte("#!/bin/sh\n"+
		`( sleep 300 ) </dev/null >/dev/null 2>&1 &
printf '%s\n' "$!" > "$1"
wait
`))

	sibling := exec.Command("sleep", "300")
	if err := sibling.Start(); err != nil {
		t.Fatalf("start unrelated process: %v", err)
	}
	t.Cleanup(func() {
		_ = sibling.Process.Kill()
		_ = sibling.Wait()
	})

	orig := detectVersionTimeout
	detectVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { detectVersionTimeout = orig })

	_, err := detectCLIVersion(context.Background(), NewCommand(probePath, []string{pidFile}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("detectCLIVersion error = %v, want context deadline exceeded", err)
	}
	if err := sibling.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process %d was signalled by probe cleanup: %v", sibling.Process.Pid, err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read probe child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse probe child pid: %v", err)
	}
	waitProcessGone(t, childPID)
}

// TestOutputOwnedMatchesStdlibContract keeps the owned probe helpers drop-in:
// callers were using cmd.Output() and cmd.CombinedOutput() and must keep
// getting the same bytes back.
func TestOutputOwnedMatchesStdlibContract(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "noisy")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		`printf 'to-stdout\n'
printf 'to-stderr\n' >&2
exit 3
`))

	out, err := outputOwned(NewCommand(fakePath, nil).exec(context.Background()), slog.Default())
	if string(out) != "to-stdout\n" {
		t.Errorf("stdout = %q, want %q", out, "to-stdout\n")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want an *exec.ExitError", err)
	}
	if exitErr.ExitCode() != 3 {
		t.Errorf("exit code = %d, want 3", exitErr.ExitCode())
	}
	if !strings.Contains(string(exitErr.Stderr), "to-stderr") {
		t.Errorf("ExitError.Stderr = %q, want it to carry the probe's stderr", exitErr.Stderr)
	}

	combined, err := combinedOutputOwned(NewCommand(fakePath, nil).exec(context.Background()), slog.Default())
	if err == nil {
		t.Error("combinedOutputOwned should report the non-zero exit")
	}
	for _, want := range []string{"to-stdout", "to-stderr"} {
		if !strings.Contains(string(combined), want) {
			t.Errorf("combined output %q missing %q", combined, want)
		}
	}
}

// TestProbeReturnsWhenLeaderExitsHoldingPipes is the normal-exit counterpart to
// the cancellation tests: nobody cancels anything here.
//
// The fake exits straight away but leaves a descendant holding the stdout and
// stderr write ends. cmd.Wait() cannot return until those close, and on Windows
// what closes them is releaseProcessGroup — which runs after Wait. A probe that
// waits for its own cleanup never returns, and checkOpenclawVersion runs on the
// caller's context before any task timeout exists, so the task would hang until
// someone cancelled it by hand.
func TestProbeReturnsWhenLeaderExitsHoldingPipes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "wrapper")
	// The descendant inherits stdout/stderr on purpose — that is what holds the
	// pipe open after the leader is gone.
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\n"+
		`( sleep 300 ) &
printf '%s %s\n' "$$" "$!" > "$1"
printf 'v1.2.3\n'
exit 0
`))

	done := make(chan []byte, 1)
	go func() {
		out, _ := outputOwned(NewCommand(fakePath, nil).exec(context.Background(), pidFile), slog.Default())
		done <- out
	}()

	pids := waitForPids(t, pidFile)

	select {
	case out := <-done:
		if string(out) != "v1.2.3\n" {
			t.Errorf("stdout = %q, want the version line", out)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("outputOwned never returned: the leader exited, but Wait is still waiting for a pipe " +
			"that only releaseProcessGroup can close, and releaseProcessGroup runs after Wait")
	}

	// Nothing the probe spawned may outlive the answer.
	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}
