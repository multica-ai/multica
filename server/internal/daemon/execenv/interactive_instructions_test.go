package execenv

import (
	"strings"
	"testing"
)

func TestInteractiveIssueInstructionsUseLiveConversation(t *testing.T) {
	for _, leader := range []bool{false, true} {
		ctx := TaskContextForEnv{IssueID: "issue", InteractiveIssue: true, IsSquadLeader: leader}
		content := buildMetaSkillContentSlim("pi", ctx)
		for _, want := range []string{"Reply directly here", "not on every live message", "assistant output is visible directly", "A normal reply ends only your current turn", "multica issue finish-run", "only your current run", "even if another agent is assigned to the issue", "does not finish other agents' runs or change the issue status"} {
			if !strings.Contains(content, want) {
				t.Errorf("missing %q", want)
			}
		}
		for _, bad := range []string{"The user does NOT see your terminal output", "this call is the only delivery channel", "Your results are only visible to the user if posted", "Multica marks the task terminal the moment"} {
			if strings.Contains(content, bad) {
				t.Errorf("interactive brief still says %q", bad)
			}
		}
		ctx.InteractiveIssue = false
		if !strings.Contains(buildMetaSkillContentSlim("pi", ctx), "The user does NOT see your terminal output") {
			t.Fatal("one-shot instructions changed")
		}
	}
}
