package daemon

import (
	"strings"
	"testing"
)

func TestCommentHandoffKeepsContextAndReplyTarget(t *testing.T) {
	t.Parallel()
	task := Task{
		IssueID: "issue-1", TriggerCommentID: "comment-2", TriggerThreadID: "thread-3",
		TriggerCommentContent: "Continue the report.\nKeep the original scope.",
		PriorSessionID:        "session-4", NewCommentsDeltaKnown: true,
		IssueStateDeltaKnown: true, IssueStatus: "in_progress",
		IssueAssigneeType: "agent", IssueAssigneeID: "agent-5",
		InitiatorName: "Test Member", InitiatorEmail: "member@example.invalid",
	}
	prompt := BuildPrompt(task, "codex")
	for _, want := range []string{
		"Comment update", "Issue ID: issue-1", "Comment ID: comment-2",
		"> Continue the report.\n> Keep the original scope.",
		"Title and description are unchanged", "assignee: agent agent-5",
		"Comment delta: no other new comments",
		"--thread thread-3 --tail 30", "--parent comment-2 --content-file ./reply.md --output table && rm ./reply.md",
		"Follow the existing workspace instructions and access rules",
		"Cleanup runs only after a successful post", "Test Member", "member@example.invalid",
		"Your Multica credentials and access remain scoped to the runtime owner",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("handoff missing %q", want)
		}
	}
	for _, unwanted := range []string{"Focus on THIS", "the server compared", "answers workflow step", "masked by the cleanup"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("handoff retained directive wrapper %q", unwanted)
		}
	}
}
