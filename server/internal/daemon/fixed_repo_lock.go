package daemon

import (
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
	mu    sync.Mutex
	locks map[string]*fixedRepoLock
}

func newFixedRepoLockTable() *fixedRepoLockTable {
	return &fixedRepoLockTable{locks: make(map[string]*fixedRepoLock)}
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
