package daemon

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFixedRepoLock_TryLockExclusive(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	path := "/data/repos/test-project"

	if !table.tryLock(path, "task-1") {
		t.Fatal("first tryLock should succeed")
	}
	if table.isPathLocked(path) == false {
		t.Fatal("path should be reported as locked")
	}

	// Second task must fail to acquire the same path.
	if table.tryLock(path, "task-2") {
		t.Fatal("second tryLock on same path should fail")
	}

	table.unlock(path)

	// After unlock, a third task should succeed.
	if !table.tryLock(path, "task-3") {
		t.Fatal("tryLock after unlock should succeed")
	}
	table.unlock(path)
}

func TestFixedRepoLock_UnlockUnknownPath(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	// Unlocking a path that was never locked should not panic.
	table.unlock("/nonexistent")
}

func TestFixedRepoLock_IsPathLockedUnknown(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	if table.isPathLocked("/nonexistent") {
		t.Fatal("unknown path should not be reported as locked")
	}
}

func TestFixedRepoLock_DifferentPathsIndependent(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()

	if !table.tryLock("/data/repos/a", "task-1") {
		t.Fatal("lock on path a should succeed")
	}
	if !table.tryLock("/data/repos/b", "task-2") {
		t.Fatal("lock on path b should succeed — different paths should be independent")
	}

	table.unlock("/data/repos/a")
	table.unlock("/data/repos/b")
}

func TestFixedRepoLock_LockedPaths(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	table.tryLock("/data/repos/a", "task-1")
	table.tryLock("/data/repos/b", "task-2")

	locked := table.lockedPaths()
	if len(locked) != 2 {
		t.Fatalf("expected 2 locked paths, got %d: %v", len(locked), locked)
	}

	table.unlock("/data/repos/a")
	locked = table.lockedPaths()
	if len(locked) != 1 {
		t.Fatalf("expected 1 locked path after unlock, got %d: %v", len(locked), locked)
	}
}

func TestFixedRepoLock_ConcurrentTryLock(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	path := "/data/repos/shared"

	var wg sync.WaitGroup
	winners := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if table.tryLock(path, "task") {
				mu.Lock()
				winners++
				mu.Unlock()
				table.unlock(path)
			}
		}(i)
	}
	wg.Wait()

	if winners != 10 {
		t.Fatalf("all 10 goroutines should eventually acquire the lock (serially), got %d", winners)
	}
}

func TestFixedRepoLock_WaitAndLock_ImmediateSuccess(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	ctx := context.Background()

	path := table.waitAndLock([]string{"/data/repos/a"}, "task-1", ctx)
	if path != "/data/repos/a" {
		t.Fatalf("expected /data/repos/a, got %q", path)
	}
	table.unlock(path)
}

func TestFixedRepoLock_WaitAndLock_BlocksUntilUnlock(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	ctx := context.Background()

	// Lock the only candidate.
	table.tryLock("/data/repos/a", "task-1")

	var wg sync.WaitGroup
	var gotPath string
	wg.Add(1)
	go func() {
		defer wg.Done()
		gotPath = table.waitAndLock([]string{"/data/repos/a"}, "task-2", ctx)
	}()

	// Give the goroutine time to block.
	time.Sleep(50 * time.Millisecond)
	table.unlock("/data/repos/a")

	wg.Wait()
	if gotPath != "/data/repos/a" {
		t.Fatalf("expected /data/repos/a after unlock, got %q", gotPath)
	}
	table.unlock(gotPath)
}

func TestFixedRepoLock_WaitAndLock_ContextCancel(t *testing.T) {
	t.Parallel()

	table := newFixedRepoLockTable()
	ctx, cancel := context.WithCancel(context.Background())

	// Lock the only candidate.
	table.tryLock("/data/repos/a", "task-1")

	var wg sync.WaitGroup
	var gotPath string
	wg.Add(1)
	go func() {
		defer wg.Done()
		gotPath = table.waitAndLock([]string{"/data/repos/a"}, "task-2", ctx)
	}()

	// Cancel the context.
	time.Sleep(50 * time.Millisecond)
	cancel()

	wg.Wait()
	if gotPath != "" {
		t.Fatalf("expected empty string on context cancel, got %q", gotPath)
	}
	table.unlock("/data/repos/a")
}