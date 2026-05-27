package service

// CEREBRO-PATCH(task-timeout-issue-comment-test): regression coverage for timeout issue comments.

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLimitRunSummaryTrimsByRune(t *testing.T) {
	got := limitRunSummary("  æøåabcd  ", 4)
	if got != "æøåa\n..." {
		t.Fatalf("limitRunSummary() = %q", got)
	}
}

func TestLimitRunSummaryKeepsShortText(t *testing.T) {
	got := limitRunSummary("  ready\nfor review  ", 50)
	if got != "ready\nfor review" {
		t.Fatalf("limitRunSummary() = %q", got)
	}
}

func TestQuoteMarkdownQuotesEveryLine(t *testing.T) {
	got := quoteMarkdown("first\n\nsecond")
	want := "> first\n> \n> second"
	if got != want {
		t.Fatalf("quoteMarkdown() = %q, want %q", got, want)
	}
}

func TestTimeoutFailureCommentBodyIncludesRunReference(t *testing.T) {
	taskID := "00000000-0000-0000-0000-000000000123"
	body := (&TaskService{}).timeoutFailureCommentBody(t.Context(), db.AgentTaskQueue{
		ID: util.MustParseUUID(taskID),
	}, "task timed out")

	for _, want := range []string{
		"BLOCKED: Agent-run timed out",
		"Run: `00000000-0000-0000-0000-000000000123`",
		"Reason: `task timed out`",
		"multica issue run-messages 00000000-0000-0000-0000-000000000123 --output json",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("timeoutFailureCommentBody() missing %q in:\n%s", want, body)
		}
	}
}

// CEREBRO-PATCH(suppress-stale-blocked-comment-test): FIR-2395 — the
// suppression helper must default to "post" when it can't make a confident
// judgement, so a real silent hang never disappears just because the DB is
// unreachable or the task never started.
func TestShouldPostTimeoutFailureComment_DefaultsTrueWhenIssueInvalid(t *testing.T) {
	got := (&TaskService{}).shouldPostTimeoutFailureComment(t.Context(), db.AgentTaskQueue{})
	if !got {
		t.Fatalf("shouldPostTimeoutFailureComment with invalid issue id = false; want true (defaults to post)")
	}
}

func TestShouldPostTimeoutFailureComment_DefaultsTrueWhenQueriesNil(t *testing.T) {
	task := db.AgentTaskQueue{
		ID:      util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		IssueID: util.MustParseUUID("00000000-0000-0000-0000-000000000099"),
		AgentID: util.MustParseUUID("00000000-0000-0000-0000-000000000007"),
	}
	got := (&TaskService{}).shouldPostTimeoutFailureComment(t.Context(), task)
	if !got {
		t.Fatalf("shouldPostTimeoutFailureComment with nil Queries = false; want true (defaults to post)")
	}
}

func TestShouldPostTimeoutFailureComment_DefaultsTrueWhenStartedAtInvalid(t *testing.T) {
	task := db.AgentTaskQueue{
		ID:      util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		IssueID: util.MustParseUUID("00000000-0000-0000-0000-000000000099"),
		AgentID: util.MustParseUUID("00000000-0000-0000-0000-000000000007"),
		// StartedAt left zero / invalid on purpose — task never started
		// means there cannot have been a real in-run reply, so the
		// fallback message is still useful.
	}
	got := (&TaskService{}).shouldPostTimeoutFailureComment(t.Context(), task)
	if !got {
		t.Fatalf("shouldPostTimeoutFailureComment with invalid StartedAt = false; want true (defaults to post)")
	}
}
