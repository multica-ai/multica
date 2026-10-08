package agent

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestQwenResumedUsageIgnoresSessionTotals(t *testing.T) {
	t.Parallel()
	first := qwenFallbackAssistant(t, "message-1", "test", &qwenUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 40})
	second := qwenFallbackAssistant(t, "message-2", "other", &qwenUsage{InputTokens: 200, OutputTokens: 20})
	final := qwenStreamEvent{Type: "result", Result: "done", Usage: &qwenUsage{InputTokens: 1300, OutputTokens: 130, CacheReadInputTokens: 640}}
	failed := final
	failed.IsError = true
	failed.Result = "failed"
	firstUsage := map[string]TokenUsage{"test": {InputTokens: 60, OutputTokens: 10, CacheReadTokens: 40}}
	for _, tc := range []struct {
		name   string
		events []qwenStreamEvent
		want   map[string]TokenUsage
	}{
		{"current_only", []qwenStreamEvent{first, final}, firstUsage},
		{"duplicate_current_message", []qwenStreamEvent{first, first, final}, firstUsage},
		{"failed_result", []qwenStreamEvent{first, failed}, firstUsage},
		{"missing_result", []qwenStreamEvent{first}, firstUsage},
		{"empty_result", []qwenStreamEvent{first, {Type: "result", Usage: &qwenUsage{}}}, firstUsage},
		{"missing_result_usage", []qwenStreamEvent{first, {Type: "result"}}, firstUsage},
		{"missing_assistant_usage", []qwenStreamEvent{final}, map[string]TokenUsage{}},
		{"multiple_models", []qwenStreamEvent{first, second, final}, map[string]TokenUsage{
			"test":  {InputTokens: 60, OutputTokens: 10, CacheReadTokens: 40},
			"other": {InputTokens: 200, OutputTokens: 20},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := qwenStreamState{model: "test", resumed: true, usage: make(map[string]TokenUsage)}
			messages := make(chan Message, len(tc.events))
			for _, event := range tc.events {
				handleQwenEvent(event, messages, &state)
			}
			if !reflect.DeepEqual(state.usage, tc.want) {
				t.Fatalf("usage = %+v, want %+v", state.usage, tc.want)
			}
			last := tc.events[len(tc.events)-1]
			if last.Type == "result" && (!state.sawResult || state.resultIsError != last.IsError || state.finalResultText != last.Result) {
				t.Fatalf("ignoring cumulative usage changed the terminal outcome: %+v", state)
			}
		})
	}
}

func TestQwenExecuteScopesUsageToCurrentRun(t *testing.T) {
	// Synthetic Qwen Code v0.23.4 replay: stored telemetry is 1000/100,
	// this invocation's assistant is 100/10, and result.usage is 1100/110.
	// The producer restores telemetry in replayUiTelemetryFromConversation
	// and computes result usage from the cumulative UiTelemetryService.
	backend := newFakeQwenBackend(t, map[string]string{
		"QWEN_MODE": "usage-resume", "QWEN_STDIN_FILE": filepath.Join(t.TempDir(), "stdin"),
	})
	for _, tc := range []struct {
		name   string
		resume string
		want   TokenUsage
	}{
		{"resumed", "sess-qwen-1", TokenUsage{InputTokens: 100, OutputTokens: 10}},
		{"resumed_again", "sess-qwen-1", TokenUsage{InputTokens: 100, OutputTokens: 10}},
		// Without a resume, all terminal usage belongs to this invocation,
		// including calls that did not emit an assistant message.
		{"fresh_keeps_final_total", "", TokenUsage{InputTokens: 1100, OutputTokens: 110}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := backend.Execute(context.Background(), "test", ExecOptions{
				ResumeSessionID: tc.resume, Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, result := awaitQwenResult(t, session)
			if result.Status != "completed" || result.Output != "done" || result.SessionID != "sess-qwen-1" {
				t.Fatalf("result = %+v", result)
			}
			if got := result.Usage["qwen-test"]; got != tc.want {
				t.Fatalf("usage = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestQwenTokenUsageExcludesCacheReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  qwenUsage
		want TokenUsage
	}{
		{"uncached", qwenUsage{InputTokens: 1000, OutputTokens: 50}, TokenUsage{InputTokens: 1000, OutputTokens: 50}},
		{"mixed", qwenUsage{InputTokens: 1000, OutputTokens: 50, CacheReadInputTokens: 600}, TokenUsage{InputTokens: 400, OutputTokens: 50, CacheReadTokens: 600}},
		{"all_cached", qwenUsage{InputTokens: 600, CacheReadInputTokens: 600}, TokenUsage{CacheReadTokens: 600}},
		{"cache_exceeds_input", qwenUsage{InputTokens: 5, OutputTokens: 2, CacheReadInputTokens: 10}, TokenUsage{OutputTokens: 2, CacheReadTokens: 10}},
		{"empty", qwenUsage{}, TokenUsage{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := qwenTokenUsage(&tc.raw); got != tc.want {
				t.Fatalf("qwenTokenUsage(%+v) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
