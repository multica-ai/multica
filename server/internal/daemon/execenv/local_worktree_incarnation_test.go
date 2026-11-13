package execenv

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareLocalWorktreePreservesSourceSnapshot(t *testing.T) {
	for _, shape := range []string{"clean", "tracked", "staged", "untracked"} {
		t.Run(shape, func(t *testing.T) {
			repo := newTestRepo(t)
			head := gitRun(t, repo, "rev-parse", "HEAD")
			if shape == "tracked" || shape == "staged" {
				writeFile(t, filepath.Join(repo, "tracked.txt"), "staged draft\n")
				if shape == "staged" {
					gitRun(t, repo, "add", "tracked.txt")
					writeFile(t, filepath.Join(repo, "tracked.txt"), "newer unstaged draft\n")
				}
			}
			if shape == "untracked" {
				writeFile(t, filepath.Join(repo, "draft.txt"), "untracked draft\n")
			}
			status := gitRun(t, repo, "status", "--porcelain")
			before, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			tracked := readFile(t, filepath.Join(repo, "tracked.txt"))
			wt := prepareTurn(t, repo, "MUL-9826", turnOneTask)
			after, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("prepare changed source index bytes")
			}
			if got := readFile(t, filepath.Join(repo, "tracked.txt")); got != tracked {
				t.Error("prepare changed source file")
			}
			if got := gitRun(t, repo, "status", "--porcelain"); got != status {
				t.Error("prepare changed source status")
			}
			if got := gitRun(t, wt.Path, "rev-parse", "HEAD"); got != wt.BaseCommit {
				t.Error("task HEAD differs from BaseCommit")
			}
			if shape == "clean" {
				if wt.BaseCommit != head {
					t.Errorf("clean BaseCommit = %s, want source HEAD %s", wt.BaseCommit, head)
				}
			} else {
				if wt.BaseCommit == head {
					t.Error("dirty snapshot has no distinct baseline")
				}
				if got := gitRun(t, wt.Path, "rev-parse", wt.BaseCommit+"^"); got != head {
					t.Error("dirty baseline is not parented at source HEAD")
				}
				if got := readFile(t, filepath.Join(wt.Path, "tracked.txt")); got != tracked {
					t.Error("task did not capture source tracked file")
				}
				if shape == "untracked" && readFile(t, filepath.Join(wt.Path, "draft.txt")) != "untracked draft\n" {
					t.Error("task did not capture untracked file")
				}
			}
			if out := finalizeOK(t, wt); out.Branch != "" {
				t.Error("read-only task left a branch")
			}
			after, err = os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || readFile(t, filepath.Join(repo, "tracked.txt")) != tracked {
				t.Error("finalize changed the source index or tracked file")
			}
			if shape == "untracked" && readFile(t, filepath.Join(repo, "draft.txt")) != "untracked draft\n" {
				t.Error("finalize changed the source untracked file")
			}
			if got := gitRun(t, repo, "rev-parse", "HEAD"); got != head {
				t.Error("task moved source HEAD")
			}
		})
	}
}

func TestPrepareLocalWorktreeRejectsLostBranchIncarnation(t *testing.T) {
	// Freeze the dates so branch recreation cannot be distinguished by timestamps.
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	for _, change := range []string{"recreated at checkpoint", "expired", "missing", "copied", "renamed", "later creation boundary", "tokenless record"} {
		t.Run(change, func(t *testing.T) {
			repo := newTestRepo(t)
			first := prepareTurn(t, repo, "MUL-9826", turnOneTask)
			writeFile(t, filepath.Join(first.Path, "agent.txt"), "delivered\n")
			finalizeOK(t, first)
			tip := gitRun(t, repo, "rev-parse", first.Branch)
			state := gitRun(t, repo, "rev-parse", userStateRef(first.Branch))
			switch change {
			case "recreated at checkpoint":
				gitRun(t, repo, "branch", "-D", first.Branch)
				gitRun(t, repo, "branch", first.Branch, tip)
			case "expired":
				gitRun(t, repo, "reflog", "expire", "--expire=all", "refs/heads/"+first.Branch)
			case "missing":
				if err := os.Remove(filepath.Join(repo, ".git", "logs", "refs", "heads", filepath.FromSlash(first.Branch))); err != nil {
					t.Fatal(err)
				}
			case "copied":
				gitRun(t, repo, "branch", "-c", first.Branch, "copy")
				gitRun(t, repo, "branch", "-D", first.Branch)
				gitRun(t, repo, "branch", "-c", "copy", first.Branch)
			case "renamed":
				gitRun(t, repo, "branch", "-m", first.Branch, "renamed")
				gitRun(t, repo, "branch", "-m", "renamed", first.Branch)
			case "later creation boundary":
				gitRun(t, repo, "update-ref", "refs/heads/"+first.Branch, tip+"^")
				gitRun(t, repo, "update-ref", "-m", "branch: Created from "+tip, "refs/heads/"+first.Branch, tip)
			case "tokenless record":
				args := []string{"commit-tree", state + "^{tree}", "-p", state + "^1", "-p", tip, "-m", "legacy record\n\nMultica-Workspace: " + testBranchOwner.WorkspaceID + "\nMultica-Agent: " + testBranchOwner.AgentID + "\nMultica-Conversation: " + testBranchOwner.ConversationID}
				legacy := gitRun(t, repo, args...)
				gitRun(t, repo, "update-ref", userStateRef(first.Branch), legacy)
				state = legacy
			}
			second := prepareTurn(t, repo, "MUL-9826", turnTwoTask)
			if second.Branch == first.Branch || second.Continued {
				t.Error("adopted branch without valid incarnation evidence")
			}
			finalizeOK(t, second)
			if got := gitRun(t, repo, "rev-parse", first.Branch); got != tip {
				t.Error("replacement branch moved")
			}
			if got := gitRun(t, repo, "rev-parse", userStateRef(first.Branch)); got != state {
				t.Error("replacement branch record changed")
			}
		})
	}
}

func TestLocalWorktreeRefusesChangedIncarnationBeforeDelivery(t *testing.T) {
	for _, evidence := range []string{"recreated", "expired", "mismatched token"} {
		t.Run(evidence, func(t *testing.T) {
			for _, action := range []string{"readonly finalize", "dirty finalize", "record", "repair", "discard"} {
				t.Run(action, func(t *testing.T) {
					repo := newTestRepo(t)
					wt := prepareTurn(t, repo, "MUL-9826", turnOneTask)
					state := gitRun(t, repo, "rev-parse", userStateRef(wt.Branch))
					tip := wt.BaseCommit
					if action == "repair" {
						gitRun(t, wt.Path, "checkout", "--detach")
						writeFile(t, filepath.Join(wt.Path, "agent.txt"), "keep this work\n")
						gitRun(t, wt.Path, "add", "-A")
						gitRun(t, wt.Path, "commit", "-m", "off branch")
					} else if action == "dirty finalize" {
						writeFile(t, filepath.Join(wt.Path, "agent.txt"), "keep this work\n")
					}
					switch evidence {
					case "recreated":
						// update-ref can replace a ref even while a worktree has it checked out.
						gitRun(t, repo, "update-ref", "-d", "refs/heads/"+wt.Branch)
						gitRun(t, repo, "branch", wt.Branch, tip)
					case "expired":
						gitRun(t, repo, "reflog", "expire", "--expire=all", "refs/heads/"+wt.Branch)
					case "mismatched token":
						wt.incarnation = "another-creation"
					}
					switch action {
					case "discard":
						wt.Discard(worktreeTestLogger())
					case "record":
						if err := wt.recordState(tip, worktreeTestLogger()); err == nil {
							t.Error("recorded replacement incarnation")
						}
					case "repair":
						if problem := wt.CheckDelivery(); problem != nil {
							t.Error("offered repair that overwrites replacement branch")
						}
						fallthrough
					default:
						out, err := wt.Finalize(worktreeTestLogger())
						if err == nil || out.Branch != "" || out.PreservedPath != wt.Path || out.AutoCommitted {
							t.Errorf("Finalize = %+v, %v; want refusal preserving worktree", out, err)
						}
					}
					if _, err := os.Stat(wt.Path); err != nil {
						t.Errorf("worktree was not preserved: %v", err)
					}
					if action == "dirty finalize" || action == "repair" {
						if got := readFile(t, filepath.Join(wt.Path, "agent.txt")); got != "keep this work\n" {
							t.Error("refusal changed the agent's work")
						}
					}
					if got := gitRun(t, repo, "rev-parse", wt.Branch); got != tip {
						t.Error("replacement branch changed")
					}
					if got := gitRun(t, repo, "rev-parse", userStateRef(wt.Branch)); got != state {
						t.Error("replacement branch record changed")
					}
				})
			}
		})
	}
}

func TestPrepareLocalWorktreeBusyBeforeFirstCommit(t *testing.T) {
	repo := newTestRepo(t)
	first := prepareTurn(t, repo, "MUL-9826", turnOneTask)
	tip := gitRun(t, repo, "rev-parse", first.Branch)
	state := gitRun(t, repo, "rev-parse", userStateRef(first.Branch))
	writeFile(t, filepath.Join(first.Path, "pending.txt"), "live task\n")
	// The clean branch now qualifies as merged, but it is still held by a live
	// task. Restart must not move it even though the user's HEAD advanced.
	writeFile(t, filepath.Join(repo, "new-source.txt"), "new source commit\n")
	gitRun(t, repo, "add", "new-source.txt")
	gitRun(t, repo, "commit", "-m", "user continued working")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	seen := map[string]bool{first.Branch: true}
	for i := 0; i < 3; i++ {
		sibling := prepareTurn(t, repo, "MUL-9826", turnTwoTask)
		if seen[sibling.Branch] {
			t.Fatal("redispatch reused a live branch")
		}
		seen[sibling.Branch] = true
		if sibling.BaseCommit != head || sibling.tracksState {
			t.Error("sibling lost base or records conversation state")
		}
		if got := gitRun(t, repo, "rev-parse", first.Branch); got != tip {
			t.Error("sibling moved live branch")
		}
		if got := gitRun(t, repo, "rev-parse", userStateRef(first.Branch)); got != state {
			t.Error("sibling changed conversation record")
		}
		defer sibling.Discard(worktreeTestLogger())
	}
	if got := readFile(t, filepath.Join(first.Path, "pending.txt")); got != "live task\n" {
		t.Error("sibling changed live edits")
	}
	finalizeOK(t, first)
}

func TestLocalWorktreeIncarnationCrossesJSON(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareTurn(t, repo, "MUL-9826", turnOneTask)
	data, err := json.Marshal(wt)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if token, ok := wire["incarnation"].(string); !ok || token == "" {
		t.Fatal("JSON lost branch incarnation")
	}
	var transferred LocalWorktree
	if err := json.Unmarshal(data, &transferred); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt.Path, "agent.txt"), "transferred\n")
	finalizeOK(t, &transferred)
	second := prepareTurn(t, repo, "MUL-9826", turnTwoTask)
	if !second.Continued {
		t.Error("transferred record did not authorize continuation")
	}
	finalizeOK(t, second)
}

// Reflog creation is explicit even for repositories that disable automatic
// reflogs, and each same-name creation gets a new token even within one second.
func TestCreateBranchWorktreeUsesFreshIncarnationWithoutAutomaticReflogs(t *testing.T) {
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	repo := newTestRepo(t)
	gitRun(t, repo, "config", "core.logAllRefUpdates", "false")
	gitRun(t, repo, "config", "user.email", "")
	gitRun(t, repo, "config", "user.name", "")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	branch := "agent/j/fresh"
	path := filepath.Join(t.TempDir(), "wt")
	previous := ""
	for i := 0; i < 2; i++ {
		token, out, err := createBranchWorktree(repo, path, branch, head)
		if err != nil {
			t.Fatalf("createBranchWorktree: %s: %v", out, err)
		}
		if token == "" || token == previous {
			t.Fatal("branch creation reused or omitted its incarnation")
		}
		if !branchHasIncarnation(repo, branch, token) {
			t.Fatal("explicit creation reflog is not readable")
		}
		previous = token
		if err := removeLocalWorktreeDir(repo, path, worktreeTestLogger()); err != nil {
			t.Fatal(err)
		}
		dropBranch(repo, branch, token, worktreeTestLogger())
		if _, err := gitTry(t, repo, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
			t.Fatal("owned branch was not removed")
		}
	}
}

func TestCreateBranchWorktreeFailurePreservesExistingRefs(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	head := gitRun(t, repo, "rev-parse", "HEAD")
	occupied := t.TempDir()
	writeFile(t, filepath.Join(occupied, "keep.txt"), "occupied\n")
	branch := "agent/j/attachment-failure"
	if _, _, err := createBranchWorktree(repo, occupied, branch, head); err == nil {
		t.Fatal("attached worktree over occupied directory")
	}
	if _, err := gitTry(t, repo, "show-ref", "--verify", "refs/heads/"+branch); err == nil {
		t.Error("failed attachment left its own unused branch")
	}
	if got := readFile(t, filepath.Join(occupied, "keep.txt")); got != "occupied\n" {
		t.Error("attachment failure changed existing directory")
	}
	gitRun(t, repo, "branch", branch, head)
	if _, _, err := createBranchWorktree(repo, filepath.Join(t.TempDir(), "wt"), branch, head); err == nil {
		t.Fatal("created over existing ref")
	}
	if got := gitRun(t, repo, "rev-parse", branch); got != head {
		t.Error("failed creation changed existing ref")
	}
}
