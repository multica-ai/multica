package execenv

import (
	"strings"
	"testing"
)

// GAP-11 (fork issue #12): flagged repos must surface their pre-checked-out
// sibling path in the Repositories section; unflagged ones must not.
func TestRepositoriesSectionExtraCheckoutPath(t *testing.T) {
	t.Parallel()

	with := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "issue-1",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/repo", ExtraCheckoutPath: "/ws/task/extra/repo"},
		},
	})
	if !strings.Contains(with, "/ws/task/extra/repo") {
		t.Fatalf("expected extra checkout path in brief, got:\n%s", with)
	}

	without := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "issue-1",
		Repos:   []RepoContextForEnv{{URL: "https://github.com/org/repo"}},
	})
	if strings.Contains(without, "already checked out at") {
		t.Fatalf("unflagged repo must not gain a checkout note, got:\n%s", without)
	}
}
