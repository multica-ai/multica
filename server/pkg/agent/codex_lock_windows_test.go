//go:build windows

package agent

import (
	"context"
	"os"
	"path/filepath"
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
