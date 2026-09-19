package agent

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func qwenFallbackAssistant(t *testing.T, id, model string, usage any) qwenStreamEvent {
	t.Helper()
	return qwenStreamEvent{Type: "assistant", Message: mustMarshal(t, map[string]any{
		"id": id, "model": model, "usage": usage,
		"content": []map[string]string{{"type": "text", "text": "visible"}},
	})}
}

func TestQwenFallbackUsageAccumulatesMessageSnapshots(t *testing.T) {
	t.Parallel()
	first := qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, OutputTokens: 50})
	second := qwenFallbackAssistant(t, "message-2", "test", &qwenUsage{InputTokens: 200, OutputTokens: 20})
	final := qwenStreamEvent{Type: "result", Usage: &qwenUsage{InputTokens: 500, OutputTokens: 80}}
	for _, tc := range []struct {
		name   string
		events []qwenStreamEvent
		want   map[string]TokenUsage
	}{
		{"two_messages", []qwenStreamEvent{first, second}, map[string]TokenUsage{"test": {InputTokens: 300, OutputTokens: 70}}},
		{"duplicate", []qwenStreamEvent{first, first}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"interleaved_duplicate", []qwenStreamEvent{first, second, first}, map[string]TokenUsage{"test": {InputTokens: 300, OutputTokens: 70}}},
		{"placeholder_then_usage", []qwenStreamEvent{qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{}), first}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"changed_duplicate_keeps_first_snapshot", []qwenStreamEvent{first, qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 120, OutputTokens: 60}), first}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"decreasing_counter_keeps_whole_snapshot", []qwenStreamEvent{
			qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, OutputTokens: 60, CacheReadInputTokens: 20}),
			qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 40}),
		}, map[string]TokenUsage{"test": {InputTokens: 80, OutputTokens: 60, CacheReadTokens: 20}}},
		{"corrected_model_does_not_count_message_twice", []qwenStreamEvent{first, qwenFallbackAssistant(t, "message-1", "other", &qwenUsage{InputTokens: 100, OutputTokens: 50})}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"empty_snapshot_does_not_claim_model", []qwenStreamEvent{qwenFallbackAssistant(t, "message-1", "other", &qwenUsage{}), first}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"missing_usage_does_not_claim_id", []qwenStreamEvent{qwenFallbackAssistant(t, "message-1", "test", nil), first}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"cache_only_snapshot_claims_id", []qwenStreamEvent{qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 20, CacheReadInputTokens: 20}), first}, map[string]TokenUsage{"test": {CacheReadTokens: 20}}},
		{"separate_models", []qwenStreamEvent{first, qwenFallbackAssistant(t, "message-2", "other", &qwenUsage{InputTokens: 20, OutputTokens: 10})}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}, "other": {InputTokens: 20, OutputTokens: 10}}},
		{"no_id_keeps_latest_snapshot", []qwenStreamEvent{qwenFallbackAssistant(t, "", "test", &qwenUsage{InputTokens: 100, OutputTokens: 50}), qwenFallbackAssistant(t, "", "test", &qwenUsage{InputTokens: 200, OutputTokens: 20})}, map[string]TokenUsage{"test": {InputTokens: 200, OutputTokens: 20}}},
		{"no_id_decrease_preserves_identified_usage", []qwenStreamEvent{first, qwenFallbackAssistant(t, "", "test", &qwenUsage{InputTokens: 200, OutputTokens: 20}), qwenFallbackAssistant(t, "", "test", &qwenUsage{InputTokens: 50, OutputTokens: 10})}, map[string]TokenUsage{"test": {InputTokens: 150, OutputTokens: 60}}},
		{"no_id_empty_snapshot_clears_only_anonymous_usage", []qwenStreamEvent{first, qwenFallbackAssistant(t, "", "test", &qwenUsage{InputTokens: 200, OutputTokens: 20}), qwenFallbackAssistant(t, "", "test", &qwenUsage{})}, map[string]TokenUsage{"test": {InputTokens: 100, OutputTokens: 50}}},
		{"empty_result_keeps_fallback", []qwenStreamEvent{first, second, {Type: "result", Usage: &qwenUsage{}}}, map[string]TokenUsage{"test": {InputTokens: 300, OutputTokens: 70}}},
		{"final_total_wins", []qwenStreamEvent{first, second, final}, map[string]TokenUsage{"test": {InputTokens: 500, OutputTokens: 80}}},
		{"late_assistant_cannot_replace_final_total", []qwenStreamEvent{first, final, second}, map[string]TokenUsage{"test": {InputTokens: 500, OutputTokens: 80}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := qwenStreamState{usage: make(map[string]TokenUsage)}
			messages := make(chan Message, len(tc.events))
			assistantCount := 0
			for _, event := range tc.events {
				handleQwenEvent(event, messages, &state)
				if event.Type == "assistant" {
					assistantCount++
				}
			}
			if !reflect.DeepEqual(state.usage, tc.want) {
				t.Fatalf("usage = %+v, want %+v", state.usage, tc.want)
			}
			if len(messages) != assistantCount {
				t.Fatalf("usage deduplication suppressed transcript messages: got %d, want %d", len(messages), assistantCount)
			}
		})
	}
}

func TestQwenFallbackUsagePreservesFirstCacheSnapshot(t *testing.T) {
	t.Parallel()
	state := qwenStreamState{usage: make(map[string]TokenUsage)}
	for _, event := range []qwenStreamEvent{
		qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, CacheReadInputTokens: 20}),
		qwenFallbackAssistant(t, "message-2", "test", &qwenUsage{InputTokens: 100, CacheReadInputTokens: 30}),
		qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, CacheReadInputTokens: 40}),
	} {
		handleQwenEvent(event, make(chan Message, 1), &state)
	}
	if got, want := state.usage["test"], (TokenUsage{InputTokens: 150, CacheReadTokens: 50}); got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestQwenExecuteRetainsEarlierUsageOnProcessFailure(t *testing.T) {
	backend := newFakeQwenBackend(t, map[string]string{"QWEN_MODE": "usage-fallback", "QWEN_STDIN_FILE": filepath.Join(t.TempDir(), "stdin")})
	// Reuse the backend to ensure snapshots belong to one Execute call.
	for i := 0; i < 2; i++ {
		session, err := backend.Execute(context.Background(), "test", ExecOptions{Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_, result := awaitQwenResult(t, session)
		if result.Status != "failed" {
			t.Fatalf("status = %s, want failed", result.Status)
		}
		if got := result.Usage["qwen-test"]; got != (TokenUsage{InputTokens: 300, OutputTokens: 70}) {
			t.Fatalf("usage = %+v, want 300 input / 70 output", got)
		}
	}
}
