package daemon

// CEREBRO-PATCH(daemon-graphify-nudge): tests for the graphify adoption nudge fold (FIR-1311).

import (
	"strings"
	"testing"
)

// TestGraphifyNudgeBlock_Off: no nudge shipped -> empty/false, prompt unchanged.
func TestGraphifyNudgeBlock_Off(t *testing.T) {
	t.Parallel()
	if block, ok := graphifyNudgeBlock(Task{}); ok || block != "" {
		t.Fatalf("expected empty/false for no nudge, got ok=%v block=%q", ok, block)
	}
}

// TestGraphifyNudgeBlock_On: when a nudge is shipped it is returned verbatim with
// a trailing separator and never claims the issue was pre-fetched (that coupling
// belongs to IssueSnapshot, not this field).
func TestGraphifyNudgeBlock_On(t *testing.T) {
	t.Parallel()
	block, ok := graphifyNudgeBlock(Task{GraphifyNudge: "## Code navigation — prefer Graphify"})
	if !ok {
		t.Fatal("expected ok=true when nudge present")
	}
	if !strings.Contains(block, "prefer Graphify") {
		t.Fatalf("nudge body missing, got:\n%s", block)
	}
	if strings.Contains(block, "do NOT need to run") {
		t.Fatalf("graphify nudge must not claim the issue was pre-fetched, got:\n%s", block)
	}
}

// TestBuildAssignmentPrompt_GraphifyNudgeIndependentOfSnapshot: the nudge appears
// in the start prompt even when no snapshot is shipped, and does NOT suppress the
// normal "run multica issue get" instruction (that is the snapshot saving's job).
func TestBuildAssignmentPrompt_GraphifyNudgeIndependentOfSnapshot(t *testing.T) {
	t.Parallel()
	got := BuildPrompt(Task{
		IssueID:       "00000000-0000-0000-0000-000000000001",
		GraphifyNudge: "## Code navigation — prefer Graphify",
	}, "claude")
	if !strings.Contains(got, "prefer Graphify") {
		t.Fatalf("expected graphify nudge in prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "multica issue get") {
		t.Fatalf("graphify nudge must not suppress the normal issue-get instruction, got:\n%s", got)
	}
}

// TestBuildCommentPrompt_GraphifyNudge: the comment prompt inlines the nudge
// alongside the trigger comment.
func TestBuildCommentPrompt_GraphifyNudge(t *testing.T) {
	t.Parallel()
	got := buildCommentPrompt(Task{
		IssueID:               "00000000-0000-0000-0000-000000000001",
		TriggerCommentID:      "00000000-0000-0000-0000-0000000000aa",
		TriggerCommentContent: "Please look at the auth flow",
		GraphifyNudge:         "## Code navigation — prefer Graphify",
	}, "claude")
	if !strings.Contains(got, "prefer Graphify") {
		t.Fatalf("expected graphify nudge in comment prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "Please look at the auth flow") {
		t.Fatalf("expected trigger comment retained, got:\n%s", got)
	}
}
