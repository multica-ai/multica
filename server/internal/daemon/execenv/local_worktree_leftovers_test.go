package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Tests for #9788: an agent that commits its own work this turn delivers only
// those commits, and whatever it left uncommitted is saved under a hidden ref
// instead of being auto-committed onto the branch.

// leftoverRefs lists every leftover ref saved for branch.
func leftoverRefs(t *testing.T, repo, branch string) []string {
	t.Helper()
	out := gitRun(t, repo, "for-each-ref", "--format=%(refname)", leftoverRefPrefix+branch+"/")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// requireLeftovers asserts the outcome names one saved leftover ref for
// branch, that the ref's parent is what the branch delivered, and that it
// carries wantFiles.
func requireLeftovers(t *testing.T, repo string, outcome LocalWorktreeOutcome, branch string, wantFiles map[string]string) {
	t.Helper()
	if outcome.AutoCommitted {
		t.Error("AutoCommitted = true, want the leftovers kept off the branch")
	}
	if !strings.HasPrefix(outcome.LeftoverRef, leftoverRefPrefix+branch+"/") {
		t.Fatalf("LeftoverRef = %q, want one under %s%s/", outcome.LeftoverRef, leftoverRefPrefix, branch)
	}
	if refs := leftoverRefs(t, repo, branch); len(refs) != 1 || refs[0] != outcome.LeftoverRef {
		t.Fatalf("leftover refs = %v, want just %s", refs, outcome.LeftoverRef)
	}
	if parent, tip := gitRun(t, repo, "rev-parse", outcome.LeftoverRef+"^"), gitRun(t, repo, "rev-parse", branch); parent != tip {
		t.Errorf("leftover commit's parent = %s, want the delivered tip %s", parent, tip)
	}
	for name, want := range wantFiles {
		if got := gitRun(t, repo, "show", outcome.LeftoverRef+":"+name); got != strings.TrimSpace(want) {
			t.Errorf("%s in the leftovers = %q, want %q", name, got, strings.TrimSpace(want))
		}
	}
}

// The #9788 report: the agent commits only the intended change and leaves a
// generated file and a scratch helper behind. Before, the daemon committed
// both onto the branch with --no-verify; now the branch carries only the
// agent's commit, and the leftovers — untracked, modified and deleted files
// alike — are recoverable from the hidden ref.
func TestFinalizeKeepsLeftoversOffTheBranchWhenTheAgentCommitted(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "build/\n")
	gitRun(t, repo, "add", ".gitignore")
	gitRun(t, repo, "commit", "-m", "ignore build output")
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)

	writeFile(t, filepath.Join(wt.WorkDir, "doc.md"), "intended\n")
	gitRun(t, wt.Path, "add", "doc.md")
	gitRun(t, wt.Path, "commit", "-m", "docs: intended change")
	committed := gitRun(t, wt.Path, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(wt.WorkDir, "artifacts", "repro", "render.bin"), "generated media\n")
	writeFile(t, filepath.Join(wt.WorkDir, "upload_helper.py"), "print('one-off')\n")
	writeFile(t, filepath.Join(wt.WorkDir, "tracked.txt"), "edited but not committed\n")
	if err := os.Remove(filepath.Join(wt.WorkDir, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt.WorkDir, "build", "out.o"), "ignored\n")

	outcome := finalizeOK(t, wt)
	if outcome.Branch != wt.Branch {
		t.Fatalf("Branch = %q, want %q", outcome.Branch, wt.Branch)
	}
	if tip := gitRun(t, repo, "rev-parse", wt.Branch); tip != committed {
		t.Fatalf("branch tip = %s, want the agent's own commit %s: the leftovers were committed onto it", tip, committed)
	}
	if got := gitRun(t, repo, "show", wt.Branch+":tracked.txt"); got != "original" {
		t.Errorf("tracked.txt on the branch = %q, want the committed version", got)
	}
	if got := gitRun(t, repo, "show", wt.Branch+":keep.txt"); got != "keep" {
		t.Errorf("keep.txt on the branch = %q, want it still there", got)
	}

	requireLeftovers(t, repo, outcome, wt.Branch, map[string]string{
		"artifacts/repro/render.bin": "generated media",
		"upload_helper.py":           "print('one-off')",
		"tracked.txt":                "edited but not committed",
	})
	ref := outcome.LeftoverRef
	if _, err := gitTry(t, repo, "cat-file", "-e", ref+":keep.txt"); err == nil {
		t.Error("the leftovers lost the agent's uncommitted deletion of keep.txt")
	}
	if _, err := gitTry(t, repo, "cat-file", "-e", ref+":build/out.o"); err == nil {
		t.Error("a gitignored file was saved with the leftovers")
	}
	if !strings.HasSuffix(ref, "/"+taskKey(turnOneTask)) {
		t.Errorf("leftover ref %s is not named after the task", ref)
	}
	message := gitRun(t, repo, "log", "-1", "--format=%B", ref)
	for _, want := range []string{"left uncommitted", "artifacts/repro/render.bin", "git restore --source="} {
		if !strings.Contains(message, want) {
			t.Errorf("leftover commit message is missing %q:\n%s", want, message)
		}
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree still present after Finalize: %v", err)
	}
	// The ref is enough to get a file back.
	gitRun(t, repo, "restore", "--source="+ref, "--", "upload_helper.py")
	if got := readFile(t, filepath.Join(repo, "upload_helper.py")); got != "print('one-off')\n" {
		t.Errorf("restored helper = %q", got)
	}
}

// The repository's commit hooks are no longer bypassed for the leftovers,
// because nothing is committed for them at all: a pre-commit size guard that
// rejects the generated file sees only the agent's own commits.
func TestFinalizeDoesNotCommitLeftoversPastAPreCommitHook(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell hook")
	}
	repo := newTestRepo(t)
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	writeFile(t, hook, "#!/bin/sh\nif git diff --cached --name-only | grep -q '^artifacts/'; then echo 'artifacts are not allowed' >&2; exit 1; fi\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "doc.md"), "intended\n")
	gitRun(t, wt.Path, "add", "doc.md")
	gitRun(t, wt.Path, "commit", "-m", "docs: intended change")
	writeFile(t, filepath.Join(wt.WorkDir, "artifacts", "big.bin"), strings.Repeat("x", 1<<20))

	outcome := finalizeOK(t, wt)
	if files := gitRun(t, repo, "log", "--format=", "--name-only", wt.BaseCommit+".."+wt.Branch); files != "doc.md" {
		t.Fatalf("files the branch gained = %q, want just doc.md", files)
	}
	requireLeftovers(t, repo, outcome, wt.Branch, nil)
}

// An agent that made no commit still delivers everything it left: for it the
// uncommitted files ARE the work, and no leftover ref is written.
func TestFinalizeStillAutoCommitsWhenTheAgentMadeNoCommit(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "uncommitted work\n")

	outcome := finalizeOK(t, wt)
	if !outcome.AutoCommitted || outcome.LeftoverRef != "" {
		t.Fatalf("AutoCommitted = %v, LeftoverRef = %q; want the work committed onto the branch", outcome.AutoCommitted, outcome.LeftoverRef)
	}
	if got := gitRun(t, repo, "show", wt.Branch+":agent.txt"); got != "uncommitted work" {
		t.Errorf("agent.txt on the branch = %q", got)
	}
	if refs := leftoverRefs(t, repo, wt.Branch); len(refs) != 0 {
		t.Errorf("leftover refs written for a turn without commits: %v", refs)
	}
}

// A committed turn with nothing left over saves nothing.
func TestFinalizeSavesNoLeftoversForACleanCommittedTurn(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "work\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")

	outcome := finalizeOK(t, wt)
	if outcome.AutoCommitted || outcome.LeftoverRef != "" {
		t.Fatalf("AutoCommitted = %v, LeftoverRef = %q; want neither", outcome.AutoCommitted, outcome.LeftoverRef)
	}
	if refs := leftoverRefs(t, repo, wt.Branch); len(refs) != 0 {
		t.Errorf("leftover refs = %v, want none", refs)
	}
}

// On a continued branch "this turn" starts at the tip the previous turn
// delivered, and the leftovers are never replayed into the next turn: they
// are not part of the branch, and the user's own directory never had them.
func TestFinalizeKeepsLeftoversOffAContinuedBranch(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	first := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "one.txt"), "turn one\n")
	finalizeOK(t, first)

	second := prepareTurn(t, repo, "MUL-9788", turnTwoTask)
	if !second.Continued {
		t.Fatal("second turn did not continue the branch")
	}
	writeFile(t, filepath.Join(second.WorkDir, "two.txt"), "turn two\n")
	gitRun(t, second.Path, "add", "two.txt")
	gitRun(t, second.Path, "commit", "-m", "turn two")
	writeFile(t, filepath.Join(second.WorkDir, "scratch.log"), "scratch\n")
	outcome := finalizeOK(t, second)
	requireLeftovers(t, repo, outcome, second.Branch, map[string]string{"scratch.log": "scratch"})
	if !strings.HasSuffix(outcome.LeftoverRef, "/"+taskKey(turnTwoTask)) {
		t.Errorf("leftover ref %s is not named after the second turn", outcome.LeftoverRef)
	}

	third := prepareTurn(t, repo, "MUL-9788", turnThreeTask)
	if _, err := os.Stat(filepath.Join(third.WorkDir, "scratch.log")); !os.IsNotExist(err) {
		t.Errorf("the previous turn's leftover reappeared in the next turn: %v", err)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(third.WorkDir, name)); err != nil {
			t.Errorf("third turn is missing delivered %s: %v", name, err)
		}
	}
	finalizeOK(t, third)
}

// A turn that started on an unresolved merge keeps the old rule. Its
// uncommitted changes include the user's own replayed edits, which the branch
// is about to be recorded as carrying; saving those aside after a partial
// commit would record edits as delivered that the branch does not hold.
func TestFinalizeCommitsEverythingOnATurnThatStartedMidMerge(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user A\n")
	first := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "tracked.txt"), "agent B\n")
	finalizeOK(t, first)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user C\n")
	writeFile(t, filepath.Join(repo, "notes.txt"), "user's other edit\n")

	second := prepareTurn(t, repo, "MUL-9788", turnTwoTask)
	if len(second.ReplayConflicts) != 1 {
		t.Fatalf("expected a replay conflict, got %v", second.ReplayConflicts)
	}
	// The prompt states the exception from this same predicate.
	if first.CommitsAllLeftovers() || !second.CommitsAllLeftovers() {
		t.Fatalf("CommitsAllLeftovers: first turn %v, mid-merge turn %v; want false, true",
			first.CommitsAllLeftovers(), second.CommitsAllLeftovers())
	}
	// The agent resolves and commits only the conflicted file, then leaves
	// generated output behind.
	writeFile(t, filepath.Join(second.WorkDir, "tracked.txt"), "agent B and user C\n")
	gitRun(t, second.Path, "add", "tracked.txt")
	gitRun(t, second.Path, "reset", "-q", "--", "notes.txt")
	gitRun(t, second.Path, "commit", "-m", "resolve")
	writeFile(t, filepath.Join(second.WorkDir, "generated.bin"), "scratch output\n")

	outcome := finalizeOK(t, second)
	if outcome.LeftoverRef != "" || !outcome.AutoCommitted {
		t.Fatalf("AutoCommitted = %v, LeftoverRef = %q; want the user's replayed edit committed", outcome.AutoCommitted, outcome.LeftoverRef)
	}
	if got := gitRun(t, repo, "show", second.Branch+":notes.txt"); got != "user's other edit" {
		t.Errorf("notes.txt on the branch = %q, want the user's replayed edit", got)
	}
	// This is the behaviour the prompt has to warn about.
	if got := gitRun(t, repo, "show", second.Branch+":generated.bin"); got != "scratch output" {
		t.Errorf("generated.bin on the branch = %q, want it committed under the exception", got)
	}
}

// When the leftovers cannot be saved, nothing is lost: the worktree stays, as
// it does for an auto-commit that fails, and the delivery is not recorded.
func TestFinalizeKeepsTheWorktreeWhenLeftoversCannotBeSaved(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "work\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	committed := gitRun(t, wt.Path, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(wt.WorkDir, "scratch.log"), "scratch\n")
	// A ref sitting where the branch's leftover directory has to go makes
	// every ref below it impossible to create.
	gitRun(t, repo, "update-ref", strings.TrimSuffix(leftoverRefPrefix, "/")+"/"+wt.Branch, committed)

	outcome, err := wt.Finalize(worktreeTestLogger())
	if err == nil {
		t.Fatal("Finalize succeeded although the leftovers could not be saved")
	}
	if !strings.Contains(err.Error(), "left uncommitted") || outcome.PreservedPath != wt.Path {
		t.Fatalf("err = %v, PreservedPath = %q; want the worktree preserved", err, outcome.PreservedPath)
	}
	if got := readFile(t, filepath.Join(wt.WorkDir, "scratch.log")); got != "scratch\n" {
		t.Errorf("the leftover is gone from the preserved worktree: %q", got)
	}
	ref, _ := readUserStateRef(repo, wt.Branch)
	if record, recErr := readBranchRecord(repo, ref); recErr != nil || record.checkpoint == committed {
		t.Errorf("the delivery was recorded although its leftovers were not saved (record %+v, err %v)", record, recErr)
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// A task re-dispatched under the same id never overwrites what the first
// attempt left: the second save takes a name of its own.
func TestSaveLeftoversNeverOverwritesAnEarlierSave(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9788", turnOneTask)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "work\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	tip := gitRun(t, wt.Path, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(wt.WorkDir, "scratch.log"), "first\n")

	first, err := wt.saveLeftovers(tip, worktreeTestLogger())
	if err != nil || first == "" {
		t.Fatalf("first save: %q, %v", first, err)
	}
	firstCommit := gitRun(t, repo, "rev-parse", first)
	writeFile(t, filepath.Join(wt.WorkDir, "scratch.log"), "second\n")
	second, err := wt.saveLeftovers(tip, worktreeTestLogger())
	if err != nil || second == "" || second == first {
		t.Fatalf("second save: %q, %v; want a ref other than %s", second, err, first)
	}
	if got := gitRun(t, repo, "rev-parse", first); got != firstCommit {
		t.Errorf("the first save was overwritten: %s, want %s", got, firstCommit)
	}
	if got := gitRun(t, repo, "show", second+":scratch.log"); got != "second" {
		t.Errorf("second save holds %q", got)
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// Leftover refs go with their branch: Multica drops them when it drops the
// branch, and a later prepare prunes those of a branch the user deleted.
func TestLeftoverRefsAreDroppedWithTheirBranch(t *testing.T) {
	t.Parallel()
	committedTurnWithLeftovers := func(t *testing.T, repo, key string) string {
		t.Helper()
		wt := prepareTurn(t, repo, key, turnOneTask)
		writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "work\n")
		gitRun(t, wt.Path, "add", "-A")
		gitRun(t, wt.Path, "commit", "-m", "work")
		writeFile(t, filepath.Join(wt.WorkDir, "scratch.log"), "scratch\n")
		if outcome := finalizeOK(t, wt); outcome.LeftoverRef == "" {
			t.Fatal("no leftovers saved")
		}
		return wt.Branch
	}

	t.Run("dropBranch", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(t)
		branch := committedTurnWithLeftovers(t, repo, "MUL-9788")
		dropBranch(repo, branch, worktreeTestLogger())
		if refs := leftoverRefs(t, repo, branch); len(refs) != 0 {
			t.Errorf("leftover refs survived dropBranch: %v", refs)
		}
	})

	t.Run("pruned after the user deletes the branch", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(t)
		branch := committedTurnWithLeftovers(t, repo, "MUL-9788")
		kept := committedTurnWithLeftovers(t, repo, "MUL-1")
		gitRun(t, repo, "branch", "-D", branch)

		next := prepareTurn(t, repo, "MUL-2", turnTwoTask)
		if refs := leftoverRefs(t, repo, branch); len(refs) != 0 {
			t.Errorf("leftover refs of a deleted branch survived the next prepare: %v", refs)
		}
		if refs := leftoverRefs(t, repo, kept); len(refs) != 1 {
			t.Errorf("leftover refs of a live branch were pruned: %v", refs)
		}
		finalizeOK(t, next)
	})
}
