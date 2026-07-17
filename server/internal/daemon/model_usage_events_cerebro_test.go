package daemon

import (
	"testing"
	"time"
)

// CEREBRO-PATCH(daemon-model-usage-event-fallback-test): FIR-3337 requires
// every runtime to fit the ledger even before it exposes native call events.
func TestModelUsageEventsForReportBuildsExplicitAggregateFallback(t *testing.T) {
	observedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	result := TaskResult{
		SessionID: "provider-session-1",
		Usage: []TaskUsageEntry{{
			Provider:               "openai",
			Model:                  "gpt-5.6-sol",
			InputTokens:            99_000,
			OutputTokens:           500,
			CacheReadTokens:        80_000,
			ContextInputTokens:     99_000,
			ContextCacheReadTokens: 80_000,
		}},
	}

	events := modelUsageEventsForReport(result, observedAt)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.ProviderSessionID != "provider-session-1" || got.Provider != "openai" || got.Model != "gpt-5.6-sol" {
		t.Fatalf("identity = %+v", got)
	}
	if got.InputTokens != 99_000 || got.ContextTokens != 99_000 {
		t.Fatalf("tokens = %+v", got)
	}
	if got.Source != "final_response" || got.Completeness != "tokens_only" || got.CounterSemantics != "cumulative" {
		t.Fatalf("measurement metadata = %+v", got)
	}
	if got.EventID == "" || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("event identity = %+v", got)
	}
}

func TestModelUsageEventsForReportPrefersNativeCallEvents(t *testing.T) {
	native := ModelUsageEventEntry{EventID: "native-call-1", CallID: "call-1"}
	result := TaskResult{
		Usage:       []TaskUsageEntry{{Provider: "openai", Model: "gpt-5.6-sol", InputTokens: 99_000}},
		UsageEvents: []ModelUsageEventEntry{native},
	}

	events := modelUsageEventsForReport(result, time.Now())
	if len(events) != 1 || events[0].EventID != native.EventID {
		t.Fatalf("events = %+v, want native event only", events)
	}
}
