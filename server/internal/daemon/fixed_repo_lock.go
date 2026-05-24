package daemon

import (
	"context"
	"sync"
	"time"
)

// fixedRepoLock serializes access to a fixed-repo local directory.
type fixedRepoLock struct {
	mu       sync.Mutex
	taskID   string
	lockedAt time.Time
}

// fixedRepoLockTable manages exclusive access to fixed-repo directories.
// Each path can be locked by at most one task at a time.
type fixedRepoLockTable struct {
	mu     sync.Mutex
	locks  map[string]*fixedRepoLock
	notify chan struct{} // broadcast when any path is unlocked
}

func newFixedRepoLockTable() *fixedRepoLockTable {
	return &fixedRepoLockTable{
		locks:  make(map[string]*fixedRepoLock),
		notify: make(chan struct{}, 1),
	}
}

// tryLock attempts to lock a path for a task. Returns true on success.
func (t *fixedRepoLockTable) tryLock(path, taskID string) bool {
	t.mu.Lock()
	lk, ok := t.locks[path]
	if !ok {
		lk = &fixedRepoLock{}
		t.locks[path] = lk
	}
	t.mu.Unlock()

	if !lk.mu.TryLock() {
		return false
	}
	lk.taskID = taskID
	lk.lockedAt = time.Now()
	return true
}

// unlock releases the lock on a path and removes it from the table.
func (t *fixedRepoLockTable) unlock(path string) {
	t.mu.Lock()
	lk, ok := t.locks[path]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.locks, path)
	t.mu.Unlock()
	lk.taskID = ""
	lk.mu.Unlock()

	// Non-blocking send to wake up waiters.
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

// waitAndLock blocks until a path from candidates can be locked or ctx is done.
// Returns the locked path, or empty string if all paths are missing or context expired.
func (t *fixedRepoLockTable) waitAndLock(candidates []string, taskID string, ctx context.Context) string {
	for {
		// Try each candidate path.
		for _, p := range candidates {
			if t.tryLock(p, taskID) {
				return p
			}
		}

		// Wait for an unlock signal or context cancellation.
		select {
		case <-t.notify:
			// A path was unlocked; loop to retry.
		case <-ctx.Done():
			return ""
		}
	}
}

// lockedPaths returns all currently locked paths (for GC and diagnostics).
func (t *fixedRepoLockTable) lockedPaths() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var paths []string
	for p, lk := range t.locks {
		if lk.taskID != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// isPathLocked reports whether a path is currently locked.
func (t *fixedRepoLockTable) isPathLocked(path string) bool {
	t.mu.Lock()
	lk, ok := t.locks[path]
	t.mu.Unlock()
	if !ok {
		return false
	}
	return lk.taskID != ""
}