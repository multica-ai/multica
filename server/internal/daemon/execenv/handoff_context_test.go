package execenv

import (
	"strings"
	"testing"
)

// TestRenderIssueContext_HandoffNote verifies the handoff note lands in
// issue_context.md under its own section, distinct from the comment-reply
// trigger framing.
func TestRenderIssueContext_HandoffNote(t *testing.T) {
	note := "Scope to the auth module only."
	md := renderIssueContext("claude", TaskContextForEnv{IssueID: "issue-1", HandoffNote: note})

	if !strings.Contains(md, "## Handoff Note") {
		t.Fatalf("expected Handoff Note section:\n%s", md)
	}
	if !strings.Contains(md, note) {
		t.Fatalf("handoff note text missing:\n%s", md)
	}
	if !strings.Contains(md, "**Trigger:** New Assignment") {
		t.Fatalf("handoff note must render under the assignment trigger:\n%s", md)
	}
}

// TestRenderIssueContext_NoHandoffNote keeps the assignment context clean when
// no note is present.
func TestRenderIssueContext_NoHandoffNote(t *testing.T) {
	md := renderIssueContext("claude", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(md, "## Handoff Note") {
		t.Fatalf("unexpected Handoff Note section when no note set:\n%s", md)
	}
}

func TestRenderIssueContext_UsesCompactCapsule(t *testing.T) {
	ctx := TaskContextForEnv{IssueID: "issue-1", IssueContext: IssueContextForEnv{
		Revision: 42, Content: "**Title:** Faster planning", Truncated: true, Bytes: 31,
	}}
	md := renderIssueContext("claude", ctx)

	if !strings.Contains(md, "## Feature Context Capsule") || !strings.Contains(md, "Faster planning") {
		t.Fatalf("compact capsule missing:\n%s", md)
	}
	if !strings.Contains(md, "revision 42") || !strings.Contains(md, "reached its size limit") {
		t.Fatalf("capsule metadata missing:\n%s", md)
	}
	if strings.Contains(md, "Run `multica issue get") {
		t.Fatalf("capsule should replace eager full issue fetch:\n%s", md)
	}
}
