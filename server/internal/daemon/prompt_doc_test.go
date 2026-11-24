package daemon

import (
	"strings"
	"testing"
)

func TestBuildPromptDocCommentNamesDiscussionAndReplyCommand(t *testing.T) {
	t.Parallel()

	task := Task{
		ID: "task-1", AgentID: "agent-1",
		DocID: "doc-1", DocThreadID: "thread-1", DocCommentID: "comment-2",
		DocTitle:             "Launch plan",
		DocThreadQuote:       "We ship on Friday.",
		DocCommentContent:    "Can you move this to Monday?\nAnd say why.",
		DocCommentAuthorName: "Bohan",
	}
	prompt := BuildPrompt(task, "claude")
	for _, want := range []string{
		`"Launch plan"`,
		"> We ship on Friday.",
		"[NEW COMMENT] Bohan",
		"> Can you move this to Monday?\n> And say why.",
		"multica doc get doc-1 --output content",
		"multica doc comment list doc-1 --thread thread-1 --output json",
		"multica doc comment add doc-1 --parent thread-1 --content-file ./reply.md --output table && rm ./reply.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "multica issue") {
		t.Fatalf("doc prompt must not send the agent to issue commands:\n%s", prompt)
	}

	task.DocCommentContent = ""
	if prompt := BuildPrompt(task, "claude"); !strings.Contains(prompt, "has been deleted") {
		t.Fatalf("prompt for a deleted trigger should say so:\n%s", prompt)
	}
}
