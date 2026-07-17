package agent

import (
	"fmt"
	"time"
)

func singleHTTPCallUsageEvent(provider, model, providerSessionID string, usage TokenUsage, observedAt time.Time) []ModelUsageEvent {
	if model == "" || (usage.InputTokens <= 0 && usage.OutputTokens <= 0 && usage.CacheReadTokens <= 0 && usage.CacheWriteTokens <= 0 && usage.CostCents <= 0) {
		return nil
	}
	contextTokens := usage.ContextInputTokens
	if contextTokens <= 0 {
		contextTokens = usage.InputTokens
	}
	eventID := fmt.Sprintf("%s:call:1", provider)
	if providerSessionID != "" {
		eventID = fmt.Sprintf("%s:%s:call:1", provider, providerSessionID)
	}

	return []ModelUsageEvent{{
		SchemaVersion:     "1",
		EventID:           eventID,
		ProviderSessionID: providerSessionID,
		Sequence:          1,
		ObservedAt:        observedAt.UTC(),
		Provider:          provider,
		Model:             model,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheReadTokens:   usage.CacheReadTokens,
		CacheWriteTokens:  usage.CacheWriteTokens,
		CostCents:         usage.CostCents,
		ContextTokens:     contextTokens,
		Source:            ModelUsageSourceFinalResponse,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	}}
}
