package agent

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

type acpUsageFields uint8

const (
	acpUsageInput acpUsageFields = 1 << iota
	acpUsageOutput
	acpUsageCacheRead
	acpUsageCacheWrite
	acpUsageCost
)

// acpUsageSnapshot keeps field presence separate from the parsed values.
// Prompt results are frequently partial, so a zero value alone cannot tell us
// whether a runtime explicitly reported zero or omitted the bucket entirely.
type acpUsageSnapshot struct {
	TokenUsage
	rawInputTokens  int64
	fields          acpUsageFields
	totalTokens     int64
	hasTotalTokens  bool
	inputNormalized bool
}

func (s acpUsageSnapshot) has(field acpUsageFields) bool {
	return s.fields&field != 0
}

// acpUsageAccumulator reconciles the two metering paths shared by ACP
// backends: cumulative usage_update snapshots and terminal prompt usage.
//
// Per-bucket maxima deduplicate equivalent snapshots while retaining buckets
// omitted from one path. Input is special: self-describing normalized and
// ambiguous candidates are retained separately, then resolved from the whole
// observed set. This makes the result independent of whether a late
// usage_update arrives before or after the terminal prompt response.
type acpUsageAccumulator struct {
	TokenUsage
	fields acpUsageFields

	ambiguousInput    int64
	hasAmbiguousInput bool
	normalizedInput   int64
	normalizedTotal   int64
	hasNormalized     bool
}

func (a *acpUsageAccumulator) merge(next acpUsageSnapshot) {
	if next.has(acpUsageInput) {
		if next.inputNormalized {
			if !a.hasNormalized ||
				next.totalTokens > a.normalizedTotal ||
				(next.totalTokens == a.normalizedTotal && next.InputTokens > a.normalizedInput) {
				a.normalizedInput = next.InputTokens
				a.normalizedTotal = next.totalTokens
				a.hasNormalized = true
			}
		} else if !a.hasAmbiguousInput || next.InputTokens > a.ambiguousInput {
			a.ambiguousInput = next.InputTokens
			a.hasAmbiguousInput = true
		}
	}
	if next.has(acpUsageOutput) && (!a.has(acpUsageOutput) || next.OutputTokens > a.OutputTokens) {
		a.OutputTokens = next.OutputTokens
	}
	if next.has(acpUsageCacheRead) && (!a.has(acpUsageCacheRead) || next.CacheReadTokens > a.CacheReadTokens) {
		a.CacheReadTokens = next.CacheReadTokens
	}
	if next.has(acpUsageCacheWrite) && (!a.has(acpUsageCacheWrite) || next.CacheWriteTokens > a.CacheWriteTokens) {
		a.CacheWriteTokens = next.CacheWriteTokens
	}
	// Provider cost is a cumulative turn total on the ACP backends that expose
	// it. Max consolidation preserves a late/richer report without charging a
	// duplicate prompt-result and usage_update twice.
	if next.has(acpUsageCost) && (!a.has(acpUsageCost) || next.CostUSDTicks > a.CostUSDTicks) {
		a.CostUSDTicks = next.CostUSDTicks
	}

	a.fields |= next.fields
	a.resolveInput()
}

func (a acpUsageAccumulator) has(field acpUsageFields) bool {
	return a.fields&field != 0
}

func (a *acpUsageAccumulator) resolveInput() {
	switch {
	case !a.hasAmbiguousInput && a.hasNormalized:
		a.InputTokens = a.normalizedInput
	case a.hasAmbiguousInput && !a.hasNormalized:
		a.InputTokens = a.ambiguousInput
	case a.hasAmbiguousInput && a.hasNormalized:
		// input + output is a conservative cumulative floor even when input
		// includes cached reads. A normalized record below that floor is a
		// per-call delta and must not replace the larger cumulative stream.
		if a.normalizedTotal >= a.ambiguousInput+a.OutputTokens {
			a.InputTokens = a.normalizedInput
		} else {
			a.InputTokens = a.ambiguousInput
		}
	}
}

// withFallback fills fields omitted (or reported as zero) by a preferred
// representation. ACP's standard top-level usage is authoritative over vendor
// `_meta`, and nested `_meta.usage` is authoritative over its flat mirror.
// Provider cost still takes the maximum because zero means "not reported" and
// runtimes may expose the priced total in only one representation.
func (s acpUsageSnapshot) withFallback(fallback acpUsageSnapshot) acpUsageSnapshot {
	result := s
	inputFromFallback := false
	if fallback.has(acpUsageInput) && (!result.has(acpUsageInput) || result.rawInputTokens == 0) {
		result.InputTokens = fallback.InputTokens
		result.rawInputTokens = fallback.rawInputTokens
		result.inputNormalized = fallback.inputNormalized
		inputFromFallback = true
	}
	if fallback.has(acpUsageOutput) && (!result.has(acpUsageOutput) || result.OutputTokens == 0) {
		result.OutputTokens = fallback.OutputTokens
	}
	if fallback.has(acpUsageCacheRead) && (!result.has(acpUsageCacheRead) || result.CacheReadTokens == 0) {
		result.CacheReadTokens = fallback.CacheReadTokens
	}
	if fallback.has(acpUsageCacheWrite) && (!result.has(acpUsageCacheWrite) || result.CacheWriteTokens == 0) {
		result.CacheWriteTokens = fallback.CacheWriteTokens
	}
	if fallback.has(acpUsageCost) && (!result.has(acpUsageCost) || fallback.CostUSDTicks > result.CostUSDTicks) {
		result.CostUSDTicks = fallback.CostUSDTicks
	}

	result.fields |= fallback.fields
	if inputFromFallback && fallback.inputNormalized {
		result.totalTokens = fallback.totalTokens
		result.hasTotalTokens = fallback.hasTotalTokens
	} else if !result.hasTotalTokens && fallback.hasTotalTokens {
		result.totalTokens = fallback.totalTokens
		result.hasTotalTokens = true
	}
	result.normalizeInput()
	return result
}

// normalizeInput always starts from the provider's raw input count so a
// preferred/fallback merge can be normalized after all complementary fields
// are present without subtracting cached reads twice.
func (s *acpUsageSnapshot) normalizeInput() {
	s.InputTokens = s.rawInputTokens
	s.inputNormalized = false
	if !s.has(acpUsageInput) || !s.has(acpUsageOutput) || !s.has(acpUsageCacheRead) {
		return
	}
	s.TokenUsage, s.inputNormalized = normalizeACPTokenUsage(s.TokenUsage, s.totalTokens)
}

func parseACPTokenUsage(data json.RawMessage) TokenUsage {
	return parseACPTokenUsageSnapshot(data).TokenUsage
}

func parseACPTokenUsageSnapshot(data json.RawMessage) acpUsageSnapshot {
	if len(data) == 0 || string(data) == "null" {
		return acpUsageSnapshot{}
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return acpUsageSnapshot{}
	}

	var snapshot acpUsageSnapshot
	if value, ok := acpUsageInt64(rawFields, "inputTokens", "input_tokens"); ok {
		snapshot.InputTokens = value
		snapshot.rawInputTokens = value
		snapshot.fields |= acpUsageInput
	}
	if value, ok := acpUsageInt64(rawFields, "outputTokens", "output_tokens"); ok {
		snapshot.OutputTokens = value
		snapshot.fields |= acpUsageOutput
	}
	if value, ok := acpUsageInt64(rawFields,
		"cachedReadTokens",
		"cacheReadTokens",
		"cached_input_tokens",
		"cache_read_tokens",
		"cache_read_input_tokens",
	); ok {
		snapshot.CacheReadTokens = value
		snapshot.fields |= acpUsageCacheRead
	}
	if value, ok := acpUsageInt64(rawFields,
		"cachedWriteTokens",
		"cacheWriteTokens",
		"cache_write_tokens",
		"cache_creation_input_tokens",
	); ok {
		snapshot.CacheWriteTokens = value
		snapshot.fields |= acpUsageCacheWrite
	}
	if value, ok := acpUsageInt64(rawFields, "costUsdTicks", "cost_usd_ticks"); ok {
		snapshot.CostUSDTicks = value
		snapshot.fields |= acpUsageCost
	}
	if value, ok := acpUsageInt64(rawFields, "totalTokens", "total_tokens"); ok {
		snapshot.totalTokens = value
		snapshot.hasTotalTokens = true
	}

	snapshot.normalizeInput()
	return snapshot
}

// parseACPTokenUsageSnapshotFromMeta extracts and reconciles token usage from
// an ACP result `_meta` object. Grok Build returns both nested `_meta.usage`
// and flat mirror fields; merging them preserves fields omitted from either
// representation without double counting their duplicate values.
func parseACPTokenUsageSnapshotFromMeta(meta json.RawMessage) acpUsageSnapshot {
	if len(meta) == 0 || string(meta) == "null" {
		return acpUsageSnapshot{}
	}

	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	var preferred acpUsageSnapshot
	if err := json.Unmarshal(meta, &envelope); err == nil &&
		len(envelope.Usage) > 0 && string(envelope.Usage) != "null" {
		preferred = parseACPTokenUsageSnapshot(envelope.Usage)
	}
	return preferred.withFallback(parseACPTokenUsageSnapshot(meta))
}

func parseACPTokenUsageFromMeta(meta json.RawMessage) TokenUsage {
	return parseACPTokenUsageSnapshotFromMeta(meta).TokenUsage
}

// normalizeACPTokenUsage re-buckets a usage record whose inputTokens already
// contains cachedReadTokens, so the persisted buckets stay mutually exclusive
// and dashboard cost math does not charge the cached prefix twice.
//
// ACP does not specify whether cached reads are counted inside inputTokens.
// Grok Build counts them inside: totalTokens == inputTokens + outputTokens.
// The re-bucketing only happens when totalTokens proves that shape. Agents
// reporting exclusive buckets or omitting totalTokens remain unchanged.
func normalizeACPTokenUsage(usage TokenUsage, totalTokens int64) (TokenUsage, bool) {
	if totalTokens <= 0 || usage.CacheReadTokens <= 0 || usage.CacheReadTokens > usage.InputTokens {
		return usage, false
	}
	if totalTokens != usage.InputTokens+usage.OutputTokens {
		return usage, false
	}
	usage.InputTokens -= usage.CacheReadTokens
	return usage, true
}

func excludeACPCachedInput(usage TokenUsage, totalTokens int64) TokenUsage {
	normalized, _ := normalizeACPTokenUsage(usage, totalTokens)
	return normalized
}

func acpUsageInt64(fields map[string]json.RawMessage, names ...string) (int64, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&n); err == nil {
			if value, err := n.Int64(); err == nil && value >= 0 {
				return value, true
			}
			if value, err := n.Float64(); err == nil && value >= 0 {
				return int64(value), true
			}
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if value, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && value >= 0 {
				return value, true
			}
		}
	}
	return 0, false
}
