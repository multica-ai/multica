package daemon

import "testing"

func TestAccountUsageTokens_SumsInputAndOutputOnly(t *testing.T) { // CEREBRO-PATCH(daemon-task-account-token-usage): regression for rolling token totals.
	got := accountUsageTokens([]TaskUsageEntry{
		{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 900, CacheWriteTokens: 80},
		{InputTokens: 7, OutputTokens: 3},
	})
	if got != 130 {
		t.Fatalf("accountUsageTokens = %d, want 130", got)
	}
}
