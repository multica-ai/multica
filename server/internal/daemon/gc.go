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
// whose issue is done/cancelled and hasn't been updated within the configured TTL.
func (d *Daemon) gcLoop(ctx context.Context) {
	if !d.cfg.GCEnabled {
		d.logger.Info("gc: disabled")
		return
	}
	d.logger.Info("gc: started",
		"interval", d.cfg.GCInterval,
		"ttl", d.cfg.GCTTL,
		"orphan_ttl", d.cfg.GCOrphanTTL,
		"artifact_ttl", d.cfg.GCArtifactTTL,
		"artifact_patterns", d.cfg.GCArtifactPatterns,
	)

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

// gcStats accumulates byte counts and per-pattern hit counts for one GC cycle.
type gcStats struct {
	cleaned         int            // whole task dirs removed (issue done/cancelled)
	orphaned        int            // whole task dirs removed (no meta / unreachable issue)
	skipped         int            // task dirs left untouched
	artifactDirs    int            // task dirs that had at least one artifact reclaimed
	artifactRemoved int            // count of removed artifact subdirs
	bytesReclaimed  int64          // total bytes freed in this cycle
	byPattern       map[string]int // basename -> reclaim count, for visibility
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

	stats := &gcStats{byPattern: map[string]int{}}
	for _, wsEntry := range entries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		d.gcWorkspace(ctx, wsDir, stats)
	}

	// Prune stale worktree references from all bare repo caches.
	d.pruneRepoWorktrees(root)

	if stats.cleaned > 0 || stats.orphaned > 0 || stats.artifactDirs > 0 {
		d.logger.Info("gc: cycle complete",
			"cleaned", stats.cleaned,
			"orphaned", stats.orphaned,
			"skipped", stats.skipped,
			"artifact_dirs", stats.artifactDirs,
			"artifact_removed", stats.artifactRemoved,
			"bytes_reclaimed", stats.bytesReclaimed,
			"by_pattern", stats.byPattern,
		)
	}
}

// gcWorkspace scans task directories inside a single workspace directory.
func (d *Daemon) gcWorkspace(ctx context.Context, wsDir string, stats *gcStats) {
	taskEntries, err := os.ReadDir(wsDir)
	if err != nil {
		d.logger.Warn("gc: read workspace dir failed", "dir", wsDir, "error", err)
		return
	}

	cleanedHere := 0
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
			bytes := dirSize(taskDir)
			d.cleanTaskDir(taskDir)
			stats.cleaned++
			stats.bytesReclaimed += bytes
			cleanedHere++
		case gcActionOrphan:
			bytes := dirSize(taskDir)
			d.cleanTaskDir(taskDir)
			stats.orphaned++
			stats.bytesReclaimed += bytes
			cleanedHere++
		case gcActionCleanArtifacts:
			removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, d.cfg.GCArtifactPatterns)
			if removed > 0 {
				stats.artifactDirs++
				stats.artifactRemoved += removed
				stats.bytesReclaimed += bytes
				for k, v := range perPattern {
					stats.byPattern[k] += v
				}
			}
			stats.skipped++ // task dir itself preserved
		default:
			stats.skipped++
		}
	}

	// Remove the workspace directory itself if it's now empty.
	if cleanedHere > 0 {
		remaining, _ := os.ReadDir(wsDir)
		if len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
}

type gcAction int

const (
	gcActionSkip           gcAction = iota
	gcActionClean                   // issue is done/cancelled and stale
	gcActionOrphan                  // no meta or unknown issue and dir is old
	gcActionCleanArtifacts          // task completed long enough ago; drop regenerable artifacts only
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
			// 404 is ambiguous: the server returns it for both "issue deleted"
			// and "daemon token has no access to the workspace" (anti-enumeration,
			// see requireDaemonWorkspaceAccess). Fall back to the mtime-gated
			// orphan cleanup so a scoped-down token can't instantly wipe dirs
			// whose issues are still live.
			info, statErr := os.Stat(taskDir)
			if statErr != nil {
				return gcActionSkip
			}
			if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
				d.logger.Info("gc: orphan directory (issue not accessible)", "dir", taskDir, "issue", meta.IssueID)
				return gcActionOrphan
			}
		}
		// API error (network, auth, etc.) — skip and retry next cycle.
		return gcActionSkip
	}

	if (status.Status == "done" || status.Status == "cancelled") &&
		time.Since(status.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"issue", meta.IssueID,
			"status", status.Status,
			"updated_at", status.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	// Artifact-only cleanup: issue is still open but the task itself completed
	// long enough ago that its build artifacts are unlikely to be reused. Skip
	// when a task is currently running on this env root, when artifact GC is
	// disabled, or when the meta is missing a completed_at (defensive — means
	// the task crashed before WriteGCMeta).
	if d.cfg.GCArtifactTTL > 0 && len(d.cfg.GCArtifactPatterns) > 0 &&
		!meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL &&
		!d.isActiveEnvRoot(taskDir) {
		d.logger.Info("gc: eligible for artifact cleanup",
			"dir", filepath.Base(taskDir),
			"issue", meta.IssueID,
			"status", status.Status,
			"completed_at", meta.CompletedAt.Format(time.RFC3339),
		)
		return gcActionCleanArtifacts
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

// cleanTaskArtifacts walks taskDir and deletes every directory whose basename
// matches one of patterns. Returns (removedCount, bytesReclaimed, perPattern).
//
// Safety contract:
//   - patterns are basename-only; entries with a path separator are dropped.
//   - .git subtrees are never descended into, so the agent's git history stays
//     intact even if a pattern would otherwise match.
//   - symlinks are not followed; if a matching name is a symlink, only the
//     link is removed.
//   - every removal target is verified to live inside taskDir, so a tampered
//     .gc_meta.json can't trick the daemon into deleting outside its sandbox.
func (d *Daemon) cleanTaskArtifacts(taskDir string, patterns []string) (removed int, bytes int64, perPattern map[string]int) {
	perPattern = map[string]int{}
	if taskDir == "" || len(patterns) == 0 {
		return
	}
	patternSet := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		patternSet[p] = struct{}{}
	}
	if len(patternSet) == 0 {
		return
	}

	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}

	walkErr := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort — keep walking
		}
		if path == absRoot {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		// Never descend into .git — preserves agent commits even if a pattern
		// like "objects" would otherwise match.
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		// Refuse to follow symlinked directories. WalkDir reports them as type
		// Dir on some platforms; lstat to be sure.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if _, ok := patternSet[entry.Name()]; !ok {
			return nil
		}
		// Containment check: target must remain inside taskDir.
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
			return filepath.SkipDir
		}
		size := dirSize(path)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			d.logger.Warn("gc: artifact remove failed", "path", path, "error", rmErr)
			return filepath.SkipDir
		}
		removed++
		bytes += size
		perPattern[entry.Name()]++
		d.logger.Info("gc: artifact removed", "path", path, "bytes", size)
		// Don't descend into the now-deleted subtree.
		return filepath.SkipDir
	})
	if walkErr != nil {
		d.logger.Warn("gc: artifact walk failed", "dir", taskDir, "error", walkErr)
	}
	return
}

// dirSize returns the total size of all regular files under root, in bytes.
// Non-fatal: errors during the walk are ignored so callers can report a
// best-effort byte count without aborting the whole GC cycle.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
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
