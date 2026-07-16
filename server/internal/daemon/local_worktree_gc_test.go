package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gcCheckHandler routes /api/daemon/issues/<id>/gc-check to a fixed status per
// issue id, so pruneLocalWorktrees can be exercised without a live server.
func gcCheckHandler(t *testing.T, terminal map[string]bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/gc-check"):
			id := idFromGCPath(r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			if terminal[id] {
				// Stale past GCTTL (5 days in newGCTestDaemon).
				old := time.Now().Add(-6 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
				_, _ = w.Write([]byte(`{"status":"done","updated_at":"` + old + `"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"open","updated_at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
}

func idFromGCPath(p string) string {
	// /api/daemon/issues/<id>/gc-check
	p = strings.TrimSuffix(p, "/gc-check")
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func TestPruneLocalWorktrees_RemovesTerminalKeepsActive(t *testing.T) {
	d := newGCTestDaemon(t, gcCheckHandler(t, map[string]bool{"terminal-issue": true}))
	d.cfg.DaemonID = "d-test"

	repo := initLocalRepo(t)
	wtTerminal, _, err := d.ensureIssueWorktree(context.Background(), repo, "terminal-issue", "")
	if err != nil {
		t.Fatalf("ensure terminal: %v", err)
	}
	wtActive, _, err := d.ensureIssueWorktree(context.Background(), repo, "active-issue", "")
	if err != nil {
		t.Fatalf("ensure active: %v", err)
	}

	d.pruneLocalWorktrees(context.Background())

	if _, statErr := os.Stat(wtTerminal); !os.IsNotExist(statErr) {
		t.Errorf("terminal worktree still exists at %s (statErr=%v)", wtTerminal, statErr)
	}
	if _, statErr := os.Stat(wtActive); os.IsNotExist(statErr) {
		t.Errorf("active worktree was pruned at %s", wtActive)
	}
}

func TestPruneLocalWorktrees_SkipsTaskHoldingIssueLock(t *testing.T) {
	d := newGCTestDaemon(t, gcCheckHandler(t, map[string]bool{"terminal-issue": true}))
	d.cfg.DaemonID = "d-test"

	repo := initLocalRepo(t)
	wtPath, _, err := d.ensureIssueWorktree(context.Background(), repo, "terminal-issue", "")
	if err != nil {
		t.Fatalf("ensure terminal: %v", err)
	}

	release, err := d.localPathLocks.Acquire(context.Background(), issueLockKey("terminal-issue"), "running-task", nil)
	if err != nil {
		t.Fatalf("acquire issue lock: %v", err)
	}
	d.pruneLocalWorktrees(context.Background())
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("GC removed worktree held by a running task: %v", statErr)
	}

	release()
	d.pruneLocalWorktrees(context.Background())
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("GC did not remove unlocked terminal worktree: %v", statErr)
	}
}

func TestPruneLocalWorktrees_PreservesDirtyWorktree(t *testing.T) {
	d := newGCTestDaemon(t, gcCheckHandler(t, map[string]bool{"terminal-issue": true}))
	d.cfg.DaemonID = "d-test"

	repo := initLocalRepo(t)
	wtPath, _, err := d.ensureIssueWorktree(context.Background(), repo, "terminal-issue", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "uncommitted.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	d.pruneLocalWorktrees(context.Background())
	if _, err := os.Stat(filepath.Join(wtPath, "uncommitted.txt")); err != nil {
		t.Fatalf("GC discarded uncommitted work: %v", err)
	}
}

func TestPruneLocalWorktrees_NoDaemonIDIsNoop(t *testing.T) {
	d := newGCTestDaemon(t, gcCheckHandler(t, nil))
	d.cfg.DaemonID = "" // daemon not registered yet
	repo := initLocalRepo(t)
	// Pre-create the worktree by temporarily setting an id, then clear it.
	d.cfg.DaemonID = "d-test"
	wt, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-x", "")
	if err != nil {
		t.Fatal(err)
	}
	d.cfg.DaemonID = ""

	d.pruneLocalWorktrees(context.Background())
	// Even a terminal issue must survive when DaemonID is unset: the daemon
	// must not touch another daemon's (or an unknown) worktree subtree.
	if _, statErr := os.Stat(wt); os.IsNotExist(statErr) {
		t.Errorf("worktree removed while DaemonID unset — scope leak")
	}
}
