//go:build !windows

package execenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// blockContentRemoval plants a payload inside dir that cannot be unlinked,
// standing in for the Windows sharing violation this sweep has to survive:
// unlinking needs write+execute on the PARENT, so a read-only subdirectory
// makes RemoveAll fail after it has already walked the rest of the tree.
// The returned func lifts the obstruction.
func blockContentRemoval(t *testing.T, dir string) func() {
	t.Helper()
	sub := filepath.Join(dir, "held")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("create blocked subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "payload"), make([]byte, 2048), 0o600); err != nil {
		t.Fatalf("write blocked payload: %v", err)
	}
	if err := os.Chmod(sub, 0o555); err != nil {
		t.Fatalf("chmod blocked subdir: %v", err)
	}
	unblocked := false
	unblock := func() {
		if unblocked {
			return
		}
		unblocked = true
		_ = os.Chmod(sub, 0o755)
	}
	t.Cleanup(unblock)
	return unblock
}

// TestRemoveTaskTempDirKeepsMarkerWhenContentSurvives is the regression for the
// reason this is not os.RemoveAll: RemoveAll keeps walking after a failure, so
// it deletes the .task_lock sitting next to the payload it could not remove.
// The directory would come back as one with no marker — indistinguishable from
// a pre-lock leftover, and so no longer reclaimable on liveness at all.
func TestRemoveTaskTempDirKeepsMarkerWhenContentSurvives(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "held", true)
	unblock := blockContentRemoval(t, dir)

	if err := RemoveTaskTempDir(dir); err == nil {
		t.Skip("process can unlink through a read-only directory (running as root?)")
	}
	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err != nil {
		t.Fatalf("cleanup destroyed the lock marker the next sweep needs: %v", err)
	}
	// Contrast: what the previous implementation did to the same directory.
	if err := os.RemoveAll(dir); err == nil {
		t.Fatal("os.RemoveAll unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); !os.IsNotExist(err) {
		t.Fatalf("os.RemoveAll was expected to destroy the marker, stat err = %v", err)
	}

	unblock()
}

// TestPruneTaskTempDirsRetriesUntilContentCanBeRemoved walks the whole failure
// path the way the GC does: a cycle that cannot finish the removal leaves the
// directory reclaimable on liveness, and a later cycle finishes the job with no
// TTL involved at any point.
func TestPruneTaskTempDirsRetriesUntilContentCanBeRemoved(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "held", true)
	unblock := blockContentRemoval(t, dir)

	// legacyTTL 0 throughout: if the marker were lost, the directory would be
	// classified legacy and this sweep would never remove it at all.
	if removed, _ := PruneTaskTempDirs(base, 0, time.Now(), testLogger()); removed != 0 {
		if _, err := os.Stat(filepath.Join(dir, "held", "payload")); os.IsNotExist(err) {
			t.Skip("process can unlink through a read-only directory (running as root?)")
		}
		t.Fatalf("prune reported %d removals despite the blocked payload, want 0", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err != nil {
		t.Fatalf("failed prune destroyed the lock marker: %v", err)
	}

	unblock()

	removed, bytesFreed := PruneTaskTempDirs(base, 0, time.Now(), testLogger())
	if removed != 1 {
		t.Fatalf("prune removed %d dirs once the payload was releasable, want 1", removed)
	}
	if bytesFreed < 2048 {
		t.Fatalf("prune reported %d bytes freed, want at least 2048", bytesFreed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still present: %v", err)
	}
}

// TestPruneTaskTempDirsFailsClosedOnUnreadableDir: a directory this sweep
// cannot inspect is one it must not delete. Another user's temp dir under a
// shared /tmp arrives here.
func TestPruneTaskTempDirsFailsClosedOnUnreadableDir(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "opaque", true)
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err == nil {
		t.Skip("process can stat through a 0000 directory (running as root?)")
	}

	// An ancient legacy TTL that would otherwise fire: the point is that an
	// unreadable directory is never classified legacy in the first place.
	removed, _ := PruneTaskTempDirs(base, time.Hour, time.Now().Add(10*365*24*time.Hour), testLogger())
	if removed != 0 {
		t.Fatalf("prune removed %d unreadable dirs, want 0", removed)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("prune removed an unreadable dir: %v", err)
	}
}

// TestRemoveTaskTempDirRestoresMarkerWhenDirCannotBeRemoved is the second
// failure branch: the contents come away cleanly, the marker is deleted so the
// directory can go — and then the directory itself cannot be removed. Without
// restoring the marker this is the worst case of all, because the cleanup that
// got furthest is the one that leaves a directory nothing can classify again.
//
// A read-only BASE blocks removing the directory while still allowing its
// contents (and marker) to be removed, since unlinking is governed by the
// parent.
func TestRemoveTaskTempDirRestoresMarkerWhenDirCannotBeRemoved(t *testing.T) {
	base := t.TempDir()
	dir := makeTaskTempDir(t, base, "undeletable", true)
	if err := os.WriteFile(filepath.Join(dir, "payload"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := os.Chmod(base, 0o555); err != nil {
		t.Fatalf("chmod base: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	if err := RemoveTaskTempDir(dir); err == nil {
		t.Skip("process can unlink through a read-only directory (running as root?)")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory unexpectedly removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, envRootLockFile)); err != nil {
		t.Fatalf("marker was not restored after the directory removal failed: %v", err)
	}
	// The restored marker must be unlocked, so the next cycle can claim it.
	if got := classifyTaskTempDir(dir, testLogger()); got != taskTempDead {
		t.Fatalf("classify after restore = %v, want taskTempDead", got)
	}

	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatalf("restore base perms: %v", err)
	}
	if removed, _ := PruneTaskTempDirs(base, 0, time.Now(), testLogger()); removed != 1 {
		t.Fatalf("next sweep removed %d dirs, want 1", removed)
	}
}
