package execenv

import (
	"strings"
	"testing"
)

func TestNamesVerbatimRuleReachesEveryTaskKind(t *testing.T) {
	t.Parallel()

	contexts := map[string]TaskContextForEnv{
		"issue": {IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		"chat":  {ChatSessionID: "chat-1"},
		"other": {},
	}

	for name, ctx := range contexts {
		t.Run(name, func(t *testing.T) {
			out := buildMetaSkillContent("claude", ctx)
			for _, want := range []string{
				"## Names",
				"Never translate a name.",
				"Explain a name, never rename it.",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("brief missing names rule %q", want)
				}
			}
		})
	}
}
