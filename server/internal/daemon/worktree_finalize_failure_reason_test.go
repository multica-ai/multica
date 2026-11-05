package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// finalizeRefusal builds the error runTask's Finalize defer returns when the
// delivery point cannot be recorded (#9447), with a worktree path chosen by the
// caller — the path is exactly the part of the text the classifier used to
// read as provider output.
func finalizeRefusal(path string) error {
	return &worktreePreservedError{err: fmt.Errorf("local_directory worktree: %w", fmt.Errorf(
		"refusing to record branch agent/codex/1a2b3c4d: the delivered commit 9c1d2e3f no longer contains a503f1e2, "+
			"the commit this turn started from; the task worktree is preserved at %s (listed by `git worktree list` in /Users/u/repo)",
		path))}
}

// TestTaskRunFailureReasonLabelsWorktreeFinalization: a run whose agent
// succeeded but whose worktree could not be finalized is a local git
// bookkeeping failure. Before #9447 its text went to taskfailure.Classify and
// came out as agent_error.provider_server_error when a path segment looked
// like a 5xx, agent_error.unknown otherwise — both blame the agent/provider.
func TestTaskRunFailureReasonLabelsWorktreeFinalization(t *testing.T) {
	cases := map[string]error{
		"5xx-looking path segment": finalizeRefusal("/Users/u/multica_workspaces/ws-500/task/worktree"),
		"plain path":               finalizeRefusal("/Users/u/multica_workspaces/ws/c503f/worktree"),
		"joined with nothing else": errors.Join(finalizeRefusal("/Users/u/w/503/worktree")),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			got := taskRunFailureReason(err)
			if got != "local_directory_error" {
				t.Fatalf("taskRunFailureReason = %q, want local_directory_error", got)
			}
			if taskfailure.Reason(got).IsAgentError() {
				t.Fatalf("finalization failure labelled as an agent error: %q", got)
			}
		})
	}
}

// TestTaskRunFailureReasonKeepsPrimaryCauseOverFinalization: Finalize runs on
// every exit path and joins its error onto the run's own. The run's error is
// the primary cause and keeps its label — classified without the worktree text,
// so the path can neither override nor produce it.
func TestTaskRunFailureReasonKeepsPrimaryCauseOverFinalization(t *testing.T) {
	primary := errors.New("stream disconnected before completion")
	err := errors.Join(primary, finalizeRefusal("/Users/u/w/ws-500/worktree"))
	want := taskfailure.Classify(primary.Error()).String()
	if got := taskRunFailureReason(err); got != want {
		t.Fatalf("taskRunFailureReason = %q, want %q (primary cause)", got, want)
	}

	// A primary cause the classifier cannot name must not borrow a label
	// from the worktree path joined after it.
	vague := errors.New("agent exited")
	err = errors.Join(vague, finalizeRefusal("/Users/u/w/ws-500/worktree"))
	if got, want := taskRunFailureReason(err), taskfailure.Classify(vague.Error()).String(); got != want {
		t.Fatalf("taskRunFailureReason = %q, want %q (path must not decide)", got, want)
	}

	// Structural sentinels on the primary side still win.
	err = errors.Join(errTaskPrepareTimeout, finalizeRefusal("/Users/u/w/worktree"))
	if got := taskRunFailureReason(err); got != taskfailure.ReasonTimeout.String() {
		t.Fatalf("taskRunFailureReason = %q, want %q", got, taskfailure.ReasonTimeout)
	}
}
