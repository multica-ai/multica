//go:build windows

package execenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestRemoveAllWithRetrySurvivesRealDirectorySharingViolation exercises
// #7548's actual Windows failure mode. A directory handle opened without
// FILE_SHARE_DELETE lets RemoveAll empty the worktree but prevents removal of
// the final directory entry until the scanner-like handle is released.
func TestRemoveAllWithRetrySurvivesRealDirectorySharingViolation(t *testing.T) {
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(worktreePath, 0o755); err != nil {
		t.Fatalf("create worktree directory: %v", err)
	}

	pathPtr, err := windows.UTF16PtrFromString(worktreePath)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(): %v", err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		t.Fatalf("CreateFile(): %v", err)
	}
	handleClosed := false
	closeHandle := func() {
		if handleClosed {
			return
		}
		handleClosed = true
		_ = windows.CloseHandle(handle)
	}
	t.Cleanup(closeHandle)

	// Prove this runner enforces the sharing violation before relying on it.
	if err := os.Remove(worktreePath); err == nil {
		t.Skip("this Windows build allowed deleting an open directory; nothing to regress against")
	}

	attempts := 0
	err = removeAllWithRetry(worktreePath, func(path string) error {
		attempts++
		return os.RemoveAll(path)
	}, func(time.Duration) {
		closeHandle()
	})
	if err != nil {
		t.Fatalf("removeAllWithRetry: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if _, statErr := os.Lstat(worktreePath); !os.IsNotExist(statErr) {
		t.Fatalf("worktree directory still exists after retry: %v", statErr)
	}
}
