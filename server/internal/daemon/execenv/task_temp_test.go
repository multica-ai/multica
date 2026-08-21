package execenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedTaskTempDir creates a multica-task-* dir under base holding one file of
// the given size, and returns the dir path.
func seedTaskTempDir(t *testing.T, base string, size int64) string {
	t.Helper()
	dir, err := os.MkdirTemp(base, taskTempDirPrefix)
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, size), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	return dir
}

func TestPruneTaskTempDirs_RemovesOrphanPastTTL(t *testing.T) {
	base := t.TempDir()
	orphan := seedTaskTempDir(t, base, 128)
	old := time.Now().Add(-30 * 24 * time.Hour)
	chtimesTree(t, orphan, old)

	removed, freed := PruneTaskTempDirs(base, 7*24*time.Hour, time.Now(), nil, testLogger())
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if freed <= 0 {
		t.Errorf("bytesFreed = %d, want > 0", freed)
	}
	assertAbsent(t, orphan)
}

func TestPruneTaskTempDirs_MarkedActiveSurvivesStaleMtime(t *testing.T) {
	base := t.TempDir()
	live := seedTaskTempDir(t, base, 128)
	// A running task can go tens of minutes without touching the root of its
	// temp dir, so the mtime is at creation time — well past any short TTL.
	chtimesTree(t, live, time.Now().Add(-30*24*time.Hour))

	reserved := ""
	release := func() {}
	reserve := func(dir string) (func(), bool) {
		if dir == live {
			reserved = dir
			return release, false // a live task holds it
		}
		return nil, true
	}

	removed, _ := PruneTaskTempDirs(base, 7*24*time.Hour, time.Now(), reserve, testLogger())
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (a marked-active dir must survive a stale mtime)", removed)
	}
	if reserved != live {
		t.Errorf("reserve was not consulted for the live dir %s", live)
	}
	assertPresent(t, live)
}

func TestPruneTaskTempDirs_YoungerThanTTLKept(t *testing.T) {
	base := t.TempDir()
	fresh := seedTaskTempDir(t, base, 64)

	removed, _ := PruneTaskTempDirs(base, 7*24*time.Hour, time.Now(), nil, testLogger())
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	assertPresent(t, fresh)
}

func TestPruneTaskTempDirs_OnlyMulticaTaskPrefix(t *testing.T) {
	base := t.TempDir()
	stale := time.Now().Add(-30 * 24 * time.Hour)

	sibling := filepath.Join(base, "not-a-task-dir")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(base, "multica-task-not-a-dir.txt")
	if err := os.WriteFile(siblingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := seedTaskTempDir(t, base, 32)
	chtimesTree(t, sibling, stale)
	chtimesTree(t, siblingFile, stale)
	chtimesTree(t, task, stale)

	removed, _ := PruneTaskTempDirs(base, 7*24*time.Hour, time.Now(), nil, testLogger())
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	assertPresent(t, sibling)
	assertPresent(t, siblingFile)
	assertAbsent(t, task)
}

func TestPruneTaskTempDirs_RetentionZeroDisables(t *testing.T) {
	base := t.TempDir()
	dir := seedTaskTempDir(t, base, 32)
	chtimesTree(t, dir, time.Now().Add(-30*24*time.Hour))

	if removed, _ := PruneTaskTempDirs(base, 0, time.Now(), nil, testLogger()); removed != 0 {
		t.Fatalf("retention<=0 must disable the sweep, removed=%d", removed)
	}
	assertPresent(t, dir)
}

func TestPruneTaskTempDirs_UnreadableRootIsNoop(t *testing.T) {
	removed, freed := PruneTaskTempDirs(filepath.Join(t.TempDir(), "missing"), 7*24*time.Hour, time.Now(), nil, testLogger())
	if removed != 0 || freed != 0 {
		t.Fatalf("unreadable root must return (0,0), got (%d,%d)", removed, freed)
	}
}
