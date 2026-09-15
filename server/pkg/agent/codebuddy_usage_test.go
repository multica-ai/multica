package agent

import (
	"encoding/json"
	"testing"
)

func TestCodebuddyUsageTracksMessageTotals(t *testing.T) {
	var tracker codebuddyUsageTracker
	first := json.RawMessage(`{"update":{"sessionUpdate":"usage_update","_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":38646,"completion_tokens":4,"prompt_cache_miss_tokens":4524,"cache_read_input_tokens":0,"cache_creation_input_tokens":34122,"credit":9.44}}}}`)
	tracker.observe("session/update", first)
	tracker.observe("session/update", first)
	tracker.observe("session/update", json.RawMessage(`{"update":{"sessionUpdate":"usage_update","used":38646,"_meta":{"codebuddy.ai/messageId":"one"}}}`))
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"two","usage":{"prompt_tokens":100,"completion_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}}}}`))
	want := TokenUsage{InputTokens: 4544, OutputTokens: 14, CacheReadTokens: 60, CacheWriteTokens: 34142}
	if got := tracker.total(); got != want {
		t.Fatalf("usage=%+v want %+v", got, want)
	}
}

// Preserve the upstream accounting invariant across the transport migration:
// duplicate blocks do not double-count, and a new execution has a new ledger.
func TestCodebuddyACPUsageIsScopedToExecution(t *testing.T) {
	event := json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"same-id","usage":{"prompt_tokens":100,"completion_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}}}}`)
	for i := 0; i < 2; i++ {
		var tracker codebuddyUsageTracker
		tracker.observe("session/update", event)
		tracker.observe("session/update", event)
		want := TokenUsage{InputTokens: 20, OutputTokens: 10, CacheReadTokens: 60, CacheWriteTokens: 20}
		if got := tracker.total(); got != want {
			t.Fatalf("execution %d usage=%+v want %+v", i, got, want)
		}
	}
}

func TestCodebuddyACPUsageIgnoresIncompleteSnapshots(t *testing.T) {
	var tracker codebuddyUsageTracker
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one"}}}`))
	if got := tracker.total(); got != (TokenUsage{}) {
		t.Fatalf("context-only update became usage: %+v", got)
	}
	event := json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":100,"completion_tokens":10}}}}`)
	tracker.observe("session/update", event)
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":0}}}}`))
	if got := tracker.total(); got != (TokenUsage{InputTokens: 100, OutputTokens: 10}) {
		t.Fatalf("incomplete update erased counters: %+v", got)
	}
}
