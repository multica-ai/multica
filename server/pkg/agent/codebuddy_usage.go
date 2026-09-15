package agent

import (
	"encoding/json"
	"sync"
)

// CodeBuddy 2.151 emits per-model-message OpenAI-shaped usage in
// usage_update._meta.usage, not the cumulative ACP usage field. A later
// context-window update repeats the message id without any token counters.
// Reconcile by message id before summing so neither repeat charges twice.
// Vendor credit and an empty-currency cost are not USD and are left unset.
type codebuddyUsageTracker struct {
	mu       sync.Mutex
	messages map[string]TokenUsage
}

func (u *codebuddyUsageTracker) observe(method string, params json.RawMessage) {
	if method != "session/update" && method != "session/notification" {
		return
	}
	var event struct {
		Update struct {
			Meta struct {
				MessageID string                     `json:"codebuddy.ai/messageId"`
				Usage     map[string]json.RawMessage `json:"usage"`
			} `json:"_meta"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &event) != nil {
		return
	}
	meta := event.Update.Meta

	if meta.MessageID == "" || len(meta.Usage) == 0 {
		return
	}
	input, hasInput := acpUsageInt64(meta.Usage, "prompt_tokens")
	output, hasOutput := acpUsageInt64(meta.Usage, "completion_tokens")
	if !hasInput || !hasOutput {
		return
	}
	read, _ := acpUsageInt64(meta.Usage, "cache_read_input_tokens", "prompt_cache_hit_tokens")
	write, _ := acpUsageInt64(meta.Usage, "cache_creation_input_tokens", "prompt_cache_write_tokens")
	if miss, ok := acpUsageInt64(meta.Usage, "prompt_cache_miss_tokens"); ok {
		input = miss
	} else if input >= read+write {
		input -= read + write
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.messages == nil {
		u.messages = make(map[string]TokenUsage)
	}
	u.messages[meta.MessageID] = TokenUsage{InputTokens: input, OutputTokens: output, CacheReadTokens: read, CacheWriteTokens: write}
}

func (u *codebuddyUsageTracker) total() TokenUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	var total TokenUsage
	for _, v := range u.messages {
		total.InputTokens += v.InputTokens
		total.OutputTokens += v.OutputTokens
		total.CacheReadTokens += v.CacheReadTokens
		total.CacheWriteTokens += v.CacheWriteTokens
	}
	return total
}
