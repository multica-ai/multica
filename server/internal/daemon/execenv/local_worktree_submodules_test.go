package execenv

import (
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func addLocalTestSubmodule(t *testing.T, parent, source, name, path string) {
	t.Helper()
	gitRun(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", "--name", name, "--", source, path)
	gitRun(t, parent, "commit", "-m", "add submodule "+name)
}

func TestLocalWorktreeDeliversSubmoduleChanges(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	childSource := newTestRepo(t)
	const childPath = "repos/web app"
	addLocalTestSubmodule(t, repo, childSource, "frontend", childPath)
	gitRun(t, repo, "config", "submodule.frontend.ignore", "all")
	child := filepath.Join(repo, childPath)
	oldGitlink := gitRun(t, child, "rev-parse", "HEAD")
	original := gitRun(t, repo, "rev-parse", "HEAD")
	first := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770001")
	taskChild := filepath.Join(first.Path, childPath)
	writeTestSubmoduleFile(t, taskChild, "tracked.txt", "edited frontend\n")
	writeTestSubmoduleFile(t, taskChild, "new.txt", "new frontend file\n")
	if got := gitRun(t, repo, "status", "--porcelain", "--ignore-submodules=none"); got != "" {
		t.Fatalf("task edits leaked into the source: %s", got)
	}

	outcome, err := first.Finalize(worktreeTestLogger())
	if err != nil || !outcome.AutoCommitted || outcome.Branch != first.Branch {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree was not removed: %v", err)
	}
	newGitlink := gitRun(t, repo, "rev-parse", outcome.Branch+":"+childPath)
	if newGitlink == oldGitlink {
		t.Fatal("delivered branch still has the original gitlink")
	}
	if got := gitRun(t, child, "show", newGitlink+":new.txt"); got != "new frontend file" {
		t.Fatalf("durable submodule commit has the wrong file: %s", got)
	}
	if got := gitRun(t, child, "rev-parse", localSubmoduleRefPrefix+newGitlink); got != newGitlink {
		t.Fatalf("submodule recovery ref points to %s, want %s", got, newGitlink)
	}
	if got := gitRun(t, child, "rev-parse", "HEAD"); got != oldGitlink {
		t.Fatalf("source submodule HEAD moved to %s", got)
	}
	if got := gitRun(t, repo, "rev-parse", "HEAD"); got != original {
		t.Fatalf("source parent HEAD moved to %s", got)
	}

	second := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770002")
	if !second.Continued || second.Branch != outcome.Branch {
		t.Fatalf("follow-up did not continue %s: %+v", outcome.Branch, second)
	}
	secondChild := filepath.Join(second.Path, childPath)
	if got := readFile(t, filepath.Join(secondChild, "tracked.txt")); got != "edited frontend\n" {
		t.Fatalf("follow-up checked out the wrong contents: %q", got)
	}
	if _, err := second.Finalize(worktreeTestLogger()); err != nil {
		t.Fatalf("finalize read-only follow-up: %v", err)
	}
	gitRun(t, repo, "checkout", outcome.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--no-fetch", "--", childPath)
	if got := readFile(t, filepath.Join(child, "new.txt")); got != "new frontend file\n" {
		t.Fatalf("ordinary Git checkout has the wrong contents: %q", got)
	}
}

func TestLocalWorktreeDeliversNestedSubmodulesInitializedInTask(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	childSource := newTestRepo(t)
	grandchildSource := newTestRepo(t)
	addLocalTestSubmodule(t, childSource, grandchildSource, "grandchild", "nested")
	addLocalTestSubmodule(t, repo, childSource, "child", "child")
	gitRun(t, repo, "submodule", "deinit", "--", "child")
	gitDir := gitRun(t, repo, "rev-parse", "--absolute-git-dir")
	// A fresh checkout would not have a cached submodule Git directory.
	if err := os.Rename(filepath.Join(gitDir, "modules"), filepath.Join(t.TempDir(), "modules")); err != nil {
		t.Fatal(err)
	}
	task := prepareForTest(t, repo)
	gitRun(t, task.Path, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive")
	writeTestSubmoduleFile(t, filepath.Join(task.Path, "child", "nested"), "new.txt", "nested change\n")

	result, err := task.Finalize(worktreeTestLogger())
	if err != nil || !result.AutoCommitted {
		t.Fatalf("Finalize = %+v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "child", ".git")); !os.IsNotExist(err) {
		t.Fatalf("delivery initialized the user's submodule checkout: %v", err)
	}
	gitRun(t, repo, "checkout", result.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--recursive", "--no-fetch")
	if got := readFile(t, filepath.Join(repo, "child", "nested", "new.txt")); got != "nested change\n" {
		t.Fatalf("ordinary recursive checkout has the wrong contents: %q", got)
	}
}

func TestLocalWorktreeKeepsSubmoduleLeftoversOffExplicitDelivery(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	task := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770003")
	child := filepath.Join(task.Path, "child")
	writeTestSubmoduleFile(t, child, "delivered.txt", "delivered\n")
	gitRun(t, child, "-c", "user.name=Test", "-c", "user.email=test@example.com", "add", "delivered.txt")
	gitRun(t, child, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "deliver child changes")
	completed := gitRun(t, child, "rev-parse", "HEAD")
	gitRun(t, task.Path, "add", "child")
	gitRun(t, task.Path, "commit", "-m", "deliver child gitlink")
	writeTestSubmoduleFile(t, child, "scratch.txt", "saved separately\n")
	result, err := task.Finalize(worktreeTestLogger())
	if err != nil || result.AutoCommitted || result.LeftoverRef == "" {
		t.Fatalf("Finalize = %+v, %v", result, err)
	}
	if got := gitRun(t, repo, "rev-parse", task.Branch+":child"); got != completed {
		t.Fatalf("explicit gitlink changed from %s to %s", completed, got)
	}
	sourceChild := filepath.Join(repo, "child")
	if got := gitRun(t, sourceChild, "show", completed+":delivered.txt"); got != "delivered" {
		t.Fatalf("explicit submodule commit is unavailable: %q", got)
	}
	leftover := gitRun(t, repo, "rev-parse", result.LeftoverRef+":child")
	if got := gitRun(t, sourceChild, "show", leftover+":scratch.txt"); got != "saved separately" {
		t.Fatalf("submodule leftovers are unavailable: %q", got)
	}
}

func TestLocalWorktreePreservesFailedSubmoduleCommit(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	task := prepareForTest(t, repo)
	child := filepath.Join(task.Path, "child")
	writeTestSubmoduleFile(t, child, "new.txt", "recoverable\n")
	gitRun(t, child, "config", "commit.gpgSign", "true")
	gitRun(t, child, "config", "gpg.program", filepath.Join(t.TempDir(), "no-such-signer"))
	result, err := task.Finalize(worktreeTestLogger())
	if err == nil || !strings.Contains(err.Error(), `save submodule "child"`) || result.PreservedPath != task.Path {
		t.Fatalf("Finalize = %+v, %v", result, err)
	}
	if got := readFile(t, filepath.Join(child, "new.txt")); got != "recoverable\n" {
		t.Fatalf("worktree lost the unsaved file: %q", got)
	}
	if got := gitRun(t, repo, "rev-parse", task.Branch); got != task.BaseCommit {
		t.Fatalf("failed save advanced the task branch to %s", got)
	}
	gitRun(t, child, "config", "commit.gpgSign", "false")
	if result, err := task.Finalize(worktreeTestLogger()); err != nil || !result.AutoCommitted {
		t.Fatalf("retry Finalize = %+v, %v", result, err)
	}
}

func writeTestSubmoduleFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorktreeRetainsNestedGitlinksFromDeliveredCommit(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	originalChild := newTestRepo(t)
	addLocalTestSubmodule(t, originalChild, newTestRepo(t), "leaf", "leaf")
	addLocalTestSubmodule(t, repo, originalChild, "child", "child")
	gitRun(t, repo, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive")

	task := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770004")
	child := filepath.Join(task.Path, "child")
	leaf := filepath.Join(child, "leaf")
	childBase := gitRun(t, child, "rev-parse", "HEAD")
	leafBase := gitRun(t, leaf, "rev-parse", "HEAD")
	writeTestSubmoduleFile(t, leaf, "deliver.txt", "nested delivery\n")
	gitRun(t, leaf, "add", "deliver.txt")
	gitRun(t, leaf, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "deliver leaf")
	gitRun(t, child, "add", "leaf")
	gitRun(t, child, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "deliver child")
	gitRun(t, task.Path, "add", "child")
	gitRun(t, task.Path, "commit", "-m", "deliver parent")
	gitRun(t, leaf, "checkout", "--detach", leafBase)
	gitRun(t, child, "checkout", "--detach", childBase)
	writeTestSubmoduleFile(t, leaf, "scratch.txt", "nested leftover\n")

	outcome, err := task.Finalize(worktreeTestLogger())
	if err != nil || outcome.LeftoverRef == "" {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}
	gitRun(t, repo, "checkout", outcome.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--recursive", "--no-fetch")
	if got := readFile(t, filepath.Join(repo, "child", "leaf", "deliver.txt")); got != "nested delivery\n" {
		t.Fatalf("recursive checkout did not restore the delivered revision: %q", got)
	}
	leftoverChild := gitRun(t, repo, "rev-parse", outcome.LeftoverRef+":child")
	leftoverLeaf := gitRun(t, filepath.Join(repo, "child"), "rev-parse", leftoverChild+":leaf")
	if got := gitRun(t, filepath.Join(repo, "child", "leaf"), "show", leftoverLeaf+":scratch.txt"); got != "nested leftover" {
		t.Fatalf("the leftover tree's nested revision is unavailable: %q", got)
	}
}

func TestLocalWorktreeRetainsGitlinkAfterDeinit(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	task := prepareForTest(t, repo)
	child := filepath.Join(task.Path, "child")
	writeTestSubmoduleFile(t, child, "deliver.txt", "saved after deinit\n")
	gitRun(t, child, "add", "deliver.txt")
	gitRun(t, child, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "deliver child")
	gitRun(t, task.Path, "add", "child")
	gitRun(t, task.Path, "commit", "-m", "deliver parent")
	gitRun(t, task.Path, "submodule", "absorbgitdirs", "--", "child")
	gitRun(t, task.Path, "submodule", "deinit", "--", "child")

	outcome, err := task.Finalize(worktreeTestLogger())
	if err != nil || outcome.Branch == "" {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}
	gitRun(t, repo, "checkout", outcome.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--no-fetch")
	if got := readFile(t, filepath.Join(repo, "child", "deliver.txt")); got != "saved after deinit\n" {
		t.Fatalf("checkout did not recover the deinitialized submodule: %q", got)
	}
}

func TestLocalWorktreeDeliversAndContinuesShallowSubmodule(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	childSource := newTestRepo(t)
	writeTestSubmoduleFile(t, childSource, "history.txt", "second commit\n")
	gitRun(t, childSource, "add", "history.txt")
	gitRun(t, childSource, "commit", "-m", "add history")
	childURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(childSource), "/")}).String()
	addLocalTestSubmodule(t, repo, childURL, "child", "child")
	gitRun(t, repo, "submodule", "deinit", "--", "child")
	gitDir := gitRun(t, repo, "rev-parse", "--absolute-git-dir")
	if err := os.Rename(filepath.Join(gitDir, "modules"), filepath.Join(t.TempDir(), "modules")); err != nil {
		t.Fatal(err)
	}

	task := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770005")
	gitRun(t, task.Path, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--depth", "1")
	child := filepath.Join(task.Path, "child")
	if got := gitRun(t, child, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fixture is not shallow: %s", got)
	}
	writeTestSubmoduleFile(t, child, "deliver.txt", "shallow delivery\n")
	outcome, err := task.Finalize(worktreeTestLogger())
	if err != nil || !outcome.AutoCommitted {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}
	delivered := gitRun(t, repo, "rev-parse", outcome.Branch+":child")
	cache := filepath.Join(gitDir, "modules", "child")
	if got := gitRun(t, cache, "--git-dir="+cache, "rev-parse", localSubmoduleRefPrefix+delivered); got != delivered {
		t.Fatalf("durable ref points at %s, want %s", got, delivered)
	}
	gitRun(t, cache, "--git-dir="+cache, "rev-list", delivered)

	next := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770006")
	if !next.Continued || gitRun(t, filepath.Join(next.Path, "child"), "rev-parse", "HEAD") != delivered {
		t.Fatalf("next turn did not continue the retained shallow commit: %+v", next)
	}
	gitRun(t, filepath.Join(next.Path, "child"), "rev-list", "HEAD")
	if _, err := next.Finalize(worktreeTestLogger()); err != nil {
		t.Fatalf("finalize shallow follow-up: %v", err)
	}
	gitRun(t, repo, "checkout", outcome.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--no-fetch")
	if got := readFile(t, filepath.Join(repo, "child", "deliver.txt")); got != "shallow delivery\n" {
		t.Fatalf("ordinary checkout did not recover the shallow commit: %q", got)
	}
}

func TestLocalWorktreeHandsGitmodulesReplayConflictToAgent(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	task := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770007")
	gitRun(t, task.Path, "config", "--file", ".gitmodules", "submodule.child.url", "https://example.invalid/agent")
	gitRun(t, task.Path, "add", ".gitmodules")
	gitRun(t, task.Path, "commit", "-m", "change child URL")
	if _, err := task.Finalize(worktreeTestLogger()); err != nil {
		t.Fatalf("finalize first turn: %v", err)
	}
	gitRun(t, repo, "config", "--file", ".gitmodules", "submodule.child.url", "https://example.invalid/user")
	next := prepareTurn(t, repo, "mul-submodule", "44444444-3333-4444-5555-666677770008")
	if !next.Continued || !slices.Contains(next.ReplayConflicts, ".gitmodules") {
		t.Fatalf("expected continued run with a .gitmodules conflict, got %+v", next)
	}
	if got := readFile(t, filepath.Join(next.Path, ".gitmodules")); !strings.Contains(got, "<<<<<<<") {
		t.Fatal("the conflicted file was not handed to the agent")
	}
}

func TestLocalWorktreePreservesCheckoutWhenDeliveredGitlinkIsMissing(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	task := prepareForTest(t, repo)
	base := gitRun(t, task.Path, "rev-parse", "HEAD:child")
	gitRun(t, task.Path, "update-index", "--cacheinfo", "160000,"+strings.Repeat("a", len(base))+",child")
	gitRun(t, task.Path, "commit", "-m", "reference unavailable child")

	result, err := task.Finalize(worktreeTestLogger())
	if err == nil || !strings.Contains(err.Error(), "referenced commit is missing") || result.PreservedPath != task.Path {
		t.Fatalf("Finalize = %+v, %v", result, err)
	}
	if _, err := os.Stat(task.Path); err != nil {
		t.Fatalf("failed delivery removed the task checkout: %v", err)
	}
}

func installTestSubmoduleCheckoutHook(t *testing.T, repo, edits string) {
	t.Helper()
	hooks := filepath.Join(repo, ".git", "hooks")
	write := "#!/bin/sh\nset -eu\ngit -c protocol.file.allow=always submodule update --init -- child\n" + edits
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte(write), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "config", "core.hooksPath", hooks)
}

func TestLocalWorktreeReusesSubmoduleInitializedByCheckoutHook(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	source := filepath.Join(repo, "child")
	installTestSubmoduleCheckoutHook(t, repo, `
printf 'staged by hook\n' > child/tracked.txt
git -C child add tracked.txt
printf 'working copy after staging\n' > child/tracked.txt
printf 'untracked hook file\n' > child/hook.txt
`)

	task := prepareForTest(t, repo)
	child := filepath.Join(task.Path, "child")
	if got := gitRun(t, child, "show", ":tracked.txt"); got != "staged by hook" {
		t.Fatalf("prepare changed the submodule's index: %q", got)
	}
	if got := readFile(t, filepath.Join(child, "tracked.txt")); got != "working copy after staging\n" {
		t.Fatalf("prepare changed the submodule's worktree: %q", got)
	}
	if got := readFile(t, filepath.Join(child, "hook.txt")); got != "untracked hook file\n" {
		t.Fatalf("prepare changed the untracked file: %q", got)
	}
	if got := gitRun(t, source, "status", "--porcelain"); got != "" {
		t.Fatalf("prepare changed the user's submodule: %s", got)
	}

	outcome, err := task.Finalize(worktreeTestLogger())
	if err != nil || !outcome.AutoCommitted {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}
	delivered := gitRun(t, repo, "rev-parse", outcome.Branch+":child")
	if got := gitRun(t, source, "show", delivered+":hook.txt"); got != "untracked hook file" {
		t.Fatalf("delivery lost the hook file: %q", got)
	}
	if got := gitRun(t, source, "show", delivered+":tracked.txt"); got != "working copy after staging" {
		t.Fatalf("delivery lost the existing modification: %q", got)
	}
	if got := gitRun(t, source, "status", "--porcelain"); got != "" {
		t.Fatalf("delivery changed the user's submodule: %s", got)
	}
}

func TestLocalWorktreePreservesDirtySubmoduleAtDifferentRevision(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	source := filepath.Join(repo, "child")
	old := gitRun(t, source, "rev-parse", "HEAD")
	writeTestSubmoduleFile(t, source, "tracked.txt", "updated pin content\n")
	gitRun(t, source, "add", "tracked.txt")
	gitRun(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "advance the source pin")
	installTestSubmoduleCheckoutHook(t, repo, "printf 'local hook edit\\n' > child/tracked.txt\n")

	envRoot := t.TempDir()
	_, err := PrepareLocalWorktree(LocalWorktreeParams{
		LocalPath: repo, EnvRoot: envRoot, AgentName: "J", TaskID: "11112222-3333-4444-5555-666677778884",
	}, worktreeTestLogger())
	path := filepath.Join(envRoot, localWorktreeDirName, "child")
	if err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("prepare = %v, want a recoverable checkout error", err)
	}
	if got := readFile(t, filepath.Join(path, "tracked.txt")); got != "local hook edit\n" {
		t.Fatalf("prepare removed or changed the hook edit: %q", got)
	}
	if got := gitRun(t, path, "rev-parse", "HEAD"); got != old {
		t.Fatalf("prepare moved the dirty submodule HEAD to %s", got)
	}
}

func TestLocalWorktreeAlignsCleanSubmoduleToReplayedGitlink(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	source := filepath.Join(repo, "child")
	writeTestSubmoduleFile(t, source, "tracked.txt", "updated pin content\n")
	gitRun(t, source, "add", "tracked.txt")
	gitRun(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "advance the source pin")
	want := gitRun(t, source, "rev-parse", "HEAD")
	installTestSubmoduleCheckoutHook(t, repo, "")

	task := prepareForTest(t, repo)
	child := filepath.Join(task.Path, "child")
	if got := gitRun(t, child, "rev-parse", "HEAD"); got != want {
		t.Fatalf("prepared checkout at %s, want replayed gitlink %s", got, want)
	}
	if got := readFile(t, filepath.Join(child, "tracked.txt")); got != "updated pin content\n" {
		t.Fatalf("prepared checkout has the wrong contents: %q", got)
	}
	if got := gitRun(t, task.Path, "rev-parse", "HEAD:child"); got != want {
		t.Fatalf("task baseline points to %s, want replayed gitlink %s", got, want)
	}
}

func TestLocalWorktreeRetainsHookCommitRecordedInBaseline(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, newTestRepo(t), "child", "child")
	source := filepath.Join(repo, "child")
	installTestSubmoduleCheckoutHook(t, repo, `
printf 'committed by hook\n' > child/hook.txt
git -C child add hook.txt
git -C child -c user.name=Test -c user.email=test@example.com commit -m 'commit hook file'
git add child
`)

	task := prepareForTest(t, repo)
	child := filepath.Join(task.Path, "child")
	hookCommit := gitRun(t, child, "rev-parse", "HEAD")
	if got := gitRun(t, task.Path, "rev-parse", task.BaseCommit+":child"); got != hookCommit {
		t.Fatalf("task baseline points at %s, want the hook commit %s", got, hookCommit)
	}
	if _, err := gitTry(t, source, "cat-file", "-e", hookCommit+"^{commit}"); err == nil {
		t.Fatal("fixture unexpectedly saved the hook commit in the source repository")
	}
	writeTestSubmoduleFile(t, task.Path, "deliver.txt", "agent delivery\n")
	outcome, err := task.Finalize(worktreeTestLogger())
	if err != nil || !outcome.AutoCommitted || outcome.Branch == "" {
		t.Fatalf("Finalize = %+v, %v", outcome, err)
	}

	gitRun(t, repo, "-c", "core.hooksPath="+t.TempDir(), "checkout", outcome.Branch)
	gitRun(t, repo, "submodule", "update", "--init", "--no-fetch")
	if got := readFile(t, filepath.Join(source, "hook.txt")); got != "committed by hook\n" {
		t.Fatalf("ordinary checkout did not restore the hook commit: %q", got)
	}
}

func TestLocalWorktreeAlignsNestedHookCheckoutUsingPreviousParent(t *testing.T) {
	t.Parallel()
	leafSource := newTestRepo(t)
	earlierLeaf := gitRun(t, leafSource, "rev-parse", "HEAD")
	writeTestSubmoduleFile(t, leafSource, "tracked.txt", "later submodule version\n")
	gitRun(t, leafSource, "add", "tracked.txt")
	gitRun(t, leafSource, "commit", "-m", "advance the nested pin")

	childSource := newTestRepo(t)
	addLocalTestSubmodule(t, childSource, leafSource, "leaf", "leaf")
	repo := newTestRepo(t)
	addLocalTestSubmodule(t, repo, childSource, "child", "child")
	gitRun(t, repo, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive")
	child := filepath.Join(repo, "child")
	gitRun(t, filepath.Join(child, "leaf"), "checkout", "--detach", earlierLeaf)
	gitRun(t, child, "add", "leaf")
	gitRun(t, child, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "restore the earlier nested pin")
	replayedChild := gitRun(t, child, "rev-parse", "HEAD")
	installTestSubmoduleCheckoutHook(t, repo, "git -c protocol.file.allow=always submodule update --init --recursive -- child\n")

	task := prepareForTest(t, repo)
	taskChild := filepath.Join(task.Path, "child")
	taskLeaf := filepath.Join(taskChild, "leaf")
	if got := gitRun(t, taskChild, "rev-parse", "HEAD"); got != replayedChild {
		t.Fatalf("task child points at %s, want the replayed commit %s", got, replayedChild)
	}
	if got := gitRun(t, taskLeaf, "rev-parse", "HEAD"); got != earlierLeaf {
		t.Fatalf("task leaf points at %s, want the earlier commit %s", got, earlierLeaf)
	}
	if got := gitRun(t, taskChild, "status", "--porcelain", "--ignore-submodules=none"); got != "" {
		t.Fatalf("the nested checkout does not match the replayed tree: %q", got)
	}
}
