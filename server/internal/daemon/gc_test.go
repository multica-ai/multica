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
		GCDoneInterval: 30 * time.Second,
		GCDoneTTL:      30 * time.Second,
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

	d.gcWorkspace(context.Background(), wsDir)

	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Fatal("empty workspace dir should be removed after all tasks cleaned")
	}
}

// ---------------------------------------------------------------------------
// Fast-tier (gcDoneLoop) tests — contract 09 §10 terminal hygiene.
// ---------------------------------------------------------------------------

// TestGCDoneFastTier_DeletesDoneWorkdirAfterTTL asserts that a workdir whose
// issue is in `done` and whose updated_at is older than GCDoneTTL is deleted
// on the next fast-tier scan.
func TestGCDoneFastTier_DeletesDoneWorkdirAfterTTL(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-1111-1111-1111-111111111111"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":          "done",
			"updated_at":      time.Now().Add(-1 * time.Minute), // beyond 30s GCDoneTTL
			"has_active_task": false,
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "fast-clean", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})

	action := d.shouldCleanDoneTaskDir(context.Background(), taskDir)
	if action != gcActionClean {
		t.Fatalf("expected gcActionClean for done+TTL+!active, got %d", action)
	}

	// runGCDone end-to-end: actually delete.
	d.runGCDone(context.Background())
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatal("task dir should be removed after fast-tier scan")
	}
}

// TestGCDoneFastTier_RespectsGracePeriod asserts that a workdir whose issue
// just flipped to `done` (within GCDoneTTL) is NOT deleted on this cycle.
func TestGCDoneFastTier_RespectsGracePeriod(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-2222-2222-2222-222222222222"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":          "done",
			"updated_at":      time.Now().Add(-1 * time.Second), // inside grace window
			"has_active_task": false,
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "fresh-done", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now(),
	})

	action := d.shouldCleanDoneTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for done within grace window, got %d", action)
	}
}

// TestGCDoneFastTier_StrictRaceGuard asserts that even if status=done, a
// workdir is NOT deleted while has_active_task=true.
func TestGCDoneFastTier_StrictRaceGuard(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-3333-3333-3333-333333333333"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":          "done",
			"updated_at":      time.Now().Add(-1 * time.Minute),
			"has_active_task": true, // strict race guard
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "race-skip", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})

	action := d.shouldCleanDoneTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip when has_active_task=true, got %d", action)
	}
}

// TestGCDoneFastTier_OldServerNilFieldRefusesDelete asserts deploy-safety:
// when the server omits has_active_task entirely (old server, daemon
// upgraded first), the fast tier MUST refuse to delete.
func TestGCDoneFastTier_OldServerNilFieldRefusesDelete(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-4444-4444-4444-444444444444"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Note: NO has_active_task field — simulates an old server.
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-1 * time.Minute),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "old-server", &execenv.GCMeta{
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})

	action := d.shouldCleanDoneTaskDir(context.Background(), taskDir)
	if action != gcActionSkip {
		t.Fatalf("expected gcActionSkip for nil has_active_task (old server), got %d", action)
	}

	// Sanity-check: a runGCDone cycle must NOT delete the dir.
	d.runGCDone(context.Background())
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatal("task dir must remain when server returns no has_active_task field")
	}
}

// TestGCDoneFastTier_IgnoresCancelledAndBlocked asserts that the fast tier
// never touches cancelled/blocked workdirs (slow tier owns those).
func TestGCDoneFastTier_IgnoresCancelledAndBlocked(t *testing.T) {
	t.Parallel()
	cancelledID := "aaaaaaaa-5555-5555-5555-555555555555"
	blockedID := "aaaaaaaa-6666-6666-6666-666666666666"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", cancelledID), func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status":          "canceled",
			"updated_at":      time.Now().Add(-1 * time.Minute),
			"has_active_task": false,
		})
	})
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", blockedID), func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status":          "blocked",
			"updated_at":      time.Now().Add(-1 * time.Minute),
			"has_active_task": false,
		})
	})

	d := newGCTestDaemon(t, mux)
	cancelledDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "cancelled-task", &execenv.GCMeta{
		IssueID:     cancelledID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})
	blockedDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "blocked-task", &execenv.GCMeta{
		IssueID:     blockedID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-1 * time.Minute),
	})

	if a := d.shouldCleanDoneTaskDir(context.Background(), cancelledDir); a != gcActionSkip {
		t.Fatalf("expected gcActionSkip for cancelled, got %d", a)
	}
	if a := d.shouldCleanDoneTaskDir(context.Background(), blockedDir); a != gcActionSkip {
		t.Fatalf("expected gcActionSkip for blocked, got %d", a)
	}

	d.runGCDone(context.Background())
	if _, err := os.Stat(cancelledDir); err != nil {
		t.Fatal("cancelled dir must remain (slow tier owns it)")
	}
	if _, err := os.Stat(blockedDir); err != nil {
		t.Fatal("blocked dir must remain (never reaped)")
	}
}

// TestGCDoneFastTier_IgnoresOrphans asserts the fast tier does not act on
// directories without .gc_meta.json (orphans). Slow tier owns orphans via
// mtime-gated cleanup after GCOrphanTTL.
func TestGCDoneFastTier_IgnoresOrphans(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	d.cfg.GCOrphanTTL = 0 // would be eligible under slow tier
	orphanDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "orphan", nil)

	if a := d.shouldCleanDoneTaskDir(context.Background(), orphanDir); a != gcActionSkip {
		t.Fatalf("expected gcActionSkip for orphan in fast tier, got %d", a)
	}
	d.runGCDone(context.Background())
	if _, err := os.Stat(orphanDir); err != nil {
		t.Fatal("orphan dir must remain after fast-tier scan")
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
