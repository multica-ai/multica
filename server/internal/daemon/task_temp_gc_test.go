package daemon

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// TestTaskTempDirSurvivesGCWhileTheRunHoldsIt wires the two halves of the fix
// together: the directory ensureTaskTempDir hands a running task is invisible to
// the GC sweep for as long as the run holds its lock, and is reclaimed by the
// very next sweep once the run releases it.
//
// The sweep runs inside the same daemon that owns the live task, so here the
// holder and the sweeper are one process on purpose. That is the case a PID
// marker could not answer and an in-memory active set would have to be built
// for; the lock answers it because it is held by an open file description
// rather than by a process.
//
// `now` is a year ahead throughout, so nothing below can pass or fail on age.
func TestTaskTempDirSurvivesGCWhileTheRunHoldsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MULTICA_AGENT_TEMP_BASE is ignored on Windows; execenv's task temp tests cover the lock contract there")
	}
	base := t.TempDir()
	t.Setenv("MULTICA_AGENT_TEMP_BASE", base)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	future := time.Now().Add(365 * 24 * time.Hour)

	dir, lock, err := ensureTaskTempDir("root", "ws", "task")
	if err != nil {
		t.Fatalf("ensureTaskTempDir(): %v", err)
	}
	t.Cleanup(func() {
		execenv.ReleaseTaskTempLock(lock)
		_ = os.RemoveAll(dir)
	})

	if _, err := os.Stat(filepath.Join(dir, ".task_lock")); err != nil {
		t.Fatalf("ensureTaskTempDir did not leave an execution lock in %s: %v", dir, err)
	}

	if removed, _ := execenv.PruneTaskTempDirs(base, DefaultGCTaskTempLegacyTTL, future, logger); removed != 0 {
		t.Fatalf("gc sweep removed %d dirs while the run held the lock, want 0", removed)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("gc sweep removed a live task's temp dir: %v", err)
	}

	// What the cleanup defer in runTask does at the end of a run.
	execenv.ReleaseTaskTempLock(lock)

	removed, _ := execenv.PruneTaskTempDirs(base, DefaultGCTaskTempLegacyTTL, future, logger)
	if removed != 1 {
		t.Fatalf("gc sweep removed %d dirs after the run released the lock, want 1", removed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("task temp dir still present after the sweep: %v", err)
	}
}
