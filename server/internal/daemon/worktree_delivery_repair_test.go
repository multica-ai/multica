package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// #9404: a worktree turn is told up front where its delivery has to end, so
// the squash / feature-branch habits that the daemon cannot record are avoided
// rather than repaired.
func TestWorktreeBranchBlock(t *testing.T) {
	task := Task{IssueID: "issue-1"}

	if out := BuildPrompt(task, "claude"); strings.Contains(out, "Your task branch") {
		t.Fatal("a non-worktree turn got the task-branch block")
	}

	out := BuildPrompt(task, "claude", WithWorktreeBranch("agent/j/mul-9404", "abc123def"))
	for _, want := range []string{
		"## Your task branch",
		"`agent/j/mul-9404`",
		"`abc123def`",
		"Do not squash, amend, rebase or reset past `abc123def`",
		"git push origin HEAD:<name>",
		// #9788: the agent is told what happens to what it leaves uncommitted,
		// on both sides of the rule.
		"If you commit, only your commits are delivered",
		"does not reach the branch",
		"If you make no commit, everything you leave that is not gitignored is committed onto the branch for you",
		"without running the repository's commit hooks",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q:\n%s", want, out)
		}
	}

	// #9870 review: a turn that started mid-merge commits everything it leaves
	// even after the agent's own commits, so it must not be promised the
	// usual rule.
	pending := BuildPrompt(task, "claude", WithWorktreeBranch("agent/j/mul-9404", "abc123def"), WithWorktreeCommitsAllLeftovers())
	for _, want := range []string{
		"## Your task branch",
		"everything you leave that is not gitignored is committed onto the branch for you — even if you have made commits of your own",
		"Delete scratch files and generated output you do not want delivered",
	} {
		if !strings.Contains(pending, want) {
			t.Errorf("mid-merge prompt missing %q:\n%s", want, pending)
		}
	}
	if strings.Contains(pending, "If you commit, only your commits are delivered") {
		t.Errorf("mid-merge prompt promises the rule Finalize does not apply to it:\n%s", pending)
	}
}

// The repair prompt renders exactly execenv's RepairSteps (which the execenv
// tests run against real repositories), and quotes them so a shell receives the
// same argv — the commit message carries an apostrophe. Both shapes of the
// sequence are checked: a turn that made commits of its own gets the one that
// delivers only those commits (#9788).
func TestBuildWorktreeDeliveryRepairPrompt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		committedOnly bool
		want          []string
	}{
		{name: "no commits of its own", want: []string{"keeps every file exactly as it is now", "git add -A"}},
		{name: "committed this turn", committedOnly: true, want: []string{"exactly what your commits contain", "puts back what you had staged", "saved aside rather than delivered", "git read-tree fff000", "git read-tree 4b825dc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problem := &execenv.DeliveryProblem{
				Branch:        "agent/j/mul-9404",
				BaseCommit:    "abc123def",
				Tip:           "fff000",
				Reason:        "the delivered commit fff000 no longer contains abc123de, the commit this turn started from",
				CommittedOnly: tc.committedOnly,
			}
			if tc.committedOnly {
				problem.IndexTree = "4b825dc"
			}
			checkWorktreeDeliveryRepairPrompt(t, problem, tc.want)
		})
	}
}

func checkWorktreeDeliveryRepairPrompt(t *testing.T, problem *execenv.DeliveryProblem, extra []string) {
	t.Helper()
	out := BuildWorktreeDeliveryRepairPrompt(problem)
	for _, want := range append([]string{problem.Reason, "`agent/j/mul-9404`", "git diff --cached abc123def", "no new comments or status updates"}, extra...) {
		if !strings.Contains(out, want) {
			t.Errorf("repair prompt missing %q:\n%s", want, out)
		}
	}

	start := strings.Index(out, "```sh\n")
	end := strings.Index(out[start+6:], "```")
	if start < 0 || end < 0 {
		t.Fatalf("repair prompt has no sh block:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out[start+6:start+6+end]), "\n")
	steps := problem.RepairSteps()
	if len(lines) != len(steps) {
		t.Fatalf("rendered %d commands, want %d:\n%s", len(lines), len(steps), out)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available to check quoting")
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "git ") {
			t.Fatalf("line %d is not a git command: %q", i, line)
		}
		// Let the shell parse the arguments and echo them back one per line.
		got, err := exec.Command("sh", "-c", `printf '%s\n' `+strings.TrimPrefix(line, "git ")).Output()
		if err != nil {
			t.Fatalf("sh could not parse %q: %v", line, err)
		}
		if want := strings.Join(steps[i], "\n"); strings.TrimSuffix(string(got), "\n") != want {
			t.Errorf("line %d parses to %q, want %q", i, got, want)
		}
	}
}

func TestMergeWorktreeRepairResult(t *testing.T) {
	original := agent.Result{
		Status:    "completed",
		Output:    "the real reply",
		SessionID: "s1",
		Usage:     map[string]agent.TokenUsage{"m": {InputTokens: 10}},
	}

	t.Run("repair completed", func(t *testing.T) {
		repair := agent.Result{Status: "completed", Output: "fixed the branch", SessionID: "s2",
			Usage: map[string]agent.TokenUsage{"m": {InputTokens: 5}}}
		got, tools := mergeWorktreeRepairResult(original, 3, repair, 2, nil)
		if got.Output != "the real reply" || got.Status != "completed" {
			t.Errorf("the task reply was replaced: %+v", got)
		}
		if got.SessionID != "s2" {
			t.Errorf("SessionID = %q, want the repair turn's s2", got.SessionID)
		}
		if got.Usage["m"].InputTokens != 15 || tools != 5 {
			t.Errorf("usage/tools not added up: %+v, %d", got.Usage, tools)
		}
	})

	t.Run("repair failed", func(t *testing.T) {
		repair := agent.Result{Status: "failed", SessionID: "s3", Usage: map[string]agent.TokenUsage{"m": {InputTokens: 1}}}
		got, _ := mergeWorktreeRepairResult(original, 3, repair, 0, nil)
		if got.SessionID != "s1" || got.Status != "completed" {
			t.Errorf("a failed repair changed the outcome: %+v", got)
		}
		if got.Usage["m"].InputTokens != 11 {
			t.Errorf("the failed repair's usage was dropped: %+v", got.Usage)
		}
	})

	t.Run("repair never started", func(t *testing.T) {
		got, tools := mergeWorktreeRepairResult(original, 3, agent.Result{}, 0, exec.ErrNotFound)
		if got.SessionID != "s1" || tools != 3 || got.Output != "the real reply" {
			t.Errorf("a repair that never started changed the result: %+v", got)
		}
	})
}

// repairingBackend plays an agent that follows the repair prompt: it records
// what it was asked and runs the prompt's git commands in the worktree.
type repairingBackend struct {
	worktree *execenv.LocalWorktree
	problem  *execenv.DeliveryProblem
	comply   bool

	gotPrompt string
	gotOpts   agent.ExecOptions
	runErr    error
}

func (b *repairingBackend) Execute(_ context.Context, prompt string, opts agent.ExecOptions) (*agent.Session, error) {
	b.gotPrompt, b.gotOpts = prompt, opts
	if b.comply {
		for _, step := range b.problem.RepairSteps() {
			if out, err := exec.Command("git", append([]string{"-C", b.worktree.Path}, step...)...).CombinedOutput(); err != nil {
				b.runErr = fmt.Errorf("git %v: %s: %w", step, out, err)
				break
			}
		}
	}
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	close(msgCh)
	if opts.OnUsage != nil {
		opts.OnUsage(map[string]agent.TokenUsage{"m": {InputTokens: 7}})
	}
	resCh <- agent.Result{Status: "completed", Output: "moved the work back onto the branch", SessionID: "sess-repair",
		Usage: map[string]agent.TokenUsage{"m": {InputTokens: 7}}}
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func gitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

// squashedWorktree builds the #9404 shape-one state: a worktree turn whose
// agent committed, then squashed its baseline away.
func squashedWorktree(t *testing.T) (string, *execenv.LocalWorktree) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	gitOK(t, repo, "init", "-q", "-b", "main")
	gitOK(t, repo, "config", "user.name", "Test")
	gitOK(t, repo, "config", "user.email", "test@example.com")
	gitOK(t, repo, "config", "gc.auto", "0")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, repo, "add", "-A")
	gitOK(t, repo, "commit", "-q", "-m", "initial")

	wt, err := execenv.PrepareLocalWorktree(execenv.LocalWorktreeParams{
		LocalPath:       repo,
		EnvRoot:         t.TempDir(),
		AgentName:       "j",
		TaskID:          "11112222-3333-4444-5555-aaaaaaaaaaaa",
		ConversationKey: "MUL-9404",
		WorkspaceID:     "ws",
		AgentID:         "agent",
		ConversationID:  "issue",
	}, slog.Default())
	if err != nil {
		t.Fatalf("PrepareLocalWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.WorkDir, "agent.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, wt.Path, "add", "-A")
	gitOK(t, wt.Path, "commit", "-q", "-m", "work")
	gitOK(t, wt.Path, "reset", "--soft", wt.BaseCommit+"^")
	gitOK(t, wt.Path, "commit", "-q", "-m", "squashed")
	return repo, wt
}

// End to end through the daemon's repair turn: the agent is resumed in its own
// session with the repair prompt, follows it, and Finalize then records the
// delivery it would have refused.
func TestRepairWorktreeDeliveryLetsTheAgentFixItsBranch(t *testing.T) {
	t.Parallel()
	repo, wt := squashedWorktree(t)
	problem := wt.CheckDelivery()
	if problem == nil {
		t.Fatal("expected a delivery problem after the squash")
	}

	d, _ := newPinRecorder(t)
	backend := &repairingBackend{worktree: wt, problem: problem, comply: true}
	original := agent.Result{Status: "completed", Output: "the real reply", SessionID: "sess-1", Usage: map[string]agent.TokenUsage{"m": {InputTokens: 100}}}
	env := &execenv.Environment{LocalWorktree: wt}

	var liveUsage map[string]agent.TokenUsage
	opts := agent.ExecOptions{Timeout: time.Hour, OnUsage: func(usage map[string]agent.TokenUsage) { liveUsage = usage }}
	got, _ := d.repairWorktreeDelivery(context.Background(), backend, opts, original, 1, problem, env, slog.Default(), "task-repair", new(atomic.Int32))
	if liveUsage["m"].InputTokens != 107 || liveUsage["m"] != got.Usage["m"] {
		t.Errorf("repair's live usage = %v, want terminal task total %v", liveUsage, got.Usage)
	}
	if backend.runErr != nil {
		t.Fatalf("the repair prompt's commands failed: %v", backend.runErr)
	}
	if backend.gotOpts.ResumeSessionID != "sess-1" {
		t.Errorf("repair turn resumed %q, want the turn's own session sess-1", backend.gotOpts.ResumeSessionID)
	}
	if backend.gotOpts.Timeout != worktreeRepairTimeout {
		t.Errorf("repair turn timeout = %s, want %s", backend.gotOpts.Timeout, worktreeRepairTimeout)
	}
	if !strings.Contains(backend.gotPrompt, problem.Reason) {
		t.Errorf("repair prompt does not name the problem:\n%s", backend.gotPrompt)
	}
	if got.Output != "the real reply" || got.SessionID != "sess-repair" {
		t.Errorf("merged result = %+v", got)
	}

	if _, err := wt.Finalize(slog.Default()); err != nil {
		t.Fatalf("Finalize still refused after the repair: %v", err)
	}
	if content := gitOK(t, repo, "show", wt.Branch+":agent.txt"); content != "work" {
		t.Errorf("agent.txt on the branch = %q", content)
	}
}

// An agent that ignores the repair prompt changes nothing: Finalize refuses
// exactly as it did before this feature.
func TestRepairWorktreeDeliveryLeavesTheRefusalWhenTheAgentDoesNotFixIt(t *testing.T) {
	t.Parallel()
	_, wt := squashedWorktree(t)
	problem := wt.CheckDelivery()
	if problem == nil {
		t.Fatal("expected a delivery problem after the squash")
	}

	d, _ := newPinRecorder(t)
	backend := &repairingBackend{worktree: wt, problem: problem, comply: false}
	env := &execenv.Environment{LocalWorktree: wt}
	d.repairWorktreeDelivery(context.Background(), backend, agent.ExecOptions{}, agent.Result{Status: "completed"}, 0, problem, env, slog.Default(), "task-norepair", new(atomic.Int32))

	if _, err := wt.Finalize(slog.Default()); err == nil || !strings.Contains(err.Error(), "no longer contains") {
		t.Fatalf("Finalize = %v, want the original refusal", err)
	}
}
