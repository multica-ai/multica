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
// Dispatches on meta.Kind so chat / autopilot / quick-create tasks each
// follow the parent record that actually governs their lifecycle.
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
		return d.orphanByMTime(taskDir, "no meta")
	}

	switch meta.Kind {
	case execenv.GCKindIssue:
		return d.gcDecisionIssue(ctx, taskDir, meta)
	case execenv.GCKindChat:
		return d.gcDecisionChat(ctx, taskDir, meta)
	case execenv.GCKindAutopilotRun:
		return d.gcDecisionAutopilotRun(ctx, taskDir, meta)
	case execenv.GCKindQuickCreate:
		return d.gcDecisionQuickCreate(ctx, taskDir, meta)
	default:
		// Unknown kind: fall back to mtime-based orphan cleanup so a future
		// daemon writing a kind we don't recognize doesn't get insta-wiped.
		return d.orphanByMTime(taskDir, "unknown kind")
	}
}

// orphanByMTime returns gcActionOrphan if the directory is older than
// GCOrphanTTL, gcActionSkip otherwise. Centralizes the "we have no parent
// record signal so just look at the disk" fallback used by every kind.
func (d *Daemon) orphanByMTime(taskDir, reason string) gcAction {
	info, err := os.Stat(taskDir)
	if err != nil {
		return gcActionSkip
	}
	if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
		d.logger.Info("gc: orphan directory", "dir", taskDir, "reason", reason, "age", time.Since(info.ModTime()).Round(time.Hour))
		return gcActionOrphan
	}
	return gcActionSkip
}

// isAccessNotFound detects the 404 returned by gc-check endpoints. The same
// status covers "row deleted" and "daemon token can't see this workspace"
// (the requireDaemonWorkspaceAccess anti-enumeration shape), so callers
// can't tell the two apart from the response alone.
func isAccessNotFound(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound
}

func (d *Daemon) gcDecisionIssue(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	status, err := d.client.GetIssueGCCheck(ctx, meta.IssueID)
	if err != nil {
		if isAccessNotFound(err) {
			// 404 is ambiguous: server returns it for both "issue deleted"
			// and "daemon token has no access to the workspace". Fall back
			// to the mtime-gated orphan cleanup so a scoped-down token
			// can't instantly wipe dirs whose issues are still live.
			return d.orphanByMTime(taskDir, "issue not accessible")
		}
		return gcActionSkip
	}

	if (status.Status == "done" || status.Status == "cancelled") &&
		time.Since(status.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", status.Status,
			"updated_at", status.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	if d.cfg.GCArtifactTTL > 0 && len(d.cfg.GCArtifactPatterns) > 0 &&
		!meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL {
		d.logger.Info("gc: eligible for artifact cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", status.Status,
			"completed_at", meta.CompletedAt.Format(time.RFC3339),
		)
		return gcActionCleanArtifacts
	}

	return gcActionSkip
}

func (d *Daemon) gcDecisionChat(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	status, err := d.client.GetChatSessionGCCheck(ctx, meta.ChatSessionID)
	if err != nil {
		if isAccessNotFound(err) {
			// 404 means the chat_session row is gone — DeleteChatSession is
			// a real DELETE, so a hard delete propagates here as soon as
			// the user clicks the button. This is the strongest reclaim
			// signal we get and it's exactly acceptance criterion #3:
			// reclaim within one GC cycle (≤ GCInterval), not 72h.
			//
			// We don't gate on mtime: every chat_session_id in a meta file
			// was written by this daemon under its current token, so there
			// is no cross-workspace probe to defend against.
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"reason", "session not accessible (hard-deleted)",
			)
			return gcActionClean
		}
		return gcActionSkip
	}

	switch status.Status {
	case "active":
		// An active chat session must never be reclaimed by mtime — that
		// would silently kill a user's idle session and break "PriorWorkDir"
		// resume on their next message. This is the explicit short-circuit
		// the issue body called out as verifyable behavior #2.
		return gcActionSkip
	case "archived":
		if time.Since(status.UpdatedAt) > d.cfg.GCTTL {
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"status", status.Status,
				"updated_at", status.UpdatedAt.Format(time.RFC3339),
			)
			return gcActionClean
		}
	}
	return gcActionSkip
}

func (d *Daemon) gcDecisionAutopilotRun(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	status, err := d.client.GetAutopilotRunGCCheck(ctx, meta.AutopilotRunID)
	if err != nil {
		if isAccessNotFound(err) {
			return d.orphanByMTime(taskDir, "autopilot run not accessible")
		}
		return gcActionSkip
	}

	// Terminal states per the autopilot_run CHECK constraint:
	//   completed, failed, skipped — the run finished its own work.
	//   issue_created            — the run produced an issue task that owns
	//                              its own workdir; this run's workdir is
	//                              dead weight from here on.
	// Non-terminal: pending, running. Skip until they reach a terminal state
	// rather than trying to bound them by mtime — long autopilots are real.
	if isAutopilotRunTerminal(status.Status) {
		anchor := status.CompletedAt
		if anchor.IsZero() {
			// Defensive: terminal status without completed_at means the
			// run finished but the column wasn't stamped (older code path).
			// Fall back to the meta's CompletedAt so we still GC eventually.
			anchor = meta.CompletedAt
		}
		if !anchor.IsZero() && time.Since(anchor) > d.cfg.GCTTL {
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "autopilot_run",
				"autopilot_run", meta.AutopilotRunID,
				"status", status.Status,
				"completed_at", anchor.Format(time.RFC3339),
			)
			return gcActionClean
		}
	}
	return gcActionSkip
}

// isAutopilotRunTerminal mirrors the run.status CHECK in
// migrations/042_autopilot.up.sql. Non-terminal states are pending/running;
// every other value the schema allows is a final resting state from the
// daemon's POV (the run is no longer producing work in this workdir).
func isAutopilotRunTerminal(status string) bool {
	switch status {
	case "completed", "failed", "skipped", "issue_created":
		return true
	default:
		return false
	}
}

func (d *Daemon) gcDecisionQuickCreate(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	status, err := d.client.GetTaskGCCheck(ctx, meta.TaskID)
	if err != nil {
		if isAccessNotFound(err) {
			// Task row was hard-deleted, or token can't see it. Either way,
			// fall back to mtime-gated orphan to stay safe across scoped
			// tokens — same reasoning as the issue path.
			return d.orphanByMTime(taskDir, "task not accessible")
		}
		return gcActionSkip
	}

	// Quick-create workdirs are not reused by the issue task that
	// LinkTaskToIssue eventually attaches — that issue gets its own
	// envRoot. So as soon as the quick-create task itself reaches a
	// terminal state we can reclaim the directory immediately, without
	// waiting for GCTTL. If the user wants to revisit, the linked issue
	// has the agent's output already.
	if isAgentTaskTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "quick_create",
			"task", meta.TaskID,
			"status", status.Status,
		)
		return gcActionClean
	}
	return gcActionSkip
}

// isAgentTaskTerminal reports whether a value of agent_task_queue.status
// represents a final state. Mirrors the status enum used across the
// task service — see service/task.go for the canonical list.
func isAgentTaskTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
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
