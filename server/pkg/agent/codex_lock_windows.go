//go:build windows

package agent

import (
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
// sandbox helper. It returns a release function that closes the file handle.
func acquireCodexLaunchLock(logger *slog.Logger) func() {
	lockPath := filepath.Join(os.TempDir(), "multica-codex-sandbox-setup.lock")
	namePtr, err := syscall.UTF16PtrFromString(lockPath)
	if err != nil {
		if logger != nil {
			logger.Error("codex lock: invalid temp path", "error", err)
		}
		return func() {}
	}

	start := time.Now()
	timeout := 15 * time.Second // bounded timeout for lock acquisition
	retryInterval := 50 * time.Millisecond

	for {
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
			}
		}

		if err != errorSharingViolation && err != errorAccessDenied {
			// Some other error, fail open to avoid deadlock
			if logger != nil {
				logger.Warn("codex lock: failed to acquire lock, failing open", "error", err)
			}
			return func() {}
		}

		if time.Since(start) > timeout {
			if logger != nil {
				logger.Warn("codex lock: timeout waiting for launch lock, failing open")
			}
			return func() {}
		}

		time.Sleep(retryInterval)
	}
}
