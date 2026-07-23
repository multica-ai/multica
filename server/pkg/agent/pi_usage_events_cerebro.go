package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func appendPiCallUsageEvent(events []ModelUsageEvent, msg *piMessage, model, providerSessionID string, observedAt time.Time) []ModelUsageEvent {
	if msg == nil || msg.Usage == nil || model == "" {
		return events
	}
	if msg.Usage.Input <= 0 && msg.Usage.Output <= 0 && msg.Usage.CacheRead <= 0 && msg.Usage.CacheWrite <= 0 {
		return events
	}

	provider := "pi"
	if prefix, _, ok := strings.Cut(model, "/"); ok && prefix != "" {
		provider = prefix
	}
	sequence := int64(len(events) + 1)

	return append(events, ModelUsageEvent{
		SchemaVersion:     "1",
		EventID:           fmt.Sprintf("pi:%s:call:%d", filepath.Base(providerSessionID), sequence),
		ProviderSessionID: providerSessionID,
		Sequence:          sequence,
		ObservedAt:        observedAt.UTC(),
		Provider:          provider,
		Model:             model,
		InputTokens:       msg.Usage.Input,
		OutputTokens:      msg.Usage.Output,
		CacheReadTokens:   msg.Usage.CacheRead,
		CacheWriteTokens:  msg.Usage.CacheWrite,
		ContextTokens: msg.Usage.Input +
			msg.Usage.CacheRead + msg.Usage.CacheWrite,
		Source:           ModelUsageSourceStream,
		Completeness:     ModelUsageTokensOnly,
		CounterSemantics: ModelUsageCounterDelta,
	})
}
