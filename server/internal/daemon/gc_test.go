package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
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

func TestGCWorkspace_BatchesAndDeduplicatesIssueChecks(t *testing.T) {
	doneID := "77777777-7777-7777-7777-777777777771"
	openID := "77777777-7777-7777-7777-777777777772"
	var batchRequests, legacyRequests int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/workspaces/ws-batch/issues/gc-check", func(w http.ResponseWriter, r *http.Request) {
		batchRequests++
		if r.Method != http.MethodPost {
			t.Fatalf("batch method = %s, want POST", r.Method)
		}
		var body struct {
			IssueIDs []string `json:"issue_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode batch request: %v", err)
		}
		if got, want := strings.Join(body.IssueIDs, ","), doneID+","+openID; got != want {
			t.Fatalf("batch issue_ids = %q, want %q", got, want)
		}
		json.NewEncoder(w).Encode(map[string]any{"issues": []map[string]any{
			{"id": doneID, "found": true, "status": "done", "updated_at": time.Now().Add(-10 * 24 * time.Hour)},
			{"id": openID, "found": true, "status": "in_progress", "updated_at": time.Now()},
		}})
	})
	mux.HandleFunc("/api/daemon/issues/", func(w http.ResponseWriter, r *http.Request) {
		legacyRequests++
		http.Error(w, "legacy endpoint must not be called", http.StatusInternalServerError)
	})

	d := newGCTestDaemon(t, mux)
	wsDir := filepath.Join(d.cfg.WorkspacesRoot, "ws-batch")
	doneA := createTaskDir(t, d.cfg.WorkspacesRoot, "ws-batch", "done-a", &execenv.GCMeta{
		IssueID: doneID, WorkspaceID: "ws-batch", CompletedAt: time.Now().Add(-10 * 24 * time.Hour),
	})
	doneB := createTaskDir(t, d.cfg.WorkspacesRoot, "ws-batch", "done-b", &execenv.GCMeta{
		IssueID: doneID, WorkspaceID: "ws-batch", CompletedAt: time.Now().Add(-10 * 24 * time.Hour),
	})
	open := createTaskDir(t, d.cfg.WorkspacesRoot, "ws-batch", "open", &execenv.GCMeta{
		IssueID: openID, WorkspaceID: "ws-batch", CompletedAt: time.Now(),
	})

	stats := &gcStats{byPattern: map[string]int{}}
	d.gcWorkspace(context.Background(), wsDir, stats)

	if batchRequests != 1 || legacyRequests != 0 {
		t.Fatalf("requests: batch=%d legacy=%d, want batch=1 legacy=0", batchRequests, legacyRequests)
	}
	for _, dir := range []string{doneA, doneB} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("done task dir %s was not removed", dir)
		}
	}
	if _, err := os.Stat(open); err != nil {
		t.Fatalf("open task dir should remain: %v", err)
	}
	if stats.cleaned != 2 || stats.skipped != 1 {
		t.Fatalf("stats = cleaned:%d skipped:%d, want 2/1", stats.cleaned, stats.skipped)
	}
}

func TestGCWorkspace_OldServerFallbackIsCached(t *testing.T) {
	issueA := "77777777-7777-7777-7777-777777777773"
	issueB := "77777777-7777-7777-7777-777777777774"
	var batchRequests, legacyRequests int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/workspaces/", func(w http.ResponseWriter, r *http.Request) {
		batchRequests++
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/daemon/issues/", func(w http.ResponseWriter, r *http.Request) {
		legacyRequests++
		json.NewEncoder(w).Encode(map[string]any{
			"status": "done", "updated_at": time.Now().Add(-10 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	for i, tc := range []struct {
		workspace string
		issueID   string
	}{{"ws-legacy-a", issueA}, {"ws-legacy-b", issueB}} {
		wsDir := filepath.Join(d.cfg.WorkspacesRoot, tc.workspace)
		taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, tc.workspace, fmt.Sprintf("task-%d", i), &execenv.GCMeta{
			IssueID: tc.issueID, WorkspaceID: tc.workspace, CompletedAt: time.Now().Add(-10 * 24 * time.Hour),
		})
		d.gcWorkspace(context.Background(), wsDir, &gcStats{byPattern: map[string]int{}})
		if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
			t.Fatalf("legacy fallback did not clean %s", taskDir)
		}
	}

	if batchRequests != 1 {
		t.Fatalf("batch requests = %d, want 1 capability probe", batchRequests)
	}
	if legacyRequests != 2 {
		t.Fatalf("legacy requests = %d, want 2", legacyRequests)
	}
}

func TestGCWorkspace_BatchFailureDoesNotFanOutOrClean(t *testing.T) {
	issueID := "77777777-7777-7777-7777-777777777775"
	var batchRequests, legacyRequests int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/workspaces/ws-fail/issues/gc-check", func(w http.ResponseWriter, r *http.Request) {
		batchRequests++
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/daemon/issues/", func(w http.ResponseWriter, r *http.Request) {
		legacyRequests++
		json.NewEncoder(w).Encode(map[string]any{
			"status": "done", "updated_at": time.Now().Add(-10 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	wsDir := filepath.Join(d.cfg.WorkspacesRoot, "ws-fail")
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws-fail", "task", &execenv.GCMeta{
		IssueID: issueID, WorkspaceID: "ws-fail", CompletedAt: time.Now().Add(-10 * 24 * time.Hour),
	})
	stats := &gcStats{byPattern: map[string]int{}}
	d.gcWorkspace(context.Background(), wsDir, stats)

	if batchRequests != 1 || legacyRequests != 0 {
		t.Fatalf("requests: batch=%d legacy=%d, want batch=1 legacy=0", batchRequests, legacyRequests)
	}
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatalf("task dir must remain on batch failure: %v", err)
	}
	if stats.cleaned != 0 || stats.orphaned != 0 || stats.skipped != 1 {
		t.Fatalf("unexpected stats after failure: %+v", stats)
	}
}

func TestGCWorkspace_ReclaimsLegacyCodexSandboxWithoutConfiguredPatterns(t *testing.T) {
	issueID := "77777777-7777-7777-7777-777777777776"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/workspaces/ws-legacy-codex/issues/gc-check", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issues": []map[string]any{
			{"id": issueID, "found": true, "status": "in_progress", "updated_at": time.Now()},
		}})
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCArtifactPatterns = nil
	wsDir := filepath.Join(d.cfg.WorkspacesRoot, "ws-legacy-codex")
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws-legacy-codex", "v0.4.0-task", nil)
	// Raw v0.4.0 metadata shape: no migration or newly introduced marker is
	// needed for an already-existing task to become eligible after upgrade.
	legacyMeta := fmt.Sprintf(
		`{"kind":"issue","issue_id":%q,"workspace_id":"ws-legacy-codex","completed_at":%q,"local_directory":true}`,
		issueID,
		time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339Nano),
	)
	if err := os.WriteFile(filepath.Join(taskDir, ".gc_meta.json"), []byte(legacyMeta), 0o644); err != nil {
		t.Fatal(err)
	}
	// A tiny representative file keeps the regression fast; the reported
	// Windows executable is about 325 MiB but has identical GC semantics.
	writeFile(t, filepath.Join(taskDir, "codex-home/.sandbox-bin/codex.exe"), 325)
	for _, rel := range []string{
		"codex-home/auth.json",
		"codex-home/config.toml",
		"codex-home/sessions/session.jsonl",
		"output/result.txt",
		"logs/task.log",
		"workdir/repo/.sandbox-bin/user-owned",
	} {
		writeFile(t, filepath.Join(taskDir, filepath.FromSlash(rel)), 10)
	}

	stats := &gcStats{byPattern: map[string]int{}}
	d.gcWorkspace(context.Background(), wsDir, stats)

	if _, err := os.Stat(filepath.Join(taskDir, "codex-home/.sandbox-bin")); !os.IsNotExist(err) {
		t.Fatalf("managed Codex sandbox should be removed, stat err=%v", err)
	}
	for _, rel := range []string{
		"codex-home/auth.json",
		"codex-home/config.toml",
		"codex-home/sessions/session.jsonl",
		"output/result.txt",
		"logs/task.log",
		"workdir/repo/.sandbox-bin/user-owned",
		".gc_meta.json",
	} {
		if _, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to be preserved: %v", rel, err)
		}
	}
	managedPattern := managedArtifactPatternPrefix + "codex-home/.sandbox-bin"
	if stats.artifactDirs != 1 || stats.artifactRemoved != 1 || stats.bytesReclaimed != 325 || stats.skipped != 1 {
		t.Fatalf("unexpected managed cleanup stats: %+v", stats)
	}
	if stats.byPattern[managedPattern] != 1 {
		t.Fatalf("managed pattern stats = %+v, want %q=1", stats.byPattern, managedPattern)
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

func TestShouldCleanTaskDir_LegacyMetaUsesManagedOnlyFallback(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-88888888888c"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "in_progress", "updated_at": time.Now(),
		})
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCOrphanTTL = 72 * time.Hour
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "legacy-meta", &execenv.GCMeta{
		Kind: execenv.GCKindIssue, IssueID: issueID, WorkspaceID: "ws1",
	})
	old := time.Now().Add(-73 * time.Hour)
	if err := os.Chtimes(filepath.Join(taskDir, ".gc_meta.json"), old, old); err != nil {
		t.Fatal(err)
	}

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionCleanManagedArtifacts {
		t.Fatalf("expected managed-only fallback for stale legacy meta, got %d", action)
	}

	recentDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "recent-legacy-meta", &execenv.GCMeta{
		Kind: execenv.GCKindIssue, IssueID: issueID, WorkspaceID: "ws1",
	})
	// Nested activity may leave taskDir's own mtime stale. A recently rewritten
	// metadata file is the authoritative fallback and must defer cleanup.
	if err := os.Chtimes(recentDir, old, old); err != nil {
		t.Fatal(err)
	}
	if action := d.shouldCleanTaskDir(context.Background(), recentDir); action != gcActionSkip {
		t.Fatalf("expected recent legacy meta to remain untouched, got %d", action)
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

func TestShouldCleanTaskDir_ArtifactTTLDisabledSkipsLocalOrphanManagedCleanup(t *testing.T) {
	t.Parallel()
	issueID := "88888888-8888-8888-8888-88888888888d"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCArtifactTTL = 0
	d.cfg.GCOrphanTTL = 0
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "disabled-local-orphan", &execenv.GCMeta{
		Kind: execenv.GCKindIssue, IssueID: issueID, WorkspaceID: "ws1", LocalDirectory: true,
	})

	if action := d.shouldCleanTaskDir(context.Background(), taskDir); action != gcActionSkip {
		t.Fatalf("expected artifact TTL zero to disable managed local orphan cleanup, got %d", action)
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

func TestCleanTaskArtifacts_ManagedPathIsExactAndDeduplicated(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())

	t.Run("managed only", func(t *testing.T) {
		taskDir := t.TempDir()
		writeFile(t, filepath.Join(taskDir, "codex-home/.sandbox-bin/codex.exe"), 300)
		writeFile(t, filepath.Join(taskDir, "workdir/repo/.sandbox-bin/keep"), 400)
		writeFile(t, filepath.Join(taskDir, "codex-home/auth.json"), 10)

		removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, nil)
		if removed != 1 || bytes != 300 {
			t.Fatalf("removed=%d bytes=%d, want 1/300", removed, bytes)
		}
		if perPattern[managedArtifactPatternPrefix+"codex-home/.sandbox-bin"] != 1 {
			t.Fatalf("unexpected per-pattern stats: %+v", perPattern)
		}
		for _, rel := range []string{"workdir/repo/.sandbox-bin/keep", "codex-home/auth.json"} {
			if _, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(rel))); err != nil {
				t.Errorf("expected %s to be preserved: %v", rel, err)
			}
		}
	})

	t.Run("explicit broad basename", func(t *testing.T) {
		taskDir := t.TempDir()
		writeFile(t, filepath.Join(taskDir, "codex-home/.sandbox-bin/codex.exe"), 300)
		writeFile(t, filepath.Join(taskDir, "workdir/repo/.sandbox-bin/cache"), 400)

		removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, []string{".sandbox-bin"})
		if removed != 2 || bytes != 700 {
			t.Fatalf("removed=%d bytes=%d, want 2/700 without double counting", removed, bytes)
		}
		if perPattern[managedArtifactPatternPrefix+"codex-home/.sandbox-bin"] != 1 || perPattern[".sandbox-bin"] != 1 {
			t.Fatalf("unexpected per-pattern stats: %+v", perPattern)
		}
	})
}

func TestCleanTaskArtifacts_ManagedPathDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())

	for _, tc := range []struct {
		name     string
		linkPath string
	}{
		{name: "leaf", linkPath: "codex-home/.sandbox-bin"},
		{name: "parent", linkPath: "codex-home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskDir := t.TempDir()
			outside := t.TempDir()
			keepFile := filepath.Join(outside, "keep")
			writeFile(t, keepFile, 10)
			linkPath := filepath.Join(taskDir, filepath.FromSlash(tc.linkPath))
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, linkPath); err != nil {
				t.Skipf("symlink not supported: %v", err)
			}

			removed, _, _ := d.cleanTaskArtifacts(taskDir, nil)
			if removed != 0 {
				t.Fatalf("removed=%d, want 0 for symlinked managed path", removed)
			}
			if _, err := os.Stat(keepFile); err != nil {
				t.Fatalf("symlink target was touched: %v", err)
			}
		})
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

func TestReserveEnvRootForGCIsExclusive(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	root := "/tmp/fake/gc-reservation"

	d.markActiveEnvRoot(root)
	if _, ok := d.reserveEnvRootForGC(root); ok {
		t.Fatal("GC reservation must fail while a task is active")
	}
	d.unmarkActiveEnvRoot(root)

	release, ok := d.reserveEnvRootForGC(root)
	if !ok {
		t.Fatal("expected GC reservation for inactive env root")
	}
	if _, ok := d.reserveEnvRootForGC(root); ok {
		t.Fatal("second GC reservation must fail while the first is held")
	}
	release()

	release, ok = d.reserveEnvRootForGC(root)
	if !ok {
		t.Fatal("expected GC reservation after release")
	}
	release()
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

func TestPruneWorktree_RemovesOnlyStaleAgentBranches(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	sourceRepo := createGCGitRepo(t)
	barePath := filepath.Join(t.TempDir(), "cache.git")

	runGitForGC(t, "", "clone", "--bare", sourceRepo, barePath)

	activeWorktree := filepath.Join(t.TempDir(), "active")
	activeBranch := "agent/live/12345678"
	staleBranch := "agent/stale/87654321"
	keepBranch := "main"

	runGitForGC(t, "", "-C", barePath, "worktree", "add", "-b", activeBranch, activeWorktree, "HEAD")
	runGitForGC(t, "", "-C", barePath, "branch", staleBranch, "HEAD")

	d.pruneWorktree(barePath)

	if gitRefExists(t, barePath, "refs/heads/"+staleBranch) {
		t.Fatalf("expected stale branch %q to be deleted", staleBranch)
	}
	if !gitRefExists(t, barePath, "refs/heads/"+activeBranch) {
		t.Fatalf("expected active branch %q to be preserved", activeBranch)
	}
	if !gitRefExists(t, barePath, "refs/heads/"+keepBranch) {
		t.Fatalf("expected non-agent branch %q to be preserved", keepBranch)
	}
}

// TestPruneWorktree_IgnoresLiteralAgentBranch ensures the GC pattern is scoped
// to the `agent/` namespace. A repo whose only `agent`-shaped ref is the
// literal `refs/heads/agent` (no slash) must be left untouched — the
// `for-each-ref` query is narrowed to `refs/heads/agent/` for that reason.
func TestPruneWorktree_IgnoresLiteralAgentBranch(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	sourceRepo := createGCGitRepo(t)
	barePath := filepath.Join(t.TempDir(), "cache.git")

	runGitForGC(t, "", "clone", "--bare", sourceRepo, barePath)
	runGitForGC(t, "", "-C", barePath, "branch", "agent", "HEAD")

	d.pruneWorktree(barePath)

	if !gitRefExists(t, barePath, "refs/heads/agent") {
		t.Fatal("expected literal `agent` branch outside the daemon namespace to be preserved")
	}
}

// TestPruneWorktree_SkipsMaintenanceWhenNothingDeleted pins the gate that
// keeps the heavy `gc --prune` step from running on every GC tick. Uses an
// unreachable loose blob backdated past the prune horizon as a sentinel: it
// survives when no agent branch was deleted (no maintenance), and disappears
// once a stale agent branch is reaped (maintenance ran).
func TestPruneWorktree_SkipsMaintenanceWhenNothingDeleted(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	sourceRepo := createGCGitRepo(t)
	barePath := filepath.Join(t.TempDir(), "cache.git")

	runGitForGC(t, "", "clone", "--bare", sourceRepo, barePath)

	// Park an active agent worktree so the scan has something to filter, and
	// to make sure pruneWorktree exercises the full code path.
	activeWorktree := filepath.Join(t.TempDir(), "active")
	runGitForGC(t, "", "-C", barePath, "worktree", "add", "-b", "agent/live/12345678", activeWorktree, "HEAD")

	sentinelPath := writeOldLooseBlob(t, barePath, "sentinel-content", 60*24*time.Hour)

	// No stale agent branch → no deletion → no `gc --prune`. The sentinel
	// blob must survive.
	d.pruneWorktree(barePath)
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("expected sentinel blob to survive when nothing was deleted: %v", err)
	}

	// Introduce a stale agent branch → deletion happens → maintenance runs →
	// `gc --prune=30.days` reaps the sentinel blob.
	runGitForGC(t, "", "-C", barePath, "branch", "agent/stale/87654321", "HEAD")
	d.pruneWorktree(barePath)
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatalf("expected sentinel blob to be pruned after maintenance ran, stat err=%v", err)
	}
}

// writeOldLooseBlob writes a dangling loose-object blob to the bare repo and
// backdates its mtime so `git gc --prune=30.days` will consider it prunable.
// Returns the absolute path to the loose object on disk.
func writeOldLooseBlob(t *testing.T, barePath, content string, age time.Duration) string {
	t.Helper()
	cmd := exec.Command("git", "-C", barePath, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hash-object failed: %v: %s", err, out)
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) < 4 {
		t.Fatalf("unexpected sha output: %q", sha)
	}
	loose := filepath.Join(barePath, "objects", sha[:2], sha[2:])
	if _, err := os.Stat(loose); err != nil {
		t.Fatalf("expected loose object at %s: %v", loose, err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(loose, old, old); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}
	return loose
}

func TestPruneWorktree_SerializesWithCreateWorktree(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	sourceRepo := createGCGitRepo(t)
	cache := repocache.New(filepath.Join(d.cfg.WorkspacesRoot, ".repos"), slog.Default())
	if err := cache.Sync("ws1", []repocache.RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("cache sync failed: %v", err)
	}

	barePath := cache.Lookup("ws1", sourceRepo)
	if barePath == "" {
		t.Fatal("expected bare repo to be cached")
	}

	runGitForGC(t, "", "-C", barePath, "branch", "agent/stale/87654321", "HEAD")

	blockingCache := &blockingRepoCache{
		inner:   cache,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	d.repoCache = blockingCache

	pruneDone := make(chan struct{})
	go func() {
		d.pruneWorktree(barePath)
		close(pruneDone)
	}()

	select {
	case <-blockingCache.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pruneWorktree to acquire repo lock")
	}

	createDone := make(chan error, 1)
	go func() {
		_, err := blockingCache.CreateWorktree(repocache.WorktreeParams{
			WorkspaceID: "ws1",
			RepoURL:     sourceRepo,
			WorkDir:     t.TempDir(),
			AgentName:   "tester",
			TaskID:      "11111111-1111-1111-1111-111111111111",
		})
		createDone <- err
	}()

	select {
	case err := <-createDone:
		t.Fatalf("CreateWorktree should wait for GC lock, returned early with err=%v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(blockingCache.release)

	select {
	case err := <-createDone:
		if err != nil {
			t.Fatalf("CreateWorktree failed after GC lock released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CreateWorktree after releasing GC lock")
	}

	select {
	case <-pruneDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pruneWorktree to finish")
	}
}

type blockingRepoCache struct {
	inner   *repocache.Cache
	entered chan struct{}
	release chan struct{}
}

func (c *blockingRepoCache) Lookup(workspaceID, url string) string {
	return c.inner.Lookup(workspaceID, url)
}

func (c *blockingRepoCache) Sync(workspaceID string, repos []repocache.RepoInfo) error {
	return c.inner.Sync(workspaceID, repos)
}

func (c *blockingRepoCache) WithRepoLock(barePath string, fn func() error) error {
	return c.inner.WithRepoLock(barePath, func() error {
		close(c.entered)
		<-c.release
		return fn()
	})
}

func (c *blockingRepoCache) CreateWorktree(params repocache.WorktreeParams) (*repocache.WorktreeResult, error) {
	return c.inner.CreateWorktree(params)
}

// TestShouldCleanTaskDir_KindDispatch covers the four GCMeta kinds across
// active / terminal / 404 / non-terminal axes. Each entry stands up a mock
// server returning the expected payload (or 404) and asserts the action.
func TestShouldCleanTaskDir_KindDispatch(t *testing.T) {
	t.Parallel()

	const (
		issueID    = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa01"
		chatID     = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01"
		runID      = "cccccccc-cccc-cccc-cccc-cccccccccc01"
		quickTask  = "dddddddd-dddd-dddd-dddd-dddddddddd01"
		legacyMeta = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeee01"
	)

	now := time.Now()
	overTTL := now.Add(-10 * 24 * time.Hour)
	withinTTL := now.Add(-1 * time.Hour)

	type serverResp struct {
		// Path to register on the mux. Empty entries are skipped (used for
		// 404 cases where the mux returns the default not-found handler).
		path   string
		status int
		body   map[string]any
	}

	cases := []struct {
		name    string
		meta    *execenv.GCMeta
		servers []serverResp
		want    gcAction
	}{
		// ---- chat ---------------------------------------------------------
		{
			name: "chat active session — never reclaimed",
			meta: &execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: chatID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/chat-sessions/" + chatID + "/gc-check",
				body: map[string]any{"status": "active", "updated_at": overTTL},
			}},
			want: gcActionSkip,
		},
		{
			name: "chat archived over TTL — clean",
			meta: &execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: chatID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/chat-sessions/" + chatID + "/gc-check",
				body: map[string]any{"status": "archived", "updated_at": overTTL},
			}},
			want: gcActionClean,
		},
		{
			name: "chat archived within TTL — skip",
			meta: &execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: chatID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/chat-sessions/" + chatID + "/gc-check",
				body: map[string]any{"status": "archived", "updated_at": withinTTL},
			}},
			want: gcActionSkip,
		},
		{
			name: "chat 404 — hard-deleted, clean immediately (no mtime gate)",
			meta: &execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: chatID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path:   "/api/daemon/chat-sessions/" + chatID + "/gc-check",
				status: http.StatusNotFound,
			}},
			want: gcActionClean,
		},

		// ---- autopilot run -----------------------------------------------
		{
			name: "autopilot completed over TTL — clean",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "completed", "completed_at": overTTL},
			}},
			want: gcActionClean,
		},
		{
			name: "autopilot issue_created counts as terminal",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "issue_created", "completed_at": overTTL},
			}},
			want: gcActionClean,
		},
		{
			name: "autopilot running — skip",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "running"},
			}},
			want: gcActionSkip,
		},
		{
			name: "autopilot pending — skip",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "pending"},
			}},
			want: gcActionSkip,
		},
		{
			// The directory is never reused, so a terminal run is reclaimed on
			// sight — the recent completed_at no longer buys it a 24h reprieve.
			name: "autopilot completed within TTL — clean immediately (no 24h gate)",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "completed", "completed_at": withinTTL},
			}},
			want: gcActionClean,
		},
		{
			// Terminal status with no completed_at stamp at all still cleans —
			// GC keys purely on the terminal status, not on any timestamp.
			name: "autopilot skipped with no completed_at — clean",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "skipped"},
			}},
			want: gcActionClean,
		},
		{
			name: "autopilot failed — clean",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/autopilot-runs/" + runID + "/gc-check",
				body: map[string]any{"status": "failed"},
			}},
			want: gcActionClean,
		},

		// ---- quick-create -------------------------------------------------
		{
			name: "quick_create completed task — clean immediately",
			meta: &execenv.GCMeta{Kind: execenv.GCKindQuickCreate, TaskID: quickTask, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/tasks/" + quickTask + "/gc-check",
				body: map[string]any{"status": "completed", "completed_at": withinTTL},
			}},
			want: gcActionClean,
		},
		{
			name: "quick_create cancelled — clean",
			meta: &execenv.GCMeta{Kind: execenv.GCKindQuickCreate, TaskID: quickTask, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/tasks/" + quickTask + "/gc-check",
				body: map[string]any{"status": "cancelled"},
			}},
			want: gcActionClean,
		},
		{
			name: "quick_create still running — skip",
			meta: &execenv.GCMeta{Kind: execenv.GCKindQuickCreate, TaskID: quickTask, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/tasks/" + quickTask + "/gc-check",
				body: map[string]any{"status": "running"},
			}},
			want: gcActionSkip,
		},

		// ---- legacy meta (no kind) → issue path ---------------------------
		{
			name: "legacy meta with no kind defaults to issue path — done over TTL = clean",
			meta: &execenv.GCMeta{IssueID: legacyMeta, WorkspaceID: "ws"},
			servers: []serverResp{{
				path: "/api/daemon/issues/" + legacyMeta + "/gc-check",
				body: map[string]any{"status": "done", "updated_at": overTTL},
			}},
			want: gcActionClean,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			for _, s := range tc.servers {
				if s.path == "" {
					continue
				}
				resp := s
				mux.HandleFunc(resp.path, func(w http.ResponseWriter, r *http.Request) {
					if resp.status != 0 {
						w.WriteHeader(resp.status)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(resp.body)
				})
			}
			d := newGCTestDaemon(t, mux)
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", tc.name, tc.meta)
			got := d.shouldCleanTaskDir(context.Background(), taskDir)
			if got != tc.want {
				t.Fatalf("kind dispatch %q: want %d, got %d", tc.name, tc.want, got)
			}
		})
	}
}

func TestShouldCleanTaskDir_EmptyParentIDFallsBackToOrphanMTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		meta *execenv.GCMeta
	}{
		{
			name: "legacy issue meta",
			meta: &execenv.GCMeta{WorkspaceID: "ws"},
		},
		{
			name: "issue meta",
			meta: &execenv.GCMeta{Kind: execenv.GCKindIssue, WorkspaceID: "ws"},
		},
		{
			name: "chat meta",
			meta: &execenv.GCMeta{Kind: execenv.GCKindChat, WorkspaceID: "ws"},
		},
		{
			name: "autopilot run meta",
			meta: &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, WorkspaceID: "ws"},
		},
		{
			name: "quick create meta",
			meta: &execenv.GCMeta{Kind: execenv.GCKindQuickCreate, WorkspaceID: "ws"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requests := 0
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.Error(w, "unexpected request", http.StatusBadRequest)
			})

			d := newGCTestDaemon(t, mux)
			d.cfg.GCOrphanTTL = 365 * 24 * time.Hour
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", tc.name, tc.meta)

			got := d.shouldCleanTaskDir(context.Background(), taskDir)
			if got != gcActionSkip {
				t.Fatalf("empty parent id should skip while under orphan TTL, got %d", got)
			}
			if requests != 0 {
				t.Fatalf("empty parent id should not call gc-check endpoint, got %d requests", requests)
			}

			old := time.Now().Add(-400 * 24 * time.Hour)
			if err := os.Chtimes(taskDir, old, old); err != nil {
				t.Fatalf("chtimes: %v", err)
			}
			got = d.shouldCleanTaskDir(context.Background(), taskDir)
			if got != gcActionOrphan {
				t.Fatalf("empty parent id over orphan TTL should orphan, got %d", got)
			}
			if requests != 0 {
				t.Fatalf("empty parent id should not call gc-check endpoint after mtime fallback, got %d requests", requests)
			}
		})
	}
}

func createGCGitRepo(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	runGitForGC(t, repoDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitForGC(t, repoDir, "add", "README.md")
	runGitForGC(t, repoDir, "commit", "-m", "initial commit")
	return repoDir
}

func runGitForGC(t *testing.T, dir string, args ...string) string {
	t.Helper()

	fullArgs := args
	if dir != "" {
		fullArgs = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", fullArgs...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(fullArgs, " "), out, err)
	}
	return strings.TrimSpace(string(out))
}

func gitRefExists(t *testing.T, repoPath, ref string) bool {
	t.Helper()

	cmd := exec.Command("git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// TestShouldCleanTaskDir_ChatHardDeletedFreshMtime locks acceptance #3:
// when a user hard-deletes a chat session, the workdir must be reclaimed
// on the next GC cycle (≤ GCInterval), not deferred to GCOrphanTTL. A
// directory that was just created (mtime well within GCOrphanTTL) but
// whose chat session now 404s must therefore return gcActionClean.
func TestShouldCleanTaskDir_ChatHardDeletedFreshMtime(t *testing.T) {
	t.Parallel()
	chatID := "ffffffff-ffff-ffff-ffff-ffffffffff02"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/chat-sessions/%s/gc-check", chatID), func(w http.ResponseWriter, r *http.Request) {
		// Simulate hard-deleted session (DeleteChatSession ran).
		w.WriteHeader(http.StatusNotFound)
	})

	d := newGCTestDaemon(t, mux)
	// Crank GCOrphanTTL up so the mtime path is unmistakably not in play —
	// the only way the directory gets reclaimed is the chat-404 fast path.
	d.cfg.GCOrphanTTL = 365 * 24 * time.Hour
	meta := &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now(),
	}
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "hard-deleted-chat", meta)
	// taskDir mtime is now-ish — well within any sane GCOrphanTTL.

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionClean {
		t.Fatalf("hard-deleted chat with fresh mtime must clean immediately, got %d", got)
	}
}

// TestShouldCleanTaskDir_ChatActiveResistsOldMtime is the explicit acceptance
// criterion #2: an active chat session whose workdir is older than
// GCOrphanTTL must NOT be reclaimed. The only path to clean an active
// session's workdir is for the user to archive or hard-delete the session.
func TestShouldCleanTaskDir_ChatActiveResistsOldMtime(t *testing.T) {
	t.Parallel()
	chatID := "ffffffff-ffff-ffff-ffff-ffffffffff01"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/chat-sessions/%s/gc-check", chatID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "active",
			"updated_at": time.Now().Add(-100 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCOrphanTTL = 0 // every directory is "older than orphan TTL"
	meta := &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-200 * 24 * time.Hour),
	}
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "active-chat", meta)
	if err := os.Chtimes(taskDir, time.Now().Add(-200*24*time.Hour), time.Now().Add(-200*24*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
		t.Fatalf("active chat session must not be reclaimed even with stale mtime, got %d", got)
	}
}

// TestGCMetaForTask covers the discriminator priority used by the daemon
// when selecting which GCMetaKind to write at task completion.
func TestGCMetaForTask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		task Task
		want execenv.GCMetaKind
		idOK func(m execenv.GCMeta) bool
	}{
		{
			name: "chat task",
			task: Task{ID: "t1", WorkspaceID: "ws", ChatSessionID: "c1"},
			want: execenv.GCKindChat,
			idOK: func(m execenv.GCMeta) bool { return m.ChatSessionID == "c1" },
		},
		{
			name: "autopilot run task",
			task: Task{ID: "t2", WorkspaceID: "ws", AutopilotRunID: "r1"},
			want: execenv.GCKindAutopilotRun,
			idOK: func(m execenv.GCMeta) bool { return m.AutopilotRunID == "r1" },
		},
		{
			name: "issue task",
			task: Task{ID: "t3", WorkspaceID: "ws", IssueID: "i1"},
			want: execenv.GCKindIssue,
			idOK: func(m execenv.GCMeta) bool { return m.IssueID == "i1" },
		},
		{
			name: "quick-create task — issue_id always empty at WriteGCMeta time",
			task: Task{ID: "t4", WorkspaceID: "ws", QuickCreatePrompt: "do the thing"},
			want: execenv.GCKindQuickCreate,
			idOK: func(m execenv.GCMeta) bool { return m.TaskID == "t4" },
		},
		{
			name: "chat wins over issue when both set (defensive ordering)",
			task: Task{ID: "t5", WorkspaceID: "ws", IssueID: "i1", ChatSessionID: "c1"},
			want: execenv.GCKindChat,
			idOK: func(m execenv.GCMeta) bool { return m.ChatSessionID == "c1" && m.IssueID == "" },
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, ok := gcMetaForTask(tc.task)
			if !ok {
				t.Fatalf("expected gcMetaForTask to recognize task, got ok=false")
			}
			if meta.Kind != tc.want {
				t.Fatalf("kind: want %q, got %q", tc.want, meta.Kind)
			}
			if !tc.idOK(meta) {
				t.Fatalf("ID field mismatch: %+v", meta)
			}
			if meta.WorkspaceID != "ws" {
				t.Fatalf("workspace_id: want %q, got %q", "ws", meta.WorkspaceID)
			}
		})
	}

	t.Run("unrecognized task — ok=false", func(t *testing.T) {
		t.Parallel()
		_, ok := gcMetaForTask(Task{ID: "tX", WorkspaceID: "ws"})
		if ok {
			t.Fatal("expected gcMetaForTask to return ok=false for task with no IDs")
		}
	})
}

// TestShouldCleanTaskDir_LocalDirectoryNeverClean confirms the GC loop
// never removes the envRoot of a local_directory task even when the parent
// issue is long-since done. Artifact-pattern cleanup is the most that
// should ever happen, so output/ and logs/ stay around for the user.
func TestShouldCleanTaskDir_LocalDirectoryNeverClean(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-30 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "local-task", &execenv.GCMeta{
		Kind:           execenv.GCKindIssue,
		IssueID:        issueID,
		WorkspaceID:    "ws1",
		CompletedAt:    time.Now().Add(-30 * 24 * time.Hour),
		LocalDirectory: true,
	})

	got := d.shouldCleanTaskDir(context.Background(), taskDir)
	if got == gcActionClean {
		t.Fatalf("expected local_directory task to never return gcActionClean, got gcActionClean")
	}
	// Either skip (no patterns configured) or artifact cleanup is OK —
	// what matters is that gcActionClean never fires for local_directory.
	if got != gcActionCleanArtifacts && got != gcActionSkip {
		t.Fatalf("unexpected action for local_directory done issue: %d", got)
	}
}

// TestShouldCleanTaskDir_LocalDirectoryOrphanUsesManagedOnly confirms that
// when the parent issue 404s, a local_directory task preserves its envRoot and
// becomes eligible only for exact daemon-managed artifact cleanup.
func TestShouldCleanTaskDir_LocalDirectoryOrphanUsesManagedOnly(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	d := newGCTestDaemon(t, mux)
	d.cfg.GCOrphanTTL = 0 // any age is "stale" enough to orphan
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "local-orphan", &execenv.GCMeta{
		Kind:           execenv.GCKindIssue,
		IssueID:        issueID,
		WorkspaceID:    "ws1",
		LocalDirectory: true,
	})

	got := d.shouldCleanTaskDir(context.Background(), taskDir)
	if got != gcActionCleanManagedArtifacts {
		t.Fatalf("expected local_directory orphan to use managed-only cleanup, got %d", got)
	}
}

// TestShouldCleanTaskDir_LocalDirectoryFalsePreservesNormalClean is the
// negative control: a regular (non-local_directory) task whose parent issue
// is done + over TTL must still be reclaimed via gcActionClean.
func TestShouldCleanTaskDir_LocalDirectoryFalsePreservesNormalClean(t *testing.T) {
	t.Parallel()
	issueID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "done",
			"updated_at": time.Now().Add(-30 * 24 * time.Hour),
		})
	})

	d := newGCTestDaemon(t, mux)
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "normal-task", &execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     issueID,
		WorkspaceID: "ws1",
		CompletedAt: time.Now().Add(-30 * 24 * time.Hour),
		// LocalDirectory unset (false).
	})

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionClean {
		t.Fatalf("expected gcActionClean for normal task, got %d", got)
	}
}
