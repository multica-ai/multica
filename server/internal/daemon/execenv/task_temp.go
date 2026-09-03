package execenv

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TaskTempDirPrefix is the basename prefix os.MkdirTemp stamps on every
// per-task temp directory the daemon creates under the task temp base. The
// sweep below removes nothing without it, so the base itself — usually a
// shared /tmp holding other programs' files — is never at risk.
const TaskTempDirPrefix = "multica-task-"

// taskTempLockClaimFile is the name the marker is locked under before being
// renamed into place, so .task_lock never exists unlocked while its owner is
// still starting up.
const taskTempLockClaimFile = envRootLockFile + ".claiming"

// LockTaskTempDir takes the temp directory's execution lock and returns the
// held file; the caller owns it for the lifetime of the task run.
//
// This is the same .task_lock an env root uses, for the same reason: the
// kernel releases it when the holding process dies, so it answers "is the
// execution that owns this directory still alive?" across processes without a
// heartbeat, a PID table, or a stale-state cleanup path. That is what lets
// PruneTaskTempDirs reclaim a directory the moment its owner is gone while
// never touching one that is still in use — including a directory owned by a
// different daemon sharing the same temp base, which no in-memory active set
// could ever see, and including a live task in this very process, because a
// lock is held by an open file description rather than by a process.
func LockTaskTempDir(dir string) (*os.File, error) {
	// Lock the marker under a name the sweep does not read, then publish it
	// with a rename. Creating .task_lock first and locking it second would
	// leave a window — however short — in which a concurrent sweep sees a
	// marker it can lock, concludes the owner is dead, and deletes the
	// directory out from under a task that is about to start using it. Rename
	// within one directory is atomic, and the lock rides on the open file
	// description rather than the name, so the marker becomes visible and
	// held in the same step.
	//
	// Before that rename the directory carries no published marker, so the
	// sweep can only reach it through the legacy branch. What keeps it safe
	// there is not its age — legacyTTL takes any duration, down to a
	// nanosecond, and a stalled owner can outlast any TTL — but the fact that
	// it holds no task content yet. The legacy branch reclaims only
	// directories that do; see taskTempDirHoldsContent.
	claim := filepath.Join(dir, taskTempLockClaimFile)
	lock, err := openLockFile(claim)
	if err != nil {
		return nil, fmt.Errorf("open task temp dir lock for %s: %w", dir, err)
	}
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil {
		lock.Close()
		_ = os.Remove(claim)
		return nil, fmt.Errorf("lock task temp dir %s: %w", dir, err)
	}
	if !locked {
		// Unreachable in practice: the directory was just created by
		// os.MkdirTemp under a name nobody else knows yet.
		lock.Close()
		_ = os.Remove(claim)
		return nil, fmt.Errorf("task temp dir %s is already locked", dir)
	}
	if err := os.Rename(claim, filepath.Join(dir, envRootLockFile)); err != nil {
		releaseLockFile(lock)
		_ = os.Remove(claim)
		return nil, fmt.Errorf("publish task temp dir lock for %s: %w", dir, err)
	}
	return lock, nil
}

// ReleaseTaskTempLock drops the temp directory's execution lock and closes the
// file. Safe on nil and safe to call twice.
func ReleaseTaskTempLock(f *os.File) {
	releaseLockFile(f)
}

// RemoveTaskTempDir removes dir's contents first, then its lock marker, then
// dir itself — and stops at the first content it cannot remove, leaving the
// marker behind.
//
// That ordering is the whole point, and it is why this is not os.RemoveAll.
// RemoveAll walks the directory and keeps going after a failure, so a payload
// Windows will not let go of still costs the .task_lock sitting next to it:
// the removal fails having deleted exactly the file the NEXT sweep needs to
// recognise this directory as a lock-bearing one. It would come back as a
// directory with no marker — indistinguishable from a pre-lock leftover, and
// so reclaimable only on age, if at all.
//
// Leaving the marker means a failed cleanup costs nothing: the directory is
// still lock-bearing, still unlocked, and the next GC cycle takes the lock and
// retries, however many cycles it takes for the holder to let go.
//
// That holds for every way this can fail, not just the content walk. The
// marker has to be gone before the directory itself can be removed, so when
// that last removal fails the marker is put back — otherwise the one case
// where cleanup gets furthest would be the one case that leaks permanently.
//
// Callers must release their own lock before calling this — the marker is
// deleted here, and on Windows removing a file this process still holds open
// only marks it for deletion, which would then block the directory's removal.
func RemoveTaskTempDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == envRootLockFile {
			continue // last, and only once everything else is gone
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	lockPath := filepath.Join(dir, envRootLockFile)
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(dir); err != nil {
		// The directory outlived its marker: a Windows process holding a handle
		// to the directory itself, or content an orphaned child recreated after
		// the sweep above walked past it. Put an unlocked marker back, or the
		// directory reads as a pre-lock leftover from here on — which, with the
		// legacy branch disabled by default, means it never gets reclaimed at
		// all. Restoring it keeps the retry available for the next cycle.
		if restored, rerr := openLockFile(lockPath); rerr == nil {
			restored.Close()
		}
		return err
	}
	return nil
}

// taskTempVerdict is what a single directory under the temp base is, as far as
// this sweep can prove.
type taskTempVerdict int

const (
	// taskTempInUse: an execution still holds the lock, or something stopped
	// this sweep from finding out. Both mean "do not touch" — the sweep fails
	// closed, since being wrong here deletes a running task's TMPDIR.
	taskTempInUse taskTempVerdict = iota
	// taskTempDead: this sweep acquired the lock the owner would still be
	// holding, so the kernel has already proven that execution is gone.
	taskTempDead
	// taskTempLegacy: no lock file at all — left by a daemon predating
	// LockTaskTempDir, about which nothing can be proven.
	taskTempLegacy
)

// PruneTaskTempDirs reclaims per-task temp directories whose owning execution
// is gone, under the base returned by the daemon's task temp base resolution.
//
// These directories are the agent process's TMPDIR. They live outside
// WorkspacesRoot — the task GC's scan root — so nothing else ever reclaims
// them: their only other exit is the cleanup the daemon defers at the end of
// runTask, which does not run when the daemon is killed and does not finish
// when a file inside is still open (the Windows case in #7364). Whatever that
// misses accumulates on disk forever.
//
// Liveness comes from the lock, not from the clock. A directory is removed
// only once this sweep has itself acquired the .task_lock its owner would
// still be holding, which the kernel grants only after that owner is gone.
//
// The lock is held by the daemon, not by the agent process, so it answers "is
// the owning EXECUTION still alive?" and not "is every child it spawned gone?".
// A daemon killed while agent processes survive it releases the lock on the
// spot, and this sweep may then reclaim a directory such an orphan is still
// writing to. That is the same exposure an env root's .task_lock already
// carries, and from the platform's point of view the task is dead either way;
// closing it properly is a job for process-group teardown, not for the GC.
//
// legacyTTL applies to the one case the lock cannot answer: directories left
// by a daemon predating LockTaskTempDir, which carry no lock file at all. For
// those, age is the only available signal — and age cannot establish liveness,
// which is why legacyTTL defaults to 0 (leave them alone) and reclaiming them
// is an operator's explicit decision. See DefaultGCTaskTempLegacyTTL.
func PruneTaskTempDirs(base string, legacyTTL time.Duration, now time.Time, logger *slog.Logger) (removed int, bytesFreed int64) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, 0 // missing or unreadable base — nothing to prune
	}
	legacyKept := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), TaskTempDirPrefix) {
			continue
		}
		dir := filepath.Join(base, e.Name())

		switch classifyTaskTempDir(dir, logger) {
		case taskTempInUse:
			continue
		case taskTempLegacy:
			if legacyTTL <= 0 {
				legacyKept++
				continue
			}
			// Age is the only signal here, and age cannot tell a pre-lock
			// leftover from a directory this daemon is in the middle of
			// publishing: both carry no .task_lock, legacyTTL accepts any
			// duration down to a nanosecond, and an owner stalled between
			// MkdirTemp and the rename outlasts any TTL at all. Content can
			// tell them apart. A directory being published holds nothing but
			// its own claim file — the task has not been handed the path yet,
			// so it cannot have written anything — while a leftover worth
			// reclaiming is by definition one with something in it.
			//
			// So this branch never touches a directory with no task content.
			// The cost is that a pre-lock daemon which crashed before its task
			// wrote anything leaves an empty directory nothing reclaims; the
			// alternative is deleting the temp dir of a task that is starting.
			if !taskTempDirHoldsContent(dir) {
				continue
			}
			newest, _ := dirStat(dir)
			if newest.IsZero() || now.Sub(newest) <= legacyTTL {
				continue
			}
			// Everything above is a series of separate observations, not one
			// snapshot: the classify that said "legacy" happened before the
			// content walk, and the owner may have finished publishing in
			// between — leaving a held .task_lock this branch has not looked
			// at, on a directory whose task is now writing to it.
			//
			// Re-reading the classification here closes that, and closes it
			// completely rather than narrowing it. Content is only ever there
			// because a task was handed this path, which happens only after
			// LockTaskTempDir has returned and therefore after the marker is
			// published and held. So content observed at the top plus no
			// marker seen here means no live task owns this directory — and a
			// task cannot appear afterwards either, since MkdirTemp only ever
			// hands out a fresh empty directory under a new name.
			if taskTempSweepBeforeRecheck != nil {
				taskTempSweepBeforeRecheck(dir)
			}
			if classifyTaskTempDir(dir, logger) != taskTempLegacy {
				continue
			}
		}

		_, size := dirStat(dir)
		if err := RemoveTaskTempDir(dir); err != nil {
			// Not worth escalating: the marker is still there, so the next
			// cycle re-acquires the lock and retries. This is the whole reason
			// the fix belongs in the GC rather than in a bounded retry on the
			// task-completion path — here, failing costs nothing and repeats
			// for free.
			if logger != nil {
				logger.Debug("execenv: prune task temp dir failed", "dir", dir, "error", err)
			}
			continue
		}
		removed++
		bytesFreed += size
	}
	if legacyKept > 0 && logger != nil {
		// Say it plainly rather than reclaiming them silently: these are the
		// directories this daemon cannot prove anything about, and an operator
		// who knows no pre-lock daemon is running can opt in.
		logger.Info("gc: pre-lock task temp dirs left in place",
			"base", base,
			"count", legacyKept,
			"hint", "set MULTICA_GC_TASK_TEMP_LEGACY_TTL to reclaim them on age",
		)
	}
	return removed, bytesFreed
}

// taskTempSweepBeforeRecheck is a test seam, nil in production. The sweep's
// correctness depends on what can happen BETWEEN its decision and its removal,
// and that interleaving cannot be produced reliably from the outside.
var taskTempSweepBeforeRecheck func(dir string)

// taskTempDirHoldsContent reports whether dir holds anything a task put there,
// as opposed to only the lock files this package maintains. It is what
// separates a pre-lock leftover from a directory mid-publication, without
// consulting the clock.
func taskTempDirHoldsContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false // unreadable: not ours to delete
	}
	for _, e := range entries {
		if e.Name() == envRootLockFile || e.Name() == taskTempLockClaimFile {
			continue
		}
		return true
	}
	return false
}

// classifyTaskTempDir decides what can be proven about one directory.
func classifyTaskTempDir(dir string, logger *slog.Logger) taskTempVerdict {
	lockPath := filepath.Join(dir, envRootLockFile)
	if _, err := os.Stat(lockPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// A directory we cannot even look into is not a directory we may
			// delete. Another user's temp dir under a shared /tmp lands here.
			if logger != nil {
				logger.Debug("execenv: task temp dir lock unreadable; leaving it alone", "dir", dir, "error", err)
			}
			return taskTempInUse
		}
		return taskTempLegacy
	}

	lock, err := openLockFile(lockPath)
	if err != nil {
		return taskTempInUse
	}
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil || !locked {
		lock.Close()
		return taskTempInUse
	}
	// Release immediately: RemoveTaskTempDir deletes this very file, and on
	// Windows deleting a file this process still has open only marks it for
	// deletion, which would then block the directory's own removal. Nothing can
	// claim the directory in between — its name is random and its owner is
	// dead — and a second sweep racing us just loses the removal to ENOENT.
	releaseLockFile(lock)
	return taskTempDead
}
