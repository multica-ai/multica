package agent

import (
	"fmt"
	"sort"
	"time"
)

func appendCursorCallUsageEvent(events []ModelUsageEvent, model, providerSessionID string, part cursorStepFinishPart, observedAt time.Time) []ModelUsageEvent {
	input := int64(part.Tokens.Input)
	output := int64(part.Tokens.Output)
	cacheRead := int64(part.Tokens.Cache.Read)
	cacheWrite := int64(part.Tokens.Cache.Write)
	if input <= 0 && output <= 0 && cacheRead <= 0 && cacheWrite <= 0 {
		return events
	}
	sequence := int64(len(events) + 1)

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           fmt.Sprintf("cursor:%s:call:%d", providerSessionID, sequence),
		ProviderSessionID: providerSessionID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          "cursor",
		Model:             model,
		InputTokens:       input,
		OutputTokens:      output,
		CacheReadTokens:   cacheRead,
		CacheWriteTokens:  cacheWrite,
		ContextTokens:     input + cacheRead + cacheWrite,
		Source:            ModelUsageSourceStream,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	})
}

func appendCursorResultUsageReconciliation(events []ModelUsageEvent, resultUsage map[string]TokenUsage, providerSessionID string, observedAt time.Time) []ModelUsageEvent {
	models := make([]string, 0, len(resultUsage))
	var authoritative TokenUsage
	for model, usage := range resultUsage {
		models = append(models, model)
		authoritative.InputTokens += usage.InputTokens
		authoritative.OutputTokens += usage.OutputTokens
		authoritative.CacheReadTokens += usage.CacheReadTokens
		authoritative.CacheWriteTokens += usage.CacheWriteTokens
	}
	if len(models) == 0 {
		return events
	}
	sort.Strings(models)

	var streamed TokenUsage
	for _, event := range events {
		streamed.InputTokens += event.InputTokens
		streamed.OutputTokens += event.OutputTokens + event.ReasoningTokens
		streamed.CacheReadTokens += event.CacheReadTokens
		streamed.CacheWriteTokens += event.CacheWriteTokens
	}
	delta := TokenUsage{
		InputTokens:      max(0, authoritative.InputTokens-streamed.InputTokens),
		OutputTokens:     max(0, authoritative.OutputTokens-streamed.OutputTokens),
		CacheReadTokens:  max(0, authoritative.CacheReadTokens-streamed.CacheReadTokens),
		CacheWriteTokens: max(0, authoritative.CacheWriteTokens-streamed.CacheWriteTokens),
	}
	if delta.InputTokens == 0 && delta.OutputTokens == 0 && delta.CacheReadTokens == 0 && delta.CacheWriteTokens == 0 {
		return events
	}
	sequence := int64(len(events) + 1)
	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           fmt.Sprintf("cursor:%s:reconciliation:%d", providerSessionID, sequence),
		ProviderSessionID: providerSessionID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          "cursor",
		Model:             models[0],
		InputTokens:       delta.InputTokens,
		OutputTokens:      delta.OutputTokens,
		CacheReadTokens:   delta.CacheReadTokens,
		CacheWriteTokens:  delta.CacheWriteTokens,
		Source:            ModelUsageSourceReconciliation,
		Completeness:      ModelUsageTokensOnly,
		CounterSemantics:  ModelUsageCounterDelta,
	})
}
