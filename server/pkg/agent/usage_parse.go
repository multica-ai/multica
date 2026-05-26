package agent

import (
	"encoding/json"
	"math"
)

func tokenUsageHasTokens(u TokenUsage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func mergeTokenUsageMax(dst *TokenUsage, src TokenUsage) {
	if src.InputTokens > dst.InputTokens {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > dst.OutputTokens {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheReadTokens > dst.CacheReadTokens {
		dst.CacheReadTokens = src.CacheReadTokens
	}
	if src.CacheWriteTokens > dst.CacheWriteTokens {
		dst.CacheWriteTokens = src.CacheWriteTokens
	}
}

func mergeTokenUsageSnapshot(existing TokenUsage, snapshot TokenUsage) TokenUsage {
	if snapshot.InputTokens > 0 {
		existing.InputTokens = snapshot.InputTokens
	}
	if snapshot.OutputTokens > 0 {
		existing.OutputTokens = snapshot.OutputTokens
	}
	if snapshot.CacheReadTokens > 0 {
		existing.CacheReadTokens = snapshot.CacheReadTokens
	}
	if snapshot.CacheWriteTokens > 0 {
		existing.CacheWriteTokens = snapshot.CacheWriteTokens
	}
	return existing
}

func tokenUsageFromRawMessage(raw json.RawMessage) TokenUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return TokenUsage{}
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return TokenUsage{}
	}
	return tokenUsageFromMap(data)
}

func tokenUsageFromMap(data map[string]any) TokenUsage {
	if data == nil {
		return TokenUsage{}
	}
	if usage, ok := tokenUsageNestedMap(data, "usage", "token_usage", "tokens"); ok {
		return tokenUsageFromFlatMap(usage)
	}
	return tokenUsageFromFlatMap(data)
}

func tokenUsageFromFlatMap(data map[string]any) TokenUsage {
	if data == nil {
		return TokenUsage{}
	}
	u := TokenUsage{
		InputTokens: tokenUsageInt64FirstOf(data,
			"input_tokens", "inputTokens", "input",
			"prompt_tokens", "promptTokens", "prompt",
		),
		OutputTokens: tokenUsageInt64FirstOf(data,
			"output_tokens", "outputTokens", "output",
			"completion_tokens", "completionTokens", "completion",
		),
		CacheReadTokens: tokenUsageInt64FirstOf(data,
			"cache_read_tokens", "cacheReadTokens",
			"cache_read_input_tokens", "cacheReadInputTokens",
			"cached_input_tokens", "cachedInputTokens",
			"cached_read_tokens", "cachedReadTokens",
			"cacheRead", "cache_read",
		),
		CacheWriteTokens: tokenUsageInt64FirstOf(data,
			"cache_write_tokens", "cacheWriteTokens",
			"cache_creation_input_tokens", "cacheCreationInputTokens",
			"cached_write_tokens", "cachedWriteTokens",
			"cached_write_input_tokens", "cachedWriteInputTokens",
			"cacheWrite", "cache_write",
		),
	}
	u.OutputTokens += tokenUsageInt64FirstOf(data,
		"reasoning_output_tokens", "reasoningOutputTokens",
		"thought_tokens", "thoughtTokens",
		"reasoning_tokens", "reasoningTokens",
		"reasoning",
	)
	if details, ok := tokenUsageNestedMap(data, "input_token_details", "inputTokenDetails"); ok {
		if u.CacheReadTokens == 0 {
			u.CacheReadTokens = tokenUsageInt64FirstOf(details, "cached_tokens", "cachedTokens")
		}
		if u.CacheWriteTokens == 0 {
			u.CacheWriteTokens = tokenUsageInt64FirstOf(details,
				"cache_creation_tokens", "cacheCreationTokens",
				"cache_write_tokens", "cacheWriteTokens",
			)
		}
	}
	if details, ok := tokenUsageNestedMap(data, "output_token_details", "outputTokenDetails"); ok {
		u.OutputTokens += tokenUsageInt64FirstOf(details,
			"reasoning_tokens", "reasoningTokens",
			"thought_tokens", "thoughtTokens",
		)
	}
	return u
}

func tokenUsageNestedMap(data map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if m, ok := tokenUsageAsMap(data[key]); ok {
			return m, true
		}
	}
	return nil, false
}

func tokenUsageAsMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[string]json.RawMessage:
		m := make(map[string]any, len(t))
		for k, raw := range t {
			var value any
			if err := json.Unmarshal(raw, &value); err == nil {
				m[k] = value
			}
		}
		return m, true
	default:
		return nil, false
	}
}

func tokenUsageInt64FirstOf(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			if n := tokenUsageInt64(v); n > 0 {
				return n
			}
		}
	}
	return 0
}

func tokenUsageInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case int32:
		return int64(t)
	case float64:
		if t <= 0 || math.IsNaN(t) || math.IsInf(t, 0) {
			return 0
		}
		return int64(t)
	case float32:
		if t <= 0 || math.IsNaN(float64(t)) || math.IsInf(float64(t), 0) {
			return 0
		}
		return int64(t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
	}
	return 0
}
