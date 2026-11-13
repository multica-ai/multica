package execenv

import (
	"strings"
	"testing"
)

func TestRuntimeBriefPinsTaskCLI(t *testing.T) {
	original := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = original })
	for _, tc := range []struct {
		goos, command string
	}{
		{"darwin", `"$MULTICA_CLI_PATH"`},
		{"linux", `"$MULTICA_CLI_PATH"`},
		{"windows", `& "$env:MULTICA_CLI_PATH"`},
	} {
		runtimeGOOS = tc.goos
		for kind, ctx := range map[string]TaskContextForEnv{
			"issue": {IssueID: "issue-1"}, "chat": {ChatSessionID: "chat-1"},
			"autopilot": {AutopilotRunID: "run-1"}, "quick-create": {QuickCreatePrompt: "create work"},
		} {
			brief := buildMetaSkillContent("codex", ctx)
			for _, want := range []string{
				"The daemon sets `MULTICA_CLI_PATH` to its own CLI executable",
				"invoke `" + tc.command + " ...` instead",
				"do not resolve `multica` through PATH",
			} {
				if !strings.Contains(brief, want) {
					t.Errorf("%s/%s brief missing %q", tc.goos, kind, want)
				}
			}
			if kind == "quick-create" && !strings.Contains(brief, "Run exactly one `"+tc.command+" issue create --output json` invocation") {
				t.Errorf("%s quick-create workflow does not use the pinned CLI", tc.goos)
			}
		}
	}
}
