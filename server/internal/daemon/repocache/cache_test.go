package repocache

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.Default()
}

func TestBareDirName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"https://github.com/org/my-repo.git", "my-repo.git"},
		{"https://github.com/org/my-repo", "my-repo.git"},
		{"git@github.com:org/my-repo.git", "my-repo.git"},
		{"git@github.com:org/my-repo", "my-repo.git"},
		{"https://github.com/org/repo/", "repo.git"},
		{"my-repo", "my-repo.git"},
		{"", "repo.git"},
	}
	for _, tt := range tests {
		if got := bareDirName(tt.input); got != tt.want {
			t.Errorf("bareDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsBareRepo(t *testing.T) {
	t.Parallel()

	// A directory with a HEAD file should be detected as bare.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	if !isBareRepo(dir) {
		t.Error("expected bare repo to be detected")
	}

	// An empty directory should not.
	emptyDir := t.TempDir()
	if isBareRepo(emptyDir) {
		t.Error("expected empty dir to not be detected as bare repo")
	}
}

// createTestRepo creates a local git repo with an initial commit and returns its path.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %s: %v", out, err)
		}
	}
	return dir
}

func TestSyncAndLookup(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	// Sync should clone the repo.
	err := cache.Sync("ws-123", []RepoInfo{
		{URL: sourceRepo, Description: "test repo"},
	})
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Lookup should find the cached repo.
	path := cache.Lookup("ws-123", sourceRepo)
	if path == "" {
		t.Fatal("expected to find cached repo")
	}
	if !isBareRepo(path) {
		t.Fatalf("expected bare repo at %s", path)
	}

	// Lookup for unknown URL should return empty.
	if got := cache.Lookup("ws-123", "https://github.com/org/unknown"); got != "" {
		t.Fatalf("expected empty for unknown URL, got %q", got)
	}

	// Lookup for unknown workspace should return empty.
	if got := cache.Lookup("ws-999", sourceRepo); got != "" {
		t.Fatalf("expected empty for unknown workspace, got %q", got)
	}
}

func TestSyncFetchesExisting(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	// First sync: clone.
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	// Record the remote-tracking default head in the cache. Under the modern
	// refspec layout, fetches write to refs/remotes/origin/*, not the bare
	// repo's own refs/heads/*, so reading the bare HEAD would return the
	// fossil snapshot from initial clone.
	barePath := cache.Lookup("ws-1", sourceRepo)
	oldHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))

	// Add a commit to source.
	addEmptyCommit(t, sourceRepo, "second")
	sourceHead := gitHead(t, sourceRepo)
	if sourceHead == oldHead {
		t.Fatal("source HEAD should differ after new commit")
	}

	// Second sync: should fetch (not re-clone).
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	// Verify the cache remote-tracking ref was updated.
	newHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))
	if newHead == oldHead {
		t.Fatal("expected cache remote-tracking head to be updated after fetch")
	}
	if newHead != sourceHead {
		t.Fatalf("expected cache head %s to match source head %s", newHead, sourceHead)
	}
}

func gitHead(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed in %s: %v", repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeFromCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	if barePath == "" {
		t.Fatal("expected cached repo")
	}

	// Create a worktree from the bare cache — this is the actual use case.
	worktreeDir := filepath.Join(t.TempDir(), "work")
	cmd := exec.Command("git", "-C", barePath, "worktree", "add", "-b", "test-branch", worktreeDir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add failed: %s: %v", out, err)
	}
	defer exec.Command("git", "-C", barePath, "worktree", "remove", "--force", worktreeDir).Run()

	// Verify worktree exists and is on the right branch.
	cmd = exec.Command("git", "-C", worktreeDir, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := trimLine(string(out)); got != "test-branch" {
		t.Fatalf("expected branch 'test-branch', got %q", got)
	}
}

func TestCreateWorktree(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "Code Reviewer",
		TaskID:      "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify the worktree was created.
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Fatalf("worktree path does not exist: %s", result.Path)
	}

	// Verify branch name format.
	if !strings.HasPrefix(result.BranchName, "agent/code-reviewer/") {
		t.Errorf("expected branch to start with 'agent/code-reviewer/', got %q", result.BranchName)
	}

	// Verify the worktree is on the correct branch.
	cmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != result.BranchName {
		t.Errorf("expected branch %q, got %q", result.BranchName, got)
	}
}

func TestCreateWorktreeNotCached(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     "https://github.com/org/nonexistent",
		WorkDir:     t.TempDir(),
		AgentName:   "Agent",
		TaskID:      "test-task-id",
	})
	if err == nil {
		t.Fatal("expected error for uncached repo")
	}
	if !strings.Contains(err.Error(), "not found in cache") {
		t.Errorf("expected 'not found in cache' error, got: %v", err)
	}
}

func trimLine(s string) string {
	return strings.TrimSpace(s)
}

// gitRefCommit resolves a git ref to its commit SHA in repoPath.
func gitRefCommit(t *testing.T, repoPath, ref string) string {
	t.Helper()
	if ref == "" {
		t.Fatalf("empty ref in %s", repoPath)
	}
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s failed in %s: %v", ref, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

// addEmptyCommit adds an empty commit on the current branch of repoPath.
func addEmptyCommit(t *testing.T, repoPath, message string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "commit", "--allow-empty", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed in %s: %s: %v", repoPath, out, err)
	}
}

// runGitAuthored runs `git -C repoPath <args...>` with the test author env set.
func runGitAuthored(t *testing.T, repoPath string, args ...string) {
	t.Helper()
	full := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s: %v", args, repoPath, out, err)
	}
}

// TestCreateWorktreeFetchesDespiteAgentBranchOnRemote reproduces the original
// stale-cache bug. Under the legacy mirror refspec (+refs/heads/*:refs/heads/*)
// the sequence below would break on the second CreateWorktree because `git
// fetch` tries to overwrite refs/heads/agent/... which is locked by the first
// worktree, and the whole fetch aborts — silently discarding the main-branch
// update too. Under the modern remote-tracking refspec, fetched heads land in
// refs/remotes/origin/* and no longer collide with worktree-locked refs.
func TestCreateWorktreeFetchesDespiteAgentBranchOnRemote(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	// Capture the default branch BEFORE any detach/commit/checkout dance — we
	// need its name later to add new commits to the correct branch.
	defaultBranch := currentBranchName(t, sourceRepo)

	// Put source repo on a detached HEAD so the first worktree's agent branch
	// can be pushed back to it as a regular update (non-bare repos refuse to
	// push to the currently checked-out branch).
	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// First worktree creates refs/heads/agent/... inside the bare cache.
	workDir1 := t.TempDir()
	result1, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir1,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("first CreateWorktree failed: %v", err)
	}

	// Simulate the agent pushing its branch back to origin (i.e. opening a PR).
	// Now sourceRepo has refs/heads/agent/... matching the locked ref in the
	// bare cache, which is the condition that triggered the legacy bug.
	if err := os.WriteFile(filepath.Join(result1.Path, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitAuthored(t, result1.Path, "add", ".")
	runGitAuthored(t, result1.Path, "commit", "-m", "first task")
	runGitAuthored(t, result1.Path, "push", "origin", result1.BranchName)

	// Add a new commit to source's default branch (not the agent branch we
	// just pushed). Then re-detach so future pushes to other branches still work.
	runGitAuthored(t, sourceRepo, "checkout", defaultBranch)
	addEmptyCommit(t, sourceRepo, "new commit on default branch")
	sourceHead := gitRefCommit(t, sourceRepo, "refs/heads/"+defaultBranch)
	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	// Second worktree: CreateWorktree fetches first. Under the legacy refspec
	// this fetch would fail (refusing to fetch into locked refs/heads/agent/...)
	// and the worktree would be based on the stale snapshot. Under the modern
	// refspec this succeeds and the new worktree sees sourceHead.
	workDir2 := t.TempDir()
	result2, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir2,
		AgentName:   "agent",
		TaskID:      "t2222222-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("second CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result2.Path); got != sourceHead {
		t.Fatalf("second worktree HEAD = %s, want %s (remote default head after new commit)", got, sourceHead)
	}
}

// currentBranchName returns the branch name that HEAD points at in repoPath.
// Fails the test if HEAD is detached.
func currentBranchName(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref --short HEAD in %s: %v", repoPath, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		t.Fatalf("empty branch name in %s", repoPath)
	}
	return name
}

// TestEnsureRemoteTrackingLayoutMigratesLegacyCache verifies that a cache
// created with the legacy mirror refspec is migrated in place on next use:
// the refspec is rewritten to the modern remote-tracking layout and
// refs/remotes/origin/* gets backfilled so getRemoteDefaultBranch can resolve
// the remote default.
func TestEnsureRemoteTrackingLayoutMigratesLegacyCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Reset to the legacy mirror refspec to simulate a cache created by an
	// older version of the daemon.
	if err := setFetchRefspec(barePath, "+refs/heads/*:refs/heads/*"); err != nil {
		t.Fatalf("set legacy refspec: %v", err)
	}
	// Wipe any refs/remotes/origin/* that may have been populated by the initial clone.
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/HEAD").Run()
	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	// Sanity check: we've successfully forced the cache into legacy state.
	if cur, _ := readFetchRefspec(barePath); cur != "+refs/heads/*:refs/heads/*" {
		t.Fatalf("precondition failed: refspec is %q, want legacy mirror", cur)
	}

	// ensureRemoteTrackingLayout should migrate: rewrite refspec, backfill
	// refs/remotes/origin/*, and set origin HEAD.
	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		t.Fatalf("ensureRemoteTrackingLayout failed: %v", err)
	}

	cur, err := readFetchRefspec(barePath)
	if err != nil {
		t.Fatalf("read refspec after migration: %v", err)
	}
	if cur != modernFetchRefspec {
		t.Errorf("refspec = %q, want %q", cur, modernFetchRefspec)
	}

	// getRemoteDefaultBranch should now return a refs/remotes/origin/<branch>.
	ref := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(ref, "refs/remotes/origin/") {
		t.Errorf("getRemoteDefaultBranch = %q, want refs/remotes/origin/*", ref)
	}
}

// TestCreateWorktreePathCollisionDoesNotLeakBranch verifies the secondary bug
// fix: when the worktree path already exists as a non-worktree (e.g. a plain
// directory), createWorktree must fail cleanly without leaking a branch into
// the bare repo. Previously the "already exists" retry logic would
// misclassify path collisions as branch collisions and create a second
// timestamp-suffixed branch before hitting the same path error.
func TestCreateWorktreePathCollisionDoesNotLeakBranch(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Pre-create the target worktree path as a plain non-empty directory.
	workDir := t.TempDir()
	dirName := repoNameFromURL(sourceRepo)
	worktreePath := filepath.Join(workDir, dirName)
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("pre-create worktree path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err == nil {
		t.Fatal("expected CreateWorktree to fail when path exists as non-worktree")
	}

	// No agent/* branches should have been created in the bare repo as a
	// side effect of the failed call.
	out, runErr := exec.Command("git", "-C", barePath, "for-each-ref", "--format=%(refname)", "refs/heads/agent").Output()
	if runErr != nil {
		t.Fatalf("for-each-ref failed: %v", runErr)
	}
	if leaked := strings.TrimSpace(string(out)); leaked != "" {
		t.Errorf("branch leaked into bare repo after path-collision failure:\n%s", leaked)
	}
}

// TestGetRemoteDefaultBranchScansForCustomDefault verifies fallback (3) of
// getRemoteDefaultBranch: when the cache has refs/remotes/origin/<custom>
// (e.g. develop, trunk) but no refs/remotes/origin/HEAD and no main/master,
// the function picks the custom branch instead of returning empty.
func TestGetRemoteDefaultBranchScansForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Resolve the existing default branch's commit so we can repoint a
	// custom-named ref at it, then wipe the standard refs to force the
	// fallback path.
	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Create refs/remotes/origin/develop pointing at that commit.
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/develop", commit)
	// Now wipe origin/HEAD (symbolic-ref -d removes the symref file itself)
	// and the common defaults so steps 1 and 2 of the resolver miss and we
	// fall through to the for-each-ref scan.
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/develop" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/develop", got)
	}
}

// TestGetRemoteDefaultBranchFallsBackToBareHead verifies fallback (5):
// a legacy / migration-pending cache that has no refs/remotes/origin/* at all
// but still has its bare HEAD pointing at refs/heads/<branch> (the snapshot
// from the original mirror clone) should resolve to that local head instead
// of failing. This protects against transient backfill-fetch failures during
// the legacy → modern refspec migration. Gated on refs/remotes/origin/* being
// completely empty — with any modern remote-tracking refs present, the
// resolver refuses to reach back into the stale bare heads.
func TestGetRemoteDefaultBranchFallsBackToBareHead(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Force the cache into a state that mimics "legacy mirror clone whose
	// post-migration backfill fetch failed":
	//   - bare HEAD still points at refs/heads/<default>
	//   - refs/remotes/origin/* is empty
	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	// Sanity: origin/* is gone, HEAD is still a symbolic ref to refs/heads/*.
	if out, err := exec.Command("git", "-C", barePath, "for-each-ref", "refs/remotes/origin/").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("precondition failed: refs/remotes/origin/* should be empty, got %s", out)
	}

	got := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(got, "refs/heads/") {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/heads/* fallback", got)
	}

	// And the resolved ref must actually exist — verifying bareHeadBranch's
	// rev-parse guard kicked in correctly.
	if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", got).Run(); err != nil {
		t.Fatalf("resolved ref %q does not exist: %v", got, err)
	}
}

// TestGitFetchRefreshesOriginHeadAfterDefaultChange verifies that an
// already-modern cache picks up a remote default-branch change. Plain `git
// fetch` never refreshes refs/remotes/origin/HEAD on its own, so without
// gitFetch's explicit `git remote set-head origin --auto` call the resolver
// would keep returning the original default branch forever after the
// upstream flipped (e.g. master → main on a long-lived repo). This guards
// against the "already-modern cache never refreshes origin/HEAD" regression.
func TestGitFetchRefreshesOriginHeadAfterDefaultChange(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	initialBranch := currentBranchName(t, sourceRepo)

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	// Precondition: cache is already modern and origin/HEAD points at the
	// source's initial default branch.
	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/"+initialBranch {
		t.Fatalf("precondition: getRemoteDefaultBranch = %q, want refs/remotes/origin/%s", got, initialBranch)
	}

	// Flip the source's default: create a new branch, commit on it, stay
	// checked out on it so the source's HEAD reflects the new default. A
	// subsequent `git ls-remote` against the source advertises this new
	// HEAD, which is what set-head --auto consumes.
	runGitAuthored(t, sourceRepo, "checkout", "-b", "new-default")
	addEmptyCommit(t, sourceRepo, "new-default commit")

	// Fetch via the cache's code path. Without the set-head call, origin/HEAD
	// would still point at the old default here.
	if err := gitFetch(barePath); err != nil {
		t.Fatalf("gitFetch failed: %v", err)
	}

	// refs/remotes/origin/HEAD must now point at the new default branch.
	out, err := exec.Command("git", "-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref origin/HEAD after fetch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "refs/remotes/origin/new-default" {
		t.Fatalf("origin/HEAD after fetch = %q, want refs/remotes/origin/new-default", got)
	}

	// And getRemoteDefaultBranch must resolve through step 1 (verified
	// origin/HEAD) to the new default — not through step 2 where origin/main
	// or origin/master could accidentally match the old branch.
	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/new-default" {
		t.Fatalf("getRemoteDefaultBranch after fetch = %q, want refs/remotes/origin/new-default", got)
	}
}

// TestGetRemoteDefaultBranchUsesBareHeadHintForCustomDefault verifies step 3
// of the resolver: when the cache has a non-standard default branch name
// (trunk, develop, …) and `git remote set-head origin --auto` didn't
// populate refs/remotes/origin/HEAD, the resolver must use the bare repo's
// own HEAD as a hint to pick refs/remotes/origin/<same name> — NOT fall
// through to a refname-order scan that would pick the wrong branch.
func TestGetRemoteDefaultBranchUsesBareHeadHintForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Simulate a custom default branch: create refs/heads/trunk in the bare
	// repo and point HEAD at it. `git clone --bare` would do the equivalent
	// when the remote's default was "trunk", so this matches real-world
	// state for such remotes.
	runGitAuthored(t, barePath, "update-ref", "refs/heads/trunk", commit)
	runGitAuthored(t, barePath, "symbolic-ref", "HEAD", "refs/heads/trunk")

	// Populate two refs/remotes/origin/* entries. "feature-alpha" is
	// alphabetically earlier than "trunk" — a refname-order scan (the old
	// bug) would return feature-alpha, not trunk.
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/trunk", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-alpha", commit)

	// Knock out the ahead-of-step-3 fallbacks so resolution must rely on
	// the bare-HEAD hint.
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/trunk" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/trunk (via bare-HEAD hint)", got)
	}
}

// TestGetRemoteDefaultBranchAmbiguousOriginReturnsEmpty verifies step 4's
// safe-scan gating: when the cache has multiple refs/remotes/origin/*
// entries, none match the common defaults, and none match the bare HEAD
// either, the resolver must refuse to guess and return "". The caller
// surfaces this as a hard error instead of silently basing new agent work
// on an arbitrary refname-order-first candidate.
func TestGetRemoteDefaultBranchAmbiguousOriginReturnsEmpty(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	// Populate two unrelated origin branches (none of which match any of
	// the step 1-3 fallbacks).
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-a", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-b", commit)

	// Wipe every ref a step 1-3 fallback could pick up:
	//   step 1: origin/HEAD
	//   step 2: origin/main, origin/master
	//   step 3: the origin/<bareHEAD-name> bridge
	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()
	if bareRef := bareHeadBranch(barePath); bareRef != "" {
		sameName := strings.TrimPrefix(bareRef, "refs/heads/")
		_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/"+sameName).Run()
	}

	got := getRemoteDefaultBranch(barePath)
	if got != "" {
		t.Fatalf("getRemoteDefaultBranch = %q, want \"\" (ambiguous origin/* must not guess)", got)
	}
}
