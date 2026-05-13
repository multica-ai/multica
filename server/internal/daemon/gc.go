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
	// A task currently running on this env root must never be reclaimed —
	// not even on the done/cancelled or orphan-404 paths. A new comment on
	// an already-done issue can dispatch a follow-up task that reuses the
	// prior workdir without bumping the issue's updated_at, so the regular
	// TTL check alone wouldn't notice the resumed activity.
	if d.isActiveEnvRoot(taskDir) {
		return gcActionSkip
	}

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
	// long enough ago that its build artifacts are unlikely to be reused.
	// Active-root protection is handled by the early return above; skip here
	// only when artifact GC is disabled or the meta has no completed_at
	// (defensive — that means the task crashed before WriteGCMeta).
	if d.cfg.GCArtifactTTL > 0 && len(d.cfg.GCArtifactPatterns) > 0 &&
		!meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL {
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
//   - symlinks are skipped entirely — neither the link nor its target is
//     touched, so a malicious or stale link can't redirect the GC outside the
//     workdir.
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
//
// PUL-94 extension (A6): also sweeps the global per-task worktrees dir
// (cfg.WorktreesRoot, typically /srv/agent-worktrees/) for orphaned per-task
// worktrees — those whose backing envRoot is no longer active and whose
// mtime is outside the 5-minute grace period that protects in-flight spawns.
func (d *Daemon) pruneRepoWorktrees(workspacesRoot string) {
	reposRoot := filepath.Join(workspacesRoot, ".repos")
	wsEntries, err := os.ReadDir(reposRoot)
	if err == nil {
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

	// PUL-94: per-task worktree sweep. Runs only if the feature is configured
	// (WorktreesRoot set, BareRepoMap populated). Looks for per-task dirs at
	// /srv/agent-worktrees/<name>/ that:
	//   1. match the per-task pattern (contain at least one "-" and aren't
	//      a known legacy "agent-N" plain dir),
	//   2. are outside the 5-minute grace period (mtime check), and
	//   3. have no corresponding active envRoot.
	// Orphans matching all three get `git worktree remove --force` against
	// their bare. The bare path comes from the GCMeta of the matching envRoot
	// (if any) — falling back to scanning the BareRepoMap is unnecessary since
	// orphans by definition have no active envRoot.
	if d.cfg.WorktreesRoot != "" {
		d.sweepPerTaskWorktrees()
	}
}

const perTaskGracePeriod = 5 * time.Minute

// sweepPerTaskWorktrees implements the PUL-94 sweeper. Scans the configured
// WorktreesRoot for per-task worktrees, classifies each entry as active /
// in-grace / orphan, removes orphans. Emits structured slog events for ops:
//
//	worktree.remove        — per orphan cleaned, with reason="sweeper"
//	worktree.sweeper_run   — one per invocation, with duration + counters
//
// "Active" means the worktree's path appears in the daemon's in-memory
// activeEnvRoots set (via the GCMeta.WtPath that runTask records at spawn).
// "Grace" means the worktree's directory was created less than
// perTaskGracePeriod ago — protects in-flight spawns that haven't yet
// registered an active envRoot.
func (d *Daemon) sweepPerTaskWorktrees() {
	start := time.Now()
	entries, err := os.ReadDir(d.cfg.WorktreesRoot)
	if err != nil {
		return
	}

	// Build the set of WtPaths owned by active envRoots — read every
	// envRoot's .gc_meta.json and collect non-empty WtPath fields. Cheap
	// even with hundreds of envRoots (small JSON reads).
	activeWtPaths := d.collectActiveWtPaths()

	scanned := 0
	cleaned := 0
	errCount := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip legacy per-agent dirs (no per-task uuid suffix). The
		// per-task pattern is "<agent-sanitized>-<short[8]>" — at least
		// 10 chars and ending with 8 hex/alnum after the last dash.
		if !looksLikePerTaskWorktree(name) {
			continue
		}
		wtPath := filepath.Join(d.cfg.WorktreesRoot, name)
		scanned++

		// Active? Skip — task is using it.
		if _, ok := activeWtPaths[wtPath]; ok {
			continue
		}

		// Grace period: skip recently-created worktrees so a spawn
		// midway through writing .gc_meta.json isn't swept.
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < perTaskGracePeriod {
			continue
		}

		// Orphan — remove the worktree. We do not know which bare it
		// was attached to without reading every bare's worktree list,
		// so fall back to deleting the directory and letting the next
		// `git worktree prune` (above) clean up the bare's record.
		ageSeconds := time.Since(info.ModTime()).Seconds()
		if err := os.RemoveAll(wtPath); err != nil {
			d.logger.Warn("worktree.sweeper_remove_failed",
				"wt_path", wtPath, "error", err)
			errCount++
			continue
		}
		d.logger.Info("worktree.remove",
			"wt_path", wtPath,
			"reason", "sweeper",
			"age_seconds", ageSeconds,
		)
		cleaned++
	}

	d.logger.Info("worktree.sweeper_run",
		"duration_ms", time.Since(start).Milliseconds(),
		"scanned", scanned,
		"orphans_cleaned", cleaned,
		"errors", errCount,
	)
}

// collectActiveWtPaths walks every workspace env root and reads
// .gc_meta.json files to build the set of WtPaths owned by currently-known
// tasks. Used by the sweeper to distinguish "this worktree belongs to a
// live task" from "this worktree is orphan."
//
// Falls back to an empty set on any walk failure — the sweeper then relies
// on the grace period alone, which is intentionally lenient.
func (d *Daemon) collectActiveWtPaths() map[string]struct{} {
	active := make(map[string]struct{})
	if d.cfg.WorkspacesRoot == "" {
		return active
	}
	wsEntries, err := os.ReadDir(d.cfg.WorkspacesRoot)
	if err != nil {
		return active
	}
	for _, ws := range wsEntries {
		if !ws.IsDir() {
			continue
		}
		// Skip the bare-cache dir (".repos") and other hidden dirs.
		if strings.HasPrefix(ws.Name(), ".") {
			continue
		}
		wsDir := filepath.Join(d.cfg.WorkspacesRoot, ws.Name())
		taskEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, te := range taskEntries {
			if !te.IsDir() {
				continue
			}
			envRoot := filepath.Join(wsDir, te.Name())
			meta, err := execenv.ReadGCMeta(envRoot)
			if err != nil {
				continue
			}
			if meta.WtPath != "" {
				active[meta.WtPath] = struct{}{}
			}
		}
	}
	return active
}

// looksLikePerTaskWorktree returns true for dir names matching the per-task
// pattern <agent>-<short_id> where short_id is the 8-char hex prefix of a
// task UUID. Legacy <agent> dirs without the suffix return false.
func looksLikePerTaskWorktree(name string) bool {
	idx := strings.LastIndex(name, "-")
	if idx < 0 {
		return false
	}
	suffix := name[idx+1:]
	if len(suffix) != 8 {
		return false
	}
	for _, c := range suffix {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	// Must have a non-empty agent prefix before the dash.
	return idx > 0
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
