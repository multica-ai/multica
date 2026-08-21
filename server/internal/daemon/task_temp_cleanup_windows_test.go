//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestCleanupTaskTempDirRetriesWindowsSharingViolation(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "node-compile-cache")
	if err := os.Mkdir(cacheDir, 0o700); err != nil {
		t.Fatalf("create node compile cache: %v", err)
	}
	lockedFile := filepath.Join(cacheDir, "cache.bin")
	if err := os.WriteFile(lockedFile, []byte("cache"), 0o600); err != nil {
		t.Fatalf("write locked file: %v", err)
	}

	path, err := windows.UTF16PtrFromString(lockedFile)
	if err != nil {
		t.Fatalf("encode locked file path: %v", err)
	}
	handle, err := windows.CreateFile(
		path,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("open locked file: %v", err)
	}

	if err := os.RemoveAll(dir); err == nil {
		_ = windows.CloseHandle(handle)
		t.Fatal("RemoveAll unexpectedly succeeded while delete sharing was denied")
	} else {
		t.Logf("reproduced Windows sharing violation: %v", err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = windows.CloseHandle(handle)
		close(released)
	}()

	attempts, err := cleanupTaskTempDirWith(
		dir,
		os.RemoveAll,
		time.Sleep,
		[]time.Duration{25 * time.Millisecond, 75 * time.Millisecond, 150 * time.Millisecond},
	)
	<-released
	if err != nil {
		t.Fatalf("cleanupTaskTempDirWith(): %v", err)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want a retry after the sharing violation", attempts)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still exists after retry: %v", err)
	}
}
