package execenv

import (
	"strings"
	"testing"
)

func TestRenderIssueContextPriorAttempt(t *testing.T) {
	md := renderIssueContext("claude", TaskContextForEnv{
		IssueID: "issue-1",
		PriorAttempt: &PriorAttemptData{
			Attempt:       2,
			FailureReason: "agent_error.provider_network",
			ErrorText:     "429 rate limited",
			FailedAt:      "2026-08-24T00:00:00Z",
		},
	})

	for _, want := range []string{
		"## Prior Attempt",
		"automatic retry #3",
		"attempt 2) failed at 2026-08-24T00:00:00Z",
		"`agent_error.provider_network`",
		"429 rate limited",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("renderIssueContext missing %q\n---\n%s", want, md)
		}
	}
}

func TestRenderIssueContextNoPriorAttemptUnchanged(t *testing.T) {
	with := renderIssueContext("claude", TaskContextForEnv{IssueID: "issue-1", PriorAttempt: nil})
	base := renderIssueContext("claude", TaskContextForEnv{IssueID: "issue-1"})
	if with != base {
		t.Errorf("nil PriorAttempt must not change output")
	}
	if strings.Contains(base, "Prior Attempt") {
		t.Errorf("base render must not contain Prior Attempt section:\n%s", base)
	}
}
