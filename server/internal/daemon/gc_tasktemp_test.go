package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunGC_PrunesTaskTempDirs proves the temp-base sweep is wired into
// runGC: an orphaned multica-task-* dir past the TTL is reclaimed and the
// new counter reaches the "gc: cycle complete" log line.
func TestRunGC_PrunesTaskTempDirs(t *testing.T) {
	tempBase := t.TempDir()
	t.Setenv("MULTICA_AGENT_TEMP_BASE", tempBase)

	orphan := filepath.Join(tempBase, "multica-task-orphan")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "payload.bin"), make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := filepath.Walk(orphan, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, old, old)
	}); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCTaskTempTTL = 7 * 24 * time.Hour
	d.logger = slog.New(slog.NewTextHandler(&logBuf, nil))

	d.runGC(context.Background())

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphaned task temp dir %s must be removed by runGC", orphan)
	}
	if !strings.Contains(logBuf.String(), "gc: cycle complete") {
		t.Fatalf("cycle complete log missing:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "task_temp_dirs_reclaimed=1") {
		t.Fatalf("task_temp_dirs_reclaimed counter missing from cycle summary:\n%s", logBuf.String())
	}
}

// The safety property, end to end through the real reservation protocol: a
// dir a live task holds via markActiveStore survives runGC even when its
// mtime is far past the TTL.
func TestRunGC_MarkedActiveTaskTempDirSurvivesStaleMtime(t *testing.T) {
	tempBase := t.TempDir()
	t.Setenv("MULTICA_AGENT_TEMP_BASE", tempBase)

	live := filepath.Join(tempBase, "multica-task-live")
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(live, old, old); err != nil {
		t.Fatal(err)
	}

	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCTaskTempTTL = 7 * 24 * time.Hour
	d.markActiveStore(live)
	defer d.unmarkActiveStore(live)

	d.runGC(context.Background())

	if _, err := os.Stat(live); err != nil {
		t.Fatalf("marked-active task temp dir must survive GC despite stale mtime: %v", err)
	}
}
