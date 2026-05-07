package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// newGCTestDaemon creates a minimal Daemon for GC testing with a mock HTTP server.
func newGCTestDaemon(t *testing.T, handler http.Handler) *Daemon {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	root := t.TempDir()
	cfg := Config{
		WorkspacesRoot:     root,
		GCEnabled:          true,
		GCInterval:         1 * time.Hour,
		GCTTL:              5 * 24 * time.Hour,
		GCOrphanTTL:        30 * 24 * time.Hour,
		GCArtifactTTL:      12 * time.Hour,
		GCArtifactPatterns: []string{"node_modules", ".next", ".turbo"},
	}
	d := New(cfg, slog.Default())
	d.client = NewClient(srv.URL)
	d.client.SetToken("test-token")
	return d
}

// createTaskDir creates a task directory with optional GC metadata.
func createTaskDir(t *testing.T, root, wsID, dirName string, meta *execenv.GCMeta) string {
	t.Helper()
	taskDir := filepath.Join(root, wsID, dirName)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		data, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(taskDir, ".gc_meta.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return taskDir
}

func TestShouldCleanTaskDir_DoneIssueOverTTL(t *testing.T) {
	t.Parallel()
	issueID := "11111111-1111-1111-1111-111111111111"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-10 * 24 * time.Hour), // 10 days ago
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task1", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-10 * 24 * time.Hour),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionClean {
		t.Fatalf("expected gcActionClean, got %d", action)
	}
}

func TestShouldCleanTaskDir_CancelledIssueOverTTL(t *testing.T) {
	t.Parallel()
	issueID := "22222222-2222-2222-2222-222222222222"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "cancelled",
			"updated_at": time.Now().Add(-6 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task2", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionClean {
		t.Fatalf("expected gcActionClean, got %d", action)
	}
}

func TestShouldCleanTaskDir_OpenIssueSkipped(t *testing.T) {
	t.Parallel()
	issueID := "33333333-3333-3333-3333-333333333333"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "in_progress",
			"updated_at": time.Now().Add(-30 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task3", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for open issue, got %d", action)
	}
}

func TestShouldCleanTaskDir_DoneButRecentSkipped(t *testing.T) {
	t.Parallel()
	issueID := "44444444-4444-4444-4444-444444444444"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-1 * 24 * time.Hour), // 1 day ago, within TTL
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task4", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for recently-done issue, got %d", action)
	}
}

func TestShouldCleanTaskDir_NoMetaRecentSkipped(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	// No meta, fresh directory — should skip.
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task5", nil)

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for recent orphan, got %d", action)
	}
}

func TestShouldCleanTaskDir_NoMetaOldOrphan(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCOrphanTTL = 0 // treat all orphans as expired
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task6", nil)

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionOrphan {
		t.Fatalf("expected gcActionOrphan, got %d", action)
	}
}

func TestShouldCleanTaskDir_APIErrorSkipped(t *testing.T) {
	t.Parallel()
	issueID := "55555555-5555-5555-5555-555555555555"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task7", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip on API error, got %d", action)
	}
}

func TestShouldCleanTaskDir_Issue404OldOrphan(t *testing.T) {
	t.Parallel()
	issueID := "66666666-6666-6666-6666-666666666666"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"issue not found"}`))
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCOrphanTTL = 0 // treat orphans as immediately eligible
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "task8", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionOrphan {
		t.Fatalf("expected gcActionOrphan for unreachable issue past TTL, got %d", action)
	}
}

// TestShouldCleanTaskDir_Issue404RecentSkipped locks in the cross-workspace
// safety: the server returns 404 both for deleted issues and for workspaces
// the daemon token can't see, so a recent 404 must NOT trigger immediate
// cleanup — otherwise a token re-scope could wipe dirs whose issues are live.
func TestShouldCleanTaskDir_Issue404RecentSkipped(t *testing.T) {
	t.Parallel()
	issueID := "66666666-6666-6666-6666-666666666667"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})

	d := newGCTestDaemon(t, mux)
	// Default production OrphanTTL; taskDir mtime is now, so it's fresh.
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "fresh-404", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for recent 404 (cross-workspace safety), got %d", action)
	}
}

func TestCleanTaskDir_RemovesDirectory(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "doomed", nil)

	if _, err := os.Stat(taskDir); err != nil {
		t.Fatal("task dir should exist before cleanup")
	}

	d.cleanTaskDir(taskDir)

	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatal("task dir should be removed after cleanup")
	}
}

func TestGcWorkspace_CleansEmptyWorkspaceDir(t *testing.T) {
	t.Parallel()
	issueID := "77777777-7777-7777-7777-777777777777"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-10 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	wsDir := filepath.Join(d.cfg.WorkspacesRoot, "ws-empty")
	createTaskDir(t, d.cfg.WorkspacesRoot, "ws-empty", "only-task", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws-empty",
		CompletedAt: time.Now(),
	})

	d.gcWorkspace(context.Background(), wsDir, &gcStats{byPattern: map[string]int{}})

	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatal("empty workspace dir should be removed after all tasks cleaned")
	}
}

func TestShouldCleanTaskDir_OpenIssueArtifactCleanup(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-888888888888"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "in_progress",
			"updated_at": time.Now(),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "open-task", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-24 * time.Hour),
	})

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionCleanArtifacts {
		t.Fatalf("expected gcActionCleanArtifacts for old completed task on open issue, got %d", action)
	}
}

func TestShouldCleanTaskDir_OpenIssueRecentTaskSkipped(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-888888888889"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "in_progress",
			"updated_at": time.Now(),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "fresh-task", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for fresh completed_at, got %d", action)
	}
}

func TestShouldCleanTaskDir_ActiveEnvRootSkipsArtifactCleanup(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-88888888888a"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "in_progress",
			"updated_at": time.Now(),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "active-task", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-24 * time.Hour),
	})

	d.markActiveEnvRoot(taskDir)
	defer d.unmarkActiveEnvRoot(taskDir)

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip while task is active, got %d", action)
	}
}

func TestShouldCleanTaskDir_ActiveEnvRootSkipsFullCleanup(t *testing.T) {
	t.Parallel()
	issueID := "99999999-9999-9999-9999-999999999999"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Done long enough ago to satisfy GCTTL — this would normally return
		// gcActionClean. But the env root is in use (e.g. follow-up comment
		// dispatched a task that reuses the prior workdir), and CreateComment
		// does not bump issue.updated_at. Active-root guard must override.
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-30 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "active-done", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-30 * 24 * time.Hour),
	})

	d.markActiveEnvRoot(taskDir)
	defer d.unmarkActiveEnvRoot(taskDir)

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip on active env root with done+stale issue, got %d", action)
	}
}

func TestShouldCleanTaskDir_ActiveEnvRootSkipsOrphan404(t *testing.T) {
	t.Parallel()
	issueID := "99999999-9999-9999-9999-99999999999a"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCOrphanTTL = 0 // would normally make this an immediate orphan delete
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "active-404", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	d.markActiveEnvRoot(taskDir)
	defer d.unmarkActiveEnvRoot(taskDir)

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip on active env root with 404 issue, got %d", action)
	}
}

func TestShouldCleanTaskDir_ActiveEnvRootSkipsNoMetaOrphan(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCOrphanTTL = 0
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "active-no-meta", nil)

	d.markActiveEnvRoot(taskDir)
	defer d.unmarkActiveEnvRoot(taskDir)

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip on active env root with no-meta orphan, got %d", action)
	}
}

func TestShouldCleanTaskDir_ArtifactTTLDisabled(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-88888888888b"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "in_progress",
			"updated_at": time.Now(),
		})
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCArtifactTTL = 0
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "no-artifact-gc", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-100 * 24 * time.Hour),
	})

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected gcActionSkip when artifact GC disabled, got %d", action)
	}
}

func TestCleanTaskArtifacts_RemovesOnlyMatchedDirs(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := t.TempDir()

	// Create a synthetic project layout.
	mustMkdir := func(rel string) string {
		p := filepath.Join(taskDir, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mustWrite := func(rel string, content string) {
		p := filepath.Join(taskDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustMkdir("workdir/repo/src")
	mustWrite("workdir/repo/src/index.ts", "console.log('hi')")
	mustMkdir("workdir/repo/.git/objects")
	mustWrite("workdir/repo/.git/objects/pack", "binary")
	mustMkdir("workdir/repo/node_modules/lodash")
	mustWrite("workdir/repo/node_modules/lodash/index.js", "module.exports = {}")
	mustMkdir("workdir/repo/.next/cache")
	mustWrite("workdir/repo/.next/cache/page.html", "<html></html>")
	mustMkdir("workdir/repo/.turbo")
	mustWrite("workdir/repo/.turbo/log", "trace")
	mustMkdir("workdir/repo/dist") // not in default patterns — must be preserved
	mustWrite("workdir/repo/dist/main.js", "compiled")
	mustWrite(".gc_meta.json", `{"issue_id":"x"}`)
	mustMkdir("output")
	mustWrite("output/result.txt", "done")

	removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, []string{"node_modules", ".next", ".turbo"})

	if removed != 3 {
		t.Fatalf("expected 3 artifact dirs removed, got %d", removed)
	}
	if bytes <= 0 {
		t.Fatalf("expected non-zero bytes reclaimed, got %d", bytes)
	}
	if perPattern["node_modules"] != 1 || perPattern[".next"] != 1 || perPattern[".turbo"] != 1 {
		t.Fatalf("unexpected per-pattern counts: %+v", perPattern)
	}

	// Verify protected paths are intact.
	for _, rel := range []string{
		"workdir/repo/src/index.ts",
		"workdir/repo/.git/objects/pack",
		"workdir/repo/dist/main.js",
		"output/result.txt",
		".gc_meta.json",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, rel)); err != nil {
			t.Errorf("expected %s to be preserved, got %v", rel, err)
		}
	}

	// Verify removed paths are gone.
	for _, rel := range []string{
		"workdir/repo/node_modules",
		"workdir/repo/.next",
		"workdir/repo/.turbo",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err=%v", rel, err)
		}
	}
}

func TestCleanTaskArtifacts_RejectsPatternsWithSeparators(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(taskDir, "workdir", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	removed, _, _ := d.cleanTaskArtifacts(taskDir, []string{"workdir/node_modules", "../etc"})
	if removed != 0 {
		t.Fatalf("expected 0 removals from separator-bearing patterns, got %d", removed)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "workdir", "node_modules")); err != nil {
		t.Fatalf("dir should still exist, got %v", err)
	}
}

func TestCleanTaskArtifacts_DoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := t.TempDir()
	outside := t.TempDir()
	keepFile := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(taskDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(taskDir, "workdir", "node_modules")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	removed, _, _ := d.cleanTaskArtifacts(taskDir, []string{"node_modules"})
	if removed != 0 {
		t.Fatalf("expected 0 removals (symlinked node_modules), got %d", removed)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("symlinked target was deleted: %v", err)
	}
}

func TestActiveEnvRootRefcount(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	root := "/tmp/fake/env"

	if d.isActiveEnvRoot(root) {
		t.Fatal("expected inactive before mark")
	}
	d.markActiveEnvRoot(root)
	d.markActiveEnvRoot(root) // second mark from reuse path
	if !d.isActiveEnvRoot(root) {
		t.Fatal("expected active after mark")
	}
	d.unmarkActiveEnvRoot(root)
	if !d.isActiveEnvRoot(root) {
		t.Fatal("expected still active after one unmark")
	}
	d.unmarkActiveEnvRoot(root)
	if d.isActiveEnvRoot(root) {
		t.Fatal("expected inactive after both unmarks")
	}
}

func TestIsBareRepo(t *testing.T) {
	t.Parallel()

	t.Run("valid bare repo", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main"), 0o644)
		os.MkdirAll(filepath.Join(dir, "objects"), 0o755)
		if !isBareRepo(dir) {
			t.Fatal("expected isBareRepo=true for dir with HEAD + objects/")
		}
	})

	t.Run("HEAD only", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main"), 0o644)
		if isBareRepo(dir) {
			t.Fatal("expected isBareRepo=false for dir with only HEAD")
		}
	})

	t.Run("empty dir", func(t *testing.T) {
		dir := t.TempDir()
		if isBareRepo(dir) {
			t.Fatal("expected isBareRepo=false for empty dir")
		}
	})
}
