//go:build windows

package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireCodexLaunchLock_SuccessAndRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	unlock, err := acquireCodexLaunchLock(ctx, nil)
	if err != nil {
		t.Fatalf("expected to acquire lock successfully, got: %v", err)
	}

	// Verify lock file exists
	lockPath := filepath.Join(os.TempDir(), "multica-codex-sandbox-setup.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("expected lock file to exist at %s, got error: %v", lockPath, err)
	}

	// Release lock
	unlock()

	// Wait a tiny bit to allow OS-level delete-on-close to process
	time.Sleep(10 * time.Millisecond)

	// Verify lock file is gone (or at least no longer actively locked by us)
	// On Windows, if a file is marked for deletion, stats might fail or say it doesn't exist.
	_, err = os.Stat(lockPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("unexpected error checking deleted file: %v", err)
	}
}

func TestAcquireCodexLaunchLock_Concurrency(t *testing.T) {
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	unlock1, err := acquireCodexLaunchLock(ctx1, nil)
	if err != nil {
		t.Fatalf("first acquisition failed: %v", err)
	}
	defer unlock1()

	// Try to acquire concurrently with a very short timeout
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	_, err = acquireCodexLaunchLock(ctx2, nil)
	if err == nil {
		t.Fatal("expected second concurrent lock acquisition to fail")
	}

	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded error, got: %v", err)
	}
}

func TestAcquireCodexLaunchLock_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Start a goroutine that holds the lock
	held := make(chan struct{})
	released := make(chan struct{})
	go func() {
		ul, err := acquireCodexLaunchLock(context.Background(), nil)
		if err != nil {
			t.Errorf("goroutine lock acquisition failed: %v", err)
			return
		}
		close(held)
		<-released
		ul()
	}()

	// Wait until lock is held
	<-held

	// Start acquisition attempt and cancel it
	acquireErrCh := make(chan error, 1)
	go func() {
		_, err := acquireCodexLaunchLock(ctx, nil)
		acquireErrCh <- err
	}()

	// Cancel the context
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-acquireErrCh
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// Cleanup goroutine
	close(released)
}

// TestCodexLaunchLockHeldUntilFirstProcessExits pins the fix for the PR
// #5007 review finding that safeUnlock() ran before drainAndWait() on
// codex.go's initialize-failure path (and in the deferred fallback order),
// releasing the launch lock while the first task's process was possibly
// still alive — letting a contending task start a second Codex process (and
// a second invocation of the native Windows sandbox helper) concurrently
// with the first one's shutdown, which is exactly what the lock exists to
// prevent.
//
// Task A's fake codex never speaks the JSON-RPC protocol at all, so its
// initialize call can only fail via its own 300ms timeout — but the fake
// process itself survives roughly 3 seconds afterward (a ping.exe child
// orphaned by the .bat's cmd.exe wrapper — Kill() on Windows only
// terminates the direct cmd.Process handle, matching how a real, slow-to-
// exit codex process behaves relative to os/exec's cancel-on-timeout
// handling). That gap — initialize failing fast, the process staying alive
// much longer — is exactly the window the review flagged.
//
// Task B then attempts to launch, bounded by its own short timeout, well
// after A's initialize has failed but while A's process is still alive. If
// the lock had already been released, B would launch immediately; with the
// fix, B's own acquisition attempt must instead time out, proving the lock
// stayed held until A's process actually exited.
func TestCodexLaunchLockHeldUntilFirstProcessExits(t *testing.T) {
	dir := t.TempDir()
	slowCodexPath := filepath.Join(dir, "codex.bat")
	writeTestExecutable(t, slowCodexPath, []byte("@echo off\r\nping -n 4 127.0.0.1 >nul\r\n"))

	backendA, err := New("codex", Config{ExecutablePath: slowCodexPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend (A): %v", err)
	}

	ctxA, cancelA := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelA()
	sessionA, err := backendA.Execute(ctxA, "prompt-ignored", ExecOptions{Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("execute (A): %v", err)
	}
	go func() {
		for range sessionA.Messages {
		}
	}()
	t.Cleanup(func() {
		select {
		case <-sessionA.Result:
		case <-time.After(10 * time.Second):
		}
	})

	// Give A's initialize call time to fail via its 300ms timeout — well
	// before its fake process (alive for ~3s) actually exits.
	time.Sleep(500 * time.Millisecond)

	fastCodexPath := filepath.Join(dir, "codex-b.bat")
	writeTestExecutable(t, fastCodexPath, []byte("@echo off\r\n"))
	backendB, err := New("codex", Config{ExecutablePath: fastCodexPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend (B): %v", err)
	}

	ctxB, cancelB := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelB()
	_, err = backendB.Execute(ctxB, "prompt-ignored", ExecOptions{Timeout: 600 * time.Millisecond})
	if err == nil {
		t.Fatal("expected B's launch to be blocked by A's still-held lock, but it succeeded")
	}
	if !strings.Contains(err.Error(), "acquire codex launch lock") {
		t.Fatalf("expected a launch-lock acquisition error, got: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the lock wait to fail with context.DeadlineExceeded, got: %v", err)
	}
}
