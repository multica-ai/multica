package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// gcLoop periodically scans local workspace directories and removes those
// whose issue is done/canceled and hasn't been updated within the configured TTL.
func (d *Daemon) gcLoop(ctx context.Context) {
	if !d.cfg.GCEnabled {
		d.logger.Info("gc: disabled")
		return
	}
	d.logger.Info("gc: started", "interval", d.cfg.GCInterval, "ttl", d.cfg.GCTTL, "orphan_ttl", d.cfg.GCOrphanTTL)

	// Run once at startup after a short delay (let the daemon finish initializing).
	if err := sleepWithContext(ctx, 30*time.Second); err != nil {
		return
	}
	d.runGC(ctx)

	ticker := time.NewTicker(d.cfg.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runGC(ctx)
		}
	}
}

// runGC performs a single GC scan across all workspace directories.
func (d *Daemon) runGC(ctx context.Context) {
	root := d.cfg.WorkspacesRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		d.logger.Warn("gc: read workspaces root failed", "error", err)
		return
	}

	var cleaned, skipped, orphaned int
	for _, wsEntry := range entries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		c, s, o := d.gcWorkspace(ctx, wsDir)
		cleaned += c
		skipped += s
		orphaned += o
	}

	// Prune stale worktree references from all bare repo caches.
	d.pruneRepoWorktrees(root)

	if cleaned > 0 || orphaned > 0 {
		d.logger.Info("gc: cycle complete", "cleaned", cleaned, "orphaned", orphaned, "skipped", skipped)
	}
}

// gcWorkspace scans task directories inside a single workspace directory.
func (d *Daemon) gcWorkspace(ctx context.Context, wsDir string) (cleaned, skipped, orphaned int) {
	taskEntries, err := os.ReadDir(wsDir)
	if err != nil {
		d.logger.Warn("gc: read workspace dir failed", "dir", wsDir, "error", err)
		return
	}

	for _, entry := range taskEntries {
		if ctx.Err() != nil {
			return
		}
		if !entry.IsDir() {
			continue
		}
		taskDir := filepath.Join(wsDir, entry.Name())
		action := d.shouldCleanTaskDir(ctx, taskDir)
		switch action {
		case gcActionClean:
			d.cleanTaskDir(taskDir)
			cleaned++
		case gcActionOrphan:
			d.cleanTaskDir(taskDir)
			orphaned++
		default:
			skipped++
		}
	}

	// Remove the workspace directory itself if it's now empty.
	if cleaned+orphaned > 0 {
		remaining, _ := os.ReadDir(wsDir)
		if len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
	return
}

type gcAction int

const (
	gcActionSkip   gcAction = iota
	gcActionClean           // issue is done/canceled and stale
	gcActionOrphan          // no meta or unknown issue and dir is old
)

// shouldCleanTaskDir decides whether a task directory should be removed.
func (d *Daemon) shouldCleanTaskDir(ctx context.Context, taskDir string) gcAction {
	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil {
		// No .gc_meta.json — check mtime for orphan cleanup.
		info, statErr := os.Stat(taskDir)
		if statErr != nil {
			return gcActionSkip
		}
		if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
			d.logger.Info("gc: orphan directory (no meta)", "dir", taskDir, "age", time.Since(info.ModTime()).Round(time.Hour))
			return gcActionOrphan
		}
		return gcActionSkip
	}

	status, err := d.client.GetIssueGCCheck(ctx, meta.IssueID)
	if err != nil {
		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound {
			// Issue deleted — check mtime for orphan cleanup.
			info, statErr := os.Stat(taskDir)
			if statErr != nil {
				return gcActionSkip
			}
			if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
				d.logger.Info("gc: orphan directory (issue deleted)", "dir", taskDir, "issue", meta.IssueID)
				return gcActionOrphan
			}
		}
		// API error (network, auth, etc.) — skip and retry next cycle.
		return gcActionSkip
	}

	if (status.Status == "done" || status.Status == "canceled") &&
		time.Since(status.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"issue", meta.IssueID,
			"status", status.Status,
			"updated_at", status.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	return gcActionSkip
}

// cleanTaskDir removes a task directory and logs the result.
func (d *Daemon) cleanTaskDir(taskDir string) {
	if err := os.RemoveAll(taskDir); err != nil {
		d.logger.Warn("gc: remove task dir failed", "dir", taskDir, "error", err)
	} else {
		d.logger.Info("gc: removed", "dir", taskDir)
	}
}

const gitCmdTimeout = 30 * time.Second

// pruneRepoWorktrees runs `git worktree prune` on all bare repos in the cache.
func (d *Daemon) pruneRepoWorktrees(workspacesRoot string) {
	reposRoot := filepath.Join(workspacesRoot, ".repos")
	wsEntries, err := os.ReadDir(reposRoot)
	if err != nil {
		return
	}

	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		wsRepoDir := filepath.Join(reposRoot, wsEntry.Name())
		repoEntries, err := os.ReadDir(wsRepoDir)
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			barePath := filepath.Join(wsRepoDir, repoEntry.Name())
			if !isBareRepo(barePath) {
				continue
			}
			d.pruneWorktree(barePath)
		}
	}
}

func (d *Daemon) pruneWorktree(barePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", barePath, "worktree", "prune")
	if out, err := cmd.CombinedOutput(); err != nil {
		d.logger.Warn("gc: worktree prune failed",
			"repo", barePath,
			"output", strings.TrimSpace(string(out)),
			"error", err,
		)
	}
}

// isBareRepo checks if a path looks like a bare git repository.
func isBareRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "objects")); err != nil {
		return false
	}
	return true
}
