package execenv // CEREBRO-PATCH(session-plan-mode): fork behavior regression coverage.

import (
	"strings"
	"testing"
)

// CEREBRO-PATCH(session-modes): FIR-3111 covers the five-mode runtime brief contract.
func TestRenderIssueContextIncludesSelectedModeRule(t *testing.T) {
	out := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", SessionMode: "plan"})
	if !strings.Contains(out, "PLAN MODE") || !strings.Contains(out, "must NOT write or edit code") {
		t.Fatalf("plan mode rule missing: %s", out)
	}
	defaultOut := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(defaultOut, "PLAN MODE") {
		t.Fatalf("default session unexpectedly has plan rule: %s", defaultOut)
	}
	researchOut := renderIssueContext("codex", TaskContextForEnv{ChatSessionID: "chat-1", SessionMode: "research"})
	if !strings.Contains(researchOut, "RESEARCH MODE") || !strings.Contains(researchOut, "read-only investigation") {
		t.Fatalf("research mode rule missing: %s", researchOut)
	}
}
