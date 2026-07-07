package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// initGitRepo initialises a git repo at dir with a single empty commit so
// `git worktree add HEAD` has something to check out. When git isn't on
// PATH — or the sequence fails for any environment reason — the test is
// skipped, matching the pattern used by repocache/cache_test.go so
// hermetic CI containers without git remain green.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	envs := append(os.Environ(),
		"GIT_AUTHOR_NAME=multica-test", "GIT_AUTHOR_EMAIL=test@multica.test",
		"GIT_COMMITTER_NAME=multica-test", "GIT_COMMITTER_EMAIL=test@multica.test",
		// Force a stable initial branch so `git worktree add HEAD` works
		// on hosts that default to `main` and on hosts that default to
		// `master` alike. HEAD itself is the ref we ask for.
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	steps := [][]string{
		{"-c", "init.defaultBranch=main", "init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Env = envs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed (%v): %s", err, strings.TrimSpace(string(out)))
		}
	}
}

// discardLogger returns a slog logger that drops all output. Used to keep
// pool test failures readable — the manager logs at info on each acquire,
// which drowns the actual assertion output when a run breaks.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWorktreePool_Acquire_ReleaseFlow(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-alpha")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if alloc == nil || alloc.WorkDir == "" {
		t.Fatalf("expected allocation with WorkDir, got %+v", alloc)
	}
	if _, err := os.Stat(alloc.WorkDir); err != nil {
		t.Fatalf("worktree dir missing after acquire: %v", err)
	}
	if alloc.Branch != "multica/task-alpha" {
		t.Errorf("branch = %q, want %q", alloc.Branch, "multica/task-alpha")
	}

	// The base repo should now advertise the new worktree.
	listOut, err := runGit(context.Background(), "-C", base, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	if !strings.Contains(listOut, "task-alpha") {
		t.Fatalf("expected task-alpha worktree in list, got:\n%s", listOut)
	}

	alloc.Release()

	// Clean-release should remove the worktree from disk.
	if _, err := os.Stat(alloc.WorkDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected worktree removed after release, stat err = %v", err)
	}
}

func TestWorktreePool_ParallelAllocations(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc1, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-1")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer alloc1.Release()

	alloc2, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-2")
	if err != nil {
		t.Fatalf("second acquire failed (parallel should be allowed): %v", err)
	}
	defer alloc2.Release()

	if alloc1.WorkDir == alloc2.WorkDir {
		t.Fatalf("expected distinct worktree paths, got %q for both tasks", alloc1.WorkDir)
	}
}

func TestWorktreePool_SaturationReturnsHolders(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc1, err := mgr.Acquire(context.Background(), base, poolRoot, 2, "task-1")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer alloc1.Release()
	alloc2, err := mgr.Acquire(context.Background(), base, poolRoot, 2, "task-2")
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer alloc2.Release()

	// max_parallel=2, two leases outstanding — the third must fail with
	// PoolSaturatedError and the two current holders.
	_, err = mgr.Acquire(context.Background(), base, poolRoot, 2, "task-3")
	if err == nil {
		t.Fatalf("expected saturation error, got nil")
	}
	var saturated *PoolSaturatedError
	if !errors.As(err, &saturated) {
		t.Fatalf("error is not PoolSaturatedError: %v", err)
	}
	if saturated.MaxParallel != 2 {
		t.Errorf("MaxParallel = %d, want 2", saturated.MaxParallel)
	}
	if len(saturated.Holders) != 2 {
		t.Errorf("Holders len = %d, want 2 (%v)", len(saturated.Holders), saturated.Holders)
	}
	// errors.Is via Unwrap must round-trip so callers can branch on a
	// sentinel without a type switch.
	if !errors.Is(err, errPoolSaturated) {
		t.Errorf("errors.Is(errPoolSaturated) = false, want true")
	}
}

func TestWorktreePool_ReleaseFreesSlot(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc1, err := mgr.Acquire(context.Background(), base, poolRoot, 1, "task-1")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if _, err := mgr.Acquire(context.Background(), base, poolRoot, 1, "task-2"); err == nil {
		t.Fatalf("expected saturation on second acquire")
	}
	alloc1.Release()
	alloc2, err := mgr.Acquire(context.Background(), base, poolRoot, 1, "task-2")
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	defer alloc2.Release()
	if alloc2.WorkDir == alloc1.WorkDir {
		// Same UUID would collide; task-2 must live under its own path.
		t.Errorf("post-release alloc reused the released WorkDir %q", alloc2.WorkDir)
	}
}

func TestWorktreePool_DirtyWorktreeIsKept(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-dirty")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	// Simulate agent output — a plain unstaged file makes the worktree
	// dirty from `git status --porcelain` perspective.
	dirty := filepath.Join(alloc.WorkDir, "notes.txt")
	if err := os.WriteFile(dirty, []byte("agent scratch"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}
	// Stage it so `--untracked-files=no` still classes the tree dirty.
	if out, err := runGit(context.Background(), "-C", alloc.WorkDir, "add", "notes.txt"); err != nil {
		t.Fatalf("git add failed (%v): %s", err, out)
	}

	alloc.Release()

	// Directory must still exist — losing user work is the exact
	// failure claude-code#55724 documents and must not regress.
	if _, err := os.Stat(dirty); err != nil {
		t.Fatalf("dirty worktree file missing after release: %v", err)
	}

	// The lease slot must still be freed even though the directory
	// stayed — otherwise MaxParallel would drift down every dirty run.
	alloc2, err := mgr.Acquire(context.Background(), base, poolRoot, 1, "task-followup")
	if err != nil {
		t.Fatalf("post-dirty acquire failed (slot should be free): %v", err)
	}
	defer alloc2.Release()
}

// TestWorktreePool_UntrackedOnlyIsKept pins the guarantee flagged in the
// #4986 review: an agent that only creates fresh, untracked files (never
// running `git add`) must not have those files deleted on release. Prior
// to the fix `worktreeIsDirty` ran with `--untracked-files=no`, which
// classified this state as "clean" and asked git to `remove` the tree —
// git itself would then refuse, but the log line lied about the disk
// state. The current implementation counts untracked as dirty so the
// "leaving on disk for user inspection" path fires directly.
func TestWorktreePool_UntrackedOnlyIsKept(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	alloc, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-untracked")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// A brand new untracked file — the exact "agent scribbled a
	// draft" scenario. Deliberately no `git add`.
	fresh := filepath.Join(alloc.WorkDir, "draft.txt")
	if err := os.WriteFile(fresh, []byte("agent draft"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	// Confirm worktreeIsDirty now classes untracked-only as dirty.
	dirty, err := worktreeIsDirty(alloc.WorkDir)
	if err != nil {
		t.Fatalf("worktreeIsDirty: %v", err)
	}
	if !dirty {
		t.Fatalf("worktreeIsDirty on untracked-only tree = false, want true")
	}

	alloc.Release()

	// The user's untracked file — and the worktree directory itself —
	// must still exist. Regression guard: reverting to
	// `--untracked-files=no` would silently delete `draft.txt` here
	// (well, git would refuse, but the code path would look correct
	// while the file was still gone in the untested async version).
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("untracked file missing after release: %v", err)
	}

	// And the slot returned to the pool.
	alloc2, err := mgr.Acquire(context.Background(), base, poolRoot, 1, "task-followup")
	if err != nil {
		t.Fatalf("post-untracked-dirty acquire failed: %v", err)
	}
	defer alloc2.Release()
}

func TestWorktreePool_ConcurrentAllocationsAreSerialised(t *testing.T) {
	// Regression guard for the claude-code#34645 class of bug: two
	// concurrent `git worktree add` calls racing on `.git/config.lock`.
	// The per-repo mutex inside the manager must serialise these
	// operations even when the caller fans out on many goroutines.
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()

	mgr := NewWorktreePoolManager(discardLogger())

	const n = 6
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		allocs []*PoolAllocation
		errs   []error
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		id := "task-parallel-" + string(rune('a'+i))
		go func(id string) {
			defer wg.Done()
			alloc, err := mgr.Acquire(context.Background(), base, poolRoot, n, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			allocs = append(allocs, alloc)
		}(id)
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent acquires produced errors: %v", errs)
	}
	if len(allocs) != n {
		t.Fatalf("got %d allocations, want %d", len(allocs), n)
	}
	// Every path must be unique — proving the pool did not hand out
	// the same directory to two goroutines racing through Acquire.
	seen := map[string]bool{}
	for _, a := range allocs {
		if seen[a.WorkDir] {
			t.Errorf("duplicate WorkDir %q handed out", a.WorkDir)
		}
		seen[a.WorkDir] = true
	}
	for _, a := range allocs {
		a.Release()
	}
}

func TestWorktreePool_SubmoduleRepoRefused(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)

	// Create a submodule source repo, then wire it in and commit
	// `.gitmodules` + the pointer. `git submodule add` writes to
	// `.git/config`, which is exactly the shared state we want to
	// protect against — the pool refuses this repo up front.
	sub := t.TempDir()
	initGitRepo(t, sub)

	if out, err := runGit(context.Background(),
		"-c", "protocol.file.allow=always",
		"-C", base, "-c", "protocol.file.allow=always",
		"submodule", "add", sub, "sub"); err != nil {
		t.Skipf("git submodule add failed (%v): %s", err, out)
	}
	if out, err := runGit(context.Background(), "-C", base, "commit", "-m", "add sub",
		"--author=multica-test <test@multica.test>"); err != nil {
		t.Skipf("git commit failed (%v): %s", err, out)
	}

	mgr := NewWorktreePoolManager(discardLogger())
	_, err := mgr.Acquire(context.Background(), base, t.TempDir(), 4, "task-sub")
	if err == nil {
		t.Fatalf("expected submodule refusal, got nil")
	}
	if !strings.Contains(err.Error(), "submodule") {
		t.Errorf("error should mention submodule, got %v", err)
	}
}

func TestWorktreePool_MissingBaseIsRejected(t *testing.T) {
	mgr := NewWorktreePoolManager(discardLogger())
	// Point at a temp dir that isn't a git repo — `git worktree add`
	// will fail loudly with the git error text.
	base := t.TempDir()
	_, err := mgr.Acquire(context.Background(), base, t.TempDir(), 4, "task-nope")
	if err == nil {
		t.Fatalf("expected acquire against non-git base to fail")
	}
	// The failure text should include either "not a git" (English) or
	// the raw exit — either way, it must not silently succeed.
}

func TestWorktreePool_PoolRootCreatedIfMissing(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	// Deliberately point at a subdir under a fresh temp dir that
	// doesn't exist yet — the manager must create it.
	root := filepath.Join(t.TempDir(), "sub", "worktrees")
	mgr := NewWorktreePoolManager(discardLogger())
	alloc, err := mgr.Acquire(context.Background(), base, root, 4, "task-mkroot")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer alloc.Release()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("pool_root not created: %v", err)
	}
}

func TestWorktreePool_NonEmptyLeftoverRefused(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)
	poolRoot := t.TempDir()
	leftover := filepath.Join(poolRoot, "task-leftover")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "residue"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewWorktreePoolManager(discardLogger())
	_, err := mgr.Acquire(context.Background(), base, poolRoot, 4, "task-leftover")
	if err == nil {
		t.Fatalf("expected refusal for non-empty leftover")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("error should mention non-empty, got %v", err)
	}
}

// TestWorktreePool_ContextCancelAborts guards against a hung Acquire when
// git itself is stuck (rare but seen in the wild). A cancelled ctx must
// surface promptly.
func TestWorktreePool_ContextCancelAborts(t *testing.T) {
	base := t.TempDir()
	initGitRepo(t, base)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr := NewWorktreePoolManager(discardLogger())
	_, err := mgr.Acquire(ctx, base, t.TempDir(), 4, "task-ctx")
	if err == nil {
		t.Fatalf("expected error from cancelled ctx")
	}
	// We don't strictly require errors.Is(err, context.Canceled) because
	// git may translate the signal to its own message; the surface
	// contract is just "acquire must not hang", which the fact that we
	// returned satisfies.
	_ = time.Now // keep the time import stable across future edits
}
