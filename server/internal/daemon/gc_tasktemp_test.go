package daemon

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// TestRunTask_MarksTaskTempDirActiveWhileInFlight proves the runTask wiring,
// not just the pruner: while a task is in flight, its multica-task-* temp dir
// is held via markActiveStore, so reserveStoreForDeletion refuses it; after
// runTask returns (dir removed and unmarked), the reservation succeeds. With
// the markActiveStore call deleted from runTask, the mid-task reserve would
// return ok=true and this test fails.
func TestRunTask_MarksTaskTempDirActiveWhileInFlight(t *testing.T) {
	tempBase := t.TempDir()
	t.Setenv("MULTICA_AGENT_TEMP_BASE", tempBase)

	var (
		startCalled      atomic.Bool
		midTaskReserveOK atomic.Bool
		tempDirSeen      atomic.Bool
		daemonRef        atomic.Pointer[Daemon]
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			startCalled.Store(true)
			d := daemonRef.Load()
			if d == nil {
				t.Error("daemon not registered when /start fired")
				w.WriteHeader(http.StatusOK)
				return
			}
			matches, err := filepath.Glob(filepath.Join(tempBase, "multica-task-*"))
			if err != nil {
				t.Errorf("glob temp base: %v", err)
			}
			if len(matches) != 1 {
				t.Errorf("expected exactly 1 task temp dir in flight, got %d", len(matches))
				return
			}
			tempDirSeen.Store(true)
			_, ok := d.reserveStoreForDeletion(matches[0])
			midTaskReserveOK.Store(ok)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	missingBin := filepath.Join(t.TempDir(), "definitely-not-claude")
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			Agents: map[string]AgentEntry{
				"claude": {Path: missingBin, Model: ""},
			},
		},
	}
	daemonRef.Store(d)

	task := Task{
		ID:          "task-temp-mark",
		WorkspaceID: "ws-temp-mark",
		RuntimeID:   "rt-1",
		IssueID:     "issue-temp-mark",
		AgentID:     "agent-temp-mark",
		Agent:       &AgentData{ID: "agent-temp-mark", Name: "test-agent"},
	}

	taskLog := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, _ = d.runTask(context.Background(), task, "claude", 0, taskLog)

	if !startCalled.Load() {
		t.Fatal("runTask did not call /start — test harness never reached the task window")
	}
	if !tempDirSeen.Load() {
		t.Fatal("no multica-task-* dir existed under the temp base when /start fired")
	}
	if midTaskReserveOK.Load() {
		t.Fatal("reserveStoreForDeletion succeeded on the task temp dir mid-task — runTask did not mark it active")
	}
}
