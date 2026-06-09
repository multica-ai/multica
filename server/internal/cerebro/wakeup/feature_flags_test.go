package wakeup

import "testing"

// TECH-3176: lock the trigger-type → feature-flag-key mapping. The keys must
// stay in sync with packages/cerebro-feature-flags/registry.ts; a drift here
// silently un-gates a wakeup type.
func TestFlagForTrigger(t *testing.T) {
	cases := []struct {
		trigger string
		wantKey string
		wantOK  bool
	}{
		{TriggerTime, "cerebro_wakeup_time", true},
		{TriggerIssueStatus, "cerebro_wakeup_issue_status", true},
		{TriggerGithubCI, "cerebro_wakeup_github_ci", true},
		{"unknown", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotKey, gotOK := flagForTrigger(c.trigger)
		if gotKey != c.wantKey || gotOK != c.wantOK {
			t.Errorf("flagForTrigger(%q) = (%q, %v), want (%q, %v)", c.trigger, gotKey, gotOK, c.wantKey, c.wantOK)
		}
	}
}
