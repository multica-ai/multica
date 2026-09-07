package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestRunWorkspaceGCRetainsInterruptedAndCompletedRooms(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "interrupted", true: "completed"}[completed], func(t *testing.T) {
			d := newGCTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("retained room must not reach parent-lifecycle GC")
				w.WriteHeader(http.StatusNotFound)
			}))
			d.cfg.GCOrphanTTL = 0
			var meta *execenv.GCMeta
			if completed {
				meta = &execenv.GCMeta{Kind: execenv.GCKindIssue, IssueID: "issue", WorkspaceID: "workspace-test", CompletedAt: time.Now().Add(-365 * 24 * time.Hour)}
			}
			dir := createTaskDir(t, d.cfg.WorkspacesRoot, "workspace-test", "task-retained", meta)
			for _, name := range []string{"run-workspace.json", "partial.txt"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("preserve even an incomplete receipt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := d.shouldCleanTaskDir(context.Background(), dir); got != gcActionSkip {
				t.Fatalf("receipt selected for GC: %v", got)
			}
			stats := &gcStats{}
			d.gcWorkspace(context.Background(), filepath.Dir(dir), stats)
			for _, action := range []gcAction{gcActionOrphan, gcActionClean, gcActionCleanArtifacts, gcActionCleanManagedArtifacts} {
				d.applyGCAction(dir, action, stats) // stale decision must recheck receipt
			}
			if _, removed := d.cleanTaskDir(dir); removed {
				t.Fatal("direct deletion discarded run-owned room")
			}
			for _, name := range []string{"run-workspace.json", "partial.txt"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatal("retained artifact lost", err)
				}
			}
		})
	}
}

func TestRunWorkspaceAdmissionDoesNotWriteSource(t *testing.T) {
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(source)
	ref, _ := json.Marshal(map[string]any{"daemon_id": "host", "local_path": source, "execution_mode": "run_owned", "inherit_workspace_repositories": false})
	d := &Daemon{cfg: Config{DaemonID: "host"}}
	task := Task{ID: "run", ProjectResources: []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: ref}}}
	release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), task, slog.Default())
	if abort || release != nil {
		t.Fatal("run-owned source unexpectedly rejected or locked")
	}
	after, _ := os.Stat(source)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("admission created/deleted a probe in the source directory")
	}
}
