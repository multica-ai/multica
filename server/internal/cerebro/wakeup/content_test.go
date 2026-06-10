package wakeup

import "testing"

// The wakeup comment content carries a leading "[wakeup:<trigger>] " tag that
// the frontend (packages/cerebro-wakeup/components/wakeup-note.tsx) parses to
// label the note and strip before showing the agent's text. Lock that format
// so the two sides can't drift.
func TestBuildWakeupCommentContent(t *testing.T) {
	cases := []struct {
		trigger string
		prompt  string
		want    string
	}{
		{TriggerTime, "check CI in 10 min", "[wakeup:time] check CI in 10 min"},
		{TriggerIssueStatus, "resume when in_review", "[wakeup:issue_status] resume when in_review"},
		{TriggerGithubCI, "react to the PR", "[wakeup:github_ci] react to the PR"},
	}
	for _, c := range cases {
		if got := buildWakeupCommentContent(c.trigger, c.prompt); got != c.want {
			t.Errorf("buildWakeupCommentContent(%q, %q) = %q, want %q", c.trigger, c.prompt, got, c.want)
		}
	}
}
