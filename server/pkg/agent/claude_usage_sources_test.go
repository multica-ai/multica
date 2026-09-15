package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Equal counters and equal exit status must still retain their different
// accounting scopes. The subprocess is the existing hermetic stream fixture.
func TestClaudeUsageSources(t *testing.T) {
	t.Parallel()
	event := json.RawMessage(`{"type":"assistant","message":{"id":"msg_a","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":200,"cache_creation_input_tokens":10},"content":[{"type":"text","text":"visible text"},{"type":"tool_use","id":"tool_a","name":"Read","input":{}}]}}`)
	for _, tt := range []struct {
		name, terminal, source string
		noAssistant, success   bool
	}{
		{name: "interrupted", source: "assistant_fallback"},
		{name: "final_models", terminal: `{"type":"result","is_error":true,"modelUsage":{"claude-sonnet-4-6":{"inputTokens":100,"outputTokens":0,"cacheReadInputTokens":200,"cacheCreationInputTokens":10}}}`, source: "final_model_usage"},
		{name: "final_main_loop", terminal: `{"type":"result","is_error":true,"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":200,"cache_creation_input_tokens":10}}`, source: "final_usage"},
		{name: "successful_without_totals", terminal: `{"type":"result","result":"done"}`, source: "assistant_fallback", success: true},
		{name: "zero_totals_keep_fallback", terminal: `{"type":"result","modelUsage":{"claude-sonnet-4-6":{}},"usage":{}}`, source: "assistant_fallback", success: true},
		{name: "unattributable_totals_keep_fallback", terminal: `{"type":"result","usage":{"input_tokens":9}}`, source: "assistant_fallback", success: true},
		{name: "no_counters", source: "none", noAssistant: true},
		{name: "zero_only_result_is_not_proof_of_no_spend", terminal: `{"type":"result","modelUsage":{"claude-sonnet-4-6":{}},"usage":{}}`, source: "none", noAssistant: true, success: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var events []json.RawMessage
			if !tt.noAssistant {
				events = []json.RawMessage{event}
			}
			var terminal json.RawMessage
			if tt.terminal != "" {
				terminal = json.RawMessage(tt.terminal)
			}
			result := executeClaudeUsageFixture(t, claudeUsageFixtureBackend(t, events, terminal, !tt.success), len(events))
			if !reflect.DeepEqual(result.UsageSources, []string{tt.source}) {
				t.Fatalf("sources = %v, want %s", result.UsageSources, tt.source)
			}
			if tt.noAssistant {
				if len(result.Usage) != 0 {
					t.Fatalf("invented usage: %v", result.Usage)
				}
			} else if got := result.Usage["claude-sonnet-4-6"]; got != (TokenUsage{InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 10}) {
				t.Fatalf("counters changed: %+v", got)
			}
			if (result.Status == "completed") != tt.success {
				t.Fatalf("status = %s: %s", result.Status, result.Error)
			}
		})
	}
}
