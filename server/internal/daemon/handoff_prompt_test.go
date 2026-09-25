package daemon

import (
	"strings"
	"testing"
)

func TestBuildPromptAssignmentHandoffIsPerTurnContext(t *testing.T) {
	note := "Only touch the login flow; do not change payments."
	out := BuildPrompt(Task{IssueID: "issue-123", HandoffNote: note}, "claude")

	for _, want := range []string{note, "handoff note", "multica issue get issue-123"} {
		if !strings.Contains(out, want) {
			t.Fatalf("assignment prompt missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "quick-create assistant") {
		t.Fatalf("handoff task must stay on the assignment prompt branch:\n%s", out)
	}
}

func TestBuildPromptAssignmentWithoutHandoffStaysClean(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-123"}, "claude")
	if strings.Contains(out, "handoff note") {
		t.Fatalf("assignment without a note gained handoff framing:\n%s", out)
	}
}
