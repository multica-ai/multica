package daemon

import (
	"strings"
	"testing"
)

// TestSnapshotIssueContext_Off: no snapshot shipped -> caller keeps the normal
// "fetch it yourself" instructions.
func TestSnapshotIssueContext_Off(t *testing.T) {
	t.Parallel()
	if section, ok := snapshotIssueContext(Task{}); ok || section != "" {
		t.Fatalf("expected empty/false for no snapshot, got ok=%v section=%q", ok, section)
	}
}

// TestSnapshotIssueContext_On: when a snapshot is shipped it is returned and
// carries the explicit "do not re-fetch" instruction.
func TestSnapshotIssueContext_On(t *testing.T) {
	t.Parallel()
	section, ok := snapshotIssueContext(Task{IssueSnapshot: "## Your issue\n\nIssue #7: Fix login"})
	if !ok {
		t.Fatal("expected ok=true when snapshot present")
	}
	if !strings.Contains(section, "Issue #7: Fix login") {
		t.Fatalf("snapshot body missing, got:\n%s", section)
	}
	if !strings.Contains(section, "do NOT need to run `multica issue get`") {
		t.Fatalf("expected do-not-refetch instruction, got:\n%s", section)
	}
}

// TestBuildCommentPrompt_SnapshotOn: with a snapshot the comment prompt inlines
// it, drops the "Start by running multica issue get" instruction, and still
// keeps the trigger comment + reply instructions.
func TestBuildCommentPrompt_SnapshotOn(t *testing.T) {
	t.Parallel()
	got := buildCommentPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "please look at this",
		IssueSnapshot:         "## Your issue (already fetched — do not re-fetch)\n\nIssue #7: Fix login",
	}, "claude")

	if strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("snapshot prompt must not ask the agent to fetch the issue, got:\n%s", got)
	}
	if strings.Contains(got, "For comment history, read the triggering thread first") {
		t.Fatalf("snapshot prompt must drop the comment-history fetch instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "Issue #7: Fix login") {
		t.Fatalf("expected inlined snapshot, got:\n%s", got)
	}
	if !strings.Contains(got, "please look at this") {
		t.Fatalf("trigger comment must still be inlined, got:\n%s", got)
	}
}

// TestBuildCommentPrompt_SnapshotOff: without a snapshot the prompt keeps the
// existing fetch-it-yourself behaviour (regression guard).
func TestBuildCommentPrompt_SnapshotOff(t *testing.T) {
	t.Parallel()
	got := buildCommentPrompt(Task{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
	}, "claude")
	if !strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("without snapshot the prompt must keep the fetch instruction, got:\n%s", got)
	}
}

// TestBuildPrompt_AssignmentSnapshotOn: assignment-triggered tasks (no trigger
// comment) also honour the snapshot.
func TestBuildPrompt_AssignmentSnapshotOn(t *testing.T) {
	t.Parallel()
	got := BuildPrompt(Task{
		IssueID:       "issue-1",
		IssueSnapshot: "## Your issue\n\nIssue #7: Fix login",
	}, "claude")
	if strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("assignment snapshot prompt must not ask to fetch the issue, got:\n%s", got)
	}
	if !strings.Contains(got, "Issue #7: Fix login") {
		t.Fatalf("expected inlined snapshot in assignment prompt, got:\n%s", got)
	}
}

// TestBuildPrompt_AssignmentSnapshotOff keeps the legacy behaviour.
func TestBuildPrompt_AssignmentSnapshotOff(t *testing.T) {
	t.Parallel()
	got := BuildPrompt(Task{IssueID: "issue-1"}, "claude")
	if !strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("without snapshot the assignment prompt must keep the fetch instruction, got:\n%s", got)
	}
}
