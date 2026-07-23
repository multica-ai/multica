package agent

// CEREBRO-PATCH(model-usage-event-contract-test): FIR-3337 validate the fork's canonical usage contract.

import (
	"testing"
	"time"
)

func TestValidateModelUsageEventAcceptsCompleteDelta(t *testing.T) {
	event := ModelUsageEvent{
		SchemaVersion:       "1",
		EventID:             "evt-1",
		ProviderSessionID:   "session-1",
		CallID:              "call-1",
		Sequence:            1,
		ObservedAt:          time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC),
		Provider:            "openai",
		Model:               "gpt-5.6-sol",
		InputTokens:         99_000,
		OutputTokens:        2_000,
		CacheReadTokens:     80_000,
		ContextTokens:       99_000,
		ContextWindowTokens: 1_050_000,
		Source:              ModelUsageSourceStream,
		Completeness:        ModelUsageComplete,
		CounterSemantics:    ModelUsageCounterDelta,
	}

	if err := ValidateModelUsageEvent(event); err != nil {
		t.Fatalf("ValidateModelUsageEvent() error = %v", err)
	}
}

func TestValidateModelUsageEventAcceptsReconciliationDelta(t *testing.T) {
	event := ModelUsageEvent{
		SchemaVersion:    "1",
		EventID:          "reconciliation-1",
		Sequence:         2,
		ObservedAt:       time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		Provider:         "cursor",
		Model:            "gpt-5-test",
		InputTokens:      40,
		Source:           ModelUsageSourceReconciliation,
		Completeness:     ModelUsageTokensOnly,
		CounterSemantics: ModelUsageCounterDelta,
	}
	if err := ValidateModelUsageEvent(event); err != nil {
		t.Fatalf("ValidateModelUsageEvent() error = %v", err)
	}
}

func TestValidateModelUsageEventRejectsInvalidContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ModelUsageEvent)
	}{
		{name: "missing event id", mutate: func(e *ModelUsageEvent) { e.EventID = "" }},
		{name: "missing provider", mutate: func(e *ModelUsageEvent) { e.Provider = "" }},
		{name: "missing model", mutate: func(e *ModelUsageEvent) { e.Model = "" }},
		{name: "invalid source", mutate: func(e *ModelUsageEvent) { e.Source = "guess" }},
		{name: "invalid completeness", mutate: func(e *ModelUsageEvent) { e.Completeness = "partial-ish" }},
		{name: "invalid counter semantics", mutate: func(e *ModelUsageEvent) { e.CounterSemantics = "snapshot" }},
		{name: "negative token count", mutate: func(e *ModelUsageEvent) { e.InputTokens = -1 }},
		{name: "context exceeds window", mutate: func(e *ModelUsageEvent) { e.ContextTokens = 1_050_001 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ModelUsageEvent{
				SchemaVersion:       "1",
				EventID:             "evt-1",
				Sequence:            1,
				ObservedAt:          time.Now().UTC(),
				Provider:            "openai",
				Model:               "gpt-5.6-sol",
				ContextTokens:       99_000,
				ContextWindowTokens: 1_050_000,
				Source:              ModelUsageSourceFinalResponse,
				Completeness:        ModelUsageComplete,
				CounterSemantics:    ModelUsageCounterCumulative,
			}
			tt.mutate(&event)
			if err := ValidateModelUsageEvent(event); err == nil {
				t.Fatal("ValidateModelUsageEvent() error = nil, want validation error")
			}
		})
	}
}
