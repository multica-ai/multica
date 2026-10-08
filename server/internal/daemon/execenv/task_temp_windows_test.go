//go:build windows

package execenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestPruneTaskTempDirsSurvivesRealSharingViolation is the Windows regression
// for #7364's actual failure mode, with a real sharing violation rather than a
// simulated one: a payload opened WITHOUT FILE_SHARE_DELETE cannot be deleted
// while the handle is open, which is exactly what a task's leftover child
// process does to its node compile cache.
//
// What has to hold: the failed cleanup leaves .task_lock intact, so the
// directory stays reclaimable on liveness, and the cycle after the handle
// closes removes it — with the legacy TTL disabled throughout, proving no part
// of this depends on age.
func TestPruneTaskTempDirsSurvivesRealSharingViolation(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "sharing-violation", true)

	payload := filepath.Join(dir, "node-compile-cache")
	if err := os.WriteFile(payload, make([]byte, 2048), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	payloadPtr, err := windows.UTF16PtrFromString(payload)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(): %v", err)
	}
	// No FILE_SHARE_DELETE: Windows now refuses to delete this file.
	handle, err := windows.CreateFile(
		payloadPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
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

	// Establish that the sharing violation is real before asserting anything
	// about how the sweep copes with it.
	if err := os.Remove(payload); err == nil {
		t.Skip("this Windows build allowed deleting an open file; nothing to regress against")
	}

	if removed, _ := PruneTaskTempDirs(base, 0, time.Now(), testLogger()); removed != 0 {
		t.Fatalf("prune reported %d removals while the payload was held, want 0", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err != nil {
		t.Fatalf("failed prune destroyed the lock marker the next cycle needs: %v", err)
	}

	closeHandle()

	removed, bytesFreed := PruneTaskTempDirs(base, 0, time.Now(), testLogger())
	if removed != 1 {
		t.Fatalf("prune removed %d dirs after the handle closed, want 1", removed)
	}
	if bytesFreed < 2048 {
		t.Fatalf("prune reported %d bytes freed, want at least 2048", bytesFreed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still present after prune: %v", err)
	}
}
