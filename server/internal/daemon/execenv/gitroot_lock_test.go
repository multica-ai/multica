package execenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// The lock must exclude ANOTHER PROCESS, not just another goroutine. Against
// the in-process mutex this test fails immediately: lockGitRoot returned while
// the child still held the repository, which is the state that let two
// prepares run `git stash create` on one index.
func TestLockGitRootExcludesOtherProcesses(t *testing.T) {
	repo := newTestRepo(t)
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
	defer func() {
		_ = os.WriteFile(releasePath, []byte("go"), 0o644)
		_ = child.Wait()
	}()
	waitForFile(t, readyPath, 30*time.Second)

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

	if err := os.WriteFile(releasePath, []byte("go"), 0o644); err != nil {
		t.Fatalf("signal release: %v", err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("lockGitRoot after release: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("lockGitRoot never acquired the lock after the holder released it")
	}
}

// The lock file belongs next to the index it protects. filepath.Join(root,
// ".git") would be wrong in a linked worktree, where .git is a file and the
// index lives under .git/worktrees/<name>/.
func TestGitRootLockPathIsInsideTheGitDir(t *testing.T) {
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
	linkedPath, err := gitRootLockPath(linked)
	if err != nil {
		t.Fatalf("gitRootLockPath(linked): %v", err)
	}
	if !strings.Contains(filepath.ToSlash(linkedPath), "/worktrees/") {
		t.Fatalf("linked worktree lock = %q, want it inside the worktree's own git dir", linkedPath)
	}
}

// Losing the index-lock race used to end the task. It is a millisecond-scale
// condition owned by the user's own tools, so the capture rides it out.
func TestCaptureDirtyStateRetriesUntilTheIndexLockClears(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by the user\n")

	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("hold index.lock: %v", err)
	}
	cleared := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.Remove(lock)
		close(cleared)
	}()

	sha, err := captureDirtyState(repo, worktreeTestLogger())
	<-cleared
	if err != nil {
		t.Fatalf("captureDirtyState: %v", err)
	}
	if sha == "" {
		t.Fatal("captured no stash commit for a dirty tree")
	}
	// The capture must be readable as a real commit carrying the user's edit.
	if got := gitRun(t, repo, "show", sha+":tracked.txt"); got != "edited by the user" {
		t.Fatalf("stash commit content = %q, want the user's edit", got)
	}
}

// git reports this failure as a bare exit status 1 with NOTHING on stderr, so
// the error has to name the index lock itself or there is nothing to act on.
func TestCaptureDirtyStateNamesTheIndexLockHolder(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by the user\n")

	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("hold index.lock: %v", err)
	}
	defer os.Remove(lock)

	_, err := captureDirtyState(repo, worktreeTestLogger())
	if err == nil {
		t.Fatal("captureDirtyState succeeded while index.lock was held")
	}
	msg := err.Error()
	if !strings.Contains(msg, "index.lock") {
		t.Errorf("error does not name the index lock, so it is not actionable: %v", err)
	}
	if !strings.Contains(msg, "times") {
		t.Errorf("error does not report that the capture was retried: %v", err)
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
