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
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// reposDirName is the bare-repo cache directory inside the workspaces root.
// It is a sibling of the per-workspace task directories rather than one of
// them, so every walk over the root has to decide explicitly what to do with it.
const reposDirName = ".repos"

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
		"repo_ttl", d.cfg.GCRepoTTL,
		"repo_maintenance_enabled", d.cfg.GCRepoMaintenanceEnabled,
		"artifact_patterns", d.cfg.GCArtifactPatterns,
		"managed_artifact_subpaths", execenv.ManagedReclaimableArtifactSubpaths(),
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
	cleaned         int // whole task dirs removed (issue done/cancelled)
	orphaned        int // whole task dirs removed (no meta / unreachable issue)
	skipped         int // task dirs left untouched
	artifactDirs    int // task dirs that had at least one artifact reclaimed
	artifactRemoved int // count of removed artifact subdirs
	storesReclaimed int // per-conversation Codex session stores reclaimed past their TTL
	// hermesMemoryStoresReclaimed is counted separately from storesReclaimed:
	// the two stores hold different things on different TTLs, so folding them
	// into one number would make either figure unreadable for an operator.
	hermesMemoryStoresReclaimed  int            // per-agent Hermes memory stores reclaimed past their TTL
	hermesSessionStoresReclaimed int            // per-conversation Hermes session stores reclaimed past their TTL
	repoCachesReclaimed          int            // bare repo caches under .repos evicted past their TTL
	bytesReclaimed               int64          // total bytes freed in this cycle
	byPattern                    map[string]int // configured basename or managed path label -> reclaim count
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
		// Skip every daemon-internal dot directory, not just .repos. A
		// workspace directory is always a UUID, so a dot-prefixed entry is one
		// of our own caches. Walking .skill-cache as if it were a workspace
		// made its `v1` directory look like a task dir with no .gc_meta.json,
		// so the orphan path would delete the entire bundle cache once its
		// mtime went 72h without a new bundle. That reclaimed a few hundred KB
		// and cost a full re-download.
		if !wsEntry.IsDir() || strings.HasPrefix(wsEntry.Name(), ".") {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		d.gcWorkspace(ctx, wsDir, stats)
	}

	// Prune stale worktree references from all bare repo caches, then evict the
	// caches nothing needs anymore. These live outside any workspace directory
	// and are never reclaimed by the task walk above.
	d.pruneRepoWorktreesContext(ctx, root, stats)

	// Reclaim per-issue Codex session stores idle past their TTL. These live
	// under the shared ~/.codex home (outside WorkspacesRoot) so resume survives
	// the task GC, which means they need their own bounded lifecycle (MUL-4424).
	if storesRemoved, storeBytes := execenv.PruneCodexSessionStores(d.cfg.Profile, d.cfg.GCCodexSessionTTL, time.Now(), d.reserveStoreForDeletion, d.logger); storesRemoved > 0 {
		stats.storesReclaimed += storesRemoved
		stats.bytesReclaimed += storeBytes
	}

	// Same for per-agent Hermes memory stores: they outlive the task by design
	// (that is what fixes #6638), so a deleted agent's memory needs its own
	// bounded lifecycle. Retention is much longer than the Codex one — these are
	// a few markdown files, and reclaiming them is user-visible amnesia.
	if storesRemoved, storeBytes := execenv.PruneHermesMemoryStores(d.cfg.Profile, d.cfg.GCHermesMemoryTTL, time.Now(), d.reserveStoreForDeletion, d.logger); storesRemoved > 0 {
		stats.hermesMemoryStoresReclaimed += storesRemoved
		stats.bytesReclaimed += storeBytes
	}

	// And per-conversation Hermes session stores, which outlive the task for the
	// same reason (that is what fixes #6806) but hold transcripts rather than
	// notes — so they get the shorter, Codex-like retention.
	if storesRemoved, storeBytes := execenv.PruneHermesSessionStores(d.cfg.Profile, d.cfg.GCHermesSessionTTL, time.Now(), d.reserveStoreForDeletion, d.logger); storesRemoved > 0 {
		stats.hermesSessionStoresReclaimed += storesRemoved
		stats.bytesReclaimed += storeBytes
	}

	if stats.cleaned > 0 || stats.orphaned > 0 || stats.artifactDirs > 0 || stats.storesReclaimed > 0 || stats.hermesMemoryStoresReclaimed > 0 || stats.hermesSessionStoresReclaimed > 0 || stats.repoCachesReclaimed > 0 {
		d.logger.Info("gc: cycle complete",
			"cleaned", stats.cleaned,
			"orphaned", stats.orphaned,
			"skipped", stats.skipped,
			"artifact_dirs", stats.artifactDirs,
			"artifact_removed", stats.artifactRemoved,
			"codex_session_stores_reclaimed", stats.storesReclaimed,
			"hermes_memory_stores_reclaimed", stats.hermesMemoryStoresReclaimed,
			"hermes_session_stores_reclaimed", stats.hermesSessionStoresReclaimed,
			"repo_caches_reclaimed", stats.repoCachesReclaimed,
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
	issueCandidates := make([]issueGCCandidate, 0, len(taskEntries))
	for _, entry := range taskEntries {
		if ctx.Err() != nil {
			return
		}
		if !entry.IsDir() {
			continue
		}
		taskDir := filepath.Join(wsDir, entry.Name())
		if d.isActiveEnvRoot(taskDir) {
			stats.skipped++
			continue
		}
		meta, metaErr := execenv.ReadGCMeta(taskDir)
		if metaErr == nil && meta.Kind == execenv.GCKindIssue && strings.TrimSpace(meta.IssueID) != "" {
			issueCandidates = append(issueCandidates, issueGCCandidate{taskDir: taskDir, meta: meta})
			continue
		}
		action := d.shouldCleanTaskDir(ctx, taskDir)
		cleanedHere += d.applyGCAction(taskDir, action, stats)
	}
	cleanedHere += d.gcWorkspaceIssues(ctx, filepath.Base(wsDir), issueCandidates, stats)

	// Remove the workspace directory itself if it's now empty.
	if cleanedHere > 0 {
		remaining, _ := os.ReadDir(wsDir)
		if len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
}

const issueGCBatchSize = 500

type issueGCCandidate struct {
	taskDir string
	meta    *execenv.GCMeta
}

// gcWorkspaceIssues resolves all issue-backed task dirs with a bounded number
// of workspace-level requests. Multiple task dirs for the same issue share one
// result. The client transparently falls back to the legacy per-issue endpoint
// when it is connected to an older server.
func (d *Daemon) gcWorkspaceIssues(ctx context.Context, workspaceID string, candidates []issueGCCandidate, stats *gcStats) int {
	if len(candidates) == 0 {
		return 0
	}

	issueIDs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		issueID := strings.TrimSpace(candidate.meta.IssueID)
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		issueIDs = append(issueIDs, issueID)
	}

	results := make(map[string]IssueGCCheckResult, len(issueIDs))
	for start := 0; start < len(issueIDs); start += issueGCBatchSize {
		if ctx.Err() != nil {
			break
		}
		end := min(start+issueGCBatchSize, len(issueIDs))
		chunkResults, err := d.client.GetIssueGCChecks(ctx, workspaceID, issueIDs[start:end])
		if err != nil {
			d.logger.Warn("gc: batch issue check failed",
				"workspace", workspaceID,
				"count", end-start,
				"error", err,
			)
			continue
		}
		for issueID, result := range chunkResults {
			results[issueID] = result
		}
	}

	cleaned := 0
	for i, candidate := range candidates {
		if ctx.Err() != nil {
			stats.skipped += len(candidates) - i
			break
		}
		issueID := strings.TrimSpace(candidate.meta.IssueID)
		result, ok := results[issueID]
		if !ok || result.Err != nil {
			// No usable answer about the parent issue this cycle, so the task
			// data stays. The regenerable Codex cache is still fair game —
			// see applyManagedArtifactFallback.
			action := d.applyManagedArtifactFallback(candidate.taskDir, candidate.meta, gcActionSkip)
			cleaned += d.applyGCAction(candidate.taskDir, action, stats)
			continue
		}
		action := d.gcDecisionIssueResult(candidate.taskDir, candidate.meta, result)
		action = d.applyLocalDirectoryGCOverride(candidate.meta, action)
		action = d.applyManagedArtifactFallback(candidate.taskDir, candidate.meta, action)
		cleaned += d.applyGCAction(candidate.taskDir, action, stats)
	}
	return cleaned
}

// applyGCAction performs one decision and updates cycle stats. Each mutation
// atomically reserves the env root because a task can start while the server
// reconciliation request is in flight.
func (d *Daemon) applyGCAction(taskDir string, action gcAction, stats *gcStats) int {
	if action != gcActionSkip {
		release, ok := d.reserveEnvRootForGC(taskDir)
		if !ok {
			stats.skipped++
			return 0
		}
		defer release()
	}
	switch action {
	case gcActionClean:
		bytes := dirSize(taskDir)
		d.cleanTaskDir(taskDir)
		stats.cleaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionOrphan:
		bytes := dirSize(taskDir)
		d.cleanTaskDir(taskDir)
		stats.orphaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionCleanArtifacts:
		removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, d.cfg.GCArtifactPatterns)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++ // task dir itself preserved
	case gcActionCleanManagedArtifacts:
		removed, bytes, perPattern := d.cleanManagedTaskArtifacts(taskDir)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++ // task dir itself preserved
	default:
		stats.skipped++
	}
	return 0
}

func recordArtifactCleanup(stats *gcStats, removed int, bytes int64, perPattern map[string]int) {
	if removed == 0 {
		return
	}
	stats.artifactDirs++
	stats.artifactRemoved += removed
	stats.bytesReclaimed += bytes
	if stats.byPattern == nil {
		stats.byPattern = map[string]int{}
	}
	for pattern, count := range perPattern {
		stats.byPattern[pattern] += count
	}
}

type gcAction int

const (
	gcActionSkip                  gcAction = iota
	gcActionClean                          // issue is done/cancelled and stale
	gcActionOrphan                         // no meta or unknown issue and dir is old
	gcActionCleanArtifacts                 // task completed long enough ago; drop regenerable artifacts only
	gcActionCleanManagedArtifacts          // preserve the task and drop exact daemon-managed artifacts only
)

// shouldCleanTaskDir decides whether a task directory should be removed.
// Dispatches on meta.Kind so chat / autopilot / quick-create tasks each
// follow the parent record that actually governs their lifecycle.
func (d *Daemon) shouldCleanTaskDir(ctx context.Context, taskDir string) gcAction {
	// A task currently running on this env root must never be reclaimed —
	// not even on the done/cancelled or orphan-404 paths. A re-dispatched or
	// still-running task can reuse the prior workdir of an already-done issue
	// whose updated_at is older than the TTL (a task re-claim doesn't advance
	// updated_at), so the regular TTL check alone wouldn't notice the resumed
	// activity.
	if d.isActiveEnvRoot(taskDir) {
		return gcActionSkip
	}

	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil {
		return d.orphanByMTime(taskDir, "no meta")
	}

	action := d.shouldCleanTaskDirForKind(ctx, taskDir, meta)
	action = d.applyLocalDirectoryGCOverride(meta, action)
	return d.applyManagedArtifactFallback(taskDir, meta, action)
}

// applyManagedArtifactFallback upgrades a skip into managed-artifact cleanup
// once the task's own regenerable Codex cache is past GCArtifactTTL.
//
// Whether a task's *data* may be removed is a per-kind question: it depends on
// the parent record, and shouldCleanTaskDirForKind is what answers it. Whether
// the daemon's *own regenerable cache* may be reclaimed is not a per-kind
// question at all — codex-home/.sandbox-bin is a ~285 MiB copy of the Codex
// binary that the next run re-provisions on demand, whoever the parent is.
// Wiring that reclaim into the issue path alone (#5654) left every other kind
// holding the cache indefinitely; for chat that is genuinely unbounded, since a
// session stays "active" with no time limit and Desktop's chat is the main
// interactive surface (#6782).
//
// Deliberately a one-way upgrade from gcActionSkip. Clean and Orphan already
// remove strictly more than this, and applyLocalDirectoryGCOverride owns the
// demotions in the other direction — so this can only ever widen what a cycle
// reclaims, never narrow it.
//
// Note this also fires when the GC check API call itself failed (a transient
// network error resolves to gcActionSkip). That is intended: the cache is
// regenerable, so it does not need a confirmed parent record the way deleting
// task data does. The isActiveEnvRoot short-circuit above and the env-root
// reservation in applyGCAction still keep a running task's cache intact.
func (d *Daemon) applyManagedArtifactFallback(taskDir string, meta *execenv.GCMeta, action gcAction) gcAction {
	if action != gcActionSkip || d.cfg.GCArtifactTTL <= 0 {
		return action
	}
	// A zero CompletedAt means the task never reported completion through
	// WriteGCMeta. Leave those to the per-kind legacy handling rather than
	// guessing from an unrelated clock.
	if meta.CompletedAt.IsZero() || time.Since(meta.CompletedAt) <= d.cfg.GCArtifactTTL {
		return action
	}
	// completed_at never moves again for a task that stays non-terminal, so
	// without this the decision stays "reclaim" forever and every cycle pays
	// for a reservation and a removal pass that finds nothing. Racing a
	// re-provision here is harmless: the next cycle picks it up.
	if !hasManagedArtifact(taskDir) {
		return action
	}
	d.logger.Info("gc: eligible for managed artifact cleanup",
		"dir", filepath.Base(taskDir),
		"kind", string(meta.Kind),
		"completed_at", meta.CompletedAt.Format(time.RFC3339),
	)
	return gcActionCleanManagedArtifacts
}

func (d *Daemon) applyLocalDirectoryGCOverride(meta *execenv.GCMeta, action gcAction) gcAction {
	if !meta.LocalDirectory {
		return action
	}
	// local_directory tasks keep their envRoot indefinitely so the user
	// can inspect output/ and logs/ for forensic context. The WorkDir is
	// the user's own path and lives outside taskDir. The envRoot contains
	// the daemon's durable logbook plus regenerable tool caches, so keep the
	// former while allowing narrowly scoped cleanup of the latter.
	//
	//   gcActionClean   → demote to artifact-pattern cleanup so envRoot
	//                     (and especially the logbook) survives.
	//   gcActionOrphan  → exact managed-artifact cleanup only; we don't ever
	//                     wipe a local_directory envRoot via the mtime path,
	//                     since the parent issue / chat record going away
	//                     should not collateral-delete the user's audit trail.
	//
	// Artifact cleanup remains disabled when GCArtifactTTL is explicitly zero.
	// gcActionCleanArtifacts, gcActionCleanManagedArtifacts, and gcActionSkip obey the
	// "no full envRoot RemoveAll" rule.
	if d.cfg.GCArtifactTTL <= 0 {
		return gcActionSkip
	}
	switch action {
	case gcActionClean:
		return gcActionCleanArtifacts
	case gcActionOrphan:
		return gcActionCleanManagedArtifacts
	default:
		return action
	}
}

// shouldCleanTaskDirForKind runs the per-Kind dispatch without applying the
// local_directory override. Split out so shouldCleanTaskDir can intercept
// the result.
func (d *Daemon) shouldCleanTaskDirForKind(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
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
	if strings.TrimSpace(meta.IssueID) == "" {
		return d.orphanByMTime(taskDir, "empty issue id")
	}

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

	return d.gcDecisionIssueResult(taskDir, meta, IssueGCCheckResult{
		ID:        meta.IssueID,
		Found:     true,
		Status:    status.Status,
		UpdatedAt: status.UpdatedAt,
	})
}

func (d *Daemon) gcDecisionIssueResult(taskDir string, meta *execenv.GCMeta, result IssueGCCheckResult) gcAction {
	if !result.Found {
		return d.orphanByMTime(taskDir, "issue not accessible")
	}

	if (result.Status == "done" || result.Status == "cancelled") &&
		time.Since(result.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", result.Status,
			"updated_at", result.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	if d.cfg.GCArtifactTTL > 0 && !meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL {
		d.logger.Info("gc: eligible for artifact cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", result.Status,
			"completed_at", meta.CompletedAt.Format(time.RFC3339),
		)
		return gcActionCleanArtifacts
	}

	// Old metadata may not have completed_at. Keep that case conservative:
	// after the metadata file itself has been idle for the longer orphan TTL,
	// reclaim only the exact daemon-managed cache. WriteGCMeta replaces this
	// file after every completed task, so a recent reuse refreshes the signal
	// even when activity below taskDir leaves the root directory mtime stale.
	if d.cfg.GCArtifactTTL > 0 && meta.CompletedAt.IsZero() {
		if age, ok := gcMetaFileAge(taskDir); ok && age > d.cfg.GCOrphanTTL {
			d.logger.Info("gc: legacy task eligible for managed artifact cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "issue",
				"issue", meta.IssueID,
				"status", result.Status,
				"age", age.Round(time.Hour),
			)
			return gcActionCleanManagedArtifacts
		}
	}

	return gcActionSkip
}

func gcMetaFileAge(taskDir string) (time.Duration, bool) {
	info, err := os.Stat(filepath.Join(taskDir, ".gc_meta.json"))
	if err != nil {
		return 0, false
	}
	return time.Since(info.ModTime()), true
}

func (d *Daemon) gcDecisionChat(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.ChatSessionID) == "" {
		return d.orphanByMTime(taskDir, "empty chat session id")
	}

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
		// An active chat session's directory must never be reclaimed by mtime
		// — that would silently kill a user's idle session and break
		// "PriorWorkDir" resume on their next message.
		//
		// This protects the session's own data, not the daemon's regenerable
		// caches. shouldCleanTaskDir layers applyManagedArtifactFallback on top
		// of this skip, so a session idle past GCArtifactTTL gives back
		// codex-home/.sandbox-bin and the next message re-provisions it. That
		// costs a ~285 MiB Codex bootstrap on resume and is the same trade-off
		// gcDecisionIssueResult already makes for a completed task whose issue
		// is still open — without it an active session pins the cache forever
		// (#6782).
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
	if strings.TrimSpace(meta.AutopilotRunID) == "" {
		return d.orphanByMTime(taskDir, "empty autopilot run id")
	}

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
	//
	// An autopilot run's workdir is never reused: unlike issue/chat tasks there
	// is no PriorWorkDir path that hands a later run the same directory, so every
	// run gets a fresh one. Whatever the run produced already lives server-side
	// (and an issue_created run handed its work to an issue task that owns its own
	// envRoot). So the moment the run reaches a terminal state the directory is
	// dead weight and we reclaim it immediately, without waiting out GCTTL — the
	// same reasoning gcDecisionQuickCreate applies to quick-create dirs. The
	// active-env-root short-circuit in shouldCleanTaskDir still protects a run
	// that is mid-flight, so this can't pull the rug from under live work.
	if isAutopilotRunTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "autopilot_run",
			"autopilot_run", meta.AutopilotRunID,
			"status", status.Status,
		)
		return gcActionClean
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
	if strings.TrimSpace(meta.TaskID) == "" {
		return d.orphanByMTime(taskDir, "empty task id")
	}

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

// linkedDirModes are the mode bits that mark a directory entry as a link to
// content the task does not own. Every task-directory walk that deletes or
// measures must refuse to descend through them: the per-task codex-home links
// the user's real skills, Codex session store and plugin cache into itself, so
// descending would put the GC inside the user's home.
//
// ModeSymlink alone is not enough on Windows. createDirLink falls back to a
// directory junction (mklink /J) when os.Symlink is denied — no Developer Mode
// — and since Go 1.23 os.Lstat reports a junction as ModeDir|ModeIrregular
// with no ModeSymlink bit, while its DirEntry still answers IsDir() == true.
// A ModeSymlink-only check therefore lets filepath.WalkDir walk straight into
// the link target.
const linkedDirModes = os.ModeSymlink | os.ModeIrregular

// cleanTaskArtifacts walks taskDir and deletes every directory whose basename
// matches one of patterns, plus exact daemon-managed artifact paths. Returns
// (removedCount, bytesReclaimed, perPattern).
//
// Safety contract:
//   - patterns are basename-only; entries with a path separator are dropped.
//   - .git subtrees are never descended into, so the agent's git history stays
//     intact even if a pattern would otherwise match.
//   - linked directories are skipped entirely — neither the link nor its
//     target is touched, so a malicious or stale link can't redirect the GC
//     outside the workdir. See linkedDirModes for what counts as a link.
//   - every removal target is verified to live inside taskDir, so a tampered
//     .gc_meta.json can't trick the daemon into deleting outside its sandbox.
func (d *Daemon) cleanTaskArtifacts(taskDir string, patterns []string) (removed int, bytes int64, perPattern map[string]int) {
	return d.cleanTaskArtifactsMatching(taskDir, newArtifactMatcher(patterns, execenv.ManagedReclaimableArtifactSubpaths()))
}

// cleanManagedTaskArtifacts removes the exact daemon-managed artifact subpaths
// under taskDir.
//
// The managed set is a list of exact relative paths, so these are addressed
// directly rather than searched for. Walking the whole task tree to find a
// directory whose location is already known costs a full repo checkout's worth
// of stat calls, and a task that stays non-terminal — an active chat session —
// pays it on every GC cycle for as long as it lives, long after the cache is
// gone. cleanTaskArtifacts still walks, because its basename patterns can match
// at any depth; this one has nothing to search for.
func (d *Daemon) cleanManagedTaskArtifacts(taskDir string) (removed int, bytes int64, perPattern map[string]int) {
	perPattern = map[string]int{}
	if taskDir == "" {
		return
	}
	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}
	for _, subpath := range execenv.ManagedReclaimableArtifactSubpaths() {
		rel, ok := safeRelativePath(subpath)
		if !ok {
			continue
		}
		target, ok := managedArtifactTarget(absRoot, rel)
		if !ok {
			continue
		}
		size := dirSize(target)
		if rmErr := os.RemoveAll(target); rmErr != nil {
			d.logger.Warn("gc: artifact remove failed", "path", target, "error", rmErr)
			continue
		}
		removed++
		bytes += size
		perPattern[managedArtifactPatternPrefix+filepath.ToSlash(rel)]++
		d.logger.Info("gc: artifact removed", "path", target, "bytes", size)
	}
	return
}

// managedArtifactTarget resolves one managed relative subpath under absRoot to
// an absolute path that is safe to remove, reporting false when there is
// nothing to reclaim.
//
// The tree walk this replaces refused to descend through symlinks and Windows
// junctions: the per-task codex-home links the user's real skills, Codex
// session store and plugin cache into itself, so following one would put
// RemoveAll inside the user's home. Addressing the path directly means every
// component between absRoot and the leaf has to be re-checked, not just the
// leaf. See linkedDirModes.
//
// Containment needs no separate check: safeRelativePath has already rejected
// absolute paths and anything that escapes upward, and filepath.Clean leaves no
// interior "..", so joining the components one at a time cannot leave absRoot.
func managedArtifactTarget(absRoot, rel string) (string, bool) {
	current := absRoot
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			// Already reclaimed, never created, or unreadable — all three mean
			// "nothing for this cycle to do".
			return "", false
		}
		if info.Mode()&linkedDirModes != 0 || !info.IsDir() {
			return "", false
		}
	}
	return current, true
}

// hasManagedArtifact reports whether any managed subpath is actually present.
// Without this the decision layer keeps returning gcActionCleanManagedArtifacts
// for a long-lived task whose completed_at never moves again, so every cycle
// takes an env-root reservation and logs a reclaim that removes nothing.
func hasManagedArtifact(taskDir string) bool {
	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return false
	}
	for _, subpath := range execenv.ManagedReclaimableArtifactSubpaths() {
		rel, ok := safeRelativePath(subpath)
		if !ok {
			continue
		}
		if _, ok := managedArtifactTarget(absRoot, rel); ok {
			return true
		}
	}
	return false
}

func (d *Daemon) cleanTaskArtifactsMatching(taskDir string, matcher artifactMatcher) (removed int, bytes int64, perPattern map[string]int) {
	perPattern = map[string]int{}
	if taskDir == "" || (len(matcher.basenames) == 0 && len(matcher.exactPaths) == 0) {
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
		// Refuse to follow linked directories. WalkDir reports them as type
		// Dir on some platforms; lstat to be sure.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil
		}
		if info.Mode()&linkedDirModes != 0 {
			return filepath.SkipDir
		}
		pattern, ok := matcher.matchDirectory(absRoot, path, entry)
		if !ok {
			return nil
		}
		size := dirSize(path)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			d.logger.Warn("gc: artifact remove failed", "path", path, "error", rmErr)
			return filepath.SkipDir
		}
		removed++
		bytes += size
		perPattern[pattern]++
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
// Linked content is not counted: os.RemoveAll would drop the link and leave
// the target, so counting it would overstate what a removal reclaims.
// Non-fatal: errors during the walk are ignored so callers can report a
// best-effort byte count without aborting the whole GC cycle.
func dirSize(root string) int64 {
	total, _ := dirSizeContext(context.Background(), root)
	return total
}

func dirSizeContext(ctx context.Context, root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if err != nil {
			return nil
		}
		if entry.Type()&linkedDirModes != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
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
	return total, err
}

const (
	gitCmdTimeout         = 30 * time.Second
	gitMaintenanceTimeout = 10 * time.Minute
	repoMaintenanceMarker = ".multica-maintenance-pending"
)

// pruneRepoWorktrees runs `git worktree prune` on all bare repos in the cache,
// then evicts the ones nothing needs anymore.
func (d *Daemon) pruneRepoWorktrees(workspacesRoot string, stats *gcStats) {
	d.pruneRepoWorktreesContext(context.Background(), workspacesRoot, stats)
}

func (d *Daemon) pruneRepoWorktreesContext(ctx context.Context, workspacesRoot string, stats *gcStats) {
	reposRoot := filepath.Join(workspacesRoot, reposDirName)
	wsEntries, err := os.ReadDir(reposRoot)
	if err != nil {
		return
	}

	for _, wsEntry := range wsEntries {
		if ctx.Err() != nil {
			return
		}
		if !wsEntry.IsDir() {
			continue
		}
		wsRepoDir := filepath.Join(reposRoot, wsEntry.Name())
		repoEntries, err := os.ReadDir(wsRepoDir)
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if ctx.Err() != nil {
				return
			}
			if !repoEntry.IsDir() {
				continue
			}
			barePath := filepath.Join(wsRepoDir, repoEntry.Name())
			if !isBareRepo(barePath) {
				continue
			}
			d.maintainRepoCache(ctx, barePath, stats)
		}
		// Drop the per-workspace directory once its last repo is gone.
		if remaining, err := os.ReadDir(wsRepoDir); err == nil && len(remaining) == 0 {
			os.Remove(wsRepoDir)
		}
	}
}

func (d *Daemon) maintainRepoCache(ctx context.Context, barePath string, stats *gcStats) {
	d.withRepoMaintenance(ctx, barePath, func(maintenanceCtx context.Context) {
		d.pruneWorktreeLocked(maintenanceCtx, barePath)
		if maintenanceCtx.Err() == nil {
			d.evictRepoCacheLocked(maintenanceCtx, barePath, stats)
		}
	})
}

// pruneWorktree runs only the maintenance half — prune stale worktrees and
// agent branches — without considering eviction.
func (d *Daemon) pruneWorktree(barePath string) {
	d.withRepoMaintenance(context.Background(), barePath, func(ctx context.Context) {
		d.pruneWorktreeLocked(ctx, barePath)
	})
}

type repoMaintenanceBackend interface {
	WithRepoMaintenance(context.Context, string, func(context.Context) error) (bool, error)
}

// withRepoMaintenance uses the cache's foreground-priority gate when
// available. The fallback preserves test/degraded backends that predate the
// optional interface without changing the repoCacheBackend contract.
func (d *Daemon) withRepoMaintenance(ctx context.Context, barePath string, fn func(context.Context)) {
	if cache, ok := d.repoCache.(repoMaintenanceBackend); ok {
		ran, err := cache.WithRepoMaintenance(ctx, barePath, func(maintenanceCtx context.Context) error {
			fn(maintenanceCtx)
			return nil
		})
		if err != nil && ctx.Err() == nil {
			d.logger.Warn("gc: repo maintenance lock failed", "repo", barePath, "error", err)
		}
		if !ran {
			d.logger.Debug("gc: repo maintenance skipped for foreground work", "repo", barePath)
		}
		return
	}
	d.withRepoLock(barePath, func() { fn(ctx) })
}

// withRepoLock serializes a mutation against Sync / CreateWorktree on the same
// bare repo. A daemon built without a repo cache (tests, degraded startup) has
// no lock to take and runs the work directly.
func (d *Daemon) withRepoLock(barePath string, fn func()) {
	if d.repoCache == nil {
		fn()
		return
	}
	if err := d.repoCache.WithRepoLock(barePath, func() error {
		fn()
		return nil
	}); err != nil {
		d.logger.Warn("gc: repo lock failed", "repo", barePath, "error", err)
	}
}

// evictRepoCacheLocked removes a bare repo cache that nothing needs anymore.
// The caller must hold the repo lock, so this cannot race a Sync or a
// CreateWorktree on the same repo.
//
// All four conditions are required:
//
//  1. GCRepoTTL > 0 — eviction is opt-out.
//
//  2. No watched workspace still claims the repo. This is a RETAIN predicate,
//     not a delete predicate, and the direction matters: Sync re-clones every
//     listed repo that is missing whenever a workspace registers, which happens
//     on every daemon start. Evicting a still-attached repo therefore just buys
//     a full re-clone on the next restart — that is not reclaiming space, it is
//     moving it. Because the set only ever *prevents* deletion, a stale or
//     empty one cannot widen what we delete; it can only drop a layer of
//     protection that conditions 3 and 4 still enforce.
//
//  3. No worktrees are left, checked after `git worktree prune` has dropped the
//     entries whose task dirs the GC already removed. A live worktree's .git
//     points into this directory, so removing it would break that checkout.
//
//  4. No task has created a worktree from it within GCRepoTTL. An unknown
//     stamp is stamped and skipped, never treated as ancient — see
//     repocache.LastUsed.
//
// Evicting wrongly costs time, not correctness: the next task that needs the
// repo takes the cache-miss path in ensureRepoReady, which re-syncs and
// re-clones on demand.
func (d *Daemon) evictRepoCacheLocked(ctx context.Context, barePath string, stats *gcStats) {
	if d.cfg.GCRepoTTL <= 0 {
		return
	}
	// Cheap early-out so an attached repo — the common case — never pays for
	// the git and filesystem work below.
	if d.repoBarePathIsLive(barePath) {
		return
	}

	worktrees, err := linkedWorktreeCountContext(ctx, barePath)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		d.logger.Warn("gc: worktree count failed", "repo", barePath, "error", err)
		return
	}
	if worktrees > 0 {
		return
	}

	lastUsed, ok := repocache.LastUsed(barePath)
	if !ok {
		// A cache created before the stamp existed. Start its clock now; the
		// alternative reading of "unknown" would evict every pre-upgrade cache
		// on the machine in the first cycle after an upgrade.
		repocache.MarkUsed(barePath, d.logger)
		return
	}
	idle := time.Since(lastUsed)
	if idle <= d.cfg.GCRepoTTL {
		return
	}

	// Measure before the final check, not after. dirSize walks every file in
	// the repo, which on a multi-GiB cache takes long enough for a workspace to
	// re-attach underneath us — putting it between the check and the delete
	// would reopen most of the window this check exists to close.
	bytes, err := dirSizeContext(ctx, barePath)
	if err != nil {
		return
	}

	// Ask again immediately before deleting. The checks above run git and walk
	// the filesystem, and a workspace can re-attach this repo while they do;
	// re-reading in-memory state costs one mutex and no network, and shrinks
	// the window from "the whole .repos walk" to these two adjacent statements.
	if d.repoBarePathIsLive(barePath) {
		return
	}

	if err := os.RemoveAll(barePath); err != nil {
		d.logger.Warn("gc: repo cache remove failed", "repo", barePath, "error", err)
		return
	}
	stats.repoCachesReclaimed++
	stats.bytesReclaimed += bytes
	d.logger.Info("gc: repo cache evicted",
		"repo", filepath.Base(barePath),
		"workspace", filepath.Base(filepath.Dir(barePath)),
		"last_used", lastUsed.UTC().Format(time.RFC3339),
		"idle", idle.Round(time.Hour),
		"bytes_reclaimed", bytes,
	)
}

// linkedWorktreeCount returns how many linked worktrees a bare repo still has.
// `git worktree list --porcelain` emits one blank-line-separated block per
// worktree and marks the bare repo's own block with a `bare` line; only the
// linked blocks represent checkouts that would break if the repo went away.
func linkedWorktreeCount(barePath string) (int, error) {
	return linkedWorktreeCountContext(context.Background(), barePath)
}

func linkedWorktreeCountContext(ctx context.Context, barePath string) (int, error) {
	out, err := runGitGCCommandContext(ctx, barePath, "worktree", "list", "--porcelain")
	if err != nil {
		return 0, err
	}

	count := 0
	inBlock := false
	isBare := false
	flush := func() {
		if inBlock && !isBare {
			count++
		}
		inBlock = false
		isBare = false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			inBlock = true
		case line == "bare":
			isBare = true
		}
	}
	flush()
	return count, nil
}

func (d *Daemon) pruneWorktreeLocked(ctx context.Context, barePath string) {
	if out, err := runGitGCCommandContext(ctx, barePath, "worktree", "prune"); err != nil {
		if ctx.Err() != nil {
			return
		}
		d.logger.Warn("gc: worktree prune failed",
			"repo", barePath,
			"output", out,
			"error", err,
		)
	}

	activeBranches, err := agentWorktreeBranchesContext(ctx, barePath)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		d.logger.Warn("gc: worktree branch scan failed", "repo", barePath, "error", err)
		return
	}

	agentBranches, err := listAgentBranchesContext(ctx, barePath)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		d.logger.Warn("gc: agent branch scan failed", "repo", barePath, "error", err)
		return
	}

	deleted := 0
	for _, branch := range agentBranches {
		if _, ok := activeBranches[branch]; ok {
			continue
		}
		if out, err := runGitGCCommandContext(ctx, barePath, "branch", "-D", "--", branch); err != nil {
			if ctx.Err() != nil {
				return
			}
			d.logger.Warn("gc: agent branch delete failed",
				"repo", barePath,
				"branch", branch,
				"output", out,
				"error", err,
			)
			continue
		}
		deleted++
	}
	markerPath := filepath.Join(barePath, repoMaintenanceMarker)
	pending := deleted > 0
	if pending {
		if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
			d.logger.Warn("gc: record pending repo maintenance failed", "repo", barePath, "error", err)
		}
		d.logger.Info("gc: deleted stale agent branches", "repo", barePath, "count", deleted)
	} else if _, err := os.Stat(markerPath); err == nil {
		pending = true
	}
	if !pending {
		return
	}
	if !d.cfg.GCRepoMaintenanceEnabled {
		d.logger.Debug("gc: heavy repo maintenance disabled", "repo", barePath)
		return
	}
	// Agent CLIs can mutate linked-worktree refs directly, outside the daemon's
	// in-process repository gate. Do not start heavy maintenance while any task
	// is active; a new task or checkout that arrives after this check preempts
	// through the maintenance context below.
	if d.activeTasks.Load() > 0 {
		d.logger.Debug("gc: heavy repo maintenance deferred while tasks are active", "repo", barePath)
		return
	}

	// Heavier maintenance only runs when we actually removed refs, so we don't
	// turn every GC tick into a full `git gc --prune` on every cached repo. The
	// prune step gets its own longer timeout because it can take minutes on a
	// real bare cache; under the shared 30s budget it would be killed mid-run.
	maintenance := []struct {
		args    []string
		timeout time.Duration
	}{
		{args: []string{"reflog", "expire", "--expire=30.days", "--all"}, timeout: gitCmdTimeout},
		{args: []string{"gc", "--prune=30.days"}, timeout: gitMaintenanceTimeout},
	}
	completed := true
	for _, step := range maintenance {
		if ctx.Err() != nil || d.activeTasks.Load() > 0 {
			return
		}
		before := snapshotRepoMaintenanceLocks(barePath)
		if out, err := runGitCommandContext(ctx, barePath, step.timeout, step.args...); err != nil {
			completed = false
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, repocache.ErrMaintenancePreempted) {
				d.cleanupNewRepoMaintenanceLocks(barePath, before)
			}
			if errors.Is(context.Cause(ctx), repocache.ErrMaintenancePreempted) {
				d.logger.Info("gc: git maintenance preempted for foreground work",
					"repo", barePath,
					"command", strings.Join(step.args, " "),
				)
				return
			}
			d.logger.Warn("gc: git maintenance failed",
				"repo", barePath,
				"command", strings.Join(step.args, " "),
				"output", out,
				"error", err,
			)
		}
	}
	if completed {
		if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("gc: clear pending repo maintenance failed", "repo", barePath, "error", err)
		}
	}
}

func runGitGCCommand(barePath string, args ...string) (string, error) {
	return runGitGCCommandContext(context.Background(), barePath, args...)
}

func runGitCommand(barePath string, timeout time.Duration, args ...string) (string, error) {
	return runGitCommandContext(context.Background(), barePath, timeout, args...)
}

func runGitGCCommandContext(ctx context.Context, barePath string, args ...string) (string, error) {
	return runGitCommandContext(ctx, barePath, gitCmdTimeout, args...)
}

func runGitCommandContext(parent context.Context, barePath string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmdArgs := append([]string{"-C", barePath}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := processtree.CombinedOutput(ctx, cmd, 5*time.Second)
	return strings.TrimSpace(string(out)), err
}

type repoMaintenanceLockSnapshot map[string]struct{}

// snapshotRepoMaintenanceLocks records only lock paths known to be produced by
// the maintenance commands below. Cleanup later removes a path only if it did
// not exist in this snapshot and the process tree is confirmed gone. Checkout
// waits on the same repo gate, and task dispatch waits for CancelMaintenance's
// barrier, so no agent Git work can create a competing lock before cleanup.
func snapshotRepoMaintenanceLocks(barePath string) repoMaintenanceLockSnapshot {
	locks := make(repoMaintenanceLockSnapshot)
	for _, path := range repoMaintenanceLockPaths(barePath) {
		locks[path] = struct{}{}
	}
	return locks
}

func repoMaintenanceLockPaths(barePath string) []string {
	var locks []string
	for _, name := range []string{"gc.pid", "packed-refs.lock"} {
		path := filepath.Join(barePath, name)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			locks = append(locks, path)
		}
	}
	for _, root := range []string{
		filepath.Join(barePath, "refs"),
		filepath.Join(barePath, "logs", "refs"),
		filepath.Join(barePath, "objects", "info"),
		filepath.Join(barePath, "objects", "pack"),
	} {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&linkedDirModes != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lock") {
				locks = append(locks, path)
			}
			return nil
		})
	}
	return locks
}

func (d *Daemon) cleanupNewRepoMaintenanceLocks(barePath string, before repoMaintenanceLockSnapshot) {
	for _, path := range repoMaintenanceLockPaths(barePath) {
		if _, existed := before[path]; existed {
			continue
		}
		rel, err := filepath.Rel(barePath, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			d.logger.Warn("gc: refused maintenance lock cleanup outside repo", "repo", barePath, "path", path)
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("gc: maintenance lock cleanup failed", "repo", barePath, "lock", rel, "error", err)
			continue
		}
		d.logger.Info("gc: removed lock left by interrupted maintenance", "repo", barePath, "lock", rel)
	}
}

func agentWorktreeBranches(barePath string) (map[string]struct{}, error) {
	return agentWorktreeBranchesContext(context.Background(), barePath)
}

func agentWorktreeBranchesContext(ctx context.Context, barePath string) (map[string]struct{}, error) {
	out, err := runGitGCCommandContext(ctx, barePath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	branches := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "branch refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(line, "branch refs/heads/")
		if strings.HasPrefix(branch, "agent/") {
			branches[branch] = struct{}{}
		}
	}
	return branches, nil
}

func listAgentBranches(barePath string) ([]string, error) {
	return listAgentBranchesContext(context.Background(), barePath)
}

func listAgentBranchesContext(ctx context.Context, barePath string) ([]string, error) {
	// Trailing slash narrows the pattern to the `agent/` namespace only. Without
	// it, `for-each-ref` would also return a branch literally named `agent`,
	// which `agentWorktreeBranches` ignores — that branch would then be deleted.
	out, err := runGitGCCommandContext(ctx, barePath, "for-each-ref", "--format=%(refname:short)", "refs/heads/agent/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		branches = append(branches, branch)
	}
	return branches, nil
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
