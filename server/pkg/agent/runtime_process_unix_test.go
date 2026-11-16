//go:build linux || darwin

package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOwnedRuntimeProcessRetainsLeaderUntilCleanup(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", "exit "+strconv.Itoa(code))
			p, err := startOwnedRuntimeProcess(context.Background(), cmd, slog.Default(), nil, func() {})
			if err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = p.Wait()
				}
			}()
			select {
			case <-p.exitReady:
			case <-time.After(2 * time.Second):
				t.Fatal("exit cleanup did not finish")
			}
			// Cleanup already completed, but ordinary Wait has not run. The
			// leader must still pin the process-group identifier, twice.
			for range 2 {
				if err := observeRuntimeExit(cmd.Process.Pid); err != nil {
					t.Fatalf("leader already reaped: %v", err)
				}
			}
			err = p.Wait()
			waited = true
			if code == 0 && err != nil {
				t.Fatal(err)
			}
			if code != 0 {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != code {
					t.Fatalf("exit status lost: %v", err)
				}
			}
		})
	}
}

func TestOwnedRuntimeProcessCancellationPreservesGrace(t *testing.T) {
	dir := t.TempDir()
	pidFile, release := filepath.Join(dir, "pids"), filepath.Join(dir, "release")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := NewCommand("/bin/sh", nil).exec(ctx, "-c", `sleep 5 &
printf '%s %s\n' "$$" "$!" > "$1"
while [ ! -f "$2" ]; do sleep 0.02; done`, "fixture", pidFile, release)
	cmd.Cancel = func() error { return nil }
	cancellationDone := make(chan struct{})
	var doneOnce sync.Once
	done := func() { doneOnce.Do(func() { close(cancellationDone) }) }
	p, err := startOwnedRuntimeProcess(ctx, cmd, slog.Default(), cancellationDone, func() {})
	if err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		done()
		_ = cmd.Process.Kill()
		if !waited {
			_ = p.Wait()
		}
	})
	pids := waitForPids(t, pidFile)
	cancel()
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := observeRuntimeExit(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.exitReady:
		t.Fatal("leader exit skipped cancellation grace")
	case <-time.After(100 * time.Millisecond):
	}
	if err := syscall.Kill(pids[1], 0); err != nil {
		t.Fatalf("descendant killed before grace completed: %v", err)
	}
	done()
	_ = p.Wait()
	waited = true
	waitRuntimeDescendantGone(t, pids[1])
}

func TestOwnedRuntimeProcessObservationFailureUnblocksOutput(t *testing.T) {
	cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", "exec sleep 5")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		t.Fatal(err)
	}
	p := &ownedRuntimeProcess{cmd: cmd, exitReady: make(chan struct{})}
	waited := false
	defer func() {
		_ = cmd.Process.Kill()
		if !waited {
			_ = p.Wait()
		}
	}()
	// A per-call seam models a sandbox denying waitid; no package-global
	// syscall hook or unrelated process is involved.
	go p.observeExit(context.Background(), nil, func() { _ = stdout.Close() }, func(int) error { return syscall.EPERM }, signalProcessGroup)
	readDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, stdout); close(readDone) }()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("failed observer stranded the output scanner")
	}
	err = p.Wait()
	waited = true
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("observation error lost: %v", err)
	}
}

func TestOwnedRuntimeProcessStartFailurePreservesArgvError(t *testing.T) {
	cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", strings.Repeat("x", 2<<20))
	p, err := startOwnedRuntimeProcess(context.Background(), cmd, slog.Default(), nil, func() {})
	if p != nil || !errors.Is(err, ErrArgvLimitExceeded) || !errors.Is(err, syscall.E2BIG) {
		t.Fatalf("start lost argv-limit error: process=%v error=%v", p, err)
	}
}

func TestDshWriteFailureAfterCancellationReturns(t *testing.T) {
	dir := t.TempDir()
	pidFile, release := filepath.Join(dir, "pids"), filepath.Join(dir, "release")
	bin := writeDshFixture(t, `
printf '%s %s\n' "$$" "$$" > "$PID_FILE"
i=0
while [ ! -f "$RELEASE_FILE" ] && [ "$i" -lt 250 ]; do
  sleep 0.02
  i=$((i + 1))
done
exec 0<&-
exit 0
`)
	b, err := New("dsh", Config{ExecutablePath: bin, Logger: slog.Default(), Env: map[string]string{"PID_FILE": pidFile, "RELEASE_FILE": release}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = os.WriteFile(release, nil, 0600) })
	done := make(chan error, 1)
	go func() { _, err := b.Execute(ctx, strings.Repeat("x", 1<<20), ExecOptions{Cwd: dir}); done <- err }()
	waitForPids(t, pidFile)
	cancel()
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "send dsh execute command") {
			t.Fatalf("write failure lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("early write failure deadlocked on cancellation cleanup")
	}
}
