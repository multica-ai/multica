package execenv

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

func worktreeTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestRepo creates a git repo with one commit and returns its path. The
// repo carries its own identity config so tests don't depend on the machine's
// global git setup.
func newTestRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	// macOS hands out /var/folders temp dirs that are symlinks to /private/var.
	// resolveGitRoot canonicalises, so the test must compare against the
	// canonical form too.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.name", "Test User")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	writeFile(t, filepath.Join(dir, "tracked.txt"), "original\n")
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	return dir
}

// requireGit skips the whole test when git is missing from the environment.
// This is the ONLY place a skip is allowed: once a test is past it, every git
// command is part of the assertion surface, so a failure there is a real
// regression and must fail loudly. An earlier version skipped on any git error,
// which would have turned "the delivered branch is gone" into a green run.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// testBranchOwner is the conversation every multi-turn test in this package
// speaks for. Tests that need a DIFFERENT conversation vary one field.
var testBranchOwner = branchOwner{
	WorkspaceID:    "11112222-3333-4444-5555-000000000001",
	AgentID:        "11112222-3333-4444-5555-000000000002",
	ConversationID: "11112222-3333-4444-5555-000000000003",
}

func prepareForTest(t *testing.T, localPath string) *LocalWorktree {
	t.Helper()
	wt, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: localPath,
		EnvRoot:   t.TempDir(),
		AgentName: "J",
		TaskID:    "11112222-3333-4444-5555-666677778888",
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("PrepareLocalWorktree: %v", err)
	}
	return wt
}

// The agent must see the user's uncommitted work, not a clean HEAD checkout.
// This is the property that makes worktree mode usable rather than confusing:
// otherwise the agent silently reviews code the user hasn't got open.
func TestPrepareLocalWorktreeReplaysUncommittedWork(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by user\n")
	writeFile(t, filepath.Join(repo, "brand-new.txt"), "untracked\n")
	writeFile(t, filepath.Join(repo, "nested/deep.txt"), "nested untracked\n")

	wt := prepareForTest(t, repo)

	if got := readFile(t, filepath.Join(wt.Path, "tracked.txt")); got != "edited by user\n" {
		t.Errorf("tracked edit not replayed into worktree: got %q", got)
	}
	if got := readFile(t, filepath.Join(wt.Path, "brand-new.txt")); got != "untracked\n" {
		t.Errorf("untracked file not copied: got %q", got)
	}
	if got := readFile(t, filepath.Join(wt.Path, "nested/deep.txt")); got != "nested untracked\n" {
		t.Errorf("nested untracked file not copied: got %q", got)
	}
	if !wt.DirtyBaseCaptured {
		t.Error("DirtyBaseCaptured = false, want true")
	}
}

// Capturing the dirty state must not disturb the user's own working tree —
// they may have a build running against it. `git stash create` writes a commit
// object but must leave the index, the files, and the stash list alone.
func TestPrepareLocalWorktreeLeavesUserTreeUntouched(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by user\n")
	writeFile(t, filepath.Join(repo, "brand-new.txt"), "untracked\n")
	statusBefore := gitRun(t, repo, "status", "--porcelain")

	prepareForTest(t, repo)

	if got := readFile(t, filepath.Join(repo, "tracked.txt")); got != "edited by user\n" {
		t.Errorf("user's file changed: got %q", got)
	}
	if got := gitRun(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Errorf("user's git status changed:\nbefore %q\nafter  %q", statusBefore, got)
	}
	if got := gitRun(t, repo, "stash", "list"); got != "" {
		t.Errorf("stash list should stay empty, got %q", got)
	}
}

// The deliverable is a branch. Whatever the agent leaves uncommitted must be
// committed onto it before the worktree is removed, or `git worktree remove
// --force` would delete the work with no way back.
func TestFinalizeCommitsLeftoversAndKeepsBranch(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)

	writeFile(t, filepath.Join(wt.Path, "agent-output.txt"), "work product\n")

	outcome := finalizeOK(t, wt)

	if !outcome.AutoCommitted {
		t.Error("AutoCommitted = false, want true")
	}
	if outcome.Branch != wt.Branch {
		t.Errorf("Branch = %q, want %q", outcome.Branch, wt.Branch)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree directory still present after Finalize: %v", err)
	}
	if list := gitRun(t, repo, "worktree", "list"); strings.Contains(list, wt.Path) {
		t.Errorf("worktree still registered in user's repo:\n%s", list)
	}
	// The branch must survive in the user's repo, carrying the agent's file.
	if got := gitRun(t, repo, "show", wt.Branch+":agent-output.txt"); got != "work product" {
		t.Errorf("branch does not carry agent output, got %q", got)
	}
	// The user's own checkout must be untouched by any of it.
	if _, err := os.Stat(filepath.Join(repo, "agent-output.txt")); !os.IsNotExist(err) {
		t.Error("agent output leaked into the user's working tree")
	}
}

// A read-only task changes nothing. Leaving an empty branch behind for every
// such run would turn `git branch` into noise, so the branch is dropped and the
// result reports no branch at all.
func TestFinalizeDropsBranchWhenNothingChanged(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)

	outcome := finalizeOK(t, wt)

	if outcome.Branch != "" {
		t.Errorf("Branch = %q, want empty for a no-op task", outcome.Branch)
	}
	if outcome.AutoCommitted {
		t.Error("AutoCommitted = true, want false")
	}
	if out, err := gitTry(t, repo, "rev-parse", "--verify", wt.Branch); err == nil {
		t.Errorf("empty branch should have been deleted, still resolves to %s", out)
	}
}

// The user's own uncommitted work must not be mistaken for the agent's output.
// It is committed as a baseline at prepare time, so a task that changes nothing
// still counts as a no-op and leaves no branch — the user's WIP is already safe
// in their own working tree, and a branch duplicating it is pure noise.
func TestFinalizeDropsBranchWhenOnlyBaseWasDirty(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by user\n")
	writeFile(t, filepath.Join(repo, "scratch.txt"), "untracked scratch\n")
	wt := prepareForTest(t, repo)

	// The baseline must be a commit of its own, not left as pending changes.
	if wt.BaseCommit == "" {
		t.Fatal("no baseline commit recorded for a dirty base")
	}
	if dirty, err := worktreeIsDirty(wt.Path); err != nil || dirty {
		t.Errorf("worktree still dirty after baseline commit (dirty=%v, err=%v)", dirty, err)
	}

	outcome := finalizeOK(t, wt)

	if outcome.Branch != "" {
		t.Errorf("Branch = %q, want empty: the agent changed nothing", outcome.Branch)
	}
	if outcome.AutoCommitted {
		t.Error("AutoCommitted = true, but there was nothing of the agent's to commit")
	}
}

// With a dirty base, the delivered branch separates the two authorships: the
// user's WIP is the baseline commit, the agent's work sits on top. That is what
// makes `git diff <baseline>..<branch>` a readable review of the agent alone.
func TestFinalizeSeparatesUserBaselineFromAgentWork(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by user\n")
	wt := prepareForTest(t, repo)

	writeFile(t, filepath.Join(wt.Path, "agent-output.txt"), "work product\n")
	outcome := finalizeOK(t, wt)

	if outcome.Branch == "" {
		t.Fatal("no branch delivered for a task that changed a file")
	}
	// The agent's commit alone.
	agentFiles := gitRun(t, repo, "diff", "--name-only", wt.BaseCommit, outcome.Branch)
	if agentFiles != "agent-output.txt" {
		t.Errorf("agent diff = %q, want just agent-output.txt", agentFiles)
	}
	// And the user's uncommitted edit still reached the branch.
	if got := gitRun(t, repo, "show", outcome.Branch+":tracked.txt"); got != "edited by user" {
		t.Errorf("branch lost the user's uncommitted edit, got %q", got)
	}
}

// A resource may point at a subdirectory of a repo. The worktree covers the
// whole repo, but the agent has to land at the same depth the user chose.
func TestPrepareLocalWorktreeSubdirectory(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "services/api/main.go"), "package main\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "add service")

	sub := filepath.Join(repo, "services/api")
	wt := prepareForTest(t, sub)

	if wt.GitRoot != repo {
		t.Errorf("GitRoot = %q, want repo root %q", wt.GitRoot, repo)
	}
	want := filepath.Join(wt.Path, "services", "api")
	if wt.WorkDir != want {
		t.Errorf("WorkDir = %q, want %q", wt.WorkDir, want)
	}
	if got := readFile(t, filepath.Join(wt.WorkDir, "main.go")); got != "package main\n" {
		t.Errorf("subdirectory content missing from worktree: %q", got)
	}
}

// Worktree mode is opt-in per resource, so a non-git directory means the user
// configured something that cannot work. Fail with an actionable message rather
// than silently running in-place, which would leave them wondering why their
// tasks still queue one at a time.
func TestPrepareLocalWorktreeRejectsNonGitDirectory(t *testing.T) {
	_, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: t.TempDir(),
		EnvRoot:   t.TempDir(),
		TaskID:    "task-1",
	}, worktreeTestLogger())
	if err == nil {
		t.Fatal("expected an error for a non-git directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") || !strings.Contains(err.Error(), "in_place") {
		t.Errorf("error should name the problem and the fix, got: %v", err)
	}
}

// A repo with no commits has nothing to branch from. The message has to say so,
// because "git worktree add failed" alone sends the user hunting in the wrong
// place.
func TestPrepareLocalWorktreeRejectsRepoWithoutCommits(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")

	_, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: dir,
		EnvRoot:   t.TempDir(),
		TaskID:    "task-1",
	}, worktreeTestLogger())
	if err == nil {
		t.Fatal("expected an error for a repo with no commits")
	}
	if !strings.Contains(err.Error(), "no commit") {
		t.Errorf("error should explain the missing commit, got: %v", err)
	}
}

// Concurrency is the entire point of the mode: two tasks on one directory must
// both get a working checkout, with distinct branches, without corrupting git's
// admin files.
func TestPrepareLocalWorktreeConcurrentTasks(t *testing.T) {
	repo := newTestRepo(t)

	const tasks = 4
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []*LocalWorktree
		errs    []error
	)
	for i := range tasks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wt, err := PrepareLocalWorktree(LocalWorktreeParams{
				LocalPath: repo,
				EnvRoot:   t.TempDir(),
				AgentName: "J",
				TaskID:    strings.Repeat(string(rune('a'+i)), 8),
			}, worktreeTestLogger())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results = append(results, wt)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		t.Errorf("concurrent prepare failed: %v", err)
	}
	if len(results) != tasks {
		t.Fatalf("got %d worktrees, want %d", len(results), tasks)
	}
	branches := map[string]bool{}
	for _, wt := range results {
		if branches[wt.Branch] {
			t.Errorf("duplicate branch %q across concurrent tasks", wt.Branch)
		}
		branches[wt.Branch] = true
		if got := readFile(t, filepath.Join(wt.Path, "tracked.txt")); got != "original\n" {
			t.Errorf("worktree %s has wrong content: %q", wt.Path, got)
		}
	}
	for _, wt := range results {
		finalizeOK(t, wt)
	}
	if list := gitRun(t, repo, "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("worktrees left registered after finalize:\n%s", list)
	}
}

// End-to-end shape of a worktree-mode task: Prepare builds the env and writes
// its sidecars into the worktree, the daemon's cleanup pass removes them, and
// Finalize delivers a branch carrying only the agent's real work. The
// sidecar-free branch is the user-visible contract — a diff full of
// .agent_context/ scaffolding would make the mode unusable for review.
func TestWorktreeModeDeliversBranchWithoutSidecars(t *testing.T) {
	repo := newTestRepo(t)
	// Start dirty, so the branch gets a baseline commit as well as the agent's
	// own — a sidecar could otherwise hide in either one.
	writeFile(t, filepath.Join(repo, "tracked.txt"), "edited by user\n")
	originalHead := gitRun(t, repo, "rev-parse", "HEAD")
	envRoot := filepath.Join(t.TempDir(), "env")

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: filepath.Dir(envRoot),
		WorkspaceID:    "ws-1",
		TaskID:         "11112222-3333-4444-5555-666677778888",
		AgentName:      "J",
		Provider:       "claude",
		LocalWorktree:  &LocalWorktreeParams{LocalPath: repo},
		Task:           TaskContextForEnv{IssueID: "issue-1", AgentName: "J"},
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.LocalWorktree == nil {
		t.Fatal("Prepare did not build a worktree")
	}
	// The env root is ordinary daemon-owned scratch in this mode, so the GC
	// exemption meant for a user's own directory must not apply.
	if env.LocalDirectory {
		t.Error("LocalDirectory = true in worktree mode; the GC would exempt this env root forever")
	}
	if env.WorkDir != env.LocalWorktree.Path {
		t.Errorf("WorkDir = %q, want the worktree root %q", env.WorkDir, env.LocalWorktree.Path)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, ".agent_context")); err != nil {
		t.Fatalf("precondition: Prepare should have written sidecars into the worktree: %v", err)
	}

	// The agent's actual work.
	writeFile(t, filepath.Join(env.WorkDir, "real-change.txt"), "actual work\n")

	// What the daemon runs before Finalize.
	if err := CleanupRuntimeConfig(env.WorkDir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	outcome := finalizeOK(t, env.LocalWorktree)

	if outcome.Branch == "" {
		t.Fatal("no branch delivered for a task that changed a file")
	}
	// Every file the branch touches, across the baseline and agent commits.
	files := gitRun(t, repo, "diff", "--name-only", originalHead, outcome.Branch)
	if !strings.Contains(files, "real-change.txt") {
		t.Errorf("branch is missing the agent's work:\n%s", files)
	}
	for _, sidecar := range []string{".agent_context", ".multica", "CLAUDE.md"} {
		if strings.Contains(files, sidecar) {
			t.Errorf("sidecar %q leaked into the delivered branch:\n%s", sidecar, files)
		}
	}
	// The user's own checkout keeps exactly the edit it started with, and
	// nothing the agent or the runtime produced.
	if got := gitRun(t, repo, "status", "--porcelain"); got != "M tracked.txt" {
		t.Errorf("user's working tree changed, want only their own edit:\n%s", got)
	}
}

// A crashed daemon leaves a registration pointing at an env root that GC later
// deletes. The next task on the same repo must clean that up rather than
// accumulating dead entries in the user's `git worktree list` forever.
func TestPrepareLocalWorktreePrunesStaleRegistrations(t *testing.T) {
	repo := newTestRepo(t)
	orphanEnv := t.TempDir()
	orphan, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: repo,
		EnvRoot:   orphanEnv,
		AgentName: "J",
		TaskID:    "dead-task",
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("seed orphan worktree: %v", err)
	}
	// Simulate the crash: the directory disappears with GC, the registration
	// survives in the user's repo.
	if err := os.RemoveAll(orphan.Path); err != nil {
		t.Fatalf("remove orphan worktree dir: %v", err)
	}
	if list := gitRun(t, repo, "worktree", "list"); !strings.Contains(list, orphan.Path) {
		t.Fatalf("precondition failed: orphan not registered:\n%s", list)
	}

	wt := prepareForTest(t, repo)
	t.Cleanup(func() { finalizeOK(t, wt) })

	if list := gitRun(t, repo, "worktree", "list"); strings.Contains(list, orphan.Path) {
		t.Errorf("stale registration not pruned:\n%s", list)
	}
}

// finalizeOK finalizes a worktree and fails the test if the work could not be
// persisted. Tests that exercise the failure path call Finalize directly.
func finalizeOK(t *testing.T, wt *LocalWorktree) LocalWorktreeOutcome {
	t.Helper()
	outcome, err := wt.Finalize(worktreeTestLogger())
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return outcome
}

// The one operation in this flow that can destroy work is `git worktree remove
// --force`. If the agent's changes could not be committed, removing the
// worktree would delete the only copy — so Finalize must keep it and say so.
// commit.gpgSign with no usable key is the realistic trigger: --no-verify does
// not disable signing.
func TestFinalizeKeepsWorktreeWhenCommitFails(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)

	writeFile(t, filepath.Join(wt.Path, "agent-output.txt"), "irreplaceable work\n")

	// Force every commit in this worktree to fail the way a signing setup the
	// daemon can't satisfy would.
	gitRun(t, wt.Path, "config", "commit.gpgSign", "true")
	gitRun(t, wt.Path, "config", "gpg.program", filepath.Join(repo, "definitely-not-a-real-gpg"))

	outcome, err := wt.Finalize(worktreeTestLogger())

	if err == nil {
		t.Fatal("Finalize returned nil error after the commit failed")
	}
	if outcome.PreservedPath != wt.Path {
		t.Errorf("PreservedPath = %q, want %q", outcome.PreservedPath, wt.Path)
	}
	// The whole point: the work is still on disk.
	if got := readFile(t, filepath.Join(wt.Path, "agent-output.txt")); got != "irreplaceable work\n" {
		t.Errorf("agent work was destroyed despite the commit failure, got %q", got)
	}
	// And it stays discoverable through git rather than only a log line.
	if list := gitRun(t, repo, "worktree", "list"); !strings.Contains(list, wt.Path) {
		t.Errorf("preserved worktree is no longer registered, so the user cannot find it:\n%s", list)
	}
	if !strings.Contains(err.Error(), wt.Path) {
		t.Errorf("error should name the preserved path, got: %v", err)
	}
}

// An in_place task on the same directory leaves .agent_context/ and .multica/
// in the user's tree while it runs. A concurrent worktree snapshot sees them as
// untracked files; copying them would hand this task another issue's brief and
// commit it to the branch.
func TestPrepareLocalWorktreeSkipsMulticaSidecars(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".agent_context", "issue_context.md"), "OTHER issue's brief\n")
	writeFile(t, filepath.Join(repo, ".multica", "project", "resources.json"), "{}\n")
	// An in_place resource may point at a SUBDIRECTORY of this repo, in which
	// case its sidecars sit below that subdirectory, not at the repo root.
	writeFile(t, filepath.Join(repo, "services", "api", ".agent_context", "issue_context.md"), "yet another issue's brief\n")
	writeFile(t, filepath.Join(repo, "services", "api", ".multica", "task.json"), "{}\n")
	writeFile(t, filepath.Join(repo, "real-untracked.txt"), "user's own file\n")
	writeFile(t, filepath.Join(repo, "services", "api", "notes.txt"), "user's nested file\n")

	wt := prepareForTest(t, repo)
	t.Cleanup(func() { finalizeOK(t, wt) })

	if _, err := os.Stat(filepath.Join(wt.Path, ".agent_context")); !os.IsNotExist(err) {
		t.Error("another task's .agent_context was copied into this worktree")
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".multica")); !os.IsNotExist(err) {
		t.Error("another task's .multica was copied into this worktree")
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "services", "api", ".agent_context")); !os.IsNotExist(err) {
		t.Error("a subdirectory task's .agent_context was copied into this worktree")
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "services", "api", ".multica")); !os.IsNotExist(err) {
		t.Error("a subdirectory task's .multica was copied into this worktree")
	}
	if got := readFile(t, filepath.Join(wt.Path, "real-untracked.txt")); got != "user's own file\n" {
		t.Errorf("the user's own untracked file was not replayed: %q", got)
	}
	if got := readFile(t, filepath.Join(wt.Path, "services", "api", "notes.txt")); got != "user's nested file\n" {
		t.Errorf("the user's nested untracked file was not replayed: %q", got)
	}
}

// Silently starting from HEAD when the user's uncommitted state cannot be
// replayed would have the agent review code the user never wrote and report on
// it confidently. Refusing to start is the recoverable outcome.
func TestPrepareLocalWorktreeFailsWhenUntrackedReplayIsTruncated(t *testing.T) {
	repo := newTestRepo(t)
	// One file over the copy budget is enough to prove the bound fails closed
	// rather than under-copying; writing 2001 files would only be slower.
	big := strings.Repeat("x", 1024)
	for i := range maxUntrackedFiles + 1 {
		writeFile(t, filepath.Join(repo, "untracked", "f"+string(rune('a'+i%26))+strings.Repeat("z", i/26)+".txt"), big)
	}

	_, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: repo,
		EnvRoot:   t.TempDir(),
		AgentName: "J",
		TaskID:    "task-truncated",
	}, worktreeTestLogger())

	if err == nil {
		t.Fatal("expected prepare to fail rather than replay a truncated tree")
	}
	if !strings.Contains(err.Error(), "in_place") {
		t.Errorf("error should offer a way out, got: %v", err)
	}
	// A failed prepare must not leave a half-built worktree registered.
	if list := gitRun(t, repo, "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("aborted prepare left a worktree registered:\n%s", list)
	}
	if branches := gitRun(t, repo, "branch", "--list", "agent/*"); branches != "" {
		t.Errorf("aborted prepare left a branch behind: %q", branches)
	}
}

// Worktree tasks get a fresh env root per task ID, exactly like in_place ones
// (shouldReusePriorWorkdir refuses any local assignment). So they need the same
// per-issue Codex session store: without it, each comment turn on an issue
// starts a new empty CODEX_HOME, the prior rollout is invisible, and the agent
// silently loses the conversation it was having with the user.
func TestPrepareWorktreeModeUsesPerIssueCodexSessionStore(t *testing.T) {
	repo := newTestRepo(t)
	workspacesRoot := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	prepareTurn := func(taskID string) *Environment {
		t.Helper()
		env, err := Prepare(PrepareParams{
			WorkspacesRoot: workspacesRoot,
			WorkspaceID:    "ws-1",
			TaskID:         taskID,
			AgentName:      "J",
			Provider:       "codex",
			LocalWorktree:  &LocalWorktreeParams{LocalPath: repo},
			Task:           TaskContextForEnv{IssueID: "issue-1", AgentID: "agent-1", AgentName: "J"},
		}, worktreeTestLogger())
		if err != nil {
			t.Fatalf("Prepare(%s): %v", taskID, err)
		}
		t.Cleanup(func() { finalizeOK(t, env.LocalWorktree) })
		return env
	}

	// Two turns on the same issue, different task IDs — the shape of a
	// follow-up comment.
	first := prepareTurn("aaaa1111-2222-3333-4444-5555666677aa")
	second := prepareTurn("bbbb1111-2222-3333-4444-5555666677bb")

	sessionsOf := func(env *Environment) string {
		t.Helper()
		target, err := filepath.EvalSymlinks(filepath.Join(env.CodexHome, "sessions"))
		if err != nil {
			t.Fatalf("resolve sessions dir: %v", err)
		}
		return target
	}

	firstSessions := sessionsOf(first)
	secondSessions := sessionsOf(second)
	if firstSessions != secondSessions {
		t.Errorf("each turn got its own sessions dir, so Codex cannot resume:\n first  %s\n second %s",
			firstSessions, secondSessions)
	}
	// And it must be the stable per-issue store, not a task-local directory.
	if strings.Contains(firstSessions, taskKey("aaaa1111-2222-3333-4444-5555666677aa")) {
		t.Errorf("sessions dir is task-scoped (%s); it will not survive the next turn", firstSessions)
	}
}

// The daemon runs its sidecar cleanup before Finalize commits. If that cleanup
// fails, committing anyway would deliver a branch whose content is Multica's
// own runtime files — the exact leak this mode promises to prevent. The abort
// must therefore stop the commit AND keep the worktree, since the agent's work
// is still in it.
func TestFinalizeAbortRefusesToCommitAndKeepsWorktree(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)

	writeFile(t, filepath.Join(wt.Path, "agent-output.txt"), "real work\n")
	writeFile(t, filepath.Join(wt.Path, ".agent_context", "issue_context.md"), "runtime brief\n")

	wt.AbortWithReason(errors.New("cleanup failed"))
	outcome, err := wt.Finalize(worktreeTestLogger())

	if err == nil {
		t.Fatal("Finalize returned nil error after an abort")
	}
	if outcome.Branch != "" {
		t.Errorf("Branch = %q, want empty: nothing may be delivered after an abort", outcome.Branch)
	}
	if outcome.AutoCommitted {
		t.Error("AutoCommitted = true; the abort must prevent the commit")
	}
	if outcome.PreservedPath != wt.Path {
		t.Errorf("PreservedPath = %q, want %q", outcome.PreservedPath, wt.Path)
	}
	// Nothing committed: the branch must still be sitting on its base.
	tip := gitRun(t, repo, "rev-parse", wt.Branch)
	if tip != wt.BaseCommit {
		t.Errorf("branch moved to %s; the sidecar-carrying commit was delivered anyway", tip)
	}
	// And the work is still recoverable on disk.
	if got := readFile(t, filepath.Join(wt.Path, "agent-output.txt")); got != "real work\n" {
		t.Errorf("agent work was destroyed: %q", got)
	}
	if list := gitRun(t, repo, "worktree", "list"); !strings.Contains(list, wt.Path) {
		t.Errorf("preserved worktree is no longer registered:\n%s", list)
	}
}

// The first reason is the one closest to the root cause, so later aborts must
// not overwrite it.
func TestAbortWithReasonKeepsFirstReason(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	t.Cleanup(func() { removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger()) })

	wt.AbortWithReason(errors.New("first"))
	wt.AbortWithReason(errors.New("second"))
	wt.AbortWithReason(nil)

	_, err := wt.Finalize(worktreeTestLogger())
	if err == nil || !strings.Contains(err.Error(), "first") {
		t.Fatalf("want the first reason preserved, got: %v", err)
	}
	if strings.Contains(err.Error(), "second") {
		t.Errorf("later abort overwrote the original reason: %v", err)
	}
}

// An untracked symlink is content the user can see. Replaying it faithfully is
// ambiguous (link vs target, targets outside the repo), so the snapshot must
// fail rather than hand the agent a tree with a file quietly missing.
func TestPrepareLocalWorktreeFailsOnUntrackedSymlink(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.Symlink(filepath.Join(repo, "tracked.txt"), filepath.Join(repo, "shortcut.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: repo,
		EnvRoot:   t.TempDir(),
		AgentName: "J",
		TaskID:    "task-symlink",
	}, worktreeTestLogger())

	if err == nil {
		t.Fatal("expected prepare to fail rather than silently drop the symlink")
	}
	if !strings.Contains(err.Error(), "untracked") {
		t.Errorf("error should name the untracked replay, got: %v", err)
	}
	if list := gitRun(t, repo, "worktree", "list"); strings.Count(list, "\n") != 0 {
		t.Errorf("aborted prepare left a worktree registered:\n%s", list)
	}
}

// Discard is the abandon path for a Prepare that fails after the worktree
// exists. Without it every such failure leaves a registration in the user's
// repo and a branch no task ever ran in.
func TestDiscardRemovesWorktreeAndBranch(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)

	wt.Discard(worktreeTestLogger())

	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree directory survived Discard: %v", err)
	}
	if list := gitRun(t, repo, "worktree", "list"); strings.Contains(list, wt.Path) {
		t.Errorf("worktree still registered after Discard:\n%s", list)
	}
	if out, err := gitTry(t, repo, "rev-parse", "--verify", wt.Branch); err == nil {
		t.Errorf("branch survived Discard, resolves to %s", out)
	}
}

// prepareTurn runs one turn of a conversation: a fresh task id and env root on
// the same conversation key, which is what a follow-up comment on an issue
// produces.
func prepareTurn(t *testing.T, localPath, conversationKey, taskID string) *LocalWorktree {
	t.Helper()
	wt, err := prepareTurnAs(localPath, conversationKey, taskID, testBranchOwner)
	if err != nil {
		t.Fatalf("PrepareLocalWorktree(%s): %v", taskID, err)
	}
	return wt
}

func prepareTurnAs(localPath, conversationKey, taskID string, owner branchOwner) (*LocalWorktree, error) {
	return PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath:       localPath,
		EnvRoot:         mustTempDir(),
		AgentName:       "J",
		TaskID:          taskID,
		ConversationKey: conversationKey,
		WorkspaceID:     owner.WorkspaceID,
		AgentID:         owner.AgentID,
		ConversationID:  owner.ConversationID,
	}, worktreeTestLogger())
}

// mustTempDir gives a prepare its own env root outside t.TempDir, for the
// helpers that do not carry a *testing.T. Cleaned up with the test process.
func mustTempDir() string {
	dir, err := os.MkdirTemp("", "multica-worktree-env")
	if err != nil {
		panic(err)
	}
	return dir
}

const (
	turnOneTask   = "11112222-3333-4444-5555-aaaaaaaaaaaa"
	turnTwoTask   = "11112222-3333-4444-5555-bbbbbbbbbbbb"
	turnThreeTask = "11112222-3333-4444-5555-cccccccccccc"
)

// The bug this fixes: every comment on one issue produced a new branch forked
// from HEAD, so the second turn stood in a tree that did not contain the first
// turn's work and nothing said so (MUL-6881).
func TestPrepareLocalWorktreeContinuesTheConversationBranch(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	if first.Branch != "agent/j/mul-6881" {
		t.Fatalf("first turn branch = %q, want agent/j/mul-6881", first.Branch)
	}
	if first.Continued {
		t.Error("first turn reports Continued = true; there was nothing to continue")
	}
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "work from turn one\n")
	firstOutcome := finalizeOK(t, first)
	if firstOutcome.Branch != "agent/j/mul-6881" {
		t.Fatalf("first outcome branch = %q", firstOutcome.Branch)
	}
	firstTip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Branch != "agent/j/mul-6881" {
		t.Fatalf("second turn branch = %q, want the conversation's branch", second.Branch)
	}
	if !second.Continued {
		t.Error("second turn reports Continued = false, want true")
	}
	if second.BaseCommit != firstTip {
		t.Errorf("second turn base = %s, want the first turn's tip %s", second.BaseCommit, firstTip)
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "turn-one.txt")); got != "work from turn one\n" {
		t.Errorf("turn one's work is missing from turn two's tree: %q", got)
	}

	writeFile(t, filepath.Join(second.WorkDir, "turn-two.txt"), "work from turn two\n")
	finalizeOK(t, second)

	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	for _, name := range []string{"turn-one.txt", "turn-two.txt"} {
		if _, err := os.Stat(filepath.Join(third.WorkDir, name)); err != nil {
			t.Errorf("turn three is missing %s: %v", name, err)
		}
	}
	finalizeOK(t, third)

	// One branch for the whole conversation, not one per comment.
	branches := gitRun(t, repo, "branch", "--list", "agent/j/*")
	if strings.Count(branches, "agent/j/") != 1 {
		t.Errorf("conversation produced more than one branch:\n%s", branches)
	}
}

// The user's uncommitted work reaches the branch once, as turn one's baseline.
// Replaying their whole tree again on every turn would ask git to merge their
// copy of a file against the agent's edits to it — a conflict raised precisely
// when the agent did what it was asked to do — so only what they changed since
// is replayed.
func TestPrepareLocalWorktreeReplaysOnlyTheUserEditsSinceTheLastTurn(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	if got := readFile(t, filepath.Join(first.WorkDir, "tracked.txt")); got != "user work in progress\n" {
		t.Fatalf("turn one did not see the user's uncommitted work: %q", got)
	}
	// The agent edits the very line the user was working on.
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "user work in progress, finished by the agent\n")
	finalizeOK(t, first)

	// Between the turns the user edits a different file and leaves the first
	// one exactly as it was.
	writeFile(t, filepath.Join(repo, "keep.txt"), "user edited this between turns\n")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if got := readFile(t, filepath.Join(second.WorkDir, "tracked.txt")); got != "user work in progress, finished by the agent\n" {
		t.Errorf("the agent's edit was reverted by replaying the user's older copy: %q", got)
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "keep.txt")); got != "user edited this between turns\n" {
		t.Errorf("the user's edit between turns did not reach the worktree: %q", got)
	}
	if strings.Contains(readFile(t, filepath.Join(second.WorkDir, "tracked.txt")), "<<<<<<<") {
		t.Error("the worktree was handed to the agent with conflict markers in it")
	}
	// The user's own directory is never written to, on any turn.
	if got := readFile(t, filepath.Join(repo, "tracked.txt")); got != "user work in progress\n" {
		t.Errorf("the user's directory was modified: %q", got)
	}
}

// A real conflict — the user rewrites lines the agent also rewrote — belongs to
// the agent, not to the daemon. The turn starts on the conflicted tree with
// both versions in it, because the only alternative that does not lose the
// user's newer edit is having something read both sides and decide.
func TestPrepareLocalWorktreeHandsConflictingUserEditsToTheAgent(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	finalizeOK(t, first)

	// The user rewrites the same line their own way.
	writeFile(t, filepath.Join(repo, "tracked.txt"), "rewritten by the user instead\n")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if len(second.ReplayConflicts) != 1 || second.ReplayConflicts[0] != "tracked.txt" {
		t.Fatalf("ReplayConflicts = %v, want [tracked.txt]", second.ReplayConflicts)
	}
	got := readFile(t, filepath.Join(second.WorkDir, "tracked.txt"))
	for _, want := range []string{"rewritten by the agent", "rewritten by the user instead", "<<<<<<<"} {
		if !strings.Contains(got, want) {
			t.Errorf("conflicted file does not contain %q:\n%s", want, got)
		}
	}
	if unmerged, err := unmergedPaths(second.Path); err != nil || len(unmerged) != 1 {
		t.Errorf("unmergedPaths = %v, %v; want one unresolved path", unmerged, err)
	}
	// A cherry-pick the agent never started must not be left for it to conclude.
	gitDir := gitRun(t, second.Path, "rev-parse", "--git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "CHERRY_PICK_HEAD")); err == nil {
		t.Error("worktree left mid-cherry-pick; the agent would have to know it was one")
	}
	finalizeAndDiscardForTest(t, second)
}

// The turn cannot be delivered while the merge is open: committing would turn
// conflict markers into the branch's content, and recording the snapshot would
// tell every later turn the user's edit had landed.
func TestFinalizeRefusesToDeliverAnUnresolvedMerge(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	finalizeOK(t, first)
	recordedAfterFirst := gitRun(t, repo, "rev-parse", userStateRef("agent/j/mul-6881"))

	writeFile(t, filepath.Join(repo, "tracked.txt"), "rewritten by the user instead\n")
	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)

	outcome, err := second.Finalize(worktreeTestLogger())
	if err == nil {
		t.Fatal("Finalize delivered a branch while the merge was still open")
	}
	if !strings.Contains(err.Error(), "tracked.txt") {
		t.Errorf("error does not name the unresolved file: %v", err)
	}
	if outcome.Branch != "" {
		t.Errorf("outcome named branch %q for a turn that committed nothing", outcome.Branch)
	}
	if outcome.PreservedPath != second.Path {
		t.Errorf("PreservedPath = %q, want the worktree at %q", outcome.PreservedPath, second.Path)
	}
	if _, statErr := os.Stat(second.Path); statErr != nil {
		t.Errorf("worktree removed despite an unresolved merge: %v", statErr)
	}
	if got := gitRun(t, repo, "rev-parse", userStateRef("agent/j/mul-6881")); got != recordedAfterFirst {
		t.Error("the snapshot advanced past an edit the branch never took")
	}
	if got := gitRun(t, repo, "show", "agent/j/mul-6881:tracked.txt"); got != "rewritten by the agent" {
		t.Errorf("branch content = %q, want the previous turn's version uncommitted over", got)
	}
	_ = removeLocalWorktreeDir(repo, second.Path, worktreeTestLogger())
}

// The A/B/C round trip: the user's conflicting edit survives until the agent
// resolves it, and is not replayed again afterwards.
func TestConflictResolvedByTheAgentIsDeliveredAndNotReplayedAgain(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "A\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "B\n")
	finalizeOK(t, first)

	writeFile(t, filepath.Join(repo, "tracked.txt"), "C\n")
	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if len(second.ReplayConflicts) == 0 {
		t.Fatal("turn two saw no conflict between the agent's B and the user's C")
	}
	// The agent resolves it the way the prompt asks: read both sides, keep both.
	writeFile(t, filepath.Join(second.WorkDir, "tracked.txt"), "B and C\n")
	gitRun(t, second.Path, "add", "tracked.txt")
	outcome := finalizeOK(t, second)
	if outcome.Branch != "agent/j/mul-6881" {
		t.Fatalf("resolved turn delivered %q", outcome.Branch)
	}
	if got := gitRun(t, repo, "show", "agent/j/mul-6881:tracked.txt"); got != "B and C" {
		t.Errorf("branch content = %q, want the agent's resolution", got)
	}

	// Turn three: the user has not touched anything since, so there is nothing
	// left to replay — and in particular C must not come back as a conflict.
	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if len(third.ReplayConflicts) != 0 {
		t.Errorf("turn three replayed the resolved edit again: %v", third.ReplayConflicts)
	}
	if got := readFile(t, filepath.Join(third.WorkDir, "tracked.txt")); got != "B and C\n" {
		t.Errorf("turn three tracked.txt = %q, want the resolution", got)
	}
	finalizeOK(t, third)
}

// A conflict the agent never resolved must stay pending: the user's edit is
// still missing from the branch, so the next turn has to offer it again.
func TestUnresolvedConflictIsReplayedOnTheNextTurn(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "A\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "B\n")
	finalizeOK(t, first)

	writeFile(t, filepath.Join(repo, "tracked.txt"), "C\n")
	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if len(second.ReplayConflicts) == 0 {
		t.Fatal("turn two saw no conflict")
	}
	// The agent walks away without resolving anything.
	if _, err := second.Finalize(worktreeTestLogger()); err == nil {
		t.Fatal("Finalize accepted an unresolved merge")
	}
	_ = removeLocalWorktreeDir(repo, second.Path, worktreeTestLogger())

	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if len(third.ReplayConflicts) == 0 {
		t.Fatal("the user's edit was forgotten after one unresolved turn")
	}
	if got := readFile(t, filepath.Join(third.WorkDir, "tracked.txt")); !strings.Contains(got, "C") {
		t.Errorf("turn three tracked.txt = %q, want the user's edit still on offer", got)
	}
	finalizeAndDiscardForTest(t, third)
}

// finalizeAndDiscardForTest ends a turn whose worktree is deliberately left
// conflicted, without asserting on the refusal that is tested elsewhere.
func finalizeAndDiscardForTest(t *testing.T, wt *LocalWorktree) {
	t.Helper()
	_, _ = wt.Finalize(worktreeTestLogger())
	_ = removeLocalWorktreeDir(wt.GitRoot, wt.Path, worktreeTestLogger())
}

// A file that started untracked is committed to the branch at the turn that
// first sees it, and the agent may edit it from there. The user's later edits
// to their own copy still have to reach the next turn — the snapshot has to
// cover untracked content for that question to be answerable at all.
func TestPrepareLocalWorktreeCarriesLaterUserEditsToOnceUntrackedFiles(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "scratch.txt"), "v1\n")
	writeFile(t, filepath.Join(repo, "notes.txt"), "notes v1\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	finalizeOK(t, first)

	// The user updates one untracked file and deletes the other.
	writeFile(t, filepath.Join(repo, "scratch.txt"), "v2\n")
	if err := os.Remove(filepath.Join(repo, "notes.txt")); err != nil {
		t.Fatalf("remove notes.txt: %v", err)
	}

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if len(second.ReplayConflicts) != 0 {
		t.Fatalf("unexpected conflict: %v", second.ReplayConflicts)
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "scratch.txt")); got != "v2\n" {
		t.Errorf("scratch.txt = %q, want the user's newer version", got)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("notes.txt survived the user deleting it: %v", err)
	}
	finalizeOK(t, second)
}

// An untracked file becomes part of the branch at the turn that first sees it.
// Copying the user's older copy over it on the next turn would revert whatever
// the agent did to it — silently, every turn.
func TestPrepareLocalWorktreeKeepsAgentEditsToOnceUntrackedFiles(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "scratch.txt"), "user scratch\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "scratch.txt"), "scratch, rewritten by the agent\n")
	finalizeOK(t, first)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if got := readFile(t, filepath.Join(second.WorkDir, "scratch.txt")); got != "scratch, rewritten by the agent\n" {
		t.Errorf("scratch.txt = %q, want the agent's version", got)
	}
}

// A turn that only reads must leave the conversation's branch alone: it holds
// every turn before it, and the read-only cleanup would take those with it.
func TestFinalizeKeepsTheConversationBranchAfterAReadOnlyTurn(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "work from turn one\n")
	finalizeOK(t, first)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	outcome := finalizeOK(t, second)
	if outcome.Branch != "agent/j/mul-6881" {
		t.Errorf("read-only turn reported branch %q, want the conversation's branch", outcome.Branch)
	}
	if _, err := gitTry(t, repo, "rev-parse", "--verify", "agent/j/mul-6881"); err != nil {
		t.Fatal("the conversation's branch was deleted by a turn that changed nothing")
	}
	if got := gitRun(t, repo, "show", "agent/j/mul-6881:turn-one.txt"); got != "work from turn one" {
		t.Errorf("turn one's work is gone from the branch: %q", got)
	}
}

// Discard runs when preparation fails after the worktree exists. It may only
// drop a branch this prepare created.
func TestDiscardKeepsTheContinuedBranch(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "work from turn one\n")
	finalizeOK(t, first)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	second.Discard(worktreeTestLogger())

	if _, err := gitTry(t, repo, "rev-parse", "--verify", "agent/j/mul-6881"); err != nil {
		t.Fatal("discarding a follow-up turn deleted the conversation's branch")
	}
	if _, err := os.Stat(second.Path); !os.IsNotExist(err) {
		t.Errorf("worktree still on disk after Discard: %v", err)
	}
}

// git allows one worktree per branch. A sibling task on the same conversation
// still has to run, and it should start from the conversation's latest work
// rather than from HEAD.
//
// It works from the first turn on because every branch this mode creates gets a
// baseline commit of its own, so there is a checkpoint to record before the
// first turn has finished — see commitBaseline.
func TestPrepareLocalWorktreeForksWhenTheConversationBranchIsBusy(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "work from turn one\n")
	// Commit inside the worktree so the sibling has something to inherit while
	// the branch is still checked out here.
	gitRun(t, first.Path, "add", "-A")
	gitRun(t, first.Path, "commit", "-m", "turn one")
	tip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	sibling := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if sibling.Branch == first.Branch {
		t.Fatalf("sibling took the branch already checked out at %s", first.Path)
	}
	if want := "agent/j/mul-6881-" + taskKey(turnTwoTask); sibling.Branch != want {
		t.Errorf("sibling branch = %q, want %q", sibling.Branch, want)
	}
	if sibling.BaseCommit != tip {
		t.Errorf("sibling base = %s, want the conversation's tip %s", sibling.BaseCommit, tip)
	}
	if _, err := os.Stat(filepath.Join(sibling.WorkDir, "turn-one.txt")); err != nil {
		t.Errorf("sibling does not carry the conversation's work: %v", err)
	}
	finalizeOK(t, sibling)
	finalizeOK(t, first)
}

// Once the user merges the branch, its tip carries nothing HEAD does not.
// Continuing from it would leave the next turn behind the user's own commits.
func TestPrepareLocalWorktreeRestartsAMergedConversationBranch(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "turn-one.txt"), "work from turn one\n")
	finalizeOK(t, first)

	gitRun(t, repo, "merge", "--no-edit", "agent/j/mul-6881")
	writeFile(t, filepath.Join(repo, "user-commit.txt"), "the user kept working\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "user work after the merge")
	head := gitRun(t, repo, "rev-parse", "HEAD")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Continued {
		t.Error("continued a branch the user had already merged")
	}
	// Restarted at HEAD: the base is this turn's own baseline commit, sitting
	// directly on the user's newest commit rather than behind it.
	if _, err := gitTry(t, repo, "merge-base", "--is-ancestor", head, second.BaseCommit); err != nil {
		t.Errorf("second turn base %s does not build on the user's HEAD %s", second.BaseCommit, head)
	}
	if parent := gitRun(t, repo, "rev-parse", second.BaseCommit+"^"); parent != head {
		t.Errorf("second turn base is parented at %s, want the user's HEAD %s", parent, head)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "user-commit.txt")); err != nil {
		t.Errorf("the user's commits after the merge are missing: %v", err)
	}
}

// A task with no conversation behind it — no issue, no chat session — has
// nothing to continue and keeps the task-scoped branch.
func TestPrepareLocalWorktreeKeepsTaskScopedBranchWithoutAConversation(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	if want := "agent/j/" + taskKey("11112222-3333-4444-5555-666677778888"); wt.Branch != want {
		t.Errorf("branch = %q, want %q", wt.Branch, want)
	}
	if wt.Continued {
		t.Error("a task with no conversation reports Continued = true")
	}
}

func TestLocalWorktreeConversation(t *testing.T) {
	issueID := "01a056ac-0eda-797d-8ac2-b7d7a3935ae7"
	chatID := "01a056ad-5b37-762a-a15f-390717f4dae1"
	tests := []struct {
		name    string
		params  PrepareParams
		wantKey string
		wantID  string
	}{
		{"issue identifier names the branch, issue id owns it", PrepareParams{IssueIdentifier: "MUL-6881", Task: TaskContextForEnv{IssueID: issueID}}, "MUL-6881", issueID},
		{"issue without an identifier", PrepareParams{Task: TaskContextForEnv{IssueID: issueID}}, taskKey(issueID), issueID},
		{"chat session", PrepareParams{Task: TaskContextForEnv{ChatSessionID: chatID}}, "chat-" + taskKey(chatID), chatID},
		{"neither", PrepareParams{}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, id := localWorktreeConversation(tt.params)
			if key != tt.wantKey || id != tt.wantID {
				t.Errorf("localWorktreeConversation() = (%q, %q), want (%q, %q)", key, id, tt.wantKey, tt.wantID)
			}
		})
	}
}

// A branch is continued because it is provably this conversation's, never
// because the name matches. `agent/j/mul-6881` is a name the user can type
// themselves, and appending to their branch — or reading their work as the
// previous turn's — is the failure this guards.
func TestPrepareLocalWorktreeRefusesToAdoptABranchItDoesNotOwn(t *testing.T) {
	repo := newTestRepo(t)

	// The user made this branch themselves, with content of their own.
	gitRun(t, repo, "branch", "agent/j/mul-6881")
	gitRun(t, repo, "worktree", "add", "--quiet", filepath.Join(t.TempDir(), "theirs"), "agent/j/mul-6881")
	theirs := gitRun(t, repo, "worktree", "list", "--porcelain")
	_ = theirs
	userBranchDir := ""
	for _, line := range strings.Split(gitRun(t, repo, "worktree", "list", "--porcelain"), "\n") {
		if strings.HasPrefix(line, "worktree ") && strings.HasSuffix(line, "theirs") {
			userBranchDir = strings.TrimPrefix(line, "worktree ")
		}
	}
	if userBranchDir == "" {
		t.Fatal("could not locate the user's own worktree")
	}
	writeFile(t, filepath.Join(userBranchDir, "unrelated.txt"), "the user's own work\n")
	gitRun(t, userBranchDir, "add", "-A")
	gitRun(t, userBranchDir, "commit", "-m", "the user's own commit")
	gitRun(t, repo, "worktree", "remove", "--force", userBranchDir)
	userTip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	if first.Continued {
		t.Error("continued a branch with no proof it belongs to this conversation")
	}
	if first.Branch == "agent/j/mul-6881" {
		t.Fatal("took over the user's own branch")
	}
	if want := "agent/j/mul-6881-" + testBranchOwner.fingerprint(); first.Branch != want {
		t.Errorf("branch = %q, want %q", first.Branch, want)
	}
	if _, err := os.Stat(filepath.Join(first.WorkDir, "unrelated.txt")); err == nil {
		t.Error("the user's unrelated work leaked into this task's tree")
	}
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)

	// The user's branch is untouched, and the conversation's own fallback
	// branch is continued by the next turn.
	if got := gitRun(t, repo, "rev-parse", "agent/j/mul-6881"); got != userTip {
		t.Error("the user's branch was moved")
	}
	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if !second.Continued || second.Branch != first.Branch {
		t.Errorf("second turn: branch = %q, continued = %v; want %q continued", second.Branch, second.Continued, first.Branch)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "agent.txt")); err != nil {
		t.Errorf("the fallback branch did not carry turn one's work: %v", err)
	}
	finalizeOK(t, second)
}

// Two workspaces can mint the same issue identifier, and two agents can share a
// display name. Neither may end up on one branch.
func TestPrepareLocalWorktreeSeparatesIdenticallyNamedConversations(t *testing.T) {
	repo := newTestRepo(t)

	mine := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(mine.WorkDir, "mine.txt"), "my work\n")
	finalizeOK(t, mine)

	other := testBranchOwner
	other.WorkspaceID = "99998888-3333-4444-5555-000000000001"
	theirs, err := prepareTurnAs(repo, "MUL-6881", turnTwoTask, other)
	if err != nil {
		t.Fatalf("PrepareLocalWorktree for the other workspace: %v", err)
	}
	if theirs.Branch == mine.Branch {
		t.Fatalf("another workspace's issue MUL-6881 landed on %q", mine.Branch)
	}
	if theirs.Continued {
		t.Error("continued a branch owned by a different workspace")
	}
	if _, err := os.Stat(filepath.Join(theirs.WorkDir, "mine.txt")); err == nil {
		t.Error("another workspace's work is visible in this task's tree")
	}
	finalizeOK(t, theirs)
}

// The branch is the user's to delete. Its snapshot must not outlive it: the ref
// pins their whole working tree as of some past turn against `git gc`, and
// nothing else would ever come back for it.
func TestPrepareLocalWorktreePrunesSnapshotsOfDeletedBranches(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)
	ref := userStateRef("agent/j/mul-6881")
	if _, err := gitTry(t, repo, "rev-parse", "--verify", ref); err != nil {
		t.Fatal("turn one recorded no snapshot")
	}

	// The user deletes the branch, the ordinary end of a piece of work once they
	// have taken what they wanted from it. Any later task on this repo is the
	// one that has to notice.
	gitRun(t, repo, "branch", "-D", "agent/j/mul-6881")

	elsewhere, err := prepareTurnAs(repo, "MUL-7000", turnTwoTask, branchOwner{
		WorkspaceID:    testBranchOwner.WorkspaceID,
		AgentID:        testBranchOwner.AgentID,
		ConversationID: "11112222-3333-4444-5555-000000000009",
	})
	if err != nil {
		t.Fatalf("PrepareLocalWorktree on another conversation: %v", err)
	}
	if _, err := gitTry(t, repo, "rev-parse", "--verify", ref); err == nil {
		t.Error("the snapshot of a deleted branch was left behind")
	}
	writeFile(t, filepath.Join(elsewhere.WorkDir, "other.txt"), "other work\n")
	finalizeOK(t, elsewhere)

	// A new conversation branch of the old name starts clean rather than
	// inheriting a snapshot the branch no longer carries.
	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if third.Continued {
		t.Error("continued a branch the user had deleted")
	}
	writeFile(t, filepath.Join(third.WorkDir, "again.txt"), "turn three\n")
	finalizeOK(t, third)
	if _, err := gitTry(t, repo, "rev-parse", "--verify", ref); err != nil {
		t.Error("the recreated branch recorded no snapshot")
	}
}

// The snapshot is the user's directory, not the daemon's view of it: a sidecar
// left in their tree by a concurrent in_place task must never reach the branch.
func TestCaptureUserSnapshotExcludesMulticaSidecars(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".agent_context", "brief.md"), "another task's brief\n")
	writeFile(t, filepath.Join(repo, "sub", ".multica", "state.json"), "{}\n")
	writeFile(t, filepath.Join(repo, "real.txt"), "the user's file\n")

	head := gitRun(t, repo, "rev-parse", "HEAD")
	snapshot, err := captureUserSnapshot(repo, t.TempDir(), head, worktreeTestLogger())
	if err != nil {
		t.Fatalf("captureUserSnapshot: %v", err)
	}
	listed := gitRun(t, repo, "ls-tree", "-r", "--name-only", snapshot)
	for _, unwanted := range []string{".agent_context/brief.md", "sub/.multica/state.json"} {
		if strings.Contains(listed, unwanted) {
			t.Errorf("snapshot carries the sidecar %s:\n%s", unwanted, listed)
		}
	}
	if !strings.Contains(listed, "real.txt") {
		t.Errorf("snapshot dropped the user's own file:\n%s", listed)
	}
}

// The record is what proves a branch is this conversation's: an owner AND the
// tip it was written at.
func TestBranchRecordRoundTrips(t *testing.T) {
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work\n")
	snapshot, err := captureUserSnapshot(repo, t.TempDir(), head, worktreeTestLogger())
	if err != nil {
		t.Fatalf("captureUserSnapshot: %v", err)
	}
	gitRun(t, repo, "branch", "agent/j/mul-6881")

	recorded, err := writeBranchRecord(repo, "agent/j/mul-6881", snapshot, head, testBranchOwner)
	if err != nil {
		t.Fatalf("writeBranchRecord: %v", err)
	}
	record, err := readBranchRecord(repo, recorded)
	if err != nil {
		t.Fatalf("readBranchRecord: %v", err)
	}
	if record.owner != testBranchOwner {
		t.Errorf("owner = %+v, want %+v", record.owner, testBranchOwner)
	}
	if record.checkpoint != head {
		t.Errorf("checkpoint = %s, want the branch tip %s", record.checkpoint, head)
	}
	// The tree is the user's directory, which is what the next turn replays from.
	if got := gitRun(t, repo, "show", record.state+":tracked.txt"); got != "user work" {
		t.Errorf("record tree tracked.txt = %q, want the user's edit", got)
	}

	// A commit this code did not write proves nothing.
	plain, err := readBranchRecord(repo, head)
	if err != nil {
		t.Fatalf("readBranchRecord(HEAD): %v", err)
	}
	if plain.owner != (branchOwner{}) || plain.checkpoint != "" {
		t.Errorf("a plain commit reported %+v", plain)
	}
}

// Ownership is not a property of the NAME. A branch Multica delivered, that the
// user then deleted and recreated for something of their own, keeps matching
// the recorded owner — and there is no prepare in between for the orphan sweep
// to notice the gap. Only the recorded checkpoint distinguishes them.
func TestPrepareLocalWorktreeRefusesABranchDeletedAndRecreatedUnderTheSameName(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)
	if first.Branch != "agent/j/mul-6881" {
		t.Fatalf("first turn branch = %q", first.Branch)
	}

	// The user deletes the delivered branch and makes their own of that name,
	// with unrelated work on it. No task runs in between.
	gitRun(t, repo, "branch", "-D", "agent/j/mul-6881")
	gitRun(t, repo, "branch", "agent/j/mul-6881")
	theirs := t.TempDir()
	gitRun(t, repo, "worktree", "add", "--quiet", filepath.Join(theirs, "wt"), "agent/j/mul-6881")
	writeFile(t, filepath.Join(theirs, "wt", "unrelated.txt"), "the user's own work\n")
	gitRun(t, filepath.Join(theirs, "wt"), "add", "-A")
	gitRun(t, filepath.Join(theirs, "wt"), "commit", "-m", "the user's own commit")
	gitRun(t, repo, "worktree", "remove", "--force", filepath.Join(theirs, "wt"))
	userTip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Continued {
		t.Error("continued a branch that no longer contains the commit it was recorded at")
	}
	if second.Branch == "agent/j/mul-6881" {
		t.Fatal("took over the branch the user recreated")
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "unrelated.txt")); err == nil {
		t.Error("the user's unrelated work leaked into this task's tree")
	}
	finalizeOK(t, second)
	if got := gitRun(t, repo, "rev-parse", "agent/j/mul-6881"); got != userTip {
		t.Error("the user's branch was moved")
	}
}

// Same proof, other shape: the branch still exists but was force-moved onto
// history that never carried this conversation's work.
func TestPrepareLocalWorktreeRefusesABranchForceMovedOffItsRecord(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)

	// The user builds their own history and points the branch at it.
	gitRun(t, repo, "checkout", "--quiet", "-b", "theirs")
	writeFile(t, filepath.Join(repo, "unrelated.txt"), "the user's own work\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "the user's own commit")
	gitRun(t, repo, "checkout", "--quiet", "main")
	gitRun(t, repo, "branch", "-f", "agent/j/mul-6881", "theirs")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Continued {
		t.Error("continued a branch force-moved off the commit it was recorded at")
	}
	if second.Branch == "agent/j/mul-6881" {
		t.Fatal("took over the force-moved branch")
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "unrelated.txt")); err == nil {
		t.Error("the user's unrelated work leaked into this task's tree")
	}
	finalizeOK(t, second)
}

// The user committing on top of a delivered branch is the normal, welcome case
// — the checkpoint is still in the history, so the conversation continues and
// picks their commit up.
func TestPrepareLocalWorktreeContinuesAfterTheUserCommitsOnTheBranch(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)

	worktree := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "--quiet", worktree, "agent/j/mul-6881")
	writeFile(t, filepath.Join(worktree, "review.txt"), "the user's tweak\n")
	gitRun(t, worktree, "add", "-A")
	gitRun(t, worktree, "commit", "-m", "the user's tweak on the agent's branch")
	gitRun(t, repo, "worktree", "remove", "--force", worktree)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if !second.Continued || second.Branch != "agent/j/mul-6881" {
		t.Fatalf("second turn: branch = %q, continued = %v", second.Branch, second.Continued)
	}
	for _, name := range []string{"agent.txt", "review.txt"} {
		if _, err := os.Stat(filepath.Join(second.WorkDir, name)); err != nil {
			t.Errorf("turn two is missing %s: %v", name, err)
		}
	}
	finalizeOK(t, second)
}

// The record IS part of delivering the branch. If it cannot be written, the
// branch is not continuable and the turn has not delivered what it claims — so
// the task fails and the worktree is kept, rather than reporting success and
// leaving a stale record behind to authorise the next turn.
func TestFinalizeFailsWhenTheBranchRecordCannotBeWritten(t *testing.T) {
	repo := newTestRepo(t)

	wt := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")

	// Hold the ref's lock so only the final update-ref fails, the way a crashed
	// git or a concurrent writer would leave it.
	lock := filepath.Join(repo, ".git", filepath.FromSlash(userStateRef("agent/j/mul-6881"))+".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatalf("prepare ref lock dir: %v", err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatalf("hold ref lock: %v", err)
	}

	outcome, err := wt.Finalize(worktreeTestLogger())
	if err == nil {
		t.Fatal("Finalize reported success without recording the branch")
	}
	if outcome.PreservedPath != wt.Path {
		t.Errorf("PreservedPath = %q, want the worktree at %q", outcome.PreservedPath, wt.Path)
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Errorf("worktree removed even though the branch could not be recorded: %v", statErr)
	}
	// The agent's work is committed to the branch regardless — that commit
	// happens before the record, and losing it would be the worse failure.
	if got := gitRun(t, repo, "show", "agent/j/mul-6881:agent.txt"); got != "turn one" {
		t.Errorf("branch content = %q, want the agent's work committed", got)
	}

	// And with no record, a later turn must not adopt the branch: the hole this
	// closes is the user deleting it and recreating one of their own at the
	// commit the prepare-time record used to point at.
	if err := os.Remove(lock); err != nil {
		t.Fatalf("release ref lock: %v", err)
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
	gitRun(t, repo, "branch", "-D", "agent/j/mul-6881")
	gitRun(t, repo, "branch", "agent/j/mul-6881")
	worktree := filepath.Join(t.TempDir(), "theirs")
	gitRun(t, repo, "worktree", "add", "--quiet", worktree, "agent/j/mul-6881")
	writeFile(t, filepath.Join(worktree, "unrelated.txt"), "the user's own work\n")
	gitRun(t, worktree, "add", "-A")
	gitRun(t, worktree, "commit", "-m", "the user's own commit")
	gitRun(t, repo, "worktree", "remove", "--force", worktree)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Continued || second.Branch == "agent/j/mul-6881" {
		t.Errorf("second turn adopted the user's branch: branch = %q, continued = %v", second.Branch, second.Continued)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "unrelated.txt")); err == nil {
		t.Error("the user's unrelated work leaked into this task's tree")
	}
	finalizeOK(t, second)
}

// The checkpoint is the commit the turn DELIVERED, not whatever the branch ref
// says afterwards. A branch moved between the delivery and any later read is
// therefore refused rather than silently continued.
func TestBranchRecordPinsTheDeliveredCommitNotTheLiveRef(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)
	delivered := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	ref, err := readUserStateRef(repo, "agent/j/mul-6881")
	if err != nil {
		t.Fatalf("readUserStateRef: %v", err)
	}
	record, err := readBranchRecord(repo, ref)
	if err != nil {
		t.Fatalf("readBranchRecord: %v", err)
	}
	if record.checkpoint != delivered {
		t.Fatalf("checkpoint = %s, want the delivered tip %s", record.checkpoint, delivered)
	}

	// Someone moves the branch after delivery — a force-push, a reset, a script.
	gitRun(t, repo, "branch", "-f", "agent/j/mul-6881", "HEAD")
	if _, owned := branchOwnedBy(repo, "agent/j/mul-6881", testBranchOwner, worktreeTestLogger()); owned {
		t.Error("a branch moved off the delivered commit still counted as ours")
	}

	// The deterministic form of the same race: a record written while the ref
	// already points somewhere else must still pin what was delivered, not
	// whatever the ref says at the moment of writing.
	written, err := writeBranchRecord(repo, "agent/j/mul-6881", record.state, delivered, testBranchOwner)
	if err != nil {
		t.Fatalf("writeBranchRecord: %v", err)
	}
	rewritten, err := readBranchRecord(repo, written)
	if err != nil {
		t.Fatalf("readBranchRecord: %v", err)
	}
	if rewritten.checkpoint != delivered {
		t.Errorf("checkpoint = %s, want the commit passed in (%s), not the live ref", rewritten.checkpoint, delivered)
	}
}

// The run itself can destroy the proof: an agent that resets its worktree back
// to the user's own HEAD leaves the branch sitting on a plain user commit.
// Recording that as the checkpoint would make a branch the user later recreates
// there look like this conversation's, so the turn refuses to record it and
// keeps the worktree instead.
func TestFinalizeRefusesToRecordADeliveryThatResetPastItsBaseline(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	head := gitRun(t, repo, "rev-parse", "HEAD")

	wt := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	if wt.BaseCommit == head {
		t.Fatal("prepare left the branch on the user's own HEAD, with no commit of its own")
	}
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")
	// The agent throws its own history away and lands back on the user's HEAD.
	gitRun(t, wt.Path, "reset", "--hard", head)

	outcome, err := wt.Finalize(worktreeTestLogger())
	if err == nil {
		t.Fatal("Finalize recorded a delivery that no longer contains the branch's own commit")
	}
	if !strings.Contains(err.Error(), "no longer contains") {
		t.Errorf("error does not explain what is missing: %v", err)
	}
	if outcome.Branch != "" {
		t.Errorf("outcome named branch %q for a delivery it refused to record", outcome.Branch)
	}
	if outcome.PreservedPath != wt.Path {
		t.Errorf("PreservedPath = %q, want the worktree at %q", outcome.PreservedPath, wt.Path)
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Errorf("worktree removed despite refusing the delivery: %v", statErr)
	}

	// And the hole this closes: the user deletes the branch, recreates one of
	// their own at that same HEAD, and the next turn must not adopt it.
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
	gitRun(t, repo, "branch", "-D", "agent/j/mul-6881")
	gitRun(t, repo, "branch", "agent/j/mul-6881")
	theirs := filepath.Join(t.TempDir(), "theirs")
	gitRun(t, repo, "worktree", "add", "--quiet", theirs, "agent/j/mul-6881")
	writeFile(t, filepath.Join(theirs, "unrelated.txt"), "the user's own work\n")
	gitRun(t, theirs, "add", "-A")
	gitRun(t, theirs, "commit", "-m", "the user's own commit")
	gitRun(t, repo, "worktree", "remove", "--force", theirs)

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if second.Continued || second.Branch == "agent/j/mul-6881" {
		t.Errorf("second turn adopted the user's branch: branch = %q, continued = %v", second.Branch, second.Continued)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "unrelated.txt")); err == nil {
		t.Error("the user's unrelated work leaked into this task's tree")
	}
	finalizeOK(t, second)
}

// The same guard from the other side: whatever the worktree delivered has to BE
// the task's branch. A run that ended somewhere else — a detached checkout, a
// different branch — delivered a commit this record has no business describing,
// and the branch it names would not carry it.
func TestFinalizeRefusesToRecordADeliveryFromOffTheBranch(t *testing.T) {
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")

	wt := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "turn one")
	delivered := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")
	// The run wanders off its own branch before it ends.
	gitRun(t, wt.Path, "checkout", "--quiet", "--detach", head)

	outcome, err := wt.Finalize(worktreeTestLogger())
	if err == nil {
		t.Fatal("Finalize recorded a delivery that is not the branch's tip")
	}
	if !strings.Contains(err.Error(), "did not deliver onto its own branch") {
		t.Errorf("error does not explain the mismatch: %v", err)
	}
	if outcome.PreservedPath != wt.Path {
		t.Errorf("PreservedPath = %q, want the worktree at %q", outcome.PreservedPath, wt.Path)
	}
	// The branch keeps what it had; nothing was recorded against the stray tip.
	if got := gitRun(t, repo, "rev-parse", "agent/j/mul-6881"); got != delivered {
		t.Errorf("branch moved to %s, want %s", got, delivered)
	}
	ref, err := readUserStateRef(repo, "agent/j/mul-6881")
	if err != nil {
		t.Fatalf("readUserStateRef: %v", err)
	}
	record, err := readBranchRecord(repo, ref)
	if err != nil {
		t.Fatalf("readBranchRecord: %v", err)
	}
	if record.checkpoint == head {
		t.Error("the stray HEAD was recorded as this branch's checkpoint")
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// A branch created by this prepare always gets a commit of its own, even when
// the user's directory was clean and there was nothing to replay. Without it
// the branch would sit exactly where the user's HEAD does, and nothing would
// tell it apart from a branch they create there themselves.
func TestPrepareLocalWorktreeAlwaysGivesANewBranchACommitOfItsOwn(t *testing.T) {
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")

	wt := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	if wt.BaseCommit == head {
		t.Fatal("a clean tree left the branch on the user's HEAD")
	}
	if parent := gitRun(t, repo, "rev-parse", wt.BaseCommit+"^"); parent != head {
		t.Errorf("baseline is parented at %s, want the user's HEAD %s", parent, head)
	}
	// It changes nothing: the point is its existence, not its content.
	if diff := gitRun(t, repo, "diff", "--stat", head, wt.BaseCommit); diff != "" {
		t.Errorf("the baseline of a clean tree is not empty:\n%s", diff)
	}
	// And a turn that produces nothing still leaves no branch behind.
	outcome := finalizeOK(t, wt)
	if outcome.Branch != "" {
		t.Errorf("read-only turn reported branch %q", outcome.Branch)
	}
	if _, err := gitTry(t, repo, "rev-parse", "--verify", "agent/j/mul-6881"); err == nil {
		t.Error("a turn that changed nothing left its branch behind")
	}
}

// A follow-up turn brings the user's newest edits in as its own baseline
// commit, and that commit — not the checkpoint the previous turn left — is what
// the delivery has to keep. A run that resets past it has not delivered those
// edits, and recording them as delivered is how they disappear from every later
// turn without anyone seeing it.
func TestFinalizeRefusesWhenAFollowUpResetsPastTheUserEditsItReplayed(t *testing.T) {
	repo := newTestRepo(t)

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "turn one\n")
	finalizeOK(t, first)
	firstTip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	// Between the turns the user edits a file of their own.
	writeFile(t, filepath.Join(repo, "tracked.txt"), "the user's newest edit\n")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if !second.Continued {
		t.Fatal("second turn did not continue the conversation's branch")
	}
	if got := readFile(t, filepath.Join(second.WorkDir, "tracked.txt")); got != "the user's newest edit\n" {
		t.Fatalf("second turn did not replay the user's edit: %q", got)
	}
	// The agent throws the turn away, landing back on what turn one delivered.
	gitRun(t, second.Path, "reset", "--hard", firstTip)

	if _, err := second.Finalize(worktreeTestLogger()); err == nil {
		t.Fatal("Finalize recorded the user's edits as delivered after they were reset away")
	}
	if _, statErr := os.Stat(second.Path); statErr != nil {
		t.Errorf("worktree removed despite refusing the delivery: %v", statErr)
	}
	_ = removeLocalWorktreeDir(repo, second.Path, worktreeTestLogger())

	// The property that matters: the third turn still sees the user's edit,
	// whichever branch it ends up on.
	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if got := readFile(t, filepath.Join(third.WorkDir, "tracked.txt")); got != "the user's newest edit\n" {
		t.Errorf("third turn tracked.txt = %q, want the user's edit still present", got)
	}
	finalizeOK(t, third)
}

// The same rule where the merge conflicted: the branch only carries this turn's
// snapshot once something is committed after the agent resolves. A resolution
// that leaves no commit is indistinguishable from throwing the merge away, so
// the record stays where it was and the edits come back next turn.
func TestConflictResolvedWithoutACommitDoesNotAdvanceTheRecord(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	finalizeOK(t, first)

	// The user rewrites the same line, so the next turn starts mid-merge.
	writeFile(t, filepath.Join(repo, "tracked.txt"), "rewritten by the user instead\n")
	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if len(second.ReplayConflicts) == 0 {
		t.Fatal("second turn saw no conflict")
	}
	// The agent resolves in favour of what the branch already had, which leaves
	// the tree exactly as it was and nothing to commit.
	writeFile(t, filepath.Join(second.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	gitRun(t, second.Path, "add", "tracked.txt")
	if outcome := finalizeOK(t, second); outcome.Branch != "agent/j/mul-6881" {
		t.Fatalf("resolved turn delivered %q", outcome.Branch)
	}

	// Because nothing was committed, the user's edit is offered again rather
	// than being recorded as something the branch took.
	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if len(third.ReplayConflicts) == 0 {
		t.Error("the user's edit was recorded as delivered even though no commit carried it")
	}
	finalizeAndDiscardForTest(t, third)
}

// The user committing on a delivered branch is supported, so a later turn can
// start well past the checkpoint the previous one recorded. What tells that
// turn whether its own merge landed is where IT started, not that older
// checkpoint: measuring against the checkpoint let a resolution that committed
// nothing count as delivered, and the user's local edit then went missing from
// the turn after.
func TestConflictAfterAUserCommitOnTheBranchStillOffersTheEditAgain(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")

	first := prepareTurn(t, repo, "MUL-6881", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	finalizeOK(t, first)
	recordedAfterFirst := gitRun(t, repo, "rev-parse", userStateRef("agent/j/mul-6881"))

	// The user reviews the branch and commits on top of it — the documented,
	// supported case. The branch tip now sits past the recorded checkpoint.
	worktree := filepath.Join(t.TempDir(), "review")
	gitRun(t, repo, "worktree", "add", "--quiet", worktree, "agent/j/mul-6881")
	writeFile(t, filepath.Join(worktree, "review.txt"), "the user's own commit on the branch\n")
	gitRun(t, worktree, "add", "-A")
	gitRun(t, worktree, "commit", "-m", "the user's tweak on the agent's branch")
	gitRun(t, repo, "worktree", "remove", "--force", worktree)
	movedTip := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")

	// And in their own directory they rewrite the same line the agent did, so
	// the next turn starts mid-merge.
	writeFile(t, filepath.Join(repo, "tracked.txt"), "rewritten by the user instead\n")

	second := prepareTurn(t, repo, "MUL-6881", turnTwoTask)
	if !second.Continued {
		t.Fatal("second turn did not continue the branch the user had committed on")
	}
	if second.BaseCommit != movedTip {
		t.Fatalf("second turn base = %s, want the branch tip the user left %s", second.BaseCommit, movedTip)
	}
	if len(second.ReplayConflicts) == 0 {
		t.Fatal("second turn saw no conflict")
	}
	// The agent resolves in favour of what the branch already had: nothing to
	// commit, so the branch stays exactly where this turn found it.
	writeFile(t, filepath.Join(second.WorkDir, "tracked.txt"), "rewritten by the agent\n")
	gitRun(t, second.Path, "add", "tracked.txt")
	finalizeOK(t, second)
	if got := gitRun(t, repo, "rev-parse", "agent/j/mul-6881"); got != movedTip {
		t.Fatalf("branch moved to %s, want %s — the turn committed nothing", got, movedTip)
	}
	if got := gitRun(t, repo, "rev-parse", userStateRef("agent/j/mul-6881")); got == recordedAfterFirst {
		t.Error("the turn recorded nothing at all; it should have re-recorded the state the branch carries")
	}

	// The edit was never committed anywhere, so the third turn has to offer it
	// again rather than treat it as delivered.
	third := prepareTurn(t, repo, "MUL-6881", turnThreeTask)
	if len(third.ReplayConflicts) == 0 {
		t.Error("the user's local edit was recorded as delivered even though no commit carried it")
	}
	finalizeAndDiscardForTest(t, third)
}

// Everything above drives PrepareLocalWorktree with hand-filled params. This
// one goes through Prepare, the entry point the daemon actually calls, so the
// plumbing between them is covered too: which claim fields become the branch
// name, and which become the identity the branch is recorded under.
func TestPrepareTwoTurnsOfOneIssueThroughPrepare(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	workspacesRoot := t.TempDir()

	turn := func(taskID, workspaceID string) *Environment {
		t.Helper()
		env, err := Prepare(PrepareParams{
			WorkspacesRoot:  workspacesRoot,
			WorkspaceID:     workspaceID,
			TaskID:          taskID,
			IssueIdentifier: "MUL-6881",
			AgentName:       "J",
			Provider:        "claude",
			LocalWorktree:   &LocalWorktreeParams{LocalPath: repo},
			Task: TaskContextForEnv{
				IssueID:   "01a056ac-0eda-797d-8ac2-b7d7a3935ae7",
				AgentID:   "5fb87ac7-23b5-4a7a-81fa-ed295a54545d",
				AgentName: "J",
			},
		}, worktreeTestLogger())
		if err != nil {
			t.Fatalf("Prepare(%s): %v", taskID, err)
		}
		return env
	}

	first := turn(turnOneTask, "ws-1")
	if first.LocalWorktree.Branch != "agent/j/mul-6881" {
		t.Fatalf("branch = %q, want the issue identifier from the claim", first.LocalWorktree.Branch)
	}
	if got := readFile(t, filepath.Join(first.WorkDir, "tracked.txt")); got != "user work in progress\n" {
		t.Fatalf("turn one did not see the user's uncommitted work: %q", got)
	}
	writeFile(t, filepath.Join(first.WorkDir, "agent.txt"), "work from turn one\n")
	if outcome := finalizeOK(t, first.LocalWorktree); outcome.Branch != "agent/j/mul-6881" {
		t.Fatalf("turn one delivered %q", outcome.Branch)
	}

	second := turn(turnTwoTask, "ws-1")
	if !second.LocalWorktree.Continued || second.LocalWorktree.Branch != "agent/j/mul-6881" {
		t.Fatalf("turn two: branch = %q, continued = %v", second.LocalWorktree.Branch, second.LocalWorktree.Continued)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "agent.txt")); err != nil {
		t.Errorf("turn two does not carry turn one's work: %v", err)
	}
	finalizeOK(t, second.LocalWorktree)

	// The identity comes from the claim too: another workspace whose issue
	// carries the same identifier gets its own branch.
	other := turn(turnThreeTask, "ws-2")
	if other.LocalWorktree.Continued || other.LocalWorktree.Branch == "agent/j/mul-6881" {
		t.Errorf("another workspace's MUL-6881 landed on %q (continued = %v)",
			other.LocalWorktree.Branch, other.LocalWorktree.Continued)
	}
	finalizeOK(t, other.LocalWorktree)
}

// Production never prepares in the daemon's own process: PrepareIsolated runs
// Prepare in a helper and the Environment comes back as JSON. Every guarantee
// Finalize makes depends on state this struct keeps unexported, and ordinary
// marshalling drops those fields without a word — the daemon then finalized a
// worktree it believed it owned nothing of. This test runs two turns across
// that boundary, which is where the in-process tests above cannot look.
func TestIsolatedPrepareCarriesTheStateFinalizeNeeds(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	workspacesRoot := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	turn := func(taskID string) *Environment {
		t.Helper()
		env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), PrepareParams{
			WorkspacesRoot:  workspacesRoot,
			WorkspaceID:     testBranchOwner.WorkspaceID,
			TaskID:          taskID,
			IssueIdentifier: "MUL-6881",
			Provider:        "claude",
			AgentName:       "J",
			Task: TaskContextForEnv{
				IssueID: testBranchOwner.ConversationID,
				AgentID: testBranchOwner.AgentID,
			},
			LocalWorktree: &LocalWorktreeParams{LocalPath: repo},
		}, worktreeTestLogger())
		if err != nil {
			t.Fatalf("PrepareIsolated(%s): %v", taskID, err)
		}
		return env
	}

	first := turn(turnOneTask)
	wt := first.LocalWorktree
	if wt.Branch != "agent/j/mul-6881" {
		t.Fatalf("branch = %q", wt.Branch)
	}
	// The proofs Finalize makes all hang off state that crosses as JSON.
	if !wt.tracksState || wt.userState == "" || wt.owner != testBranchOwner || !wt.createdBranch {
		t.Fatalf("state lost crossing the helper boundary: tracksState=%v userState=%q owner=%+v createdBranch=%v",
			wt.tracksState, wt.userState, wt.owner, wt.createdBranch)
	}

	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "work from turn one\n")
	finalizeOK(t, wt)

	// Finalize's record is the observable proof it had the state: the checkpoint
	// must be the tip it delivered, not the baseline Prepare recorded.
	delivered := gitRun(t, repo, "rev-parse", "agent/j/mul-6881")
	ref, err := readUserStateRef(repo, "agent/j/mul-6881")
	if err != nil {
		t.Fatalf("readUserStateRef: %v", err)
	}
	record, err := readBranchRecord(repo, ref)
	if err != nil {
		t.Fatalf("readBranchRecord: %v", err)
	}
	if record.checkpoint != delivered {
		t.Errorf("checkpoint = %s, want the delivered tip %s", record.checkpoint, delivered)
	}
	if record.owner != testBranchOwner {
		t.Errorf("record owner = %+v, want %+v", record.owner, testBranchOwner)
	}

	// And the continuation the whole feature is for still holds across it.
	second := turn(turnTwoTask)
	if !second.LocalWorktree.Continued || second.LocalWorktree.Branch != "agent/j/mul-6881" {
		t.Fatalf("turn two: branch = %q continued = %v", second.LocalWorktree.Branch, second.LocalWorktree.Continued)
	}
	if _, err := os.Stat(filepath.Join(second.WorkDir, "agent.txt")); err != nil {
		t.Errorf("turn two does not carry turn one's work: %v", err)
	}
	finalizeOK(t, second.LocalWorktree)
}

// A read-only turn drops its branch — which the daemon could not do either
// while createdBranch was being lost on the way back from the helper.
func TestIsolatedPrepareKeepsTheReadOnlyBranchDrop(t *testing.T) {
	repo := newTestRepo(t)
	workspacesRoot := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), PrepareParams{
		WorkspacesRoot:  workspacesRoot,
		WorkspaceID:     testBranchOwner.WorkspaceID,
		TaskID:          turnOneTask,
		IssueIdentifier: "MUL-6881",
		Provider:        "claude",
		AgentName:       "J",
		Task: TaskContextForEnv{
			IssueID: testBranchOwner.ConversationID,
			AgentID: testBranchOwner.AgentID,
		},
		LocalWorktree: &LocalWorktreeParams{LocalPath: repo},
	}, worktreeTestLogger())
	if err != nil {
		t.Fatalf("PrepareIsolated: %v", err)
	}

	// The daemon strips its own sidecars before finalizing; without that they
	// are untracked files in the worktree and every turn looks like it produced
	// something.
	if err := CleanupRuntimeConfig(env.WorkDir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}

	outcome := finalizeOK(t, env.LocalWorktree)
	if outcome.Branch != "" {
		t.Errorf("read-only turn reported branch %q", outcome.Branch)
	}
	if _, err := gitTry(t, repo, "rev-parse", "--verify", "agent/j/mul-6881"); err == nil {
		t.Error("a turn that changed nothing left its branch behind")
	}
}
