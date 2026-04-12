package repocache

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestCreateWorktreeFromLocal_Basic covers the happy path: a brand-new
// worktree is created off the user's local repo and points at a fresh
// agent/* branch rooted at HEAD.
func TestCreateWorktreeFromLocal_Basic(t *testing.T) {
	t.Parallel()
	local := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	workDir := t.TempDir()

	res, err := cache.CreateWorktreeFromLocal(LocalWorktreeParams{
		LocalPath: local,
		WorkDir:   workDir,
		AgentName: "Lambda",
		TaskID:    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeFromLocal: %v", err)
	}
	if res == nil || res.Path == "" {
		t.Fatalf("expected non-nil result with path; got %+v", res)
	}
	if !strings.HasPrefix(res.BranchName, "agent/lambda/") {
		t.Errorf("branch name should start with agent/lambda/, got %q", res.BranchName)
	}
	if fi, err := os.Stat(res.Path); err != nil || !fi.IsDir() {
		t.Errorf("worktree path not a directory: %v", err)
	}

	// The worktree must NOT live inside the user's repo.
	if strings.HasPrefix(res.Path+string(os.PathSeparator), local+string(os.PathSeparator)) {
		t.Errorf("worktree path %q is inside user repo %q", res.Path, local)
	}

	// The worktree branch must be rooted at the user's HEAD commit.
	userHead := gitHeadCommit(t, local)
	wtHead := gitHeadCommit(t, res.Path)
	if userHead != wtHead {
		t.Errorf("worktree HEAD %q != user HEAD %q", wtHead, userHead)
	}
}

// TestCreateWorktreeFromLocal_ConcurrentAgents is the critical concurrency
// case: multiple agents start tasks against the same user repo simultaneously,
// and each gets an independent worktree on a unique branch with no lockfile
// contention. The user's working tree must be byte-identical at the end.
func TestCreateWorktreeFromLocal_ConcurrentAgents(t *testing.T) {
	t.Parallel()
	local := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	// Record the user's tree state before agents run — we verify it's
	// unchanged after all concurrent worktrees finish.
	beforeStatus := gitStatusPorcelain(t, local)
	beforeHead := gitHeadCommit(t, local)

	const n = 6
	type result struct {
		path   string
		branch string
		err    error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each task uses a unique id and a unique parent workdir so
			// they can't stomp on each other's worktree directories; the
			// test exercises concurrency on the *same user repo* only.
			wd := t.TempDir()
			res, err := cache.CreateWorktreeFromLocal(LocalWorktreeParams{
				LocalPath: local,
				WorkDir:   wd,
				AgentName: "agent-" + string(rune('a'+i)),
				TaskID:    strings.Repeat(string(rune('0'+i)), 32),
			})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			results[i] = result{path: res.Path, branch: res.BranchName}
		}()
	}
	wg.Wait()

	paths := make(map[string]bool)
	branches := make(map[string]bool)
	for i, r := range results {
		if r.err != nil {
			t.Errorf("task %d: %v", i, r.err)
			continue
		}
		if paths[r.path] {
			t.Errorf("task %d: duplicate worktree path %q", i, r.path)
		}
		if branches[r.branch] {
			t.Errorf("task %d: duplicate branch %q", i, r.branch)
		}
		paths[r.path] = true
		branches[r.branch] = true
	}
	if len(paths) != n {
		t.Errorf("expected %d unique paths, got %d", n, len(paths))
	}
	if len(branches) != n {
		t.Errorf("expected %d unique branches, got %d", n, len(branches))
	}

	// User tree must be unchanged.
	if afterStatus := gitStatusPorcelain(t, local); afterStatus != beforeStatus {
		t.Errorf("user tree status changed:\nbefore=%q\nafter=%q", beforeStatus, afterStatus)
	}
	if afterHead := gitHeadCommit(t, local); afterHead != beforeHead {
		t.Errorf("user HEAD changed: before=%q after=%q", beforeHead, afterHead)
	}
}

// TestCreateWorktreeFromLocal_RefusesWorkdirInsideUserRepo guards against
// agent work sprawling into the user's tracked directory tree.
func TestCreateWorktreeFromLocal_RefusesWorkdirInsideUserRepo(t *testing.T) {
	t.Parallel()
	local := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	_, err := cache.CreateWorktreeFromLocal(LocalWorktreeParams{
		LocalPath: local,
		WorkDir:   filepath.Join(local, "agents"), // inside user repo — forbidden
		AgentName: "Lambda",
		TaskID:    "deadbeef-0000-0000-0000-000000000000",
	})
	if err == nil {
		t.Fatalf("expected error when work dir is inside user repo, got nil")
	}
	if !strings.Contains(err.Error(), "must not be inside local repo") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCreateWorktreeFromLocal_RemoveCleansUp verifies the cleanup helper
// removes the worktree dir and prunes the admin entry so `git worktree list`
// no longer lists it.
func TestCreateWorktreeFromLocal_RemoveCleansUp(t *testing.T) {
	t.Parallel()
	local := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	workDir := t.TempDir()

	res, err := cache.CreateWorktreeFromLocal(LocalWorktreeParams{
		LocalPath: local,
		WorkDir:   workDir,
		AgentName: "Lambda",
		TaskID:    "11111111-2222-3333-4444-555555555555",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeFromLocal: %v", err)
	}

	if err := cache.RemoveLocalWorktree(local, res.Path); err != nil {
		t.Fatalf("RemoveLocalWorktree: %v", err)
	}
	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir removed; stat err=%v", err)
	}

	// `git worktree list` should no longer show it.
	out, err := exec.Command("git", "-C", local, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if strings.Contains(string(out), res.Path) {
		t.Errorf("worktree still listed after remove:\n%s", string(out))
	}
}

// TestGitCommonDir_AcceptsSubdirectory ensures users can register a path
// that is a subdirectory of their repo (common when they cd into src/).
func TestGitCommonDir_AcceptsSubdirectory(t *testing.T) {
	t.Parallel()
	local := createTestRepo(t)
	sub := filepath.Join(local, "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	commonDir, err := GitCommonDir(sub)
	if err != nil {
		t.Fatalf("GitCommonDir(subdir): %v", err)
	}
	// The common dir should be the user repo's .git (or the repo itself if
	// it was initialized as a plain dir).
	if !strings.HasPrefix(commonDir, local) {
		t.Errorf("common dir %q not under local repo %q", commonDir, local)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func gitHeadCommit(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func gitStatusPorcelain(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain=v1").Output()
	if err != nil {
		t.Fatalf("git status in %s: %v", dir, err)
	}
	// Normalize line ordering for stable comparison.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
