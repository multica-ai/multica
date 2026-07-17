package agent

// CEREBRO-PATCH(model-usage-event-contract): FIR-3337 defines one runtime-neutral
// measurement contract before the existing usage stores are consolidated.

import (
	"fmt"
	"strings"
	"time"
)

const (
	ModelUsageSourceStream             = "stream"
	ModelUsageSourceFinalResponse      = "final_response"
	ModelUsageSourceTranscriptFallback = "transcript_fallback"
	ModelUsageSourceReconciliation     = "reconciliation"

	ModelUsageComplete    = "complete"
	ModelUsageTokensOnly  = "tokens_only"
	ModelUsageContextOnly = "context_only"
	ModelUsageEstimated   = "estimated"

	ModelUsageCounterDelta      = "delta"
	ModelUsageCounterCumulative = "cumulative"

	ModelUsageCompactionProviderExplicit = "provider_explicit"
	ModelUsageCompactionInferredDrop     = "inferred_drop"
)

// ModelUsageEvent is the canonical per-model-call usage contract emitted by
// runtime adapters. Task and workspace attribution are added by the daemon and
// server; adapters only report provider-native identity and measurements.
type ModelUsageEvent struct {
	SchemaVersion       string    `json:"schema_version"`
	EventID             string    `json:"event_id"`
	ProviderSessionID   string    `json:"provider_session_id,omitempty"`
	CallID              string    `json:"call_id,omitempty"`
	Sequence            int64     `json:"sequence"`
	ObservedAt          time.Time `json:"observed_at"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	InputTokens         int64     `json:"input_tokens,omitempty"`
	OutputTokens        int64     `json:"output_tokens,omitempty"`
	ReasoningTokens     int64     `json:"reasoning_tokens,omitempty"`
	CacheReadTokens     int64     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    int64     `json:"cache_write_tokens,omitempty"`
	CostCents           int64     `json:"cost_cents,omitempty"`
	ContextTokens       int64     `json:"context_tokens,omitempty"`
	ContextWindowTokens int64     `json:"context_window_tokens,omitempty"`
	CompactionKind      string    `json:"compaction_kind,omitempty"`
	Source              string    `json:"source"`
	Completeness        string    `json:"completeness"`
	CounterSemantics    string    `json:"counter_semantics"`
}

// ValidateModelUsageEvent rejects ambiguous or impossible measurements before
// they cross the runtime boundary.
func ValidateModelUsageEvent(event ModelUsageEvent) error {
	if event.SchemaVersion != "1" {
		return fmt.Errorf("unsupported model usage schema version %q", event.SchemaVersion)
	}
	if strings.TrimSpace(event.EventID) == "" {
		return fmt.Errorf("model usage event_id is required")
	}
	if strings.TrimSpace(event.Provider) == "" {
		return fmt.Errorf("model usage provider is required")
	}
	if strings.TrimSpace(event.Model) == "" {
		return fmt.Errorf("model usage model is required")
	}
	if event.ObservedAt.IsZero() {
		return fmt.Errorf("model usage observed_at is required")
	}
	if event.Sequence < 0 {
		return fmt.Errorf("model usage sequence cannot be negative")
	}
	if !oneOf(event.Source, ModelUsageSourceStream, ModelUsageSourceFinalResponse, ModelUsageSourceTranscriptFallback, ModelUsageSourceReconciliation) {
		return fmt.Errorf("invalid model usage source %q", event.Source)
	}
	if !oneOf(event.Completeness, ModelUsageComplete, ModelUsageTokensOnly, ModelUsageContextOnly, ModelUsageEstimated) {
		return fmt.Errorf("invalid model usage completeness %q", event.Completeness)
	}
	if !oneOf(event.CounterSemantics, ModelUsageCounterDelta, ModelUsageCounterCumulative) {
		return fmt.Errorf("invalid model usage counter semantics %q", event.CounterSemantics)
	}
	if event.CompactionKind != "" && !oneOf(event.CompactionKind, ModelUsageCompactionProviderExplicit, ModelUsageCompactionInferredDrop) {
		return fmt.Errorf("invalid model usage compaction kind %q", event.CompactionKind)
	}
	for name, value := range map[string]int64{
		"input_tokens": event.InputTokens, "output_tokens": event.OutputTokens,
		"reasoning_tokens": event.ReasoningTokens, "cache_read_tokens": event.CacheReadTokens,
		"cache_write_tokens": event.CacheWriteTokens, "cost_cents": event.CostCents,
		"context_tokens": event.ContextTokens, "context_window_tokens": event.ContextWindowTokens,
	} {
		if value < 0 {
			return fmt.Errorf("model usage %s cannot be negative", name)
		}
	}
	if event.ContextTokens > 0 && event.ContextWindowTokens > 0 && event.ContextTokens > event.ContextWindowTokens {
		return fmt.Errorf("model usage context_tokens cannot exceed context_window_tokens")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
