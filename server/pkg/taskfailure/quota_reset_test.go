package taskfailure

import (
	"testing"
	"time"
)

func TestParseQuotaResetHint(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, time.September, 27, 12, 0, 0, 0, zone)
	tests := []struct {
		name, message string
		reason        Reason
		wantKind      QuotaResetKind
		wantDate      string
		wantAt        string
	}{
		{"opus exact", "You've hit your weekly limit; resets Sep 29 at 3am (America/Los_Angeles)", ReasonAgentProviderQuotaLimit, QuotaResetExact, "", "2026-09-29T10:00:00Z"},
		{"cursor date only", "You've hit your monthly usage limit; resets Oct 11", ReasonAgentProviderQuotaLimit, QuotaResetDateOnly, "2026-10-11", ""},
		{"explicit iso", "quota exhausted; resets at 2026-09-29T03:00:00-07:00", ReasonAgentProviderQuotaLimit, QuotaResetExact, "", "2026-09-29T10:00:00Z"},
		{"explicit iso date", "quota exhausted; resets on 2026-10-11", ReasonAgentProviderQuotaLimit, QuotaResetDateOnly, "2026-10-11", ""},
		{"no reset", "You've hit your monthly usage limit", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"capacity is not quota", "429 rate limit; resets Sep 29 at 3am", ReasonAgentProviderCapacityOrRateLimit, QuotaResetUnknown, "", ""},
		{"past exact", "quota exhausted; resets Sep 26 at 3am", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"bad date", "quota exhausted; resets Feb 30", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"bad timezone", "quota exhausted; resets Sep 29 at 3am (PST)", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"bad time", "quota exhausted; resets Sep 29 at 13pm", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"ambiguous timezone", "quota exhausted; resets Sep 29 at 3am PST", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"distant yearless date", "quota exhausted; resets Dec 1", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
		{"iso date with unparsed time", "quota exhausted; resets 2026-10-11 at 3am", ReasonAgentProviderQuotaLimit, QuotaResetUnknown, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseQuotaResetHint(tt.reason, tt.message, observed, zone)
			if got.Kind != tt.wantKind || got.Date != tt.wantDate {
				t.Fatalf("got %+v, want kind=%s date=%q", got, tt.wantKind, tt.wantDate)
			}
			if tt.wantAt != "" && got.At.Format(time.RFC3339) != tt.wantAt {
				t.Fatalf("got %s, want %s", got.At.Format(time.RFC3339), tt.wantAt)
			}
		})
	}
}
