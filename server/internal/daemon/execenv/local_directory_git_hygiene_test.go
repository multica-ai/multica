package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end for the in_place half of GitHub #7114: run a real Prepare against
// a real git repository and prove the daemon's own files cannot reach the
// user's history through the agent's `git add -A`.
//
// The leaf functions have unit tests in git_exclude_test.go. This one exists
// because the bug was never in a leaf — it was in the wiring: the sidecars were
// written, the cleanup was correct, and nothing connected the two to git in
// between.
func TestPrepareInPlaceKeepsRepositoryCleanForGitAddAll(t *testing.T) {
	repo := newTestRepo(t)
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-gh7114-001",
		TaskID:         "aaaaaaaa-1111-2222-3333-444444444444",
		AgentName:      "Test Agent",
		Provider:       "grok",
		LocalWorkDir:   repo,
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if len(env.SidecarPaths) == 0 {
		t.Fatal("Prepare wrote sidecars into the user's repo but reported none to hide from git")
	}
	if dirty := gitRun(t, repo, "status", "--porcelain"); dirty == "" {
		t.Fatalf("precondition: expected Prepare to dirty the repo, got a clean tree")
	}

	// What the daemon does with SidecarPaths once Prepare hands them over.
	prot, err := PrepareGitExcludes(env.RootDir, repo, env.SidecarPaths)
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	if err := prot.Verify(prot.Env); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	gitEnvVars := prot.Env

	if status := gitEnv(t, repo, gitEnvVars, "status", "--porcelain"); status != "" {
		t.Errorf("runtime files still visible to the agent's git:\n%s", status)
	}
	gitEnv(t, repo, gitEnvVars, "add", "-A")
	if staged := gitEnv(t, repo, gitEnvVars, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("the agent's `git add -A` staged Multica runtime files:\n%s", staged)
	}

	// And the repository is handed back exactly as it was found.
	gitRun(t, repo, "reset")
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if status := gitRun(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("repository not restored to its pre-task state:\n%s", status)
	}
}

// Elon review, must-fix 2, end to end. This is the state a repository already
// hit by #7114 is in: sidecars were committed by an earlier run and then
// deleted by its cleanup, so the paths are gone from disk but still tracked.
// Prepare must not write there again — no ignore rule can hide a modification
// to a tracked file, so doing so would re-arm the exact loop reported.
func TestPrepareInPlaceDoesNotRewriteCommittedThenDeletedSidecars(t *testing.T) {
	repo := newTestRepo(t)

	// Reproduce the damage the old behaviour left behind.
	firstEnv, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-gh7114-004",
		TaskID:         "dddddddd-1111-2222-3333-444444444444",
		AgentName:      "Test Agent",
		Provider:       "grok",
		LocalWorkDir:   repo,
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "the bug: runtime files committed")
	if err := CleanupSidecars(firstEnv.RootDir); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	// Deliberately NOT committing the deletion. That is the state the bug
	// leaves behind and the one that matters: the paths are gone from the
	// working tree but still in the index, so os.Lstat calls them free while
	// git calls them tracked. Committing the removal here would drop them from
	// the index and quietly stop testing anything.
	stillTracked := gitRun(t, repo, "ls-files", "--", ".multica", ".agent_context")
	if stillTracked == "" {
		t.Fatal("precondition: the sidecars should still be tracked after cleanup")
	}
	for _, rel := range strings.Split(stillTracked, "\n") {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("precondition: %s should be gone from the working tree but still tracked", rel)
		}
	}

	// A later task on the same repository.
	secondEnv, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-gh7114-004",
		TaskID:         "eeeeeeee-1111-2222-3333-444444444444",
		AgentName:      "Test Agent",
		Provider:       "grok",
		LocalWorkDir:   repo,
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	prot, err := PrepareGitExcludes(secondEnv.RootDir, repo, secondEnv.SidecarPaths)
	if err != nil {
		t.Fatalf("PrepareGitExcludes: %v", err)
	}
	gitEnvVars := prot.Env

	gitEnv(t, repo, gitEnvVars, "add", "-A")
	// Deletions of the previously-committed copies are the damage this test
	// set up; what must not appear is Multica content being added or modified
	// again, because that is the loop re-arming.
	for _, line := range strings.Split(gitEnv(t, repo, gitEnvVars, "diff", "--cached", "--name-status"), "\n") {
		if line == "" || strings.HasPrefix(line, "D\t") {
			continue
		}
		t.Errorf("the injection loop is still armed on an already-polluted repo; staged: %s", line)
	}

	// And Prepare must have refused the tracked paths outright rather than
	// recreating them on disk.
	for _, rel := range []string{".multica/daemon_task_context.json", ".agent_context/issue_context.md"} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s was rewritten at a path git still tracks (stat err: %v)", rel, err)
		}
	}
}

// A local_directory resource may point at a plain folder. Prepare must still
// work there, and must not turn it into a git repository.
func TestPrepareInPlaceOnNonGitFolder(t *testing.T) {
	userDir := t.TempDir()
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-gh7114-002",
		TaskID:         "bbbbbbbb-1111-2222-3333-444444444444",
		AgentName:      "Test Agent",
		Provider:       "grok",
		LocalWorkDir:   userDir,
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := PrepareGitExcludes(t.TempDir(), userDir, env.SidecarPaths); err != nil {
		t.Errorf("excluding in a non-git folder must be a no-op, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(userDir, ".git")); !os.IsNotExist(statErr) {
		t.Errorf("must not create a git repository in the user's plain folder (stat err: %v)", statErr)
	}
}

// Cloud workdirs are daemon scratch wiped wholesale by the GC, and worktree
// mode deletes the worktree; neither should carry the in_place bookkeeping.
func TestPrepareCloudWorkdirReportsNoSidecarPaths(t *testing.T) {
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-gh7114-003",
		TaskID:         "cccccccc-1111-2222-3333-444444444444",
		AgentName:      "Test Agent",
		Provider:       "grok",
		Task: TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
			AgentID: "99999999-8888-7777-6666-555555555555",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(env.SidecarPaths) != 0 {
		t.Errorf("cloud workdir should report no sidecar paths, got %v", env.SidecarPaths)
	}
}

// Codex receives the runtime brief through app-server thread/inject_items on
// the in-place path. Building that inline payload must leave the repository's
// tracked AGENTS.md byte-exact throughout the task (GitHub #7879).
func TestBuildCodexRuntimeBriefDoesNotTouchTrackedAgentsFile(t *testing.T) {
	repo := newTestRepo(t)
	agentsMD := filepath.Join(repo, "AGENTS.md")
	writeFile(t, agentsMD, "# Project rules\n\nUse tabs.\n")
	gitRun(t, repo, "add", "AGENTS.md")
	gitRun(t, repo, "commit", "-m", "add agents.md")

	brief := BuildRuntimeBrief("codex", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
		AgentID: "99999999-8888-7777-6666-555555555555",
	})
	if strings.TrimSpace(brief) == "" {
		t.Fatal("BuildRuntimeBrief returned nothing; the agent would run with no brief at all")
	}
	if status := gitRun(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("building the brief modified the repository:\n%s", status)
	}
	got, err := os.ReadFile(agentsMD)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if want := "# Project rules\n\nUse tabs.\n"; string(got) != want {
		t.Errorf("AGENTS.md changed while building inline brief\nwant: %q\n got: %q", want, got)
	}
}
