package execenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// gitRootLockHolderMode re-execs this test binary as a process that takes the
// repository lock and holds it. A second process is the only way to test what
// broke here: the previous lock was an in-process mutex, which is exactly the
// scope that does NOT cover production, where every prepare runs in its own
// helper process.
const gitRootLockHolderMode = "execenv-gitroot-lock-holder"

// TestGitRootLockHolderProcess is a no-op in the parent and the lock holder in
// the child.
func TestGitRootLockHolderProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != gitRootLockHolderMode {
		return
	}
	repo := os.Getenv("MULTICA_TEST_LOCK_REPO")
	readyPath := os.Getenv("MULTICA_TEST_LOCK_READY")
	releasePath := os.Getenv("MULTICA_TEST_LOCK_RELEASE")

	unlock, err := lockGitRoot(repo, worktreeTestLogger())
	if err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(readyPath, []byte("held"), 0o644); err != nil {
		os.Exit(4)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(releasePath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(5)
		}
		time.Sleep(10 * time.Millisecond)
	}
	unlock()
	os.Exit(0)
}

func waitForFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startLockHolder runs another PROCESS that takes repo's lock and holds it
// until the returned release func is called. Another process is the only scope
// that tests what broke here: the previous lock was an in-process mutex, and
// production never has two contenders inside one process.
func startLockHolder(t *testing.T, repo string) (release func()) {
	t.Helper()
	signals := t.TempDir()
	readyPath := filepath.Join(signals, "ready")
	releasePath := filepath.Join(signals, "release")

	child := exec.Command(os.Args[0], "-test.run=^TestGitRootLockHolderProcess$", "--", gitRootLockHolderMode)
	child.Env = append(os.Environ(),
		"MULTICA_TEST_LOCK_REPO="+repo,
		"MULTICA_TEST_LOCK_READY="+readyPath,
		"MULTICA_TEST_LOCK_RELEASE="+releasePath,
	)
	if err := child.Start(); err != nil {
		t.Fatalf("start lock holder: %v", err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = os.WriteFile(releasePath, []byte("go"), 0o644)
			_ = child.Wait()
		})
	}
	t.Cleanup(stop)
	waitForFile(t, readyPath, 30*time.Second)
	return stop
}

// The lock must exclude ANOTHER PROCESS, not just another goroutine. Against
// the in-process mutex this test fails immediately: lockGitRoot returned while
// the child still held the repository, which is the state that let two
// prepares run `git stash create` on one index.
func TestLockGitRootExcludesOtherProcesses(t *testing.T) {
	repo := newTestRepo(t)
	release := startLockHolder(t, repo)

	acquired := make(chan error, 1)
	go func() {
		unlock, err := lockGitRoot(repo, worktreeTestLogger())
		if unlock != nil {
			unlock()
		}
		acquired <- err
	}()

	// The holder is alive and has not released, so this must not come back.
	select {
	case err := <-acquired:
		t.Fatalf("lockGitRoot returned (err=%v) while another process held the repository lock", err)
	case <-time.After(500 * time.Millisecond):
	}

	release()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("lockGitRoot after release: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("lockGitRoot never acquired the lock after the holder released it")
	}
}

// One repository, one lock — including across its linked worktrees.
//
// The section under this lock writes refs and worktrees/ in the COMMON git
// dir, which every working tree of a repo shares. Keying on the per-worktree
// git dir gives two resources bound to two linked worktrees two different
// locks while they still race on that shared state.
func TestGitRootLockPathIsRepoWide(t *testing.T) {
	repo := newTestRepo(t)
	path, err := gitRootLockPath(repo)
	if err != nil {
		t.Fatalf("gitRootLockPath: %v", err)
	}
	want := filepath.Join(repo, ".git", gitRootLockFileName)
	if path != want {
		t.Fatalf("lock path = %q, want %q", path, want)
	}

	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, repo, "worktree", "add", "-b", "linked-branch", linked)
	if resolved, evalErr := filepath.EvalSymlinks(linked); evalErr == nil {
		linked = resolved
	}
	linkedPath, err := gitRootLockPath(linked)
	if err != nil {
		t.Fatalf("gitRootLockPath(linked): %v", err)
	}
	if linkedPath != path {
		t.Fatalf("linked worktree locks %q, want the repository's single lock %q", linkedPath, path)
	}
	// The per-worktree git dir is still what the index.lock hint must read:
	// that is where a linked worktree's own index lives.
	linkedGitDir, err := gitDirFor(linked)
	if err != nil {
		t.Fatalf("gitDirFor(linked): %v", err)
	}
	if !strings.Contains(filepath.ToSlash(linkedGitDir), "/worktrees/") {
		t.Fatalf("linked worktree git dir = %q, want it under worktrees/", linkedGitDir)
	}
}

// A held lock file is never stale: the kernel drops the lock when the holder
// exits, and the leftover file means nothing on its own. Telling the user to
// delete it would undo this whole change — the lock binds to an open file
// description, not to the path, so unlinking it lets the next task create and
// lock a NEW file while the holder still owns the old inode, and both then
// believe they are alone in the section.
func TestGitRootLockTimeoutDoesNotAdviseDeletingTheLock(t *testing.T) {
	repo := newTestRepo(t)
	original := gitRootLockWait
	gitRootLockWait = 200 * time.Millisecond
	t.Cleanup(func() { gitRootLockWait = original })

	startLockHolder(t, repo)

	unlock, err := lockGitRoot(repo, worktreeTestLogger())
	if unlock != nil {
		unlock()
	}
	if err == nil {
		t.Fatal("lockGitRoot succeeded while another process held the lock")
	}
	if !errors.Is(err, errGitRootLockBusy) {
		t.Fatalf("error is not errGitRootLockBusy: %v", err)
	}

	msg := err.Error()
	for _, banned := range []string{"can be deleted", "stale lock", "delete the lock"} {
		if strings.Contains(msg, banned) {
			t.Errorf("timeout message advises %q, which breaks the exclusion: %v", banned, err)
		}
	}
	if !strings.Contains(msg, "wait for that task to finish or stop it") {
		t.Errorf("timeout message does not say what to actually do: %v", err)
	}
}

// The index-lock race that used to end the task cannot happen any more: the
// snapshot is built in a private index inside the task's env root, so the only
// index lock it takes is on its own file. This is the regression test for the
// property, not for the retry that used to work around the absence of it
// (#7434).
func TestCaptureUserSnapshotIgnoresTheRepositoryIndexLock(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by the user\n")
	writeFile(t, filepath.Join(repo, "brand-new.txt"), "untracked\n")

	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("hold index.lock: %v", err)
	}
	defer os.Remove(lock)

	head := gitRun(t, repo, "rev-parse", "HEAD")
	snapshot, err := captureUserSnapshot(repo, t.TempDir(), head, worktreeTestLogger())
	if err != nil {
		t.Fatalf("captureUserSnapshot with index.lock held: %v", err)
	}
	if got := gitRun(t, repo, "show", snapshot+":tracked.txt"); got != "edited by the user" {
		t.Errorf("snapshot tracked.txt = %q, want the user's edit", got)
	}
	if got := gitRun(t, repo, "show", snapshot+":brand-new.txt"); got != "untracked" {
		t.Errorf("snapshot brand-new.txt = %q, want the untracked file", got)
	}
	// The user's own index is theirs; the capture may not have touched it.
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the capture removed the user's index.lock: %v", err)
	}
}

// Every failure through runGitStdout used to arrive as "exit status 1": stderr
// was captured by cmd.Output() and then dropped on the floor.
func TestRunGitSurfacesStderrInTheError(t *testing.T) {
	repo := newTestRepo(t)
	_, err := runGitTrimmed(repo, "rev-parse", "--verify", "definitely-not-a-ref")
	if err == nil {
		t.Fatal("rev-parse of a bogus ref succeeded")
	}
	if !strings.Contains(err.Error(), "fatal:") {
		t.Errorf("error dropped git's stderr: %v", err)
	}
}

// The production shape: two tasks on one local_directory, each preparing in
// its own helper process. Both must get a worktree; before the cross-process
// lock one of them routinely died in `git stash create`.
func TestConcurrentIsolatedPreparesOnOneRepo(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new file\n")

	workspacesRoot := t.TempDir()
	logger := worktreeTestLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	type result struct {
		env *Environment
		err error
	}
	taskIDs := []string{
		"aaaaaaaa-1111-4111-8111-aaaaaaaaaaa1",
		"aaaaaaaa-1111-4111-8111-aaaaaaaaaaa2",
	}
	results := make(chan result, len(taskIDs))
	for _, taskID := range taskIDs {
		go func(taskID string) {
			env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), PrepareParams{
				WorkspacesRoot: workspacesRoot,
				WorkspaceID:    "ws-concurrent-worktree",
				TaskID:         taskID,
				Provider:       "claude",
				AgentName:      "J",
				Task:           TaskContextForEnv{IssueID: "issue-" + taskID},
				LocalWorktree:  &LocalWorktreeParams{LocalPath: repo},
			}, logger)
			results <- result{env: env, err: err}
		}(taskID)
	}

	branches := map[string]bool{}
	for range taskIDs {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent PrepareIsolated failed: %v", got.err)
		}
		wt := got.env.LocalWorktree
		if wt == nil {
			t.Fatal("prepared environment has no worktree")
		}
		if branches[wt.Branch] {
			t.Fatalf("two concurrent tasks landed on branch %q", wt.Branch)
		}
		branches[wt.Branch] = true
		// Each task must see the user's uncommitted state, which is precisely
		// what the lost capture would have silently replaced with a clean HEAD.
		if content := readFile(t, filepath.Join(wt.Path, "tracked.txt")); content != "user work in progress\n" {
			t.Fatalf("worktree %s missing the user's edit: %q", wt.Path, content)
		}
		if content := readFile(t, filepath.Join(wt.Path, "untracked.txt")); content != "new file\n" {
			t.Fatalf("worktree %s missing the user's untracked file: %q", wt.Path, content)
		}
	}
}
