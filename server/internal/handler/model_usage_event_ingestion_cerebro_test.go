package handler

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// CEREBRO-PATCH(model-usage-event-validation-test): FIR-3337 keeps provider
// normalization in core and rejects malformed runtime measurements.
func TestNormalizeAndValidateModelUsageEvent(t *testing.T) {
	event := agent.ModelUsageEvent{
		SchemaVersion:    "1",
		EventID:          "event-1",
		ObservedAt:       time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		Model:            "gpt-5.6-sol",
		Source:           agent.ModelUsageSourceFinalResponse,
		Completeness:     agent.ModelUsageTokensOnly,
		CounterSemantics: agent.ModelUsageCounterCumulative,
	}

	got, err := normalizeAndValidateModelUsageEvent(event, "OpenAI")
	if err != nil {
		t.Fatalf("normalizeAndValidateModelUsageEvent: %v", err)
	}
	if got.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", got.Provider)
	}

	event.EventID = ""
	if _, err := normalizeAndValidateModelUsageEvent(event, "OpenAI"); err == nil {
		t.Fatal("missing event id accepted")
	}
}
