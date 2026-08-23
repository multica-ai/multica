package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Serialising git admin work on one repository is a CROSS-PROCESS problem.
//
// A daemon never prepares a task in its own process: PrepareIsolated runs the
// preparation in a short-lived helper (see isolation.go), so N concurrent
// tasks on one local_directory are N separate processes, all issuing git
// commands against the SAME repository. Worktree mode deliberately skips the
// per-path task mutex — sibling tasks running concurrently is the whole point
// of the mode — so nothing above this layer serialises them either.
//
// An in-process sync.Mutex therefore protected nothing in production: each
// helper held its own copy and none of them ever contended. `git stash create`
// (which needs the repository's index lock) lost that race routinely, and git
// reports the loss as a bare exit status 1 with no stderr at all, so the task
// failed with nothing to diagnose it by.
//
// The mutex is kept for the in-process callers — Finalize and Discard run in
// the daemon parent — and an advisory file lock in the repository's common git
// dir extends the same exclusion across every process that touches the repo,
// including two resources bound to two linked worktrees of it.

const (
	// gitRootLockFileName lives in the repository's COMMON git dir, which is
	// what makes one lock cover one repository.
	//
	// The section it guards spans both scopes a repo has: `git stash create`
	// contends for the per-working-tree index.lock, while `worktree add` /
	// `prune` / `remove` and `branch -D` write refs and worktrees/ in the
	// common dir, which every linked worktree shares. Keying on the
	// per-worktree git dir would hand two resources pointing at two linked
	// worktrees of one repo two different locks while they still raced on that
	// shared state; the common dir is the coarser key that covers both.
	//
	// Git ignores files it does not know about under $GIT_DIR, and nothing
	// here is visible from a working tree — `git status` never sees it, and
	// `git ls-files --others` cannot list it.
	gitRootLockFileName = "multica-worktree.lock"

	// gitRootLockPoll is the retry interval while waiting. flock has no
	// portable timed variant, so the wait is a poll over the non-blocking
	// primitive shared with the env-root claim (envlock_unix.go /
	// envlock_windows.go).
	gitRootLockPoll = 25 * time.Millisecond
)

// errGitRootLockBusy reports that another process held the repository lock for
// longer than gitRootLockWait. Distinct from a setup failure: busy means the
// exclusion worked and we chose not to wait any longer, so the caller must
// fail rather than proceed unprotected.
var errGitRootLockBusy = errors.New("execenv: git repository lock is held by another task")

// gitRootLockWait bounds the wait for a sibling's git section. That section is
// a handful of local git commands plus the bounded untracked-file replay, so a
// wait this long means the holder is wedged rather than busy: failing with a
// message that says who to wait for beats occupying a daemon slot forever.
//
// A var only so tests can shrink it; nothing at runtime writes it.
var gitRootLockWait = 3 * time.Minute

// gitRootLocks serialises git admin operations per repository within one
// process. Concurrent `git worktree add` / `remove` / `prune` on one repo race
// on the same lockfiles (worktrees/, packed-refs.lock, config.lock), and
// unlike a fetch these are fast, so a plain mutex costs nothing. Keyed by the
// repo root so tasks on different repos never wait on each other.
var gitRootLocks sync.Map // gitRoot -> *sync.Mutex

// gitDirCache memoises the git dir per repo root. Resolving it costs a git
// invocation and the answer cannot change for the life of a process: a repo
// root's git dir is fixed once the repo exists.
var gitDirCache sync.Map // gitRoot -> string

// gitDirFor returns the absolute git dir backing gitRoot's working tree.
//
// Not filepath.Join(gitRoot, ".git"): in a linked worktree .git is a FILE
// pointing at $COMMON/.git/worktrees/<name>, and that directory — not the
// common one — is where this working tree's index and index.lock live. Asking
// git keeps the lock in the same place as the contention in both layouts.
func gitDirFor(gitRoot string) (string, error) {
	if v, ok := gitDirCache.Load(gitRoot); ok {
		return v.(string), nil
	}
	dir, err := runGitTrimmed(gitRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("execenv: resolve git dir for %q: %w", gitRoot, err)
	}
	if dir == "" {
		return "", fmt.Errorf("execenv: resolve git dir for %q: empty result", gitRoot)
	}
	// git reports paths with forward slashes on every platform, including
	// Windows. Normalise before this becomes a filesystem path, so the lock
	// file and every path derived from it are native.
	dir = filepath.Clean(filepath.FromSlash(dir))
	gitDirCache.Store(gitRoot, dir)
	return dir, nil
}

// gitCommonDirCache memoises the common git dir per repo root, for the same
// reason gitDirCache does: one git invocation, an answer that cannot change.
var gitCommonDirCache sync.Map // gitRoot -> string

// gitCommonDirFor returns the git dir shared by every working tree of the
// repository behind gitRoot. For a main worktree that is its own git dir; for
// a linked one it is the main repo's, where the refs and worktrees/ admin
// state they both write actually live.
func gitCommonDirFor(gitRoot string) (string, error) {
	if v, ok := gitCommonDirCache.Load(gitRoot); ok {
		return v.(string), nil
	}
	dir, err := runGitTrimmed(gitRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("execenv: resolve common git dir for %q: %w", gitRoot, err)
	}
	if dir == "" {
		return "", fmt.Errorf("execenv: resolve common git dir for %q: empty result", gitRoot)
	}
	dir = filepath.FromSlash(dir)
	// git answers this one relative to the cwd it ran in when the working tree
	// IS the main one (plain ".git"), and absolute from a linked worktree.
	// --path-format=absolute would settle it but only exists since git 2.31,
	// and resolving it here costs nothing.
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(gitRoot, dir)
	}
	dir = filepath.Clean(dir)
	gitCommonDirCache.Store(gitRoot, dir)
	return dir, nil
}

// gitRootLockPath is where the advisory lock for gitRoot's repository lives.
func gitRootLockPath(gitRoot string) (string, error) {
	commonDir, err := gitCommonDirFor(gitRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, gitRootLockFileName), nil
}

// acquireGitRootFileLock takes the cross-process lock for gitRoot, waiting up
// to gitRootLockWait for a current holder to finish.
func acquireGitRootFileLock(gitRoot string, logger *slog.Logger) (*os.File, error) {
	path, err := gitRootLockPath(gitRoot)
	if err != nil {
		return nil, err
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("execenv: open git repository lock %s: %w", path, err)
	}

	deadline := time.Now().Add(gitRootLockWait)
	announced := false
	for {
		ok, lockErr := lockFileExclusiveNonBlocking(f)
		if lockErr != nil {
			f.Close()
			return nil, fmt.Errorf("execenv: lock %s: %w", path, lockErr)
		}
		if ok {
			return f, nil
		}
		if !announced {
			announced = true
			if logger != nil {
				logger.Info("execenv: waiting for another task's git section on this repository",
					"git_root", gitRoot, "lock", path)
			}
		}
		if time.Now().After(deadline) {
			f.Close()
			// Deliberately NOT "delete the lock file if nothing is running".
			// The lock binds to an open file description, not to a path:
			// unlinking it leaves the holder locking the old inode while the
			// next task creates and locks a NEW file at the same name, and
			// both then believe they hold it — the exact overlap this lock
			// exists to prevent. The kernel already releases it when the
			// holding process exits, so an idle lock file is never stale.
			return nil, fmt.Errorf("%w: %q was still locked after %s by another Multica task (%s) — "+
				"wait for that task to finish or stop it, then retry; the lock is released "+
				"automatically when the holding process exits, so the file itself is never stale "+
				"and deleting it would let two tasks into this section at once",
				errGitRootLockBusy, gitRoot, gitRootLockWait, path)
		}
		time.Sleep(gitRootLockPoll)
	}
}

// lockGitRoot takes exclusive ownership of gitRoot's git admin state for this
// task, across processes, and returns the release func.
//
// Setup failures are NOT fatal. A repo whose git dir cannot hold a lock file
// (read-only mount, an exotic filesystem) still gets the in-process mutex,
// which is exactly the protection this code had before — a task that refuses
// to start is worse than one that races on a millisecond window it can also
// retry out of. A BUSY lock is different: the exclusion worked, we waited, and
// proceeding anyway would defeat it, so that one is returned to the caller.
func lockGitRoot(gitRoot string, logger *slog.Logger) (func(), error) {
	// The file lock comes FIRST, before the mutex. Waiting on it while holding
	// the mutex would serialise the waits themselves: three queued goroutines
	// would each start their own gitRootLockWait only after the one ahead gave
	// up, so the last of them could block for three times the bound it was
	// promised. Waiting on the file lock directly lets them all wait at once,
	// each bounded by gitRootLockWait.
	//
	// Both platforms' primitives already exclude within a process — flock
	// treats separate open file descriptions independently, and so does
	// LockFileEx with separate handles — so this ordering loses nothing.
	f, err := acquireGitRootFileLock(gitRoot, logger)
	if err != nil {
		if errors.Is(err, errGitRootLockBusy) {
			return nil, err
		}
		if logger != nil {
			logger.Warn("execenv: cross-process git lock unavailable; falling back to the in-process lock",
				"git_root", gitRoot, "error", err)
		}
		return lockGitRootInProcess(gitRoot), nil
	}

	// The mutex is redundant while the file lock holds and is what protects
	// this repo if the file lock ever falls back mid-flight. Uncontended in
	// the normal case, since no sibling goroutine can be past the file lock.
	unlockLocal := lockGitRootInProcess(gitRoot)
	return func() {
		unlockLocal()
		if unlockErr := unlockFile(f); unlockErr != nil && logger != nil {
			logger.Warn("execenv: release git repository lock failed", "git_root", gitRoot, "error", unlockErr)
		}
		f.Close()
	}, nil
}

// lockGitRootInProcess takes the per-repo mutex and returns its release func.
func lockGitRootInProcess(gitRoot string) func() {
	v, _ := gitRootLocks.LoadOrStore(gitRoot, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
