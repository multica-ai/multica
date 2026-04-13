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
		WorkspacesRoot: root,
		GCEnabled:      true,
		GCInterval:     1 * time.Hour,
		GCTTL:          5 * 24 * time.Hour,
		GCOrphanTTL:    30 * 24 * time.Hour,
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

func TestShouldCleanTaskDir_CanceledIssueOverTTL(t *testing.T) {
	t.Parallel()
	issueID := "22222222-2222-2222-2222-222222222222"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "canceled",
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
		t.Fatalf("expected gcActionOrphan for deleted issue, got %d", action)
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

	d.gcWorkspace(context.Background(), wsDir)

	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatal("empty workspace dir should be removed after all tasks cleaned")
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
