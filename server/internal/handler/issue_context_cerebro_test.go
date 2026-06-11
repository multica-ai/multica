package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueContextAgentCommentsSkipsSystemRows(t *testing.T) {
	t.Parallel()

	// CEREBRO-PATCH(issue-context-agent-comments-test): wakeup/system rows must not inflate agent context.
	comments := []db.Comment{
		{Content: "keep member comment", Type: "comment"},
		{Content: "drop status change", Type: "status_change"},
		{Content: "drop system note", Type: "system"},
		{Content: "drop wakeup note", Type: "wakeup"},
		{Content: "keep agent comment", Type: "comment"},
	}

	got := issueContextAgentComments(comments)
	if len(got) != 2 {
		t.Fatalf("expected 2 ordinary comments, got %d: %+v", len(got), got)
	}
	if got[0].Content != "keep member comment" || got[1].Content != "keep agent comment" {
		t.Fatalf("unexpected comments kept: %+v", got)
	}
}
