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

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

func TestGCWorkspaceRootsDefaultConfigAndDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")
	current := filepath.Join(home, "current")
	oldDefault := filepath.Join(home, "other-volume", "default")
	shared := filepath.Join(home, "other-volume", "shared")
	for profile, root := range map[string]string{"": oldDefault, "team-a": current, "old-a": shared, "old-b": shared} {
		if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{WorkspacesRoot: root}, profile); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := New(Config{Profile: "team-a", WorkspacesRoot: current}, slog.Default())
	roots := d.gcWorkspaceRoots()
	if len(roots) != 3 {
		t.Fatalf("roots = %+v, want current, default, shared once", roots)
	}
	seen := map[string]int{}
	for _, root := range roots {
		seen[root.root]++
	}
	for _, root := range []string{current, oldDefault, shared} {
		if seen[root] != 1 {
			t.Fatalf("root %s count = %d", root, seen[root])
		}
	}
	orphan := createTaskDir(t, oldDefault, "ws", "old", nil)
	past := time.Now().Add(-73 * time.Hour)
	if err := os.Chtimes(orphan, past, past); err != nil {
		t.Fatal(err)
	}
	d.cfg.GCOrphanTTL = 72 * time.Hour
	d.gcRoot(context.Background(), oldDefault, &gcStats{byPattern: map[string]int{}})
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("abandoned default task not reclaimed: %v", err)
	}
	t.Setenv("MULTICA_WORKSPACES_ROOT", current)
	if got := d.gcWorkspaceRoots(); len(got) != 1 {
		t.Fatalf("environment override scanned more than once: %+v", got)
	}
}

func TestGCForeignRootStartDuringIssueCheck(t *testing.T) {
	for _, state := range []string{"running", "completed-again"} {
		t.Run(state, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("MULTICA_WORKSPACES_ROOT", "")
			requested, resume := make(chan struct{}), make(chan struct{})
			mux := http.NewServeMux()
			mux.HandleFunc("/api/daemon/workspaces/ws-race/issues/gc-check", func(w http.ResponseWriter, r *http.Request) {
				close(requested)
				<-resume
				json.NewEncoder(w).Encode(map[string]any{"issues": []map[string]any{{"id": "issue-race", "found": true, "status": "done", "updated_at": time.Now().Add(-10 * 24 * time.Hour)}}})
			})
			d := newGCTestDaemon(t, mux)
			d.cfg.Profile = "team-a"
			foreign := filepath.Join(home, "multica_workspaces")
			params := execenv.RootDirParams{WorkspacesRoot: foreign, WorkspaceID: "ws-race", TaskID: "task-race"}
			claim, err := execenv.ClaimEnvRoot(params)
			if err != nil {
				t.Fatal(err)
			}
			claim.Release()
			root, err := execenv.ResolveRootDir(params)
			if err != nil {
				t.Fatal(err)
			}
			meta := execenv.GCMeta{Kind: execenv.GCKindIssue, WorkspaceID: "ws-race", IssueID: "issue-race", CompletedAt: time.Now().Add(-10 * 24 * time.Hour)}
			data, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".gc_meta.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			// Enumerate before the other daemon starts; no PID file exists at all.
			targets := d.gcWorkspaceRoots()
			if len(targets) != 2 {
				t.Fatalf("targets = %+v", targets)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				d.gcRoot(context.Background(), foreign, &gcStats{byPattern: map[string]int{}})
			}()
			select {
			case <-requested:
			case <-time.After(5 * time.Second):
				close(resume)
				t.Fatal("GC did not reach issue check")
			}
			claim, err = execenv.ClaimEnvRoot(params)
			if err != nil {
				close(resume)
				<-done
				t.Fatal(err)
			}
			defer claim.Release()
			payload := filepath.Join(root, "running-task")
			if err := os.WriteFile(payload, []byte("do not delete"), 0o644); err != nil {
				close(resume)
				<-done
				t.Fatal(err)
			}
			if state == "completed-again" {
				if err := execenv.WriteGCMeta(root, meta, slog.Default()); err != nil {
					close(resume)
					<-done
					t.Fatal(err)
				}
				claim.Release()
			}
			close(resume)
			<-done
			if _, err := os.Stat(payload); err != nil {
				t.Fatalf("GC deleted a newly started foreign task: %v", err)
			}
		})
	}
}

func TestGCForeignRepoLockPreventsEviction(t *testing.T) {
	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCRepoTTL = time.Hour
	bare := newEvictTestRepo(t, d, "ws-foreign", testRepoURL)
	writeLastUsed(t, bare, time.Now().Add(-48*time.Hour))
	foreign := repocache.New(filepath.Join(d.cfg.WorkspacesRoot, reposDirName), slog.Default())
	if err := foreign.WithRepoLock(bare, func() error {
		stats := runRepoGC(d)
		if stats.repoCachesReclaimed != 0 {
			t.Fatal("GC evicted a repo locked by another cache")
		}
		if _, err := os.Stat(bare); err != nil {
			t.Fatal(err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if stats := runRepoGC(d); stats.repoCachesReclaimed != 1 {
		t.Fatalf("unlocked idle repo not reclaimed: %+v", stats)
	}
}

// Legacy metadata has no workspace ID and therefore uses the single-issue
// endpoint instead of the batch path. A completed rerun must survive there too.
func TestGCForeignRootCompletedAgainDuringLegacyIssueCheck(t *testing.T) {
	foreign := t.TempDir()
	params := execenv.RootDirParams{WorkspacesRoot: foreign, WorkspaceID: "ws-race", TaskID: "task-race"}
	claim, err := execenv.ClaimEnvRoot(params)
	if err != nil {
		t.Fatal(err)
	}
	claim.Release()
	root, err := execenv.ResolveRootDir(params)
	if err != nil {
		t.Fatal(err)
	}
	meta := execenv.GCMeta{Kind: execenv.GCKindIssue, IssueID: "issue-race", CompletedAt: time.Now().Add(-10 * 24 * time.Hour)}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gc_meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "rerun-output")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/issues/issue-race/gc-check", func(w http.ResponseWriter, r *http.Request) {
		claim, err := execenv.ClaimEnvRoot(params)
		if err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer claim.Release()
		if err := os.WriteFile(payload, []byte("new execution output"), 0o644); err != nil {
			t.Error(err)
		}
		if err := execenv.WriteGCMeta(root, meta, slog.Default()); err != nil {
			t.Error(err)
		}
		claim.Release()
		json.NewEncoder(w).Encode(map[string]any{"status": "done", "updated_at": time.Now().Add(-10 * 24 * time.Hour)})
	})
	d := newGCTestDaemon(t, mux)
	d.gcRoot(context.Background(), foreign, &gcStats{byPattern: map[string]int{}})
	if _, err := os.Stat(payload); err != nil {
		t.Fatalf("GC deleted the completed foreign rerun: %v", err)
	}
}
