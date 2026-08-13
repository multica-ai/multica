package execenv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Local worktree mode gives every task on a local_directory resource its own
// git worktree of the user's repo, created inside the daemon-owned env root.
// Tasks on the same directory then run concurrently instead of queueing on the
// per-path mutex, and each one delivers its work as a branch in the user's own
// repo — discoverable with `git branch`, no new result channel needed.
//
// Three properties this file exists to guarantee:
//
//  1. The agent sees what the user sees. `git worktree add` alone would check
//     out HEAD, silently hiding the user's uncommitted work. We replay the
//     dirty state into the worktree instead (tracked edits via a stash commit,
//     untracked files by copy).
//  2. The user's directory is never written to. Everything — including the
//     sidecar context files Prepare writes — lands inside the worktree, which
//     is disposable. The only lasting effect on the user's repo is the branch.
//  3. Nothing is silently discarded. Whatever the agent leaves uncommitted is
//     committed to the branch before the worktree goes away.

const (
	// localWorktreeDirName is the env-root-relative directory holding the
	// worktree. Kept short: on Windows the worktree path plus the deepest
	// repo path must stay under MAX_PATH for tools that predate long paths.
	localWorktreeDirName = "worktree"

	// gitTimeout bounds every git invocation this file makes. These are all
	// local-only operations (no network), so a slow one means a wedged index
	// lock rather than a slow remote; failing the task beats hanging a daemon
	// slot forever.
	gitTimeout = 2 * time.Minute

	// maxUntrackedFiles / maxUntrackedBytes bound the untracked-file replay.
	// `--exclude-standard` already drops anything gitignored (node_modules,
	// build output, venvs), so a repo hitting these limits has an unusual
	// amount of untracked-but-not-ignored content. We copy up to the bound and
	// report the remainder rather than silently truncating or hanging on a
	// multi-gigabyte copy.
	maxUntrackedFiles = 2000
	maxUntrackedBytes = 200 << 20 // 200 MiB
)

// gitRootLocks serialises git admin operations per repository. Concurrent
// `git worktree add` / `remove` / `prune` on one repo race on the same
// lockfiles (worktrees/, packed-refs.lock, config.lock), and unlike a fetch
// these are fast, so a plain mutex costs nothing. Keyed by the repo root so
// tasks on different repos never wait on each other.
var gitRootLocks sync.Map // gitRoot -> *sync.Mutex

func lockGitRoot(gitRoot string) func() {
	v, _ := gitRootLocks.LoadOrStore(gitRoot, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// LocalWorktreeParams describes the worktree Prepare should build for a
// local_directory task running in worktree mode.
type LocalWorktreeParams struct {
	// LocalPath is the user's configured directory. It may be the repo root
	// or any subdirectory of it; the worktree always covers the whole repo,
	// and the agent's cwd is the matching subdirectory inside it.
	LocalPath string
	// EnvRoot is the daemon-owned task env root. The worktree is created
	// inside it so the ordinary env-root GC reclaims it.
	EnvRoot string
	// AgentName and TaskID name the branch: agent/<name>/<short-task-id>.
	AgentName string
	TaskID    string
}

// LocalWorktree is a prepared worktree plus everything the daemon needs to
// finalize it after the agent exits.
type LocalWorktree struct {
	// GitRoot is the user's repository root — the repo that owns the branch.
	GitRoot string
	// Path is the worktree root inside the env root.
	Path string
	// WorkDir is the agent's cwd: Path, plus the offset of LocalPath inside
	// the repo when the user pointed the resource at a subdirectory.
	WorkDir string
	// Branch is the branch created for this task, in the user's repo.
	Branch string
	// BaseCommit is the commit the worktree started from. Finalize compares
	// the branch tip against it to decide whether the task produced anything.
	BaseCommit string
	// DirtyBaseCaptured records that the user had uncommitted tracked edits
	// which were replayed into the worktree.
	DirtyBaseCaptured bool
	// aborted, when set, makes Finalize refuse to commit or remove anything.
	// Set by the daemon when a pre-commit step failed in a way that would make
	// the committed branch wrong (see AbortWithReason).
	aborted error
	// UntrackedCopied / UntrackedSkipped report the untracked-file replay.
	// A non-zero skip count means the bounds below were hit and the agent is
	// looking at less than the user has on disk; it is logged at warn level so
	// the gap is findable rather than invisible.
	UntrackedCopied  int
	UntrackedSkipped int
}

// LocalWorktreeOutcome is what a finished worktree task delivered.
type LocalWorktreeOutcome struct {
	// Branch is the branch holding the task's work, or "" when the task made
	// no changes at all (a read-only run) — in that case the branch is deleted
	// so it never shows up in the user's `git branch` as an empty artifact.
	Branch string
	// AutoCommitted is true when the agent left uncommitted changes that
	// Finalize committed so they would survive the worktree's removal.
	AutoCommitted bool
	// PreservedPath is set only when Finalize could NOT commit the agent's
	// changes. The worktree at this path was intentionally left on disk because
	// it is the only remaining copy of that work.
	PreservedPath string
}

// PrepareLocalWorktree creates the task's worktree and replays the user's
// uncommitted state into it. It never writes to the user's working tree: the
// dirty state is read through `git stash create`, which builds a commit object
// without touching the index or the files on disk.
func PrepareLocalWorktree(params LocalWorktreeParams, logger *slog.Logger) (*LocalWorktree, error) {
	if params.LocalPath == "" {
		return nil, errors.New("execenv: local worktree requires a local path")
	}
	if params.EnvRoot == "" {
		return nil, errors.New("execenv: local worktree requires an env root")
	}
	if params.TaskID == "" {
		return nil, errors.New("execenv: local worktree requires a task id")
	}

	gitRoot, err := resolveGitRoot(params.LocalPath)
	if err != nil {
		return nil, err
	}

	// The agent's cwd keeps the user's chosen depth: a resource pointed at
	// <repo>/services/api must land the agent in <worktree>/services/api, not
	// at the repo root, or the task's whole notion of "the project" shifts.
	//
	// Canonicalise before the comparison: gitRoot comes back canonical, while
	// the configured path routinely isn't (on macOS every /tmp and /var path is
	// a symlink into /private). Comparing the two forms directly reads a repo
	// root as "outside itself".
	localPath := params.LocalPath
	if resolved, evalErr := filepath.EvalSymlinks(localPath); evalErr == nil {
		localPath = resolved
	}
	rel, err := filepath.Rel(gitRoot, localPath)
	if err != nil {
		return nil, fmt.Errorf("execenv: locate %q inside repo %q: %w", localPath, gitRoot, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("execenv: %q is not inside its repository root %q", localPath, gitRoot)
	}

	worktreePath := filepath.Join(params.EnvRoot, localWorktreeDirName)

	// Everything below mutates the repo's worktree admin state, so take the
	// per-repo lock first — including the stale-path cleanup, which runs `git
	// worktree remove` and would otherwise race a sibling task's `worktree add`.
	unlock := lockGitRoot(gitRoot)
	defer unlock()

	if _, statErr := os.Stat(worktreePath); statErr == nil {
		// Prepare wipes and recreates envRoot, so an existing worktree path
		// means a stale registration in the user's repo pointing here. Remove
		// both rather than failing the task.
		removeLocalWorktreeDir(gitRoot, worktreePath, logger)
	}

	// Self-heal registrations orphaned by a crashed daemon: their env roots are
	// long gone, but the user's repo still lists them. Prune only drops entries
	// whose directory no longer exists, so it can never disturb a live task.
	if out, pruneErr := runGit(gitRoot, "worktree", "prune"); pruneErr != nil && logger != nil {
		logger.Warn("execenv: git worktree prune failed (non-fatal)",
			"git_root", gitRoot, "output", out, "error", pruneErr)
	}

	headSHA, err := runGitTrimmed(gitRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("execenv: repository %q has no commit to branch from "+
			"(worktree mode needs at least one commit; make an initial commit or switch the resource back to in_place): %w", gitRoot, err)
	}

	// `git stash create` builds a commit capturing tracked modifications and
	// returns its sha WITHOUT stashing — the user's index and working tree are
	// untouched. Empty output means the tree is clean. The identity args cover
	// a repo with no user.email configured: writing a commit object needs a
	// committer, and without them the user's uncommitted work would be dropped
	// on a technicality.
	stashSHA, stashErr := runGitTrimmed(gitRoot, append(commitIdentityArgs(gitRoot), "stash", "create")...)
	if stashErr != nil {
		// Fail closed. The promise of this mode is that the agent reasons about
		// the code the user actually has; silently starting from HEAD instead
		// would have it review a tree the user never saw and report confidently
		// on it. A task that does not start is recoverable — one that answers
		// from the wrong sources is not.
		return nil, fmt.Errorf("execenv: could not capture the uncommitted changes in %q, "+
			"so the worktree would not match what you have on disk: %w", gitRoot, stashErr)
	}

	branch := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), shortID(params.TaskID))
	actualBranch, err := addLocalWorktree(gitRoot, worktreePath, branch, headSHA)
	if err != nil {
		return nil, err
	}

	wt := &LocalWorktree{
		GitRoot:    gitRoot,
		Path:       worktreePath,
		WorkDir:    filepath.Join(worktreePath, rel),
		Branch:     actualBranch,
		BaseCommit: headSHA,
	}

	// Replay tracked edits. Applied as unstaged modifications on top of HEAD so
	// the branch history stays linear and the agent sees the same
	// work-in-progress the user has open in their editor.
	//
	// Every failure below aborts the prepare and tears the worktree back down.
	// A half-replayed tree is the worst outcome available: it looks like a
	// working checkout, so nothing downstream questions it, while the agent
	// silently reads different code than the user has.
	if stashSHA != "" {
		if out, applyErr := runGit(worktreePath, "stash", "apply", stashSHA); applyErr != nil {
			removeLocalWorktreeDir(gitRoot, worktreePath, logger)
			deleteBranch(gitRoot, actualBranch, logger)
			return nil, fmt.Errorf("execenv: could not replay the uncommitted changes from %q into the task worktree "+
				"(the agent would have seen a different tree than you have): %s: %w",
				gitRoot, strings.TrimSpace(out), applyErr)
		}
		wt.DirtyBaseCaptured = true
	}

	copied, skipped, err := copyUntrackedFiles(gitRoot, worktreePath, logger)
	if err != nil {
		removeLocalWorktreeDir(gitRoot, worktreePath, logger)
		deleteBranch(gitRoot, actualBranch, logger)
		return nil, fmt.Errorf("execenv: could not replay the untracked files from %q into the task worktree: %w", gitRoot, err)
	}
	if skipped > 0 {
		// Any untracked file we could not reproduce makes the worktree a tree
		// the user would not recognise, so this fails rather than quietly
		// under-copying. Causes: the size/count bounds (usually build output
		// that should have been gitignored), an untracked symlink, or a file
		// that disappeared mid-snapshot. The message names the common fix
		// without claiming to know which one it was.
		removeLocalWorktreeDir(gitRoot, worktreePath, logger)
		deleteBranch(gitRoot, actualBranch, logger)
		return nil, fmt.Errorf("execenv: could not replay every untracked file from %q into the task worktree "+
			"(copied %d, %d left over; the replay covers regular files up to %d files / %d MiB and does not follow symlinks) "+
			"— gitignore or clean up the untracked files, or switch the resource back to in_place",
			gitRoot, copied, skipped, maxUntrackedFiles, maxUntrackedBytes>>20)
	}
	wt.UntrackedCopied = copied
	wt.UntrackedSkipped = skipped

	// Commit the replayed state as a baseline so "did this task change
	// anything?" has an exact answer later. Without it the user's own
	// uncommitted work counts as a change: a read-only task on a repo with an
	// untracked scratch file would auto-commit that file at the end and leave
	// behind a branch the agent never touched. The baseline also makes the
	// delivered branch readable — `git diff <baseline>..<branch>` is precisely
	// the agent's work, with the user's WIP as its own labelled commit.
	dirty, dirtyErr := worktreeIsDirty(worktreePath)
	if dirtyErr != nil {
		removeLocalWorktreeDir(gitRoot, worktreePath, logger)
		deleteBranch(gitRoot, actualBranch, logger)
		return nil, fmt.Errorf("execenv: could not inspect the prepared worktree for %q: %w", gitRoot, dirtyErr)
	}
	if dirty {
		baseline, baseErr := commitBaseline(worktreePath)
		if baseErr != nil {
			// Without a baseline the task cannot tell the user's work from the
			// agent's, so it would later commit the user's files as if the agent
			// had produced them. Refuse rather than deliver a misleading branch.
			removeLocalWorktreeDir(gitRoot, worktreePath, logger)
			deleteBranch(gitRoot, actualBranch, logger)
			return nil, fmt.Errorf("execenv: could not record a baseline commit for the replayed state of %q: %w", gitRoot, baseErr)
		}
		wt.BaseCommit = baseline
	}

	// Note on keeping sidecars out of the delivered branch: we deliberately do
	// NOT write .git/info/exclude here. A linked worktree reads info/exclude
	// from the repo's COMMON git dir, so the only file that would take effect
	// is the user's own .git/info/exclude — editing it would change what `git
	// status` shows in the user's checkout, which is theirs, not ours. Instead
	// the daemon runs the existing CleanupRuntimeConfig + CleanupSidecars pass
	// over the worktree before Finalize, so the sidecars are simply gone by the
	// time anything is committed. That also preserves a genuine agent edit to a
	// tracked CLAUDE.md, which a blanket exclude would have swallowed.

	if logger != nil {
		logger.Info("execenv: local worktree ready",
			"git_root", gitRoot,
			"path", worktreePath,
			"branch", actualBranch,
			"base", headSHA,
			"dirty_base_captured", wt.DirtyBaseCaptured,
			"untracked_copied", copied,
			"untracked_skipped", skipped,
		)
	}
	return wt, nil
}

// Finalize commits whatever the agent left behind, removes the worktree, and
// reports the branch. Called after the agent exits, before the env root is
// handed to the GC.
//
// The auto-commit is the reason a worktree task can't lose work: `git worktree
// remove --force` would happily delete uncommitted edits, and the user would
// have no way to get them back. Committing first turns "the agent edited files"
// into "the branch has a commit", which is the delivery contract for this mode.
//
// If that commit cannot be made — a repo with commit.gpgSign and no signing key
// available to the daemon, a full disk, a ref lock we lost — Finalize returns an
// error and DELIBERATELY LEAVES THE WORKTREE IN PLACE. Removing it would be the
// one operation in this file that destroys work with no way back, and a warning
// in the daemon log is not an acceptable substitute for the user's changes. The
// surviving worktree stays registered in the user's repo, so `git worktree list`
// points straight at it.
func (w *LocalWorktree) Finalize(logger *slog.Logger) (LocalWorktreeOutcome, error) {
	if w == nil {
		return LocalWorktreeOutcome{}, nil
	}
	unlock := lockGitRoot(w.GitRoot)
	defer unlock()

	outcome := LocalWorktreeOutcome{Branch: w.Branch}

	// Something before the commit went wrong in a way that would make the
	// delivered branch misleading. Commit nothing and keep the worktree: the
	// agent's work is still in it, and so is whatever the caller could not
	// clean up, which a human can now look at directly.
	if w.aborted != nil {
		// Report NO branch. One exists in the user's repo, but nothing was
		// committed to it, so naming it as this task's result would point them
		// at a branch that is missing the very work they are looking for. The
		// preserved worktree path below is the honest pointer.
		outcome.Branch = ""
		outcome.PreservedPath = w.Path
		if logger != nil {
			logger.Error("execenv: worktree finalize aborted; nothing committed, worktree kept for inspection",
				"path", w.Path, "branch", w.Branch, "git_root", w.GitRoot, "error", w.aborted)
		}
		return outcome, fmt.Errorf(
			"refusing to deliver branch %s: %w; the task worktree is preserved at %s (listed by `git worktree list` in %s)",
			w.Branch, w.aborted, w.Path, w.GitRoot)
	}

	// Treat "can't tell" like "dirty": committing costs an empty commit at
	// worst, while assuming clean risks deleting the agent's edits.
	dirty, statusErr := worktreeIsDirty(w.Path)
	if statusErr != nil {
		if logger != nil {
			logger.Warn("execenv: inspect worktree status failed; committing defensively",
				"path", w.Path, "error", statusErr)
		}
		dirty = true
	}
	if dirty {
		committed, err := w.commitAll(logger)
		if err != nil {
			outcome.PreservedPath = w.Path
			if logger != nil {
				logger.Error("execenv: could not commit the agent's changes; keeping the worktree so the work is recoverable",
					"path", w.Path, "branch", w.Branch, "git_root", w.GitRoot, "error", err)
			}
			return outcome, fmt.Errorf(
				"could not commit the agent's changes to branch %s: %w; the work is preserved in the worktree at %s (listed by `git worktree list` in %s) — recover it before that directory is reclaimed",
				w.Branch, err, w.Path, w.GitRoot)
		}
		outcome.AutoCommitted = committed
	}

	// A branch still sitting exactly on its base commit means the task changed
	// nothing — the read-only case. Delete it so the user's branch list only
	// ever grows for tasks that actually produced work.
	tip, err := runGitTrimmed(w.Path, "rev-parse", "--verify", "HEAD")
	producedWork := err != nil || tip != w.BaseCommit

	removeLocalWorktreeDir(w.GitRoot, w.Path, logger)

	if !producedWork {
		deleteBranch(w.GitRoot, w.Branch, logger)
		outcome.Branch = ""
	}

	if logger != nil {
		logger.Info("execenv: local worktree finalized",
			"git_root", w.GitRoot,
			"branch", outcome.Branch,
			"auto_committed", outcome.AutoCommitted,
			"produced_work", producedWork,
		)
	}
	return outcome, nil
}

// Discard tears a worktree down without delivering anything: unregister it,
// delete its directory, drop its branch.
//
// For the abandon-before-the-agent-ran case only. Finalize is the path that
// preserves work; this one assumes there is none to preserve, so callers must
// be sure nothing has run in the worktree yet.
func (w *LocalWorktree) Discard(logger *slog.Logger) {
	if w == nil {
		return
	}
	unlock := lockGitRoot(w.GitRoot)
	defer unlock()
	removeLocalWorktreeDir(w.GitRoot, w.Path, logger)
	deleteBranch(w.GitRoot, w.Branch, logger)
	if logger != nil {
		logger.Info("execenv: local worktree discarded before the agent ran",
			"git_root", w.GitRoot, "path", w.Path, "branch", w.Branch)
	}
}

// AbortWithReason marks the worktree undeliverable. Finalize will then commit
// nothing, remove nothing, and return an error naming the preserved path.
//
// This exists because the decision "is this branch safe to deliver?" is made
// outside this package — the daemon knows whether its own sidecar cleanup
// succeeded — while the only code that can act on it is Finalize. The first
// reason wins: it is the one closest to the root cause.
func (w *LocalWorktree) AbortWithReason(err error) {
	if w == nil || err == nil || w.aborted != nil {
		return
	}
	w.aborted = err
}

// commitBaseline records the user's replayed uncommitted state as the first
// commit on the task branch, returning the new tip.
func commitBaseline(worktreePath string) (string, error) {
	if _, err := commitEverything(worktreePath, "chore(agent): baseline — uncommitted work from the local directory"); err != nil {
		return "", err
	}
	tip, err := runGitTrimmed(worktreePath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve baseline commit: %w", err)
	}
	return tip, nil
}

// commitAll stages and commits everything the agent left behind. Returns
// whether a commit was actually created; an error means the changes are still
// only on disk and the caller must not delete the worktree.
func (w *LocalWorktree) commitAll(logger *slog.Logger) (bool, error) {
	return commitEverything(w.Path, "chore(agent): uncommitted changes from task")
}

// commitEverything returns (false, nil) for the benign "there was nothing to
// commit" case and (false, err) for a real failure — the distinction callers
// need to decide whether the tree is safe to discard.
func commitEverything(worktreePath, message string) (bool, error) {
	if out, err := runGit(worktreePath, "add", "-A"); err != nil {
		return false, fmt.Errorf("git add: %s: %w", strings.TrimSpace(out), err)
	}
	// --no-verify: the user's commit hooks are written for the user's own
	// workflow (interactive linters, test suites, signing prompts) and a hook
	// failure here would mean losing the agent's work to save a lint run. Note
	// it does NOT disable commit.gpgSign, which is why the caller has to treat
	// a commit failure as "keep the worktree" rather than a warning.
	args := append(commitIdentityArgs(worktreePath), "commit", "--no-verify", "-m", message)
	if out, err := runGit(worktreePath, args...); err != nil {
		if strings.Contains(out, "nothing to commit") {
			return false, nil
		}
		return false, fmt.Errorf("git commit: %s: %w", strings.TrimSpace(out), err)
	}
	return true, nil
}

// commitIdentityArgs supplies a committer identity only when the repo doesn't
// already have one. A repo with user.email configured keeps it, so commits
// still look like they came from the user's own setup.
func commitIdentityArgs(dir string) []string {
	if email, err := runGitTrimmed(dir, "config", "user.email"); err == nil && email != "" {
		return nil
	}
	return []string{
		"-c", "user.name=Multica Agent",
		"-c", "user.email=agent@multica.local",
	}
}

func worktreeIsDirty(worktreePath string) (bool, error) {
	out, err := runGit(worktreePath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %s: %w", strings.TrimSpace(out), err)
	}
	return strings.TrimSpace(out) != "", nil
}

// removeLocalWorktreeDir unregisters the worktree from the user's repo and
// deletes its directory. The branch is deliberately left alone — it is the
// task's deliverable.
func removeLocalWorktreeDir(gitRoot, worktreePath string, logger *slog.Logger) {
	if out, err := runGit(gitRoot, "worktree", "remove", "--force", worktreePath); err != nil {
		if logger != nil {
			logger.Warn("execenv: git worktree remove failed; pruning registration",
				"path", worktreePath, "output", out, "error", err)
		}
		// Fall back to deleting the directory ourselves and dropping the now
		// dangling registration, so the user's repo isn't left listing a
		// worktree that no longer exists.
		if rmErr := os.RemoveAll(worktreePath); rmErr != nil && logger != nil {
			logger.Warn("execenv: remove worktree directory failed", "path", worktreePath, "error", rmErr)
		}
		if out, pruneErr := runGit(gitRoot, "worktree", "prune"); pruneErr != nil && logger != nil {
			logger.Warn("execenv: git worktree prune failed", "output", out, "error", pruneErr)
		}
	}
}

// deleteBranch drops a task branch that carries nothing worth keeping — an
// empty read-only run, or a prepare that aborted partway. Best-effort: a
// leftover branch is untidy, never harmful.
func deleteBranch(gitRoot, branch string, logger *slog.Logger) {
	if branch == "" {
		return
	}
	if out, err := runGit(gitRoot, "branch", "-D", branch); err != nil && logger != nil {
		logger.Warn("execenv: delete task branch failed (non-fatal)",
			"branch", branch, "output", out, "error", err)
	}
}

// resolveGitRoot returns the repository root containing dir. Worktree mode is
// opt-in per resource, so a non-git directory here is a misconfiguration the
// user needs to see and fix — we fail closed with an actionable message rather
// than silently degrading to the in-place lock, which would leave the user
// wondering why their tasks still queue.
func resolveGitRoot(dir string) (string, error) {
	root, err := runGitTrimmed(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", fmt.Errorf("execenv: local_directory %q is not a git repository, "+
			"but its project resource is set to execution_mode=worktree; "+
			"initialise a repository there or switch the resource back to in_place", dir)
	}
	// EvalSymlinks so the root matches the path git reports from inside the
	// worktree later — on macOS /tmp vs /private/tmp otherwise produce two
	// different lock keys for one repo.
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = resolved
	}
	return filepath.Clean(root), nil
}

// addLocalWorktree creates the worktree, retrying once under a suffixed branch
// name when the branch already exists (a re-dispatched task keeps its id, so
// its branch can survive from the previous run).
func addLocalWorktree(gitRoot, worktreePath, branch, baseRef string) (string, error) {
	out, err := runGit(gitRoot, "worktree", "add", "-b", branch, worktreePath, baseRef)
	if err != nil && strings.Contains(strings.ToLower(out), "already exists") {
		branch = fmt.Sprintf("%s-%d", branch, time.Now().Unix())
		out, err = runGit(gitRoot, "worktree", "add", "-b", branch, worktreePath, baseRef)
	}
	if err != nil {
		return "", fmt.Errorf("execenv: git worktree add: %s: %w", strings.TrimSpace(out), err)
	}
	return branch, nil
}

// copyUntrackedFiles replays the user's untracked-but-not-ignored files into
// the worktree. `git worktree add` only materialises committed content, so
// without this a brand-new file the user just created would be invisible to the
// agent. Bounded by maxUntrackedFiles / maxUntrackedBytes; the number skipped
// is returned so the caller can tell the user instead of quietly under-copying.
func copyUntrackedFiles(gitRoot, worktreePath string, logger *slog.Logger) (copied, skipped int, err error) {
	// stdout only: a warning on stderr would otherwise be split apart and
	// treated as file paths to copy. Raw, not trimmed: with -z the entries are
	// exact filenames, and a file whose name begins or ends with whitespace
	// would be trim-corrupted into a path that fails to stat and silently
	// vanishes from the replay.
	out, err := runGitStdout(gitRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return 0, 0, fmt.Errorf("git ls-files: %w", err)
	}

	var budget int64 = maxUntrackedBytes
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		// Never replay Multica's own sidecars. They are untracked files in the
		// user's directory whenever an in_place task is mid-flight on the same
		// path, or was killed before its cleanup ran. Copying them would put
		// another issue's brief inside this task's worktree — where the agent
		// would read it as its own context — and commit it to the branch.
		if isMulticaSidecarPath(rel) {
			continue
		}
		if copied >= maxUntrackedFiles || budget <= 0 {
			skipped++
			continue
		}
		src := filepath.Join(gitRoot, rel)
		info, statErr := os.Lstat(src)
		if statErr != nil {
			// Listed a moment ago, unreadable now — the tree changed under us,
			// so the snapshot no longer matches what the user has. Counted, not
			// skipped silently: the caller fails the task on a non-zero count.
			skipped++
			if logger != nil {
				logger.Warn("execenv: untracked file vanished between listing and copy",
					"file", rel, "error", statErr)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// An untracked symlink is content the user can see. Reproducing it
			// faithfully means deciding whether to copy the link or its target
			// — including targets outside the repo — so this replay does not
			// try. Count it so the task fails rather than handing the agent a
			// tree with a file quietly missing.
			skipped++
			if logger != nil {
				logger.Warn("execenv: untracked symlink not replayed into worktree", "file", rel)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			// Sockets, FIFOs, devices: not content, and not something an agent
			// can meaningfully read from a copy. Skipping them does not make the
			// snapshot misleading, so this one stays uncounted.
			continue
		}
		if info.Size() > budget {
			skipped++
			continue
		}
		if copyErr := copyUntrackedFile(src, filepath.Join(worktreePath, rel), info.Mode()); copyErr != nil {
			skipped++
			if logger != nil {
				logger.Warn("execenv: copy untracked file into worktree failed", "file", rel, "error", copyErr)
			}
			continue
		}
		budget -= info.Size()
		copied++
	}
	return copied, skipped, nil
}

// copyUntrackedFile copies one untracked file into the worktree, creating
// parent directories and preserving the executable bit — a script the user just
// wrote and hasn't committed has to stay runnable for the agent.
// multicaSidecarDirNames are the directories Prepare writes into a workdir. A
// task running in_place on the same directory leaves these present as
// untracked files for the length of its run, so a concurrent worktree snapshot
// sees them. CLAUDE.md / AGENTS.md are deliberately absent: those are
// ordinarily the user's own tracked files, and the runtime only injects a
// marker block into them, which CleanupRuntimeConfig removes.
var multicaSidecarDirNames = []string{
	".agent_context",
	".multica",
}

// isMulticaSidecarPath reports whether a repo-relative path is one of the
// daemon's own sidecars rather than the user's content. Matched as a whole
// path segment at ANY depth, not just the repo root: an in_place resource may
// point at a subdirectory of this repo, in which case its sidecars sit at
// <subdir>/.agent_context — replaying those would put another issue's brief
// inside this task's worktree and commit it to the delivered branch.
func isMulticaSidecarPath(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		for _, name := range multicaSidecarDirNames {
			if seg == name {
				return true
			}
		}
	}
	return false
}

func copyUntrackedFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Chmod(dst, mode.Perm())
}

// runGit runs git in dir and returns combined output. Callers inspect the
// output for git's own error text, so stdout and stderr stay merged.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runGitTrimmed runs git for its stdout value, discarding stderr so a
// diagnostic line can't be mistaken for the value (`rev-parse` output, a
// config value, a stash sha).
func runGitTrimmed(dir string, args ...string) (string, error) {
	out, err := runGitStdout(dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// runGitStdout is runGitTrimmed without the trimming, for output where
// whitespace is significant — NUL-separated file listings, where a leading or
// trailing space is part of a filename.
func runGitStdout(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
