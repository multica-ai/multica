package repocache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Re-running checkout on a path that already holds a checkout must never
// silently lose work (MUL-7284). These tests cover both checkout shapes: the
// linked worktree most runtimes get and the task-local clone Linux and
// Windows Codex get.

var checkoutModes = []struct {
	name     string
	isolated bool
}{
	{name: "linked", isolated: false},
	{name: "isolated", isolated: true},
}

const (
	firstTaskID  = "11111111-1111-1111-1111-111111111111"
	secondTaskID = "22222222-2222-2222-2222-222222222222"
)

// existingCheckoutFixture is a source repository with one tracked file, a
// cache synced from it, and one workdir every checkout in the test targets.
type existingCheckoutFixture struct {
	source        string
	defaultBranch string
	cache         *Cache
	workDir       string
	isolated      bool
}

func newExistingCheckoutFixture(t *testing.T, isolated bool) *existingCheckoutFixture {
	t.Helper()
	source := createTestRepo(t)
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitAuthored(t, source, "add", "tracked.txt")
	runGitAuthored(t, source, "commit", "-m", "add tracked file")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: source}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	return &existingCheckoutFixture{
		source:        source,
		defaultBranch: currentBranchName(t, source),
		cache:         cache,
		workDir:       t.TempDir(),
		isolated:      isolated,
	}
}

func (f *existingCheckoutFixture) checkout(t *testing.T, taskID string, fresh bool) *WorktreeResult {
	t.Helper()
	result, err := f.cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             f.source,
		WorkDir:             f.workDir,
		AgentName:           "Agent",
		TaskID:              taskID,
		IsolatedGitMetadata: f.isolated,
		Fresh:               fresh,
	})
	if err != nil {
		t.Fatalf("CreateWorktree(task %s, fresh=%v) failed: %v", taskID, fresh, err)
	}
	return result
}

// advanceUpstream commits to the source's default branch and syncs the cache,
// so a checkout that moved to the latest default branch would land on the
// returned commit.
func (f *existingCheckoutFixture) advanceUpstream(t *testing.T) string {
	t.Helper()
	addEmptyCommit(t, f.source, "upstream advance")
	if err := f.cache.Sync("ws-1", []RepoInfo{{URL: f.source}}); err != nil {
		t.Fatalf("refresh sync failed: %v", err)
	}
	return gitHead(t, f.source)
}

func taskBranchName(taskID string) string {
	return "agent/agent/" + taskKey(taskID)
}

func localAgentBranches(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := runGitOutput("-C", repoPath, "for-each-ref", "--format=%(refname)", "refs/heads/agent/")
	if err != nil {
		t.Fatalf("list agent branches: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRepeatedCheckoutKeepsLocalWork(t *testing.T) {
	t.Parallel()

	kinds := []struct {
		name            string
		makeWork        func(t *testing.T, path string)
		assertKept      func(t *testing.T, path string)
		wantUncommitted int
		wantUnpushed    int
	}{
		{
			name: "uncommitted change",
			makeWork: func(t *testing.T, path string) {
				if err := os.WriteFile(filepath.Join(path, "tracked.txt"), []byte("edited\n"), 0o644); err != nil {
					t.Fatalf("edit tracked file: %v", err)
				}
			},
			assertKept: func(t *testing.T, path string) {
				if got := readFile(t, filepath.Join(path, "tracked.txt")); got != "edited\n" {
					t.Fatalf("tracked.txt = %q, want the uncommitted edit", got)
				}
			},
			wantUncommitted: 1,
		},
		{
			name: "untracked file",
			makeWork: func(t *testing.T, path string) {
				if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("new\n"), 0o644); err != nil {
					t.Fatalf("write untracked file: %v", err)
				}
			},
			assertKept: func(t *testing.T, path string) {
				if got := readFile(t, filepath.Join(path, "new.txt")); got != "new\n" {
					t.Fatalf("new.txt = %q, want the untracked file", got)
				}
			},
			wantUncommitted: 1,
		},
		{
			name: "unpushed commit",
			makeWork: func(t *testing.T, path string) {
				if err := os.WriteFile(filepath.Join(path, "committed.txt"), []byte("committed\n"), 0o644); err != nil {
					t.Fatalf("write committed file: %v", err)
				}
				runGitAuthored(t, path, "add", "committed.txt")
				runGitAuthored(t, path, "commit", "-m", "unpushed work")
			},
			assertKept: func(t *testing.T, path string) {
				if got := readFile(t, filepath.Join(path, "committed.txt")); got != "committed\n" {
					t.Fatalf("committed.txt = %q, want the committed file", got)
				}
			},
			wantUnpushed: 1,
		},
	}

	for _, mode := range checkoutModes {
		for _, kind := range kinds {
			t.Run(mode.name+"/"+kind.name, func(t *testing.T) {
				t.Parallel()
				f := newExistingCheckoutFixture(t, mode.isolated)
				first := f.checkout(t, firstTaskID, false)
				kind.makeWork(t, first.Path)
				headBefore := gitHead(t, first.Path)
				upstream := f.advanceUpstream(t)

				second := f.checkout(t, secondTaskID, false)

				if second.Kept != KeptLocalWork {
					t.Fatalf("Kept = %q, want %q", second.Kept, KeptLocalWork)
				}
				if second.Path != first.Path || second.BranchName != first.BranchName {
					t.Fatalf("result = %+v, want the existing checkout %s on %s", second, first.Path, first.BranchName)
				}
				if second.UncommittedFiles != kind.wantUncommitted || second.UnpushedCommits != kind.wantUnpushed {
					t.Fatalf("reported %d uncommitted files and %d unpushed commits, want %d and %d",
						second.UncommittedFiles, second.UnpushedCommits, kind.wantUncommitted, kind.wantUnpushed)
				}
				if got := currentBranchName(t, second.Path); got != first.BranchName {
					t.Fatalf("checkout switched to %q, want it left on %q", got, first.BranchName)
				}
				if got := gitHead(t, second.Path); got != headBefore {
					t.Fatalf("HEAD = %s, want it left at %s", got, headBefore)
				}
				kind.assertKept(t, second.Path)
				if got := localAgentBranches(t, second.Path); got != "refs/heads/"+first.BranchName {
					t.Fatalf("agent branches = %q, want only the kept %s", got, first.BranchName)
				}
				// Keeping the checkout still refreshes what it knows of the remote.
				if got := gitRefCommit(t, second.Path, "refs/remotes/origin/"+f.defaultBranch); got != upstream {
					t.Fatalf("origin/%s = %s, want the fetched upstream %s", f.defaultBranch, got, upstream)
				}
			})
		}
	}
}

func TestRepeatedCheckoutOnTaskBranchIsNoop(t *testing.T) {
	t.Parallel()
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			f := newExistingCheckoutFixture(t, mode.isolated)
			first := f.checkout(t, firstTaskID, false)
			headBefore := gitHead(t, first.Path)
			f.advanceUpstream(t)

			again := f.checkout(t, firstTaskID, false)

			if again.Kept != KeptTaskBranch {
				t.Fatalf("Kept = %q, want %q", again.Kept, KeptTaskBranch)
			}
			if again.BranchName != first.BranchName {
				t.Fatalf("branch = %q, want this task's %q", again.BranchName, first.BranchName)
			}
			if got := gitHead(t, again.Path); got != headBefore {
				t.Fatalf("HEAD = %s, want it left at %s", got, headBefore)
			}
			// No timestamp-suffixed sibling of the task branch.
			if got := localAgentBranches(t, again.Path); got != "refs/heads/"+first.BranchName {
				t.Fatalf("agent branches = %q, want only %s", got, first.BranchName)
			}
		})
	}
}

func TestRepeatedCheckoutMovesCleanCheckoutToNewBranch(t *testing.T) {
	t.Parallel()
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			f := newExistingCheckoutFixture(t, mode.isolated)
			first := f.checkout(t, firstTaskID, false)
			// Committed and pushed work leaves nothing to lose.
			if err := os.WriteFile(filepath.Join(first.Path, "pushed.txt"), []byte("pushed\n"), 0o644); err != nil {
				t.Fatalf("write pushed file: %v", err)
			}
			runGitAuthored(t, first.Path, "add", "pushed.txt")
			runGitAuthored(t, first.Path, "commit", "-m", "pushed work")
			runGitAuthored(t, first.Path, "push", "origin", first.BranchName)
			upstream := f.advanceUpstream(t)

			second := f.checkout(t, secondTaskID, false)

			if second.Kept != "" {
				t.Fatalf("Kept = %q, want the clean checkout moved", second.Kept)
			}
			if second.BranchName != taskBranchName(secondTaskID) {
				t.Fatalf("branch = %q, want %q", second.BranchName, taskBranchName(secondTaskID))
			}
			if got := gitHead(t, second.Path); got != upstream {
				t.Fatalf("HEAD = %s, want the latest default branch %s", got, upstream)
			}
			if mode.isolated {
				// The earlier task's branch is fully pushed, so pruning it loses nothing.
				if got := localAgentBranches(t, second.Path); got != "refs/heads/"+second.BranchName {
					t.Fatalf("agent branches = %q, want only %s", got, second.BranchName)
				}
			}
		})
	}
}

func TestFreshCheckoutDiscardsLocalWork(t *testing.T) {
	t.Parallel()
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			f := newExistingCheckoutFixture(t, mode.isolated)
			first := f.checkout(t, firstTaskID, false)
			if err := os.WriteFile(filepath.Join(first.Path, "committed.txt"), []byte("committed\n"), 0o644); err != nil {
				t.Fatalf("write committed file: %v", err)
			}
			runGitAuthored(t, first.Path, "add", "committed.txt")
			runGitAuthored(t, first.Path, "commit", "-m", "unpushed work")
			unpushed := gitHead(t, first.Path)
			if err := os.WriteFile(filepath.Join(first.Path, "tracked.txt"), []byte("edited\n"), 0o644); err != nil {
				t.Fatalf("edit tracked file: %v", err)
			}
			if err := os.WriteFile(filepath.Join(first.Path, "new.txt"), []byte("new\n"), 0o644); err != nil {
				t.Fatalf("write untracked file: %v", err)
			}
			upstream := f.advanceUpstream(t)

			fresh := f.checkout(t, secondTaskID, true)

			if fresh.Kept != "" {
				t.Fatalf("Kept = %q, want --fresh to start over", fresh.Kept)
			}
			if fresh.BranchName != taskBranchName(secondTaskID) {
				t.Fatalf("branch = %q, want %q", fresh.BranchName, taskBranchName(secondTaskID))
			}
			if got := gitHead(t, fresh.Path); got != upstream {
				t.Fatalf("HEAD = %s, want the latest default branch %s", got, upstream)
			}
			if got := readFile(t, filepath.Join(fresh.Path, "tracked.txt")); got != "base\n" {
				t.Fatalf("tracked.txt = %q, want the uncommitted edit discarded", got)
			}
			for _, name := range []string{"new.txt", "committed.txt"} {
				if _, err := os.Stat(filepath.Join(fresh.Path, name)); !os.IsNotExist(err) {
					t.Fatalf("%s survived --fresh, err=%v", name, err)
				}
			}
			// Starting over never deletes the only copy of committed work: the
			// earlier branch keeps its unpushed commit in both shapes.
			if got := gitRefCommit(t, fresh.Path, "refs/heads/"+first.BranchName); got != unpushed {
				t.Fatalf("earlier branch %s = %s, want its unpushed commit %s", first.BranchName, got, unpushed)
			}
		})
	}
}

// The linked worktree left by a pre-isolation daemon is migrated by removing
// it, which deletes its working tree. That must not happen to one holding work.
func TestIsolatedCheckoutKeepsLinkedWorktreeHoldingWork(t *testing.T) {
	t.Parallel()
	f := newExistingCheckoutFixture(t, false)
	linked := f.checkout(t, firstTaskID, false)
	if err := os.WriteFile(filepath.Join(linked.Path, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	f.isolated = true
	kept := f.checkout(t, secondTaskID, false)

	if kept.Kept != KeptLocalWork || kept.UncommittedFiles != 1 {
		t.Fatalf("result = %+v, want the linked worktree kept for its untracked file", kept)
	}
	if !isGitWorktree(kept.Path) || isIsolatedCheckout(kept.Path) {
		t.Fatal("linked worktree holding work was migrated")
	}
	if got := readFile(t, filepath.Join(kept.Path, "new.txt")); got != "new\n" {
		t.Fatalf("new.txt = %q, want the untracked file", got)
	}

	migrated := f.checkout(t, secondTaskID, true)
	if migrated.Kept != "" || !isIsolatedCheckout(migrated.Path) {
		t.Fatalf("result = %+v, want --fresh to migrate to isolated metadata", migrated)
	}
}

// Fresh discards a migrated linked worktree's working tree, not its commits:
// a branch holding unpushed commits comes along into the isolated checkout.
// Left in the shared cache it would be out of the agent's reach and dropped by
// the next GC. No other cache branch comes along.
func TestFreshMigrationCarriesUnpushedBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		taskID string
	}{
		{name: "another task", taskID: secondTaskID},
		{name: "same task", taskID: firstTaskID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newExistingCheckoutFixture(t, false)
			const foreignTaskID = "33333333-3333-3333-3333-333333333333"
			if _, err := f.cache.CreateWorktree(WorktreeParams{
				WorkspaceID: "ws-1",
				RepoURL:     f.source,
				WorkDir:     t.TempDir(),
				AgentName:   "Agent",
				TaskID:      foreignTaskID,
			}); err != nil {
				t.Fatalf("foreign CreateWorktree failed: %v", err)
			}
			linked := f.checkout(t, firstTaskID, false)
			if err := os.WriteFile(filepath.Join(linked.Path, "committed.txt"), []byte("committed\n"), 0o644); err != nil {
				t.Fatalf("write committed file: %v", err)
			}
			runGitAuthored(t, linked.Path, "add", "committed.txt")
			runGitAuthored(t, linked.Path, "commit", "-m", "unpushed work")
			unpushed := gitHead(t, linked.Path)
			upstream := f.advanceUpstream(t)

			f.isolated = true
			migrated := f.checkout(t, tc.taskID, true)

			if migrated.Kept != "" || !isIsolatedCheckout(migrated.Path) {
				t.Fatalf("result = %+v, want --fresh to migrate to isolated metadata", migrated)
			}
			if got := gitHead(t, migrated.Path); got != upstream {
				t.Fatalf("HEAD = %s, want the latest default branch %s", got, upstream)
			}
			if got := gitRefCommit(t, migrated.Path, "refs/heads/"+linked.BranchName); got != unpushed {
				t.Fatalf("carried branch %s = %s, want its unpushed commit %s", linked.BranchName, got, unpushed)
			}
			if migrated.BranchName == linked.BranchName || !isTaskBranch(migrated.BranchName, taskBranchName(tc.taskID)) {
				t.Fatalf("branch = %q, want a new branch for task %s beside the carried %s", migrated.BranchName, tc.taskID, linked.BranchName)
			}
			// Sorted by refname, as for-each-ref lists them.
			heads := []string{"refs/heads/" + linked.BranchName, "refs/heads/" + migrated.BranchName}
			if heads[1] < heads[0] {
				heads[0], heads[1] = heads[1], heads[0]
			}
			if got, want := localAgentBranches(t, migrated.Path), strings.Join(heads, "\n"); got != want {
				t.Fatalf("agent branches = %q, want only %q (no other task's cache branch)", got, want)
			}
		})
	}
}

func TestIsTaskBranch(t *testing.T) {
	t.Parallel()
	const branch = "agent/agent/111111111111"
	for _, tc := range []struct {
		current string
		want    bool
	}{
		{current: branch, want: true},
		{current: branch + "-1757570000", want: true},
		{current: branch + "-", want: false},
		{current: branch + "-fix", want: false},
		{current: branch + "2", want: false},
		{current: "agent/agent/222222222222", want: false},
		{current: "", want: false},
	} {
		if got := isTaskBranch(tc.current, branch); got != tc.want {
			t.Errorf("isTaskBranch(%q) = %v, want %v", tc.current, got, tc.want)
		}
	}
}
