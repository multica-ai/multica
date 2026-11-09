package execenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cross-process exclusion for one work-state scope (GH #8280).
//
// Before the work-state scope existed, two profiles on one machine generally
// owned two persistent trees, so the daemon in-process guard maps
// (activeStores/deletingStores, activeEnvRoots/deletingEnvRoots) were the whole
// safety story: one process was the only writer of the tree it guarded.
//
// A work-state scope is shared by design now: the CLI daemon and the Desktop
// daemon on one machine and one backend mount the same stores. The in-process
// maps cannot see each other, so the authoritative boundary has to be something
// the OS enforces: a file lock inside the scope that every daemon of that scope
// contends on, released by the kernel when its holder dies. The maps stay as the
// local reference count and as the fast path that keeps one process from handing
// the same store to two of its own tasks.
//
// The protocol is readers-writer:
//
//	task using a store      shared ownership, held for the whole task
//	GC reclaiming a store   exclusive ownership, non-blocking, for the removal
//
// which preserves the semantics each store already had — two tasks of one Hermes
// agent share one memory store (last-writer-wins), while one conversation owns
// its Codex shard — without serialising unrelated legitimate work.

// scopeLockRetryInterval is the poll interval while a task waits for a GC that
// is reclaiming the store it wants to use. The window is one RemoveAll of a
// store directory, so the wait is expected to be milliseconds; the caller sets
// the deadline.
const scopeLockRetryInterval = 20 * time.Millisecond

// DefaultScopeLockTimeout bounds how long a task waits for a store another
// daemon in the scope is actively deleting. Waiting is the safe answer — failing
// closed is better than mounting a store mid-removal — but a daemon that never
// gets it must not hang its task forever, so the wait is bounded and the
// failure names the store.
const DefaultScopeLockTimeout = 15 * time.Second

// ErrScopeLockTimeout reports that a claim could not be taken before its bound
// because another daemon in the same work state holds it. Callers that advertise
// retryability (the repo cache's ErrRepoBusy path) match on it.
var ErrScopeLockTimeout = errors.New("scope lock is held by another daemon in this work state")

// ScopeLocks owns the lock files for one work-state scope. The zero value is
// disabled (no coordination), which is what hand-built daemon configurations in
// tests get; LoadConfig always sets a directory.
type ScopeLocks struct{ dir string }

// NewScopeLocks returns the scope lock set rooted at dir.
func NewScopeLocks(dir string) ScopeLocks { return ScopeLocks{dir: strings.TrimSpace(dir)} }

// Enabled reports whether this set can coordinate anything.
func (l ScopeLocks) Enabled() bool { return l.dir != "" }

// Dir returns the lock directory (diagnostics and tests).
func (l ScopeLocks) Dir() string { return l.dir }

// lockPath names the lock file for one guarded target. The file deliberately
// lives in the scope, NOT inside the tree it guards: the GC removes whole store
// directories, and a lock inode that can be unlinked while still held would let
// the next acquirer lock a fresh file at the same path and win a claim the
// holder still believes it owns. A stable path outside the target keeps every
// process contending on one inode for the target path lifetime.
func (l ScopeLocks) lockPath(target string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(target)))
	return filepath.Join(l.dir, hex.EncodeToString(sum[:8])+".lock")
}

func (l ScopeLocks) open(target string) (*os.File, error) {
	if !l.Enabled() {
		return nil, errors.New("scope locks: no lock directory configured")
	}
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return nil, fmt.Errorf("scope locks: create %s: %w", l.dir, err)
	}
	f, err := openLockFile(l.lockPath(target))
	if err != nil {
		return nil, fmt.Errorf("scope locks: open for %s: %w", target, err)
	}
	return f, nil
}

// AcquireTargetUse takes shared ownership of target for the lifetime of a task.
//
// It waits (bounded by timeout) while another process holds the exclusive claim,
// so a task never mounts a store that is being reclaimed. A nil claim with a nil
// error means this scope has no lock directory and nothing is coordinated.
func (l ScopeLocks) AcquireTargetUse(target string, timeout time.Duration) (*ScopeClaim, error) {
	if !l.Enabled() || strings.TrimSpace(target) == "" {
		return nil, nil
	}
	f, err := l.open(target)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := lockFileSharedNonBlocking(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("scope locks: take use claim on %s: %w", target, err)
		}
		if ok {
			return &ScopeClaim{file: f, target: target}, nil
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, fmt.Errorf("%w: %s is still being reclaimed after %s", ErrScopeLockTimeout, target, timeout)
		}
		time.Sleep(scopeLockRetryInterval)
	}
}

// TryAcquireTargetDelete takes the exclusive claim the GC needs before it
// mutates target. Non-blocking: ok=false means some task in some process still
// holds the store (or another GC does), which is a skip, not a wait.
func (l ScopeLocks) TryAcquireTargetDelete(target string) (claim *ScopeClaim, ok bool, err error) {
	if !l.Enabled() || strings.TrimSpace(target) == "" {
		return nil, true, nil
	}
	f, err := l.open(target)
	if err != nil {
		return nil, false, err
	}
	locked, err := lockFileExclusiveNonBlocking(f)
	if err != nil {
		f.Close()
		return nil, false, fmt.Errorf("scope locks: take delete claim on %s: %w", target, err)
	}
	if !locked {
		f.Close()
		return nil, false, nil
	}
	return &ScopeClaim{file: f, target: target}, true, nil
}

// AcquireTargetExclusive takes the exclusive claim for target, waiting for a
// holder in another process up to timeout. Shorter-lived and coarser than
// AcquireTargetUse: it is what a daemon-level mutation takes when it must not
// overlap the same mutation in a sibling daemon of this scope, but can afford to
// wait for the one already running.
func (l ScopeLocks) AcquireTargetExclusive(target string, timeout time.Duration) (*ScopeClaim, error) {
	if !l.Enabled() || strings.TrimSpace(target) == "" {
		return nil, nil
	}
	f, err := l.open(target)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := lockFileExclusiveNonBlocking(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("scope locks: take exclusive claim on %s: %w", target, err)
		}
		if ok {
			return &ScopeClaim{file: f, target: target}, nil
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, fmt.Errorf("%w: %s is held after %s", ErrScopeLockTimeout, target, timeout)
		}
		time.Sleep(scopeLockRetryInterval)
	}
}

// RepoActivityTarget names the scope claim a task holds, shared, for every bare
// repo its run may touch, and that heavy maintenance and eviction take
// exclusively (non-blocking) so neither can run while any same-scope daemon has
// a task that could be reading or writing the repository.
//
// The target is the bare path, tagged so it can never collide with the
// env-root or provider-store claims on that path.
func RepoActivityTarget(barePath string) string {
	return "repo-activity:" + filepath.Clean(barePath)
}

// RepoMutationTarget names the scope claim every daemon-level repository
// mutation holds exclusively: clone, fetch, ref-layout migration, worktree add,
// branch and ref updates, worktree prune, heavy maintenance, and eviction.
// Different repositories are different targets and stay concurrent.
//
// The claim is keyed on the bare path but lives OUTSIDE it (ScopeLocks keeps its
// files beside the scope), which is what lets eviction hold the claim across the
// RemoveAll that destroys the repo itself.
func RepoMutationTarget(barePath string) string {
	return "repo-mutation:" + filepath.Clean(barePath)
}

// AcquireEnvRootGCClaim takes the env root existing .task_lock exclusively,
// non-blocking.
//
// An env root already carries the one lock that answers "is the owning execution
// still alive?" — task execution holds .task_lock for its whole run — so the GC
// reuses it instead of inventing a second format. Holding it for the duration of
// the mutation is what makes "no other process owns this root" and "the root is
// being mutated" one atomic fact rather than a stat-then-delete race.
//
// ok=false means a live execution (in this process or another) owns the root.
// On Windows the file is opened with FILE_SHARE_DELETE, so the handle does not
// pin the directory: the removal this claim authorises can delete .task_lock
// along with the rest of the root.
func AcquireEnvRootGCClaim(envRoot string) (claim *ScopeClaim, ok bool, err error) {
	if strings.TrimSpace(envRoot) == "" {
		return nil, false, nil
	}
	lock, err := openLockFile(filepath.Join(envRoot, envRootLockFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The root itself is gone. Nothing can be running on it and whatever
			// mutation the caller makes is a no-op, so there is nothing to exclude -
			// but the in-process guard is still applied by the caller.
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("env root claim: open %s: %w", envRoot, err)
	}
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil {
		lock.Close()
		return nil, false, fmt.Errorf("env root claim: lock %s: %w", envRoot, err)
	}
	if !locked {
		lock.Close()
		return nil, false, nil
	}
	return &ScopeClaim{file: lock, target: envRoot}, true, nil
}

// ScopeClaim is one held claim: shared (a task using a store) or exclusive (a GC
// about to mutate one). Release is idempotent and nil-safe so a caller can defer
// it unconditionally.
type ScopeClaim struct {
	file   *os.File
	target string
}

// TryLockFileExclusiveNonBlocking opens (creating if needed) path and takes an
// exclusive advisory lock on it without waiting. ok=false means another process
// holds it. Callers poll with their own deadline when they want to wait.
//
// Exported for the work-state manifest, which needs the same crash-safe
// exclusion the scope locks use but guards a single file rather than a tree.
func TryLockFileExclusiveNonBlocking(path string) (claim *ScopeClaim, ok bool, err error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, false, err
	}
	locked, err := lockFileExclusiveNonBlocking(f)
	if err != nil {
		f.Close()
		return nil, false, err
	}
	if !locked {
		f.Close()
		return nil, false, nil
	}
	return &ScopeClaim{file: f, target: path}, true, nil
}

// Target is the path this claim guards.
func (c *ScopeClaim) Target() string {
	if c == nil {
		return ""
	}
	return c.target
}

// Release drops the claim. Closing the file would release it too; this makes the
// release explicit at the call site and tolerant of a nil claim.
func (c *ScopeClaim) Release() {
	if c == nil || c.file == nil {
		return
	}
	_ = unlockFile(c.file)
	_ = c.file.Close()
	c.file = nil
}

// AcquireTargetUseContext takes shared ownership of target, waiting until the
// claim is free or ctx ends. It is the cancellable form of AcquireTargetUse and
// exists for holders that legitimately stay locked for hours: a local_directory
// task can wait for a sibling writer for as long as the task itself is allowed
// to run, so the wait must end on task cancellation or daemon shutdown rather
// than on a fixed short timeout (GH #8280).
func (l ScopeLocks) AcquireTargetUseContext(ctx context.Context, target string) (*ScopeClaim, error) {
	if !l.Enabled() || strings.TrimSpace(target) == "" {
		return nil, nil
	}
	f, err := l.open(target)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := lockFileSharedNonBlocking(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("scope locks: take use claim on %s: %w", target, err)
		}
		if ok {
			return &ScopeClaim{file: f, target: target}, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, context.Cause(ctx)
		case <-time.After(scopeLockRetryInterval):
		}
	}
}

// AcquireTargetExclusiveContext takes the exclusive claim for target, waiting
// until it is free or ctx ends. The cancellable counterpart to
// AcquireTargetExclusive, for the same reason as AcquireTargetUseContext.
func (l ScopeLocks) AcquireTargetExclusiveContext(ctx context.Context, target string) (*ScopeClaim, error) {
	if !l.Enabled() || strings.TrimSpace(target) == "" {
		return nil, nil
	}
	f, err := l.open(target)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := lockFileExclusiveNonBlocking(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("scope locks: take exclusive claim on %s: %w", target, err)
		}
		if ok {
			return &ScopeClaim{file: f, target: target}, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, context.Cause(ctx)
		case <-time.After(scopeLockRetryInterval):
		}
	}
}

// LocalDirectoryTarget names the machine-wide claim for one local_directory
// real path.
//
// Deliberately NOT scoped to a backend: a local directory is a filesystem
// resource, not a runtime one, so two daemons on one machine that talk to
// different backends can still be told to work in the same checkout. The lock
// file therefore lives in the machine-global lock root, and the target is the
// canonical real path the assignment already resolved (symlinks and alternate
// spellings of one directory must land on one claim).
func LocalDirectoryTarget(canonicalRealPath string) string {
	return "local-directory:" + filepath.Clean(canonicalRealPath)
}
