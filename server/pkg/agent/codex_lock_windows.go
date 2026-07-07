//go:build windows

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	fileFlagDeleteOnClose = 0x04000000
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
)

// acquireCodexLaunchLock acquires an exclusive cross-process lock on Windows
// to serialize Codex startup and prevent concurrent invocations of the native
// sandbox helper. It respects context cancellation and returns an error if
// the lock cannot be acquired within the context deadline.
func acquireCodexLaunchLock(ctx context.Context, logger *slog.Logger) (func(), error) {
	lockPath := filepath.Join(os.TempDir(), "multica-codex-sandbox-setup.lock")
	namePtr, err := syscall.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, fmt.Errorf("invalid temp path: %w", err)
	}

	retryInterval := 50 * time.Millisecond

	for {
		// Check context cancellation first
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// sharemode = 0 means exclusive access.
		// FILE_FLAG_DELETE_ON_CLOSE deletes the file when the handle is closed.
		h, err := syscall.CreateFile(
			namePtr,
			syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			0, // exclusive
			nil,
			syscall.OPEN_ALWAYS,
			syscall.FILE_ATTRIBUTE_NORMAL|fileFlagDeleteOnClose,
			0,
		)
		if err == nil {
			if logger != nil {
				logger.Debug("codex lock: acquired launch lock", "path", lockPath)
			}
			return func() {
				_ = syscall.CloseHandle(h)
				if logger != nil {
					logger.Debug("codex lock: released launch lock", "path", lockPath)
				}
			}, nil
		}

		if err != errorSharingViolation && err != errorAccessDenied {
			return nil, fmt.Errorf("unexpected lock error on %s: %w", lockPath, err)
		}

		// Wait or respect context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}
