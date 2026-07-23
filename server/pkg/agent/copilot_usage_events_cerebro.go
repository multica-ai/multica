package agent

import (
	"fmt"
	"time"
)

func appendCopilotCallUsageEvent(events []ModelUsageEvent, msg copilotAssistantMessage, model, providerSessionID, rawTimestamp string) []ModelUsageEvent {
	if msg.OutputTokens <= 0 || model == "" {
		return events
	}
	observedAt, err := time.Parse(time.RFC3339Nano, rawTimestamp)
	if err != nil {
		observedAt = time.Now()
	}
	sequence := int64(len(events) + 1)
	eventID := fmt.Sprintf("copilot:%s:call:%d", providerSessionID, sequence)
	if msg.MessageID != "" {
		eventID = fmt.Sprintf("copilot:%s:call:%s", providerSessionID, msg.MessageID)
	}

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           eventID,
		ProviderSessionID: providerSessionID,
		CallID:            msg.MessageID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          "copilot",
		Model:             model,
		OutputTokens:      msg.OutputTokens,
		Source:            ModelUsageSourceStream,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	})
}
