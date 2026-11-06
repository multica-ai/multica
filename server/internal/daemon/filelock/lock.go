// Package filelock provides cross-process exclusion on persistent lock files.
package filelock

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Try acquires path without waiting. A nil file and nil error means busy.
// Lock files must live outside the data they protect and must never be removed:
// unlinking one would let a new opener lock a different inode.
func Try(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	ok, err := TryFile(f)
	if !ok || err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Acquire waits for the lock or cancellation.
func Acquire(ctx context.Context, path string) (*os.File, error) {
	for {
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		f, err := Try(path)
		if f != nil || err != nil {
			return f, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, context.Cause(ctx)
		case <-timer.C:
		}
	}
}

// Release drops a held lock. Closing also releases it after a process crash.
func Release(f *os.File) {
	if f != nil {
		_ = Unlock(f)
		_ = f.Close()
	}
}
