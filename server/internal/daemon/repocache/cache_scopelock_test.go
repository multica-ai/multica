package repocache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Cross-process repository coordination (GH #8280).
//
// Two daemon processes on one machine and one backend share one workspaces root,
// and therefore one .repos tree, while their local repoLocks maps stay per
// process. These tests use two independent Cache instances - never one shared
// repoLock - over the same on-disk repo and the same scope lock directory, which
// is exactly the pair of processes the scope claim has to exclude.

func scopeTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// scopeLockedRepos returns two caches sharing one cache root and one scope lock
// directory, plus the bare path of a repo both can see.
func scopeLockedRepos(t *testing.T) (cacheA, cacheB *Cache, locks execenv.ScopeLocks, bare, lockDir string) {
	t.Helper()
	root := t.TempDir()
	lockDir = filepath.Join(t.TempDir(), "locks")
	locks = execenv.NewScopeLocks(lockDir)
	cacheA = NewScoped(root, scopeTestLogger(), locks)
	cacheB = NewScoped(root, scopeTestLogger(), locks)
	bare = cacheA.BarePath("11111111-1111-1111-1111-111111111111", "https://example.com/org/repo.git")
	if bare == "" {
		t.Fatal("BarePath returned an empty path")
	}
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}
	return cacheA, cacheB, locks, bare, lockDir
}

// holdRepoMutation keeps one mutation of bare in flight until the returned
// release runs, and reports when it has entered the critical section.
func holdRepoMutation(t *testing.T, cache *Cache, bare string) (release func()) {
	t.Helper()
	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cache.WithRepoLock(bare, func() error {
			close(started)
			<-finish
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the held mutation never started")
	}
	return func() {
		close(finish)
		<-done
	}
}

// shortRepoCtx is the bounded wait a caller advertises as retryable.
func shortRepoCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 250*time.Millisecond)
}

// TestScopeLock_EvictionCannotRemoveARepoInUse is case C. Eviction runs inside the
// maintenance gate, so a repo another daemon is using - foreground work here,
// task activity in the next test - cannot have its bare path removed.
func TestScopeLock_EvictionCannotRemoveARepoInUse(t *testing.T) {
	cacheA, cacheB, _, bare, _ := scopeLockedRepos(t)
	release := holdRepoMutation(t, cacheB, bare)

	removed := false
	ran, err := cacheA.WithRepoMaintenance(context.Background(), bare, func(context.Context) error {
		// What evictRepoCacheLocked does at the end of its run.
		if err := os.RemoveAll(bare); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	if ran || removed {
		t.Fatal("eviction ran while another daemon was using the repo")
	}
	if _, err := os.Stat(bare); err != nil {
		t.Fatalf("repo in use was removed: %v", err)
	}

	release()
	ran, err = cacheA.WithRepoMaintenance(context.Background(), bare, func(context.Context) error {
		if err := os.RemoveAll(bare); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil || !ran || !removed {
		t.Fatalf("eviction after release: ran=%v removed=%v err=%v, want a removal", ran, removed, err)
	}
}

// TestScopeLock_HeavyMaintenanceSkipsWhileTaskActivityHolds is case D: reflog
// expiry and git gc must not run while a task in ANY daemon of this work state
// may be touching the repository, and a task activity claim is what says so.
func TestScopeLock_HeavyMaintenanceSkipsWhileTaskActivityHolds(t *testing.T) {
	cacheA, _, locks, bare, _ := scopeLockedRepos(t)

	// What a running task in the sibling daemon holds for its whole lifetime.
	activity, err := locks.AcquireTargetUse(execenv.RepoActivityTarget(bare), 5*time.Second)
	if err != nil {
		t.Fatalf("task activity claim: %v", err)
	}
	if activity == nil {
		t.Fatal("task activity claim is nil; the scope locks are disabled")
	}

	ran, err := cacheA.WithRepoMaintenance(context.Background(), bare, func(context.Context) error {
		t.Error("heavy maintenance ran while a task in another daemon held the repo")
		return nil
	})
	if err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	if ran {
		t.Fatal("maintenance reported a run while a task held the repo")
	}

	activity.Release()
	ran, err = cacheA.WithRepoMaintenance(context.Background(), bare, func(context.Context) error { return nil })
	if err != nil || !ran {
		t.Fatalf("maintenance after the task finished: ran=%v err=%v, want a run", ran, err)
	}
}

// TestScopeLock_RepoRecreationWaitsForTheDeleteClaim is case E, and the reason
// the claim file lives beside the scope rather than inside the repo: a deletion
// claim survives the removal of the repo it authorises, so a sibling daemon
// cannot clone a new repo at that path until the deletion is done.
func TestScopeLock_RepoRecreationWaitsForTheDeleteClaim(t *testing.T) {
	cacheA, cacheB, _, bare, lockDir := scopeLockedRepos(t)
	if _, err := os.Stat(lockDir); err == nil {
		t.Fatal("scope lock files must not be created inside the repo they guard")
	}

	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cacheA.WithRepoLock(bare, func() error {
			close(started)
			<-finish
			return nil
		})
	}()
	<-started
	// The same claim excludes ordinary foreground mutation before the path is
	// removed, as well as recreation after removal below.
	ctx, cancel := shortRepoCtx()
	entered := false
	err := cacheB.WithRepoLockContext(ctx, bare, func() error { entered = true; return nil })
	cancel()
	if err == nil || entered {
		t.Fatal("a sibling entered foreground mutation while the claim was held")
	}
	// The deletion this claim authorises: the bare path itself is gone.
	if err := os.RemoveAll(bare); err != nil {
		t.Fatalf("remove bare repo: %v", err)
	}

	ctx, cancel = shortRepoCtx()
	recreated := false
	err = cacheB.WithRepoLockContext(ctx, bare, func() error {
		recreated = true
		return os.MkdirAll(bare, 0o755)
	})
	cancel()
	if recreated {
		t.Fatal("a second cache recreated the repo while the deletion claim was still held")
	}
	if err == nil {
		t.Fatal("recreating a removed repo reported success without entering")
	}
	if _, statErr := os.Stat(bare); !os.IsNotExist(statErr) {
		t.Fatalf("the removed repo came back while the claim was held: %v", statErr)
	}

	close(finish)
	<-done

	if err := cacheB.WithRepoLock(bare, func() error { return os.MkdirAll(bare, 0o755) }); err != nil {
		t.Fatalf("recreating the repo after the deletion finished: %v", err)
	}
	if _, err := os.Stat(bare); err != nil {
		t.Fatalf("repo was not recreated after the claim was released: %v", err)
	}
}
