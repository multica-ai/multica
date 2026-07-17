package agent

import (
	"fmt"
	"os"
	"time"
)

type codexUsageEventBuilder struct {
	sequence                         int64
	previousTotal                    [5]int64
	previousTotalSet                 bool
	lastContextTokens                int64
	lastContextWindow                int64
	compactionRecordedSinceLastUsage bool
	fallbackObservedAt               time.Time
}

func newCodexUsageEventBuilder(fallbackObservedAt time.Time) *codexUsageEventBuilder {
	return &codexUsageEventBuilder{fallbackObservedAt: fallbackObservedAt}
}

func (b *codexUsageEventBuilder) appendCompaction(events []ModelUsageEvent, sessionID, model string, event codexSessionTokenCount) []ModelUsageEvent {
	if b.compactionRecordedSinceLastUsage {
		return events
	}
	b.sequence++
	events = append(events, ModelUsageEvent{
		SchemaVersion:       "1",
		EventID:             fmt.Sprintf("codex-compaction:%d", b.sequence),
		ProviderSessionID:   sessionID,
		Sequence:            b.sequence,
		ObservedAt:          b.observedAt(event.Timestamp),
		Provider:            "openai",
		Model:               model,
		ContextTokens:       b.lastContextTokens,
		ContextWindowTokens: b.lastContextWindow,
		CompactionKind:      ModelUsageCompactionProviderExplicit,
		Source:              ModelUsageSourceTranscriptFallback,
		Completeness:        ModelUsageContextOnly,
		CounterSemantics:    ModelUsageCounterDelta,
	})
	b.compactionRecordedSinceLastUsage = true
	return events
}

func (b *codexUsageEventBuilder) appendCall(events []ModelUsageEvent, sessionID, model string, event codexSessionTokenCount) []ModelUsageEvent {
	if event.Payload == nil || event.Payload.Info == nil || event.Payload.Info.LastTokenUsage == nil {
		return events
	}
	info := event.Payload.Info
	last := info.LastTokenUsage
	b.lastContextTokens = last.InputTokens
	b.lastContextWindow = info.ModelContextWindow

	total := info.TotalTokenUsage
	if total == nil {
		total = last
	}
	totalKey := [5]int64{total.InputTokens, total.OutputTokens, total.CachedInputTokens, total.CacheReadInputTokens, total.ReasoningOutputTokens}
	if b.previousTotalSet && totalKey == b.previousTotal {
		return events
	}

	b.sequence++
	cachedTokens := max(last.CachedInputTokens, last.CacheReadInputTokens)
	completeness := ModelUsageTokensOnly
	if info.ModelContextWindow > 0 {
		completeness = ModelUsageComplete
	}
	events = append(events, ModelUsageEvent{
		SchemaVersion:       "1",
		EventID:             fmt.Sprintf("codex-call:%d", b.sequence),
		ProviderSessionID:   sessionID,
		Sequence:            b.sequence,
		ObservedAt:          b.observedAt(event.Timestamp),
		Provider:            "openai",
		Model:               model,
		InputTokens:         max(0, last.InputTokens-cachedTokens),
		OutputTokens:        last.OutputTokens,
		ReasoningTokens:     last.ReasoningOutputTokens,
		CacheReadTokens:     cachedTokens,
		ContextTokens:       last.InputTokens,
		ContextWindowTokens: info.ModelContextWindow,
		Source:              ModelUsageSourceTranscriptFallback,
		Completeness:        completeness,
		CounterSemantics:    ModelUsageCounterDelta,
	})
	b.compactionRecordedSinceLastUsage = false
	b.previousTotal = totalKey
	b.previousTotalSet = true
	return events
}

func (b *codexUsageEventBuilder) observedAt(timestamp time.Time) time.Time {
	if timestamp.IsZero() {
		return b.fallbackObservedAt
	}
	return timestamp
}

func finalizeCodexUsageEvents(events []ModelUsageEvent, sessionID, model string) []ModelUsageEvent {
	for i := range events {
		if events[i].ProviderSessionID == "" {
			events[i].ProviderSessionID = sessionID
		}
		if events[i].Model == "" {
			events[i].Model = model
		}
		events[i].EventID = "codex:" + events[i].ProviderSessionID + ":" + events[i].EventID
	}
	return events
}

func selectCodexSessionUsage(files []string, startTime time.Time, targetSessionID string) *codexSessionUsage {
	var exact, fallback *codexSessionUsage
	var exactModTime, fallbackModTime time.Time
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(startTime) {
			continue
		}
		usage := parseCodexSessionFile(path)
		if usage == nil || (usage.usage.InputTokens == 0 && usage.usage.OutputTokens == 0) {
			continue
		}
		if fallback == nil || info.ModTime().After(fallbackModTime) {
			fallback, fallbackModTime = usage, info.ModTime()
		}
		if targetSessionID != "" && usage.sessionID == targetSessionID &&
			(exact == nil || info.ModTime().After(exactModTime)) {
			exact, exactModTime = usage, info.ModTime()
		}
	}
	if exact != nil {
		return exact
	}
	return fallback
}
