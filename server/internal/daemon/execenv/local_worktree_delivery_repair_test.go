package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// applyRepairSteps runs exactly the git sequence the repair prompt hands the
// agent, so these tests prove the instruction itself, not a paraphrase of it.
func applyRepairSteps(t *testing.T, wt *LocalWorktree, problem *DeliveryProblem) {
	t.Helper()
	for _, step := range problem.RepairSteps() {
		gitRun(t, wt.Path, step...)
	}
}

// requireRepairable asserts CheckDelivery predicts the refusal Finalize would
// make, then repairs it and checks Finalize records the delivery with the
// agent's files intact.
func requireRepairable(t *testing.T, repo string, wt *LocalWorktree, wantReason string, wantFiles map[string]string) {
	t.Helper()
	problem := wt.CheckDelivery()
	if problem == nil {
		t.Fatal("CheckDelivery found nothing to repair, but Finalize would refuse this delivery")
	}
	if !strings.Contains(problem.Reason, wantReason) {
		t.Errorf("Reason = %q, want it to contain %q", problem.Reason, wantReason)
	}
	if problem.Branch != wt.Branch || problem.BaseCommit != wt.BaseCommit {
		t.Errorf("problem names branch %q base %q, want %q %q", problem.Branch, problem.BaseCommit, wt.Branch, wt.BaseCommit)
	}

	applyRepairSteps(t, wt, problem)
	if again := wt.CheckDelivery(); again != nil {
		t.Fatalf("the repair steps did not fix the delivery: %s", again.Reason)
	}

	outcome := finalizeOK(t, wt)
	if outcome.Branch != wt.Branch {
		t.Fatalf("outcome.Branch = %q, want %q", outcome.Branch, wt.Branch)
	}
	if _, err := gitTry(t, repo, "merge-base", "--is-ancestor", wt.BaseCommit, wt.Branch); err != nil {
		t.Error("the delivered branch does not contain the commit this turn started from")
	}
	for name, want := range wantFiles {
		if got := gitRun(t, repo, "show", wt.Branch+":"+name); got != strings.TrimSpace(want) {
			t.Errorf("%s on the branch = %q, want %q", name, got, strings.TrimSpace(want))
		}
	}
}

// #9404 shape one: the agent squashes its commits and drops the empty baseline
// along with them. Every change is still there; only the ancestry is wrong.
func TestCheckDeliveryRepairsASquashThatDroppedTheBaseline(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	// `git reset --soft <baseline>^ && git commit`, as in the report.
	gitRun(t, wt.Path, "reset", "--soft", wt.BaseCommit+"^")
	gitRun(t, wt.Path, "commit", "-m", "squashed")

	requireRepairable(t, repo, wt, "no longer contains", map[string]string{"agent.txt": "turn one"})
}

// #9404 shape two: the work landed on a sibling line while the task branch
// still sits on its baseline.
func TestCheckDeliveryRepairsWorkCommittedOffTheBranch(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	gitRun(t, wt.Path, "checkout", "--quiet", "--detach", head)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "on a sibling\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work on the main line")

	requireRepairable(t, repo, wt, "did not deliver onto its own branch", map[string]string{"agent.txt": "on a sibling"})
}

// The common #8541 shape: the agent checked out a feature branch for its PR.
// Its own commits there extend the task branch; only HEAD is elsewhere.
func TestCheckDeliveryRepairsWorkOnAFeatureBranch(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	gitRun(t, wt.Path, "checkout", "--quiet", "-b", "feature/x")
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "feature\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "feature work")
	// And some leftovers it never committed.
	writeFile(t, filepath.Join(wt.WorkDir, "tracked.txt"), "edited\n")

	requireRepairable(t, repo, wt, "HEAD is not on branch", map[string]string{
		"agent.txt":   "feature",
		"tracked.txt": "edited",
	})
}

// Uncommitted changes on a detached HEAD would be auto-committed onto the
// detached line, not the branch — the same refusal, predicted before it happens.
func TestCheckDeliveryRepairsUncommittedWorkOnADetachedHead(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	gitRun(t, wt.Path, "checkout", "--quiet", "--detach")
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "detached\n")

	requireRepairable(t, repo, wt, "HEAD is not on branch", map[string]string{"agent.txt": "detached"})
}

// A follow-up turn that squashes the whole branch — earlier turns included —
// back onto the user's HEAD. The repair keeps the follow-up's own starting
// point, so the earlier turns stay in the branch's history.
func TestCheckDeliveryRepairsAFollowUpThatSquashedTheWholeBranch(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")

	first := prepareTurn(t, repo, "MUL-9404", turnOneTask)
	writeFile(t, filepath.Join(first.WorkDir, "one.txt"), "turn one\n")
	finalizeOK(t, first)

	second := prepareTurn(t, repo, "MUL-9404", turnTwoTask)
	if !second.Continued {
		t.Fatal("second turn did not continue the branch")
	}
	writeFile(t, filepath.Join(second.WorkDir, "two.txt"), "turn two\n")
	gitRun(t, second.Path, "add", "-A")
	gitRun(t, second.Path, "commit", "-m", "turn two")
	gitRun(t, second.Path, "reset", "--soft", head)
	gitRun(t, second.Path, "commit", "-m", "everything in one")

	requireRepairable(t, repo, second, "no longer contains", map[string]string{
		"one.txt": "turn one",
		"two.txt": "turn two",
	})
}

// Work done on a line that never saw the baseline's content is not repaired
// either: here the baseline carries the user's uncommitted edit, the agent
// worked on a detached checkout of their HEAD, and re-committing that tree on
// the baseline would silently revert the user's edit.
func TestCheckDeliveryDoesNotOfferARepairThatRevertsTheUsersEdits(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	gitRun(t, wt.Path, "checkout", "--quiet", "--force", "--detach", head)
	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "off the baseline\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work without the user's edit")

	if problem := wt.CheckDelivery(); problem != nil {
		t.Fatalf("CheckDelivery offered a repair that would revert tracked.txt: %s", problem.Reason)
	}
	if _, err := wt.Finalize(worktreeTestLogger()); err == nil {
		t.Fatal("Finalize must still refuse this delivery")
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// A squash that keeps the user's replayed edit is still repairable.
func TestCheckDeliveryRepairsASquashThatKeptTheUsersEdits(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	gitRun(t, wt.Path, "reset", "--soft", wt.BaseCommit+"^")
	gitRun(t, wt.Path, "commit", "-m", "squashed")

	requireRepairable(t, repo, wt, "no longer contains", map[string]string{
		"agent.txt":   "turn one",
		"tracked.txt": "user work in progress",
	})
}

// MUL-8139 review: the same file carries both sides. The user's uncommitted
// edit changed one line; the agent, working on a checkout without it, changed
// another. Path-level checks see "both touched it" — only the content shows the
// user's line would be reverted, so no repair may be offered.
func TestCheckDeliveryDoesNotOfferARepairThatRevertsAUserEditInTheSameFile(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user=old\nagent=old\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "two independent settings")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user=NEW\nagent=old\n")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	gitRun(t, wt.Path, "checkout", "--quiet", "--force", "--detach", head)
	writeFile(t, filepath.Join(wt.Path, "tracked.txt"), "user=old\nagent=NEW\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "agent edit without the user's edit")

	if problem := wt.CheckDelivery(); problem != nil {
		t.Fatalf("CheckDelivery offered a repair that would revert user=NEW: %s", problem.Reason)
	}
	if _, err := wt.Finalize(worktreeTestLogger()); err == nil {
		t.Fatal("Finalize must still refuse this delivery")
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// The legitimate same-file case stays repairable: the agent built on the
// user's edit, changed another line of the same file, then squashed the
// baseline away. Its file already contains the user's change. (Edits on
// touching lines conflict in the three-way check and are refused instead —
// the safe direction, covered by the same-file revert test above.)
func TestCheckDeliveryRepairsASquashThatAlsoEditedTheUsersFile(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user=old\nctx1\nctx2\nctx3\nagent=old\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "two independent settings")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user=NEW\nctx1\nctx2\nctx3\nagent=old\n")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	writeFile(t, filepath.Join(wt.Path, "tracked.txt"), "user=NEW\nctx1\nctx2\nctx3\nagent=NEW\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	gitRun(t, wt.Path, "reset", "--soft", wt.BaseCommit+"^")
	gitRun(t, wt.Path, "commit", "-m", "squashed")

	requireRepairable(t, repo, wt, "no longer contains", map[string]string{"tracked.txt": "user=NEW\nctx1\nctx2\nctx3\nagent=NEW"})
}

// userChmodBaseline commits run.sh without the executable bit, then makes it
// executable as the user's uncommitted change, which the turn's baseline
// records as mode 100755. It returns the repo, the pre-chmod HEAD and the turn.
func userChmodBaseline(t *testing.T) (string, string, *LocalWorktree) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix executable bits")
	}
	repo := newTestRepo(t)
	gitRun(t, repo, "config", "core.filemode", "true")
	writeFile(t, filepath.Join(repo, "run.sh"), "#!/bin/sh\nexit 0\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "script before the user's chmod")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	if err := os.Chmod(filepath.Join(repo, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)
	if entry := gitRun(t, repo, "ls-tree", wt.BaseCommit, "--", "run.sh"); !strings.HasPrefix(entry, "100755 ") {
		t.Fatalf("baseline did not record the user's chmod: %s", entry)
	}
	return repo, head, wt
}

// MUL-8139 review: a mode-only change carries no content difference, so a
// content check alone would offer a repair that delivers run.sh as 100644 —
// a silent revert of the user's chmod +x.
func TestCheckDeliveryDoesNotOfferARepairThatRevertsAUsersChmod(t *testing.T) {
	t.Parallel()
	repo, head, wt := userChmodBaseline(t)
	gitRun(t, wt.Path, "checkout", "--quiet", "--force", "--detach", head)
	writeFile(t, filepath.Join(wt.Path, "agent.txt"), "agent's separate change\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "agent work without the user's chmod")

	if problem := wt.CheckDelivery(); problem != nil {
		t.Fatalf("CheckDelivery offered a repair that would revert run.sh to 100644: %s", problem.Reason)
	}
	if _, err := wt.Finalize(worktreeTestLogger()); err == nil {
		t.Fatal("Finalize must still refuse this delivery")
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// A squash keeps the executable bit in the agent's files, so it stays
// repairable, and the delivered script is still 100755.
func TestCheckDeliveryRepairsASquashThatKeptTheUsersChmod(t *testing.T) {
	t.Parallel()
	repo, _, wt := userChmodBaseline(t)
	writeFile(t, filepath.Join(wt.Path, "agent.txt"), "work\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "work")
	gitRun(t, wt.Path, "reset", "--soft", wt.BaseCommit+"^")
	gitRun(t, wt.Path, "commit", "-m", "squashed")

	requireRepairable(t, repo, wt, "no longer contains", map[string]string{"agent.txt": "work"})
	if entry := gitRun(t, repo, "ls-tree", wt.Branch, "--", "run.sh"); !strings.HasPrefix(entry, "100755 ") {
		t.Fatalf("delivered run.sh lost the user's executable bit: %s", entry)
	}
}

// The MUL-6881 case stays a plain refusal: a run that reset back to the user's
// own HEAD discarded what the baseline carried, and re-committing that tree on
// top of the baseline would deliver a revert of the user's edits.
func TestCheckDeliveryDoesNotOfferToRepairAResetPastTheBaseline(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user work in progress\n")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)

	writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "turn one\n")
	gitRun(t, wt.Path, "reset", "--hard", head)

	if problem := wt.CheckDelivery(); problem != nil {
		t.Fatalf("CheckDelivery offered to repair a reset past the baseline: %s", problem.Reason)
	}
	if _, err := wt.Finalize(worktreeTestLogger()); err == nil || !strings.Contains(err.Error(), "no longer contains") {
		t.Fatalf("Finalize must still refuse this delivery, got %v", err)
	}
	_ = removeLocalWorktreeDir(repo, wt.Path, worktreeTestLogger())
}

// Ordinary turns are never sent to repair.
func TestCheckDeliveryAcceptsOrdinaryTurns(t *testing.T) {
	t.Parallel()

	t.Run("committed on the branch", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(t)
		wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)
		writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "x\n")
		gitRun(t, wt.Path, "add", "-A")
		gitRun(t, wt.Path, "commit", "-m", "work")
		if problem := wt.CheckDelivery(); problem != nil {
			t.Fatalf("unexpected problem: %s", problem.Reason)
		}
		finalizeOK(t, wt)
	})

	t.Run("left uncommitted on the branch", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(t)
		wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)
		writeFile(t, filepath.Join(wt.WorkDir, "agent.txt"), "x\n")
		if problem := wt.CheckDelivery(); problem != nil {
			t.Fatalf("unexpected problem: %s", problem.Reason)
		}
		finalizeOK(t, wt)
	})

	t.Run("read-only", func(t *testing.T) {
		t.Parallel()
		repo := newTestRepo(t)
		wt := prepareTurn(t, repo, "MUL-9404", turnOneTask)
		if problem := wt.CheckDelivery(); problem != nil {
			t.Fatalf("unexpected problem: %s", problem.Reason)
		}
		finalizeOK(t, wt)
	})

	t.Run("nil worktree", func(t *testing.T) {
		var wt *LocalWorktree
		if wt.CheckDelivery() != nil {
			t.Fatal("nil worktree reported a problem")
		}
	})
}
