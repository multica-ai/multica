package agent

import (
	"fmt"
	"time"
)

func appendFirtalLocalCallUsageEvent(events []ModelUsageEvent, response firtalLocalResponse, model, providerSessionID string, observedAt time.Time) []ModelUsageEvent {
	if response.Usage == nil || model == "" || (response.Usage.PromptTokens <= 0 && response.Usage.CompletionTokens <= 0) {
		return events
	}
	sequence := int64(len(events) + 1)

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           fmt.Sprintf("firtal-local:%s:call:%d", providerSessionID, sequence),
		ProviderSessionID: providerSessionID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          firtalLocalProvider,
		Model:             model,
		InputTokens:       response.Usage.PromptTokens,
		OutputTokens:      response.Usage.CompletionTokens,
		ContextTokens:     response.Usage.PromptTokens,
		Source:            ModelUsageSourceFinalResponse,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	})
}
