package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/rundiff"
)

// Run file changes: the daemon snapshots each git root's worktree into
// two pinned trees (before / after) inside the repository's own object
// database and uploads only the manifest. This file owns the local record of
// that: the per-task index under .run-file-changes/, the resident-ref
// lifecycle (task GC deletes the refs, a daily sweep removes orphans), and the
// relay responder that fetches blobs on demand when a user opens a file.

const (
	runDiffIndexVersion = 2
	// runDiffSweepInterval is the orphan-ref sweep cadence.
	runDiffSweepInterval = 24 * time.Hour
)

type runDiffIndex struct {
	// Version 2 is the resident index introduced with the resident index. Zero means a
	// legacy legacy spool whose upload-success cleanup (release pins,
	// delete file) still applies.
	Version        int                `json:"version"`
	TaskID         string             `json:"task_id"`
	CollectionID   string             `json:"collection_id,omitempty"`
	TaskDir        string             `json:"task_dir,omitempty"`
	Collection     rundiff.Collection `json:"collection"`
	Roots          []rundiff.RootRef  `json:"roots,omitempty"`
	UploadComplete bool               `json:"upload_complete,omitempty"`
	ExpiresAt      time.Time          `json:"expires_at,omitempty"`
	// Pins belongs to legacy spools only; resident indexes never set it.
	Pins map[string]string `json:"pins,omitempty"`
}

func (d *Daemon) runDiffDir() string {
	sum := sha256.Sum256([]byte(d.cfg.ServerBaseURL + "/" + d.cfg.DaemonID))
	return filepath.Join(d.cfg.WorkspacesRoot, ".run-file-changes", hex.EncodeToString(sum[:12]))
}

func (d *Daemon) runDiffIndexPath(taskID string) string {
	return filepath.Join(d.runDiffDir(), taskID+".json")
}

func (d *Daemon) loadRunDiffIndex(taskID string) *runDiffIndex {
	if _, err := uuid.Parse(taskID); err != nil {
		return nil
	}
	raw, err := os.ReadFile(d.runDiffIndexPath(taskID))
	if err != nil {
		return nil
	}
	var s runDiffIndex
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	if s.TaskID != taskID {
		return nil
	}
	return &s
}

// uploadRunDiff strips the local-only blob OIDs before upload: the server
// stores the manifest, never the anchors of this machine's object database.
func (d *Daemon) uploadRunDiff(ctx context.Context, s runDiffIndex) error {
	c := s.Collection
	files := make([]rundiff.File, len(c.Files))
	copy(files, c.Files)
	c.Files = files
	for i := range c.Files {
		c.Files[i].BeforeOID = ""
		c.Files[i].AfterOID = ""
	}
	return d.client.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/file-changes", s.TaskID), c, nil)
}

func (d *Daemon) saveRunDiff(s runDiffIndex) error {
	if _, err := uuid.Parse(s.TaskID); err != nil {
		return err
	}
	if err := s.Collection.ValidateAndSanitize(); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	// Preserve metadata when JSON escaping expands the text beyond the wire cap.
	if len(raw) > rundiff.MaxUploadBytes {
		for i := range s.Collection.Files {
			f := &s.Collection.Files[i]
			if f.Before != nil || f.After != nil {
				f.Before = nil
				f.After = nil
				f.BodyStatus = "omitted"
				f.Reason = "encoded_body_budget"
			}
		}
		s.Collection.Status = "partial"
		raw, err = json.Marshal(s)
		if err != nil {
			return err
		}
	}
	dir := d.runDiffDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, d.runDiffIndexPath(s.TaskID)); err != nil {
		return err
	}
	// Windows does not expose directory fsync through os.File. The file itself
	// was synced above; interrupted rename remains unavailable on the server.
	if runtime.GOOS == "windows" {
		return nil
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func (d *Daemon) startRunDiff(ctx context.Context, task Task, workdir string, log *slog.Logger) *rundiff.Collector {
	if d.cfg.WorkspacesRoot == "" {
		return nil
	}
	c := rundiff.New(workdir)
	s := runDiffIndex{Version: runDiffIndexVersion, TaskID: task.ID, TaskDir: workdir, Collection: c.Pending()}
	beginCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := d.uploadRunDiff(beginCtx, s)
	cancel()
	if err != nil {
		// Old/public servers do not expose this endpoint; the UI shows unavailable.
		var req *requestError
		if errors.As(err, &req) && req.StatusCode == 404 {
			return nil
		}
		log.Warn("run diff registration failed", "error", err)
		return nil
	}
	if err := d.saveRunDiff(s); err != nil {
		log.Warn("run diff index unavailable", "error", err)
		return nil
	}
	// Journal the ref anchors before each ref is created, so a crash between
	// journal and update-ref leaves the sweep — not the user — holding the bag.
	c.SetRefJournal(func(refs []rundiff.RootRef) error {
		s.Roots = refs
		return d.saveRunDiff(s)
	})
	s.Collection = c.Start(ctx)
	s.CollectionID = s.Collection.CollectionID
	s.Roots = c.RootRefs()
	if err := d.saveRunDiff(s); err != nil {
		c.Abort()
		log.Warn("run diff baseline index failed", "error", err)
		return nil
	}
	return c
}

func (d *Daemon) finishRunDiff(ctx context.Context, task Task, c *rundiff.Collector, log *slog.Logger) {
	if c == nil {
		return
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	s := d.loadRunDiffIndex(task.ID)
	if s == nil {
		s = &runDiffIndex{Version: runDiffIndexVersion, TaskID: task.ID}
	}
	s.Collection = c.Finish(finishCtx)
	s.CollectionID = s.Collection.CollectionID
	s.Roots = c.RootRefs()
	if err := d.saveRunDiff(*s); err != nil {
		log.Warn("run diff final index failed", "error", err)
		return
	}
	// Upload the exact durable representation so retries are byte-equivalent.
	// The index and its refs stay resident: they are the record the relay
	// serves from until task GC.
	d.flushRunDiffFile(finishCtx, d.runDiffIndexPath(task.ID), false)
}

func (d *Daemon) flushRunDiffFile(ctx context.Context, path string, recoverInterrupted bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > rundiff.MaxUploadBytes+65536 {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var s runDiffIndex
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	if _, err := uuid.Parse(s.TaskID); err != nil {
		return
	}
	if s.Version < runDiffIndexVersion {
		d.flushLegacyRunDiffSpool(ctx, path, &s, info, recoverInterrupted)
		return
	}
	if s.Collection.ValidateAndSanitize() != nil {
		return
	}
	if s.Collection.Status == "collecting" {
		if !recoverInterrupted {
			return
		}
		// The daemon restarted with an unfinished collection: no diff can be
		// shown, so retire the record — terminal manifest, refs deleted,
		// index removed.
		s.Collection.Status = "unavailable"
		s.Collection.Reason = "interrupted"
		s.Collection.TotalsComplete = false
		d.deleteRunDiffRootRefs(s.Roots)
		if err := d.saveRunDiff(s); err != nil {
			return
		}
		sendCtx, sendCancel := context.WithTimeout(ctx, 5*time.Second)
		_ = d.uploadRunDiff(sendCtx, s)
		sendCancel()
		_ = os.Remove(path)
		return
	}
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = info.ModTime().Add(7 * 24 * time.Hour)
	}
	if !s.UploadComplete {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = d.uploadRunDiff(sendCtx, s)
		cancel()
		var req *requestError
		s.UploadComplete = err == nil || errors.As(err, &req) && (req.StatusCode == 404 || req.StatusCode == 409) || time.Now().After(s.ExpiresAt)
		if s.UploadComplete {
			if err := d.saveRunDiff(s); err != nil {
				d.logger.Warn("run diff index persist failed", "error", err)
			}
		}
	}
	// Resident by design: neither the index nor the refs are removed here.
	// Task GC and the daily orphan sweep own their lifecycle.
}

// flushLegacyRunDiffSpool keeps the legacy cleanup contract for spools the
// previous daemon version wrote: pins are released once the upload reaches a
// terminal state and the file is removed when nothing is owed.
func (d *Daemon) flushLegacyRunDiffSpool(ctx context.Context, path string, s *runDiffIndex, info os.FileInfo, recoverInterrupted bool) {
	if s.Collection.ValidateAndSanitize() != nil {
		return
	}
	if s.Collection.Status == "collecting" {
		if !recoverInterrupted {
			return
		}
		s.Collection.Status = "unavailable"
		s.Collection.Reason = "interrupted"
		s.Collection.TotalsComplete = false
		if err := d.saveRunDiff(*s); err != nil {
			return
		}
	}
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = info.ModTime().Add(7 * 24 * time.Hour)
	}
	oldPinCount := len(s.Pins)
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Second)
	s.Pins = rundiff.ReleasePins(cleanupCtx, s.Pins, s.Collection.CollectionID)
	cleanupCancel()
	if !s.UploadComplete {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := d.uploadRunDiff(sendCtx, *s)
		cancel()
		var req *requestError
		s.UploadComplete = err == nil || errors.As(err, &req) && (req.StatusCode == 404 || req.StatusCode == 409) || time.Now().After(s.ExpiresAt)
	}
	if s.UploadComplete && len(s.Pins) == 0 {
		_ = os.Remove(path)
		return
	}
	// Never discard cleanup obligations because upload succeeded or expired.
	// Avoid rewriting unchanged failed uploads, preserving their expiry age.
	if s.UploadComplete || len(s.Pins) > 0 || oldPinCount > 0 {
		if err := d.saveRunDiff(*s); err != nil {
			d.logger.Warn("run diff cleanup journal failed", "error", err)
		}
	}
}

func (d *Daemon) flushRunDiffSpool(ctx context.Context, recoverInterrupted bool) {
	entries, err := os.ReadDir(d.runDiffDir())
	if err != nil {
		return
	}
	if recoverInterrupted {
		d.runDiffRecovery = map[string]bool{}
		for _, e := range entries {
			d.runDiffRecovery[e.Name()] = true
		}
	}
	if len(entries) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := d.runDiffCursor % len(entries)
	for n := 0; n < len(entries) && n < 20; n++ {
		if ctx.Err() != nil {
			return
		}
		i := (start + n) % len(entries)
		entry := entries[i]
		d.runDiffCursor = i + 1
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		d.flushRunDiffFile(ctx, filepath.Join(d.runDiffDir(), entry.Name()), d.runDiffRecovery[entry.Name()])
	}
}

// ---------------------------------------------------------------------------
// Relay responder ()
// ---------------------------------------------------------------------------

// handleFileChangesRequested reacts to a daemon:file_changes_requested hint by
// claiming the queued requests and answering each one from the pinned trees.
// Like the pending-work hint, the frame carries no work: a lost, duplicated,
// or stale hint is harmless, and an empty claim is the common case.
func (d *Daemon) handleFileChangesRequested(runtimeID string) {
	if runtimeID == "" || d.findRuntime(runtimeID) == nil {
		return
	}
	if d.fileChangePullUnsupported.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(d.recoveryContext(), 30*time.Second)
	defer cancel()
	requests, err := d.client.GetPendingFileChangeRequests(ctx, runtimeID)
	var reqErr *requestError
	if errors.As(err, &reqErr) && reqErr.StatusCode == 404 {
		// Server predates the relay; nothing to pull, stop probing.
		d.fileChangePullUnsupported.Store(true)
		d.logger.Info("run file change relay unsupported by server; disabling pull", "runtime_id", runtimeID)
		return
	}
	if err != nil {
		d.logger.Debug("run file change request pull failed", "runtime_id", runtimeID, "error", err)
		return
	}
	for _, request := range requests {
		d.serveFileChangeRequest(ctx, request)
	}
}

func (d *Daemon) serveFileChangeRequest(ctx context.Context, request FileChangeRequest) {
	resp := d.buildFileChangeResponse(ctx, request)
	postCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
		err := d.client.PostFileChangeResponse(postCtx, request.RequestID, resp)
		if err == nil {
			return
		}
		var reqErr *requestError
		if errors.As(err, &reqErr) && (reqErr.StatusCode == 404 || reqErr.StatusCode == 400) {
			// Expired, unknown, or rejected — retrying cannot fix it.
			return
		}
		lastErr = err
	}
	if lastErr != nil {
		d.logger.Warn("run file change response post failed", "request_id", request.RequestID, "error", lastErr)
	}
}

type fileChangeResponse struct {
	State string        `json:"state"`
	File  *rundiff.File `json:"file,omitempty"`
}

// buildFileChangeResponse reads the requested file's two sides from the pinned
// trees. Anything missing — index, manifest row, root anchor, ref, blob —
// answers purged: the local record is gone and the UI must say so instead of
// pretending nothing changed.
func (d *Daemon) buildFileChangeResponse(ctx context.Context, request FileChangeRequest) fileChangeResponse {
	purged := func() fileChangeResponse { return fileChangeResponse{State: "purged"} }
	s := d.loadRunDiffIndex(request.TaskID)
	if s == nil {
		return purged()
	}
	var row *rundiff.File
	for i := range s.Collection.Files {
		if s.Collection.Files[i].ID == request.FileID {
			row = &s.Collection.Files[i]
			break
		}
	}
	if row == nil {
		return purged()
	}
	var root *rundiff.RootRef
	for i := range s.Roots {
		if s.Roots[i].ID == row.RootID {
			root = &s.Roots[i]
			break
		}
	}
	if root == nil || root.GitCommonDir == "" {
		return purged()
	}
	gitDir := root.GitCommonDir
	if _, err := os.Stat(gitDir); err != nil {
		// The common dir is gone; the refs died with it.
		return purged()
	}
	var before, after []byte
	if row.Change != "added" {
		if root.BeforeRef == "" {
			return purged()
		}
		b, err := rundiff.ReadPinnedBlob(ctx, gitDir, root.BeforeRef, row.Path, rundiff.MaxSideBytes+1)
		if err != nil {
			return purged()
		}
		before = b
	}
	if row.Change != "deleted" {
		if root.AfterRef == "" {
			return purged()
		}
		b, err := rundiff.ReadPinnedBlob(ctx, gitDir, root.AfterRef, row.Path, rundiff.MaxSideBytes+1)
		if err != nil {
			return purged()
		}
		after = b
	}
	if len(before) > rundiff.MaxSideBytes || len(after) > rundiff.MaxSideBytes {
		// Cannot happen for rows collected as available; treat drift as purged
		// rather than serving a body the manifest does not promise.
		return purged()
	}
	f := rundiff.AssembleRelayFile(*row, before, after)
	f.BeforeOID, f.AfterOID = "", ""
	return fileChangeResponse{State: "ok", File: f}
}

// ---------------------------------------------------------------------------
// Lifecycle: task GC hook and the daily orphan sweep
// ---------------------------------------------------------------------------

// releaseRunDiffForTask deletes the task's pinned refs and its index. Called
// from cleanTaskDir immediately before the task directory is removed, so the
// refs never outlive the record that explains them.
func (d *Daemon) releaseRunDiffForTask(taskID string) {
	s := d.loadRunDiffIndex(taskID)
	if s == nil {
		return
	}
	d.deleteRunDiffRootRefs(s.Roots)
	_ = os.Remove(d.runDiffIndexPath(taskID))
}

func (d *Daemon) deleteRunDiffRootRefs(roots []rundiff.RootRef) {
	for _, root := range roots {
		dir := root.GitCommonDir
		if dir == "" {
			dir = root.Dir
		}
		if dir == "" {
			continue
		}
		for _, ref := range []string{root.BeforeRef, root.AfterRef} {
			if ref == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := rundiff.DeleteGitRef(ctx, dir, ref); err != nil {
				d.logger.Debug("run diff ref delete failed", "ref", ref, "error", err)
			}
			cancel()
		}
	}
}

// sweepOrphanRunDiffRefs runs at most once per day (called from runGC):
//
//   - indexes whose task directory is gone lose their refs and the index —
//     the crash residue of a GC that never reached the hook;
//   - refs/multica/run-diff/* in the repo caches that no index references are
//     deleted outright. Ref names embed the collection UUID, so matching by
//     name across repos is safe and immune to path-normalization drift.
//
// Mirrors the retry tolerance of ReleasePins: failures are logged at debug and
// retried by the next sweep.
func (d *Daemon) sweepOrphanRunDiffRefs(ctx context.Context) {
	last := d.runDiffSweepAt.Load()
	now := time.Now()
	if last != 0 && now.Sub(time.Unix(0, last)) < runDiffSweepInterval {
		return
	}
	if !d.runDiffSweepAt.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	entries, err := os.ReadDir(d.runDiffDir())
	if err != nil {
		return
	}
	referenced := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		taskID := strings.TrimSuffix(entry.Name(), ".json")
		if _, err := uuid.Parse(taskID); err != nil {
			continue
		}
		s := d.loadRunDiffIndex(taskID)
		if s == nil {
			continue
		}
		if d.runDiffIndexTaskGone(s) {
			d.deleteRunDiffRootRefs(s.Roots)
			_ = os.Remove(d.runDiffIndexPath(taskID))
			d.logger.Info("run diff: swept index for missing task dir", "task_id", taskID)
			continue
		}
		for _, root := range s.Roots {
			if root.BeforeRef != "" {
				referenced[root.BeforeRef] = true
			}
			if root.AfterRef != "" {
				referenced[root.AfterRef] = true
			}
		}
	}
	sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, gitDir := range d.repoCacheGitDirs() {
		refs, err := rundiff.ListRunDiffRefs(sweepCtx, gitDir)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			if referenced[ref] {
				continue
			}
			if err := rundiff.DeleteGitRef(sweepCtx, gitDir, ref); err == nil {
				d.logger.Info("run diff: swept orphan ref", "repo", gitDir, "ref", ref)
			}
		}
	}
}

// runDiffIndexTaskGone reports whether the index's task directory no longer
// exists. TaskDir is authoritative; fall back to the roots' own dirs when a
// legacy write left it empty.
func (d *Daemon) runDiffIndexTaskGone(s *runDiffIndex) bool {
	if s.TaskDir != "" {
		if _, err := os.Stat(s.TaskDir); os.IsNotExist(err) {
			return true
		}
		return false
	}
	if len(s.Roots) == 0 {
		return false
	}
	for _, root := range s.Roots {
		if _, err := os.Stat(root.Dir); err == nil {
			return false
		}
	}
	return true
}

// repoCacheGitDirs lists the bare repositories of every workspace's repo
// cache, plus the git dirs the indexes themselves anchor.
func (d *Daemon) repoCacheGitDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	root := d.cfg.WorkspacesRoot
	workspaceEntries, err := os.ReadDir(root)
	if err == nil {
		for _, ws := range workspaceEntries {
			if !ws.IsDir() || strings.HasPrefix(ws.Name(), ".") {
				continue
			}
			reposRoot := filepath.Join(root, ws.Name(), ".repos")
			repoEntries, err := os.ReadDir(reposRoot)
			if err != nil {
				continue
			}
			for _, repo := range repoEntries {
				if !repo.IsDir() {
					continue
				}
				// One bare cache per repo entry; descend one level for the
				// hash-named layout the cache uses.
				inner, err := os.ReadDir(filepath.Join(reposRoot, repo.Name()))
				if err != nil {
					add(filepath.Join(reposRoot, repo.Name()))
					continue
				}
				for _, candidate := range inner {
					if candidate.IsDir() {
						add(filepath.Join(reposRoot, repo.Name(), candidate.Name()))
					}
				}
			}
		}
	}
	// Also sweep the git dirs live indexes anchor (task-local metadata), so a
	// crash mid-run cannot strand refs outside the cache layout.
	entries, err := os.ReadDir(d.runDiffDir())
	if err == nil {
		for _, entry := range entries {
			s := d.loadRunDiffIndex(strings.TrimSuffix(entry.Name(), ".json"))
			if s == nil {
				continue
			}
			for _, root := range s.Roots {
				if root.GitCommonDir != "" {
					add(root.GitCommonDir)
				}
			}
		}
	}
	return out
}
