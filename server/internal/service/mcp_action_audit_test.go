package service

import "testing"

func TestClampMCPAuditRetentionDays(t *testing.T) {
	if got := ClampMCPAuditRetentionDays(0); got != 1 {
		t.Fatalf("zero = %d, want 1", got)
	}
	if got := ClampMCPAuditRetentionDays(30); got != 30 {
		t.Fatalf("30 = %d", got)
	}
	if got := ClampMCPAuditRetentionDays(9000); got != 365 {
		t.Fatalf("9000 = %d, want 365", got)
	}
}

func TestSkipProviderStreamMCPServer(t *testing.T) {
	if !SkipProviderStreamMCPServer("multica-plugins") {
		t.Fatal("plugin hook server should be skipped")
	}
	if !SkipProviderStreamMCPServer("plugin-toolbox-0123456789abcdef") {
		t.Fatal("broker server name should be skipped")
	}
	if SkipProviderStreamMCPServer("plugin-exa-search") {
		t.Fatal("a provider-reported server name without the broker digest must be kept")
	}
}

func TestTranscriptMCPOutcome(t *testing.T) {
	outcome, duration, errText := TranscriptMCPOutcome("completed\nduration: 1429 ms")
	if outcome != "ok" || duration == nil || *duration != 1429 || errText != "" {
		t.Fatalf("success = %s %v %q", outcome, duration, errText)
	}
	outcome, _, errText = TranscriptMCPOutcome("failed\nerror: Bearer [REDACTED]")
	if outcome != "error" || errText != "Bearer [REDACTED]" {
		t.Fatalf("failure = %s %q", outcome, errText)
	}
}
