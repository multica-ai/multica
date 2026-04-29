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

// ---------------------------------------------------------------------------
// Fast-tier GC — terminal hygiene for `done` issues (contract 09 §10).
//
// The slow tier (gcLoop) handles `done`, `canceled`, and orphan workdirs at
// hour-scale cadence with a 24h+ grace window. That's appropriate for
// `canceled` (operators may want to inspect a cancelled card) and for
// orphans (no meta → conservative), but it leaves successfully-merged
// issues' worktrees on disk for up to 25h.
//
// The HARD RULE for `done` is: when an issue is merged, the local worktree
// MUST be deleted. The fast tier enforces this. It runs every
// GCDoneInterval (30s default), checks each workdir's issue status, and
// deletes after a small grace window (GCDoneTTL, 30s default) so any
// in-flight final actions can settle.
//
// End-to-end SLA: ≤ GCDoneInterval + GCDoneTTL + ~2s = ~62s from the
// moment the GitHub webhook flips status to `done` to os.RemoveAll return.
//
// The fast tier does NOT touch `canceled` (slow tier owns it) or `blocked`
// (never reaped — may need a human revival). Orphans are also slow-tier
// only since fast-tier latency offers no benefit there.
// ---------------------------------------------------------------------------

// gcDoneLoop is the fast-tier GC loop. Independent from gcLoop; both run
// concurrently. Safe to coexist — each operates on disjoint subsets:
//
//	gcLoop:     {canceled} ∪ {orphan} ∪ {done very old}
//	gcDoneLoop: {done within slow-tier window}
//
// The two loops compete only when an issue is `done` AND older than
// GCTTL (24h). In that case the first to win the os.RemoveAll succeeds
// and the other logs a benign "remove failed" warning — acceptable, since
// in practice the fast tier reaps `done` workdirs in <1m so the slow
// tier never sees them.
func (d *Daemon) gcDoneLoop(ctx context.Context) {
	if !d.cfg.GCEnabled {
		d.logger.Info("gc_done: disabled (GCEnabled=false)")
		return
	}
	d.logger.Info("gc_done: started",
		"interval", d.cfg.GCDoneInterval,
		"ttl", d.cfg.GCDoneTTL,
	)

	// Small initial delay so we don't race the slow tier's startup scan.
	if err := sleepWithContext(ctx, 5*time.Second); err != nil {
		return
	}

	ticker := time.NewTicker(d.cfg.GCDoneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runGCDone(ctx)
		}
	}
}

// runGCDone performs a single fast-tier scan across all workspaces.
func (d *Daemon) runGCDone(ctx context.Context) {
	root := d.cfg.WorkspacesRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			d.logger.Warn("gc_done: read workspaces root failed", "error", err)
		}
		return
	}

	var cleaned, skipped int
	for _, wsEntry := range entries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		c, s := d.gcDoneWorkspace(ctx, wsDir)
		cleaned += c
		skipped += s
	}

	if cleaned > 0 {
		// Bare-cache references to the now-removed worktrees: prune them.
		// Reuses the slow-tier helper; idempotent.
		d.pruneRepoWorktrees(root)
		d.logger.Info("gc_done: cycle complete", "cleaned", cleaned, "skipped", skipped)
	}
}

// gcDoneWorkspace scans task directories inside one workspace directory.
func (d *Daemon) gcDoneWorkspace(ctx context.Context, wsDir string) (cleaned, skipped int) {
	taskEntries, err := os.ReadDir(wsDir)
	if err != nil {
		d.logger.Warn("gc_done: read workspace dir failed", "dir", wsDir, "error", err)
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
		if d.shouldCleanDoneTaskDir(ctx, taskDir) == gcActionClean {
			d.cleanTaskDir(taskDir)
			cleaned++
		} else {
			skipped++
		}
	}

	if cleaned > 0 {
		if remaining, _ := os.ReadDir(wsDir); len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
	return
}

// shouldCleanDoneTaskDir is the fast-tier decision. It only acts when:
//
//   - the workdir has a .gc_meta.json (orphans go to the slow tier);
//   - the issue's status is exactly `done`;
//   - the server's has_active_task field is present and false (deploy-safe);
//   - the done flip is older than GCDoneTTL (grace window).
//
// Anything else returns gcActionSkip. The function is read-only — the caller
// performs the actual deletion — so it is safe to call concurrently with
// shouldCleanTaskDir from the slow tier.
func (d *Daemon) shouldCleanDoneTaskDir(ctx context.Context, taskDir string) gcAction {
	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil {
		// No meta → orphan; slow tier owns this case (mtime-gated cleanup
		// after GCOrphanTTL). Don't act here.
		return gcActionSkip
	}

	status, err := d.client.GetIssueGCCheck(ctx, meta.IssueID)
	if err != nil {
		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound {
			// 404 ambiguity (deleted vs no access). Slow tier handles it via
			// mtime-gated orphan cleanup; fast tier never deletes on 404.
			return gcActionSkip
		}
		// Network/auth errors — retry next cycle.
		return gcActionSkip
	}

	if status.Status != "done" {
		return gcActionSkip
	}

	// Strict race guard with deploy-safe nil handling. Three states:
	//   - nil    → old server didn't send the field; refuse to delete.
	//             Behavior degrades to slow-tier-only.
	//   - &true  → server confirmed an active task; refuse + log.
	//   - &false → server confirmed no active task; clear to proceed.
	if status.HasActiveTask == nil {
		d.logger.Info("gc_done: skip (server has_active_task field absent — old server, refusing to delete)",
			"dir", filepath.Base(taskDir),
			"issue", meta.IssueID,
		)
		return gcActionSkip
	}
	if *status.HasActiveTask {
		d.logger.Info("gc_done: skip (active task on done issue — strict race guard)",
			"dir", filepath.Base(taskDir),
			"issue", meta.IssueID,
		)
		return gcActionSkip
	}
	if time.Since(status.UpdatedAt) < d.cfg.GCDoneTTL {
		return gcActionSkip // grace window
	}

	d.logger.Info("gc_done: eligible",
		"dir", filepath.Base(taskDir),
		"issue", meta.IssueID,
		"age", time.Since(status.UpdatedAt).Round(time.Second),
	)
	return gcActionClean
}
