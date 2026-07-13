package execenv // CEREBRO-PATCH(session-plan-mode): fork behavior regression coverage.

import (
	"strings"
	"testing"
)

func TestRenderIssueContextIncludesPlanModeRule(t *testing.T) {
	out := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", PlanMode: true})
	if !strings.Contains(out, "PLAN MODE") || !strings.Contains(out, "must NOT write or edit code") {
		t.Fatalf("plan mode rule missing: %s", out)
	}
	defaultOut := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(defaultOut, "PLAN MODE") {
		t.Fatalf("default session unexpectedly has plan rule: %s", defaultOut)
	}
}
