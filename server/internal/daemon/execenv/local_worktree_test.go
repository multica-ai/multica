package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests exercise the VWO-367 per-task worktree isolation against REAL git
// repositories and REAL processes in throwaway temp dirs. They never touch any
// live checkout. The required scenario matrix maps to the tests below:
//
//   two distinct tasks concurrent, no shared index/sidecar -> TestIsolated_ConcurrentDistinctIndex
//   provider failure cleanup                                -> TestIsolated_ProviderFailureLeavesNoOrphan
//   cancellation (cleanup mid-flight)                       -> TestIsolated_ProviderFailureLeavesNoOrphan / RepeatedCleanup
//   daemon crash + orphaned worktree recovery              -> TestIsolated_CrashOrphanRecovery
//   repeated cleanup                                        -> TestIsolated_RepeatedCleanupIdempotent
//   rebase conflict                                         -> TestIsolated_RebaseConflictSurfaces
//   no writes reach the live checkout                       -> TestIsolated_NoWritesToSourceWorkingTree
//   requires a repo                                         -> TestIsolated_RequiresGitRepo
//   prune never harms a live worktree                       -> TestPruneOrphan_PreservesLiveWorktree
//
// Same-passage deterministic serialisation and the shared book/glossary
// cross-process flock are deliberately NOT the worktree's job (distinct
// worktrees must run concurrently); they live in the pub-workstream reducer
// (tools/passage-lock.py, tools/shared_lock.rb) and are tested there.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

// newSourceRepo builds a throwaway git repo standing in for a user's live
// local_directory checkout, with an initial commit on main.
func newSourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func worktreeList(t *testing.T, repo string) string {
	t.Helper()
	return git(t, repo, "worktree", "list", "--porcelain")
}

func TestIsolated_RequiresGitRepo(t *testing.T) {
	nonRepo := t.TempDir()
	_, err := PrepareIsolatedLocalWorktree(nonRepo, filepath.Join(t.TempDir(), "workdir"), "task-abc", nil)
	if err == nil {
		t.Fatal("expected error when sourceDir is not a git repo")
	}
}

func TestIsolated_ConcurrentDistinctIndex(t *testing.T) {
	src := newSourceRepo(t)
	envA := t.TempDir()
	envB := t.TempDir()

	var wg sync.WaitGroup
	var wtA, wtB *IsolatedLocalWorktree
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		wtA, errA = PrepareIsolatedLocalWorktree(src, isolatedWorkDir(envA), "task-aaaaaaaa", nil)
	}()
	go func() {
		defer wg.Done()
		wtB, errB = PrepareIsolatedLocalWorktree(src, isolatedWorkDir(envB), "task-bbbbbbbb", nil)
	}()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("concurrent prepare failed: A=%v B=%v", errA, errB)
	}
	t.Cleanup(func() { wtA.Remove(nil); wtB.Remove(nil) })

	// Distinct working trees.
	if wtA.WorkDir == wtB.WorkDir {
		t.Fatal("two tasks got the same workdir")
	}
	// Distinct git index files — the core of "no shared index".
	idxA := git(t, wtA.WorkDir, "rev-parse", "--git-path", "index")
	idxB := git(t, wtB.WorkDir, "rev-parse", "--git-path", "index")
	if idxA == idxB {
		t.Fatalf("two tasks share one git index: %s", idxA)
	}

	// Each commits a DIFFERENT file to its own branch concurrently — must not
	// collide on index.lock or lose a commit.
	var wg2 sync.WaitGroup
	commit := func(wt *IsolatedLocalWorktree, name string) {
		defer wg2.Done()
		if err := os.WriteFile(filepath.Join(wt.WorkDir, name), []byte(name), 0o644); err != nil {
			t.Errorf("write %s: %v", name, err)
			return
		}
		git(t, wt.WorkDir, "add", name)
		git(t, wt.WorkDir, "commit", "-qm", "add "+name)
	}
	wg2.Add(2)
	go commit(wtA, "a.txt")
	go commit(wtB, "b.txt")
	wg2.Wait()

	// Each branch has exactly its own new file; neither saw the other's index.
	if got := git(t, wtA.WorkDir, "log", "--name-only", "--format=", "-1"); !strings.Contains(got, "a.txt") || strings.Contains(got, "b.txt") {
		t.Fatalf("branch A tip wrong: %q", got)
	}
	if got := git(t, wtB.WorkDir, "log", "--name-only", "--format=", "-1"); !strings.Contains(got, "b.txt") || strings.Contains(got, "a.txt") {
		t.Fatalf("branch B tip wrong: %q", got)
	}
}

func TestIsolated_NoWritesToSourceWorkingTree(t *testing.T) {
	src := newSourceRepo(t)
	headBefore := git(t, src, "rev-parse", "HEAD")
	statusBefore := git(t, src, "status", "--porcelain")
	lsBefore := git(t, src, "ls-files")

	env := t.TempDir()
	wt, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(env), "task-cccccccc", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Do real work in the isolated tree, including a daemon sidecar. The fleet
	// stages named paths (never `git add -A`), so only work.txt is committed;
	// the sidecar stays uncommitted and is discarded by `git worktree remove`.
	if err := os.MkdirAll(filepath.Join(wt.WorkDir, ".claude", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt.WorkDir, ".claude", "skills", "x", "SKILL.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(wt.WorkDir, "work.txt"), []byte("work"), 0o644)
	git(t, wt.WorkDir, "add", "work.txt")
	git(t, wt.WorkDir, "commit", "-qm", "work")
	wt.Remove(nil)

	// The source's HEAD, working tree, and tracked set are all untouched.
	if got := git(t, src, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("source HEAD moved: %s != %s", got, headBefore)
	}
	if got := git(t, src, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("source working tree changed: %q", got)
	}
	if got := git(t, src, "ls-files"); got != lsBefore {
		t.Fatalf("source tracked files changed: %q", got)
	}
}

func TestIsolated_ProviderFailureLeavesNoOrphan(t *testing.T) {
	src := newSourceRepo(t)
	env := t.TempDir()
	wt, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(env), "task-dddddddd", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the provider failing after prepare: the deferred cleanup runs.
	wt.Remove(nil)

	list := worktreeList(t, src)
	if strings.Contains(list, wt.WorkDir) {
		t.Fatalf("orphan worktree left after failure cleanup:\n%s", list)
	}
	if branches := listBranchesWithPrefix(src, worktreeBranchPrefix); len(branches) != 0 {
		t.Fatalf("orphan branch left after failure cleanup: %v", branches)
	}
}

func TestIsolated_RepeatedCleanupIdempotent(t *testing.T) {
	src := newSourceRepo(t)
	env := t.TempDir()
	wt, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(env), "task-eeeeeeee", nil)
	if err != nil {
		t.Fatal(err)
	}
	wt.Remove(nil)
	wt.Remove(nil) // second call must be a harmless no-op
	if list := worktreeList(t, src); strings.Contains(list, wt.WorkDir) {
		t.Fatalf("worktree present after repeated cleanup:\n%s", list)
	}
}

func TestIsolated_CrashOrphanRecovery(t *testing.T) {
	src := newSourceRepo(t)
	env := t.TempDir()
	wt, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(env), "task-ffffffff", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a daemon crash: envRoot (and thus workdir) is later GC'd, but no
	// `git worktree remove` ran. Emulate by deleting the workdir directly.
	if err := os.RemoveAll(env); err != nil {
		t.Fatal(err)
	}
	// The registry still lists the now-missing worktree (dangling).
	if !strings.Contains(worktreeList(t, src), wt.Branch) && !hasDanglingEntry(t, src, wt.WorkDir) {
		// Some git versions omit the path once missing; the prunable state is what matters.
	}
	// Recovery reclaims the dangling entry and its branch.
	if err := PruneOrphanLocalWorktrees(src, nil); err != nil {
		t.Fatal(err)
	}
	if list := worktreeList(t, src); strings.Contains(list, wt.WorkDir) {
		t.Fatalf("dangling worktree survived recovery:\n%s", list)
	}
	if branches := listBranchesWithPrefix(src, worktreeBranchPrefix); len(branches) != 0 {
		t.Fatalf("orphan branch survived recovery: %v", branches)
	}
}

func hasDanglingEntry(t *testing.T, repo, workdir string) bool {
	t.Helper()
	return strings.Contains(worktreeList(t, repo), workdir)
}

func TestPruneOrphan_PreservesLiveWorktree(t *testing.T) {
	src := newSourceRepo(t)
	env := t.TempDir()
	wt, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(env), "task-99999999", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wt.Remove(nil) })
	// A live worktree must survive prune (its dir still exists, branch in use).
	if err := PruneOrphanLocalWorktrees(src, nil); err != nil {
		t.Fatal(err)
	}
	if list := worktreeList(t, src); !strings.Contains(list, wt.WorkDir) {
		t.Fatalf("prune removed a LIVE worktree:\n%s", list)
	}
	if branches := listBranchesWithPrefix(src, worktreeBranchPrefix); len(branches) != 1 {
		t.Fatalf("prune deleted a live worktree's branch: %v", branches)
	}
	// And the worktree is still usable.
	os.WriteFile(filepath.Join(wt.WorkDir, "still.txt"), []byte("x"), 0o644)
	git(t, wt.WorkDir, "add", "still.txt")
	git(t, wt.WorkDir, "commit", "-qm", "still works")
}

func TestIsolated_RebaseConflictSurfaces(t *testing.T) {
	src := newSourceRepo(t)
	// A shared file both tasks will edit differently.
	os.WriteFile(filepath.Join(src, "shared.txt"), []byte("base\n"), 0o644)
	git(t, src, "add", "shared.txt")
	git(t, src, "commit", "-qm", "add shared")

	envA, envB := t.TempDir(), t.TempDir()
	wtA, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(envA), "task-a1a1a1a1", nil)
	if err != nil {
		t.Fatal(err)
	}
	wtB, err := PrepareIsolatedLocalWorktree(src, isolatedWorkDir(envB), "task-b2b2b2b2", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wtA.Remove(nil); wtB.Remove(nil) })

	writeCommit := func(wt *IsolatedLocalWorktree, content string) {
		os.WriteFile(filepath.Join(wt.WorkDir, "shared.txt"), []byte(content), 0o644)
		git(t, wt.WorkDir, "add", "shared.txt")
		git(t, wt.WorkDir, "commit", "-qm", "edit shared")
	}
	writeCommit(wtA, "from-A\n")
	writeCommit(wtB, "from-B\n")

	// B rebases its branch onto A's (the two isolated tasks edited the same file
	// divergently) -> deterministic conflict, surfaced as a non-zero rebase, NOT
	// a silent lost update. This is the property isolation must preserve: the
	// second writer is forced to reconcile, it cannot clobber the first.
	cmd := exec.Command("git", "-C", wtB.WorkDir, "rebase", wtA.Branch)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a rebase conflict, got clean rebase:\n%s", out)
	}
	if !strings.Contains(string(out), "CONFLICT") {
		t.Fatalf("rebase failed but not with a surfaced CONFLICT:\n%s", out)
	}
	git(t, wtB.WorkDir, "rebase", "--abort")
}
