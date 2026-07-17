package agent

import (
	"fmt"
	"time"
)

func appendOpenCodeCallUsageEvent(events []ModelUsageEvent, tokens *opencodeTokens, providerSessionID string, observedAt time.Time) []ModelUsageEvent {
	if tokens == nil {
		return events
	}
	var cacheRead, cacheWrite int64
	if tokens.Cache != nil {
		cacheRead = tokens.Cache.Read
		cacheWrite = tokens.Cache.Write
	}
	if tokens.Input <= 0 && tokens.Output <= 0 && cacheRead <= 0 && cacheWrite <= 0 {
		return events
	}
	sequence := int64(len(events) + 1)

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		ProviderSessionID: providerSessionID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          "opencode",
		InputTokens:       tokens.Input,
		OutputTokens:      tokens.Output,
		CacheReadTokens:   cacheRead,
		CacheWriteTokens:  cacheWrite,
		ContextTokens:     tokens.Input + cacheRead + cacheWrite,
		Source:            ModelUsageSourceStream,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	})
}

func finalizeOpenCodeCallUsageEvents(events []ModelUsageEvent, model, providerSessionID string) []ModelUsageEvent {
	if model == "" {
		model = "unknown"
	}
	for i := range events {
		if events[i].ProviderSessionID == "" {
			events[i].ProviderSessionID = providerSessionID
		}
		events[i].Model = model
		events[i].EventID = fmt.Sprintf("opencode:%s:call:%d", events[i].ProviderSessionID, events[i].Sequence)
	}
	return events
}
