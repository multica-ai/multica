package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/codechange"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// A run's code change (MUL-7651): what the run changed in each repository it
// worked in, diffed when it ends and uploaded before the run is reported
// done, so the issue page can show it on the run's reply.
//
// Two ways a run works in a repository, two ways to find its change:
//
//   - Local worktree: the run delivers onto a branch in the user's repo, and
//     Finalize knows exactly which commits it added (baseline..tip).
//   - Repository checkout (`multica repo checkout`): the run commits in a
//     checkout inside its workdir, so its change is that checkout's HEAD when
//     the run first saw it against HEAD when the run ends.
//
// Each change travels as a "run" row and, when the delivered branch carries
// more than this run did, a "branch" row: the whole line of work, for the
// issue-wide view when no pull request can supply it.

// codeChangeCollectTimeout bounds diffing after the agent has exited. The run
// is already over; this only delays its completion report.
const codeChangeCollectTimeout = 2 * time.Minute

// TaskCodeChangeReport is one row of a run's code change upload.
type TaskCodeChangeReport struct {
	Scope          string            `json:"scope"`
	Source         string            `json:"source"`
	RepoKey        string            `json:"repo_key"`
	RepoLabel      string            `json:"repo_label"`
	RepoURL        string            `json:"repo_url,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	BaseRef        string            `json:"base_ref,omitempty"`
	BaseCommit     string            `json:"base_commit"`
	HeadCommit     string            `json:"head_commit"`
	FileCount      int               `json:"file_count"`
	Additions      int               `json:"additions"`
	Deletions      int               `json:"deletions"`
	Files          []codechange.File `json:"files"`
	FilesTruncated bool              `json:"files_truncated,omitempty"`
	Patch          string            `json:"patch,omitempty"`
	PatchOmitted   string            `json:"patch_omitted,omitempty"`
}

const (
	codeChangeSourceLocalWorktree = "local_worktree"
	codeChangeSourceRepoCheckout  = "repo_checkout"
)

func newCodeChangeReport(scope, source string, repo codechange.Repository, branch, baseRef string, d codechange.Diff) TaskCodeChangeReport {
	files := d.Files
	if files == nil {
		files = []codechange.File{}
	}
	return TaskCodeChangeReport{
		Scope:          scope,
		Source:         source,
		RepoKey:        repo.Key,
		RepoLabel:      repo.Label,
		RepoURL:        repo.URL,
		Branch:         branch,
		BaseRef:        baseRef,
		BaseCommit:     d.BaseCommit,
		HeadCommit:     d.HeadCommit,
		FileCount:      d.FileCount,
		Additions:      d.Additions,
		Deletions:      d.Deletions,
		Files:          files,
		FilesTruncated: d.FilesTruncated,
		Patch:          d.Patch,
		PatchOmitted:   d.PatchOmitted,
	}
}

// collectRepoChanges diffs one repository: the run's own change from runBase,
// and — when lineBase is set and differs — the branch's whole line of work.
// Returns nothing when the run changed nothing here.
func collectRepoChanges(ctx context.Context, dir, source string, repo codechange.Repository, branch, runBase, head, lineBase, baseRef string) ([]TaskCodeChangeReport, error) {
	if runBase == "" || head == "" || runBase == head {
		return nil, nil
	}
	run, err := codechange.Collect(ctx, dir, runBase, head)
	if err != nil {
		return nil, fmt.Errorf("diff run %s..%s: %w", runBase, head, err)
	}
	if run.Empty() {
		return nil, nil
	}
	reports := []TaskCodeChangeReport{newCodeChangeReport("run", source, repo, branch, baseRef, run)}
	if lineBase == "" || lineBase == runBase || lineBase == head {
		return reports, nil
	}
	line, err := codechange.Collect(ctx, dir, lineBase, head)
	if err != nil {
		// The run's own change still stands; the issue-wide view falls back
		// to it.
		return reports, nil
	}
	return append(reports, newCodeChangeReport("branch", source, repo, branch, baseRef, line)), nil
}

// worktreeCodeChanges reads what a local worktree run delivered onto its
// branch. Finalize has already committed everything and removed the worktree;
// the commits live on in the user's repository.
func worktreeCodeChanges(ctx context.Context, wt *execenv.LocalWorktree, outcome execenv.LocalWorktreeOutcome, logger *slog.Logger) []TaskCodeChangeReport {
	if wt == nil || outcome.Branch == "" || outcome.HeadCommit == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codeChangeCollectTimeout)
	defer cancel()

	lineBase, err := execenv.LocalWorktreeLineStart(wt.GitRoot, outcome.HeadCommit)
	if err != nil {
		lineBase = ""
	}
	repo := codechange.Identify(ctx, wt.GitRoot, wt.GitRoot)
	reports, err := collectRepoChanges(ctx, wt.GitRoot, codeChangeSourceLocalWorktree, repo,
		outcome.Branch, outcome.BaseCommit, outcome.HeadCommit, lineBase, "")
	if err != nil && logger != nil {
		logger.Warn("code change: could not diff the worktree delivery (non-fatal)",
			"git_root", wt.GitRoot, "branch", outcome.Branch, "error", err)
	}
	return reports
}

// checkoutChangeTracker remembers where each repository checkout in a run's
// workdir stood when the run first saw it, so the run's change is exactly the
// commits it added there.
type checkoutChangeTracker struct {
	mu     sync.Mutex
	starts map[string]string // checkout path → HEAD at the run's start
}

// newCheckoutChangeTracker records the checkouts a reused workdir already
// holds. Checkouts the run makes later are recorded as they are made.
func newCheckoutChangeTracker(ctx context.Context, workDir string) *checkoutChangeTracker {
	t := &checkoutChangeTracker{starts: map[string]string{}}
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return t
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(workDir, entry.Name())
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			continue
		}
		if head, err := codechange.RevParse(ctx, path, "HEAD"); err == nil && head != "" {
			t.starts[path] = head
		}
	}
	return t
}

// noteCheckout records a checkout the run just made. A checkout that was
// created or moved to a new branch starts over from its new HEAD; one that
// was kept as it was keeps the start already recorded, so commits the run
// made before checking it out again still count.
func (t *checkoutChangeTracker) noteCheckout(ctx context.Context, path string, kept bool) {
	if t == nil || path == "" {
		return
	}
	head, err := codechange.RevParse(ctx, path, "HEAD")
	if err != nil || head == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, seen := t.starts[path]; seen && kept {
		return
	}
	t.starts[path] = head
}

// collect diffs every tracked checkout whose HEAD moved during the run.
func (t *checkoutChangeTracker) collect(ctx context.Context, logger *slog.Logger) []TaskCodeChangeReport {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	starts := make(map[string]string, len(t.starts))
	for path, head := range t.starts {
		starts[path] = head
	}
	t.mu.Unlock()
	if len(starts) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codeChangeCollectTimeout)
	defer cancel()

	paths := make([]string, 0, len(starts))
	for path := range starts {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var reports []TaskCodeChangeReport
	for _, path := range paths {
		start := starts[path]
		head, err := codechange.RevParse(ctx, path, "HEAD")
		if err != nil || head == "" || head == start {
			continue
		}
		baseRef, baseSHA := codechange.RemoteDefaultRef(ctx, path)
		lineBase := ""
		if baseSHA != "" {
			if mb, err := codechange.MergeBase(ctx, path, baseSHA, head); err == nil {
				lineBase = mb
			}
		}
		repo := codechange.Identify(ctx, path, path)
		repoReports, err := collectRepoChanges(ctx, path, codeChangeSourceRepoCheckout, repo,
			codechange.CurrentBranch(ctx, path), start, head, lineBase, baseRef)
		if err != nil {
			if logger != nil {
				logger.Warn("code change: could not diff a repository checkout (non-fatal)", "path", path, "error", err)
			}
			continue
		}
		reports = append(reports, repoReports...)
	}
	return reports
}

// reportTaskCodeChanges uploads a finished run's code changes. Best effort:
// the run's result does not depend on it, and an older server without the
// endpoint simply answers 404.
func (d *Daemon) reportTaskCodeChanges(ctx context.Context, taskID string, changes []TaskCodeChangeReport, taskLog *slog.Logger) {
	if len(changes) == 0 {
		return
	}
	if err := d.client.ReportTaskCodeChanges(ctx, taskID, changes); err != nil {
		taskLog.Warn("report run code changes failed (non-fatal)", "changes", len(changes), "error", err)
		return
	}
	taskLog.Info("reported run code changes", "changes", len(changes))
}
