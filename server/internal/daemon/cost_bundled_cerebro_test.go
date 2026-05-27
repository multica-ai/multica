package daemon

// CEREBRO-PATCH(daemon-bundled-prompt): tests for the bundled_read start-prompt flip (FIR-2384).

import (
	"strings"
	"testing"
)

// TestBundleContextInstruction_Off: no hint -> caller keeps the normal
// separate-reads instructions.
func TestBundleContextInstruction_Off(t *testing.T) {
	t.Parallel()
	if section, ok := bundleContextInstruction(Task{}); ok || section != "" {
		t.Fatalf("expected empty/false without hint, got ok=%v section=%q", ok, section)
	}
}

// TestBundleContextInstruction_On: with the hint set the instruction points at
// the single `issue context` call for this issue.
func TestBundleContextInstruction_On(t *testing.T) {
	t.Parallel()
	section, ok := bundleContextInstruction(Task{IssueID: "issue-9", BundleContextHint: true})
	if !ok {
		t.Fatal("expected ok=true when hint set")
	}
	if !strings.Contains(section, "multica issue context issue-9") {
		t.Fatalf("expected bundled-call instruction for the issue, got:\n%s", section)
	}
}

// TestBuildPrompt_AssignmentBundleOn: assignment-triggered tasks steer to the
// bundled call and drop the separate `issue get` instruction.
func TestBuildPrompt_AssignmentBundleOn(t *testing.T) {
	t.Parallel()
	got := BuildPrompt(Task{IssueID: "issue-1", BundleContextHint: true}, "claude")
	if strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("bundle prompt must not ask for a separate issue get, got:\n%s", got)
	}
	if !strings.Contains(got, "multica issue context issue-1") {
		t.Fatalf("expected bundled-call instruction, got:\n%s", got)
	}
}

// TestBuildCommentPrompt_BundleOn: comment-triggered tasks steer to the bundled
// call, drop the separate fetch instructions, and still keep the trigger
// comment + reply instructions.
func TestBuildCommentPrompt_BundleOn(t *testing.T) {
	t.Parallel()
	got := buildCommentPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "please look at this",
		BundleContextHint:     true,
	}, "claude")
	if strings.Contains(got, "Start by running `multica issue get") {
		t.Fatalf("bundle prompt must not ask for a separate issue get, got:\n%s", got)
	}
	if strings.Contains(got, "For comment history, read the triggering thread first") {
		t.Fatalf("bundle prompt must drop the separate comment-history fetch, got:\n%s", got)
	}
	if !strings.Contains(got, "multica issue context issue-1") {
		t.Fatalf("expected bundled-call instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "please look at this") {
		t.Fatalf("trigger comment must still be inlined, got:\n%s", got)
	}
}

// TestBuildPrompt_SnapshotWinsOverBundle: when BOTH a snapshot and the bundle
// hint are present the snapshot takes precedence (the reads are already inlined,
// so the bundled-call instruction would be contradictory).
func TestBuildPrompt_SnapshotWinsOverBundle(t *testing.T) {
	t.Parallel()
	got := BuildPrompt(Task{
		IssueID:           "issue-1",
		IssueSnapshot:     "## Your issue\n\nIssue #7: Fix login",
		BundleContextHint: true,
	}, "claude")
	if !strings.Contains(got, "Issue #7: Fix login") {
		t.Fatalf("expected inlined snapshot to win, got:\n%s", got)
	}
	if strings.Contains(got, "multica issue context") {
		t.Fatalf("snapshot must win; bundled-call instruction must not appear, got:\n%s", got)
	}
}
