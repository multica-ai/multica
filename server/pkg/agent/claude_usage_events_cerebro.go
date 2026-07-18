package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

type claudeCallUsage struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage *claudeUsage `json:"usage,omitempty"`
}

func appendClaudeCallUsageEvent(events []ModelUsageEvent, msg claudeSDKMessage, providerSessionID string, observedAt time.Time) []ModelUsageEvent {
	var call claudeCallUsage
	if err := json.Unmarshal(msg.Message, &call); err != nil || call.Usage == nil || call.Model == "" {
		return events
	}
	if !claudeUsageHasTokens(call.Usage.InputTokens, call.Usage.OutputTokens, call.Usage.CacheReadInputTokens, call.Usage.CacheCreationInputTokens) {
		return events
	}

	if msg.SessionID != "" {
		providerSessionID = msg.SessionID
	}
	sequence := int64(len(events) + 1)
	eventID := fmt.Sprintf("claude:%s:call:%d", providerSessionID, sequence)
	if call.ID != "" {
		eventID = fmt.Sprintf("claude:%s:call:%s", providerSessionID, call.ID)
	}

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           eventID,
		ProviderSessionID: providerSessionID,
		CallID:            call.ID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          "anthropic",
		Model:             call.Model,
		InputTokens:       call.Usage.InputTokens,
		OutputTokens:      call.Usage.OutputTokens,
		CacheReadTokens:   call.Usage.CacheReadInputTokens,
		CacheWriteTokens:  call.Usage.CacheCreationInputTokens,
		ContextTokens: call.Usage.InputTokens +
			call.Usage.CacheReadInputTokens + call.Usage.CacheCreationInputTokens,
		Source:           ModelUsageSourceStream,
		Completeness:     ModelUsageTokensOnly,
		CounterSemantics: ModelUsageCounterDelta,
	})
}
