package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// modelUsageEventsForReport preserves native call-level events. Runtimes that
// have not exposed them yet still enter the canonical ledger as an explicitly
// incomplete per-model aggregate, never disguised as a real call measurement.
func modelUsageEventsForReport(result TaskResult, observedAt time.Time) []ModelUsageEventEntry {
	if len(result.UsageEvents) > 0 {
		return result.UsageEvents
	}

	events := make([]ModelUsageEventEntry, 0, len(result.Usage))
	for i, usage := range result.Usage {
		digest := sha256.Sum256([]byte(usage.Provider + "\x00" + usage.Model))
		events = append(events, ModelUsageEventEntry{
			SchemaVersion:     "1",
			EventID:           "aggregate:" + hex.EncodeToString(digest[:12]),
			ProviderSessionID: result.SessionID,
			Sequence:          int64(i),
			ObservedAt:        observedAt,
			Provider:          usage.Provider,
			Model:             usage.Model,
			InputTokens:       usage.InputTokens,
			OutputTokens:      usage.OutputTokens,
			CacheReadTokens:   usage.CacheReadTokens,
			CacheWriteTokens:  usage.CacheWriteTokens,
			CostCents:         usage.CostCents,
			ContextTokens:     usage.ContextInputTokens,
			Source:            "final_response",
			Completeness:      "tokens_only",
			CounterSemantics:  "cumulative",
		})
	}
	return events
}
