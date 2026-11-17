package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCodebuddyExecuteFallbackUsage drives the real adapter with a stream-json
// fixture served by a test-created fake CLI. No installed CodeBuddy executable
// is resolved and no account is touched.
//
// CodeBuddy is a Claude Code fork emitting the same stream-json shapes, so the
// scenarios mirror TestClaudeExecuteFallbackUsage.
func TestCodebuddyExecuteFallbackUsage(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const model = "claude-sonnet-4-20250514"
	const otherModel = "claude-haiku-4-5"
	// output_tokens on assistant messages is a message_start placeholder, not
	// the completed response's output count. Only terminal usage supplies it.
	usage := &codebuddyUsage{InputTokens: 100, OutputTokens: 1, CacheReadInputTokens: 200, CacheCreationInputTokens: 10}
	secondUsage := &codebuddyUsage{InputTokens: 30, OutputTokens: 1, CacheReadInputTokens: 60, CacheCreationInputTokens: 5}

	blockIndex := 0
	assistant := func(id, model, parent string, u *codebuddyUsage) json.RawMessage {
		blockIndex++
		return mustMarshal(t, map[string]any{
			"type": "assistant", "parent_tool_use_id": parent,
			"message": map[string]any{"id": id, "role": "assistant", "model": model, "usage": u,
				"content": []any{
					map[string]any{"type": "text", "text": "visible text"},
					map[string]any{"type": "tool_use", "id": fmt.Sprintf("tool_%d", blockIndex), "name": "Read", "input": map[string]string{"file_path": "fixture.txt"}},
				}},
		})
	}
	terminal := func(failed bool, usage any, modelUsage any) json.RawMessage {
		subtype := "success"
		if failed {
			subtype = "error_during_execution"
		}
		return mustMarshal(t, map[string]any{
			"type": "result", "subtype": subtype, "is_error": failed,
			"result": "fixture result", "model": model, "usage": usage, "modelUsage": modelUsage,
		})
	}

	a := assistant("msg_a", model, "", usage)
	b := assistant("msg_b", model, "", secondUsage)
	a2 := assistant("msg_a", model, "", usage)
	split := []json.RawMessage{a, a2}
	baseWant := map[string]TokenUsage{model: {InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 10}}
	finalUsage := map[string]codebuddyResultModelUsage{
		model:      {InputTokens: 150, OutputTokens: 50, CacheReadInputTokens: 300, CacheCreationInputTokens: 15},
		otherModel: {InputTokens: 20, OutputTokens: 10, CacheReadInputTokens: 40, CacheCreationInputTokens: 2},
	}
	finalWant := map[string]TokenUsage{
		model:      {InputTokens: 150, OutputTokens: 50, CacheReadTokens: 300, CacheWriteTokens: 15},
		otherModel: {InputTokens: 20, OutputTokens: 10, CacheReadTokens: 40, CacheWriteTokens: 2},
	}

	type testCase struct {
		name            string
		events          []json.RawMessage
		result          json.RawMessage
		success         bool
		resumeSessionID string
		want            map[string]TokenUsage
	}
	cumulative := &codebuddyUsage{InputTokens: 999, OutputTokens: 99, CacheReadInputTokens: 9999, CacheCreationInputTokens: 999}
	cases := []testCase{
		{name: "split_response_without_result", events: split, want: baseWant},
		{name: "split_response_zero_result_usage", events: split, result: terminal(true, &codebuddyUsage{}, map[string]codebuddyResultModelUsage{model: {}}), want: baseWant},
		{name: "zero_usage_arrives_before_real_usage", events: []json.RawMessage{assistant("msg_a", model, "", &codebuddyUsage{OutputTokens: 1}), a, a2}, want: baseWant},
		{name: "cache_only_response", events: []json.RawMessage{
			assistant("msg_cache", model, "", &codebuddyUsage{CacheReadInputTokens: 200, CacheCreationInputTokens: 10}),
			assistant("msg_cache", model, "", &codebuddyUsage{CacheReadInputTokens: 200, CacheCreationInputTokens: 10}),
		}, want: map[string]TokenUsage{model: {CacheReadTokens: 200, CacheWriteTokens: 10}}},
		{name: "interleaved_response_ids", events: []json.RawMessage{a, b, a2}, want: map[string]TokenUsage{model: {InputTokens: 130, CacheReadTokens: 260, CacheWriteTokens: 15}}},
		{name: "distinct_models", events: []json.RawMessage{a, assistant("msg_b", otherModel, "", secondUsage), a}, want: map[string]TokenUsage{
			model:      baseWant[model],
			otherModel: {InputTokens: 30, CacheReadTokens: 60, CacheWriteTokens: 5},
		}},
		{name: "usage_arrives_on_later_block", events: []json.RawMessage{assistant("msg_a", model, "", nil), a, a}, want: baseWant},
		{name: "model_arrives_on_later_block", events: []json.RawMessage{assistant("msg_a", "", "", usage), a, a}, want: baseWant},
		{name: "no_response_ids_remain_best_effort", events: []json.RawMessage{assistant("", model, "", usage), assistant("", model, "", secondUsage)}, want: map[string]TokenUsage{model: {InputTokens: 130, CacheReadTokens: 260, CacheWriteTokens: 15}}},
		{name: "subagent_events_are_not_main_loop_fallback", events: []json.RawMessage{assistant("msg_a", model, "parent_tool", usage), a, a}, want: baseWant},
		{name: "placeholder_output_alone_is_not_usage", events: []json.RawMessage{assistant("msg_a", model, "", &codebuddyUsage{OutputTokens: 1})}, want: map[string]TokenUsage{}},
		{name: "successful_model_totals_override_fallback", events: split, result: terminal(false, &codebuddyUsage{InputTokens: 999}, finalUsage), success: true, want: finalWant},
		{name: "failed_model_totals_override_fallback", events: split, result: terminal(true, &codebuddyUsage{InputTokens: 999}, finalUsage), want: finalWant},
		{name: "terminal_usage_without_model_totals", events: split, result: terminal(true, &codebuddyUsage{InputTokens: 150, OutputTokens: 50, CacheReadInputTokens: 300, CacheCreationInputTokens: 15}, nil), want: map[string]TokenUsage{model: finalWant[model]}},
		{name: "resumed_successful_model_totals_override_fallback", events: split, result: terminal(false, &codebuddyUsage{InputTokens: 999}, finalUsage), success: true, resumeSessionID: "session-resumed", want: finalWant},
		{name: "resumed_failed_model_totals_override_fallback", events: split, result: terminal(true, &codebuddyUsage{InputTokens: 999}, finalUsage), resumeSessionID: "session-resumed", want: finalWant},
		{name: "resumed_without_result", events: split, resumeSessionID: "session-resumed", want: baseWant},
		{name: "resumed_without_current_usage", result: terminal(true, cumulative, nil), resumeSessionID: "session-resumed", want: map[string]TokenUsage{}},
	}

	// The reporter captured per-run modelUsage alongside session-cumulative
	// usage. Exercise the missing/unusable modelUsage paths separately; those
	// result shapes are synthetic, not additional captured failure events.
	for _, failed := range []bool{false, true} {
		for _, totals := range []struct {
			name  string
			value any
		}{
			{name: "null"},
			{name: "empty", value: map[string]codebuddyResultModelUsage{}},
			{name: "zero", value: map[string]codebuddyResultModelUsage{model: {}}},
			{name: "unnamed", value: map[string]codebuddyResultModelUsage{"": {InputTokens: 999}}},
		} {
			cases = append(cases, testCase{
				name:   fmt.Sprintf("resumed_failed_%t_%s_model_totals", failed, totals.name),
				events: split, result: terminal(failed, cumulative, totals.value),
				success: !failed, resumeSessionID: "session-resumed", want: baseWant,
			})
		}
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend := codebuddyUsageFixtureBackend(t, tt.events, tt.result, !tt.success)
			result := executeCodebuddyUsageFixtureWithOptions(t, backend, len(tt.events), ExecOptions{
				Timeout: 5 * time.Second, ResumeSessionID: tt.resumeSessionID,
			})
			wantStatus := "failed"
			if tt.success {
				wantStatus = "completed"
			}
			if result.Status != wantStatus {
				t.Fatalf("status = %q, want %q: %s", result.Status, wantStatus, result.Error)
			}
			if !reflect.DeepEqual(result.Usage, tt.want) {
				t.Fatalf("usage = %#v, want %#v", result.Usage, tt.want)
			}
		})
	}
}

func TestCodebuddyResultUsageAcrossResume(t *testing.T) {
	t.Parallel()

	// Sanitized events from #10643: modelUsage is per run, while usage and
	// num_turns include the previous run on resume. No baseline subtraction
	// should be applied to modelUsage.
	events := []json.RawMessage{
		json.RawMessage(`{"type":"result","subtype":"success","is_error":false,"num_turns":2,"usage":{"input_tokens":29821,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"modelUsage":{"<MODEL>":{"inputTokens":29821,"outputTokens":2,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}}}`),
		json.RawMessage(`{"type":"result","subtype":"success","is_error":false,"num_turns":4,"usage":{"input_tokens":59665,"output_tokens":4,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"modelUsage":{"<MODEL>":{"inputTokens":29844,"outputTokens":2,"cacheReadInputTokens":0,"cacheCreationInputTokens":0}}}`),
	}
	var total TokenUsage
	for i, event := range events {
		var msg codebuddySDKMessage
		if err := json.Unmarshal(event, &msg); err != nil {
			t.Fatal(err)
		}
		usage := codebuddyResultUsage(msg, "<MODEL>", i > 0)
		wantInput := []int64{29821, 29844}[i]
		want := map[string]TokenUsage{"<MODEL>": {InputTokens: wantInput, OutputTokens: 2}}
		if !reflect.DeepEqual(usage, want) {
			t.Fatalf("run %d: usage = %#v, want %#v", i, usage, want)
		}
		total.InputTokens += usage["<MODEL>"].InputTokens
		total.OutputTokens += usage["<MODEL>"].OutputTokens
	}
	if want := (TokenUsage{InputTokens: 59665, OutputTokens: 4}); total != want {
		t.Fatalf("issue total = %+v, want %+v", total, want)
	}
}

// The seen-ID set must be per execution, not per backend: a reused backend
// must not treat the second run's response IDs as already counted.
func TestCodebuddyFallbackUsageIsScopedToExecution(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	event := json.RawMessage(`{"type":"assistant","message":{"id":"msg_reused","role":"assistant","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":200,"cache_creation_input_tokens":10},"content":[{"type":"text","text":"visible text"},{"type":"tool_use","id":"tool_reused","name":"Read","input":{"file_path":"fixture.txt"}}]}}`)
	backend := codebuddyUsageFixtureBackend(t, []json.RawMessage{event, event}, nil, true)
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprintf("execution_%d", i), func(t *testing.T) {
			t.Parallel()
			result := executeCodebuddyUsageFixture(t, backend, 2)
			want := TokenUsage{InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 10}
			if got := result.Usage["claude-sonnet-4-20250514"]; got != want {
				t.Fatalf("usage = %+v, want %+v", got, want)
			}
		})
	}
}

func codebuddyUsageFixtureBackend(t *testing.T, events []json.RawMessage, result json.RawMessage, fail bool) Backend {
	t.Helper()
	var stream strings.Builder
	for _, event := range events {
		stream.Write(event)
		stream.WriteByte('\n')
	}
	if result != nil {
		stream.Write(result)
		stream.WriteByte('\n')
	}
	dir := t.TempDir()
	fixture := filepath.Join(dir, "stream.jsonl")
	if err := os.WriteFile(fixture, []byte(stream.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCode := "0"
	if fail {
		exitCode = "1"
	}
	// Consume the prompt line first so the adapter's stdin write completes,
	// then replay the fixture. Never resolves an installed CodeBuddy CLI.
	script := "#!/bin/sh\nIFS= read -r _\ncat " + fixture + "\nexit " + exitCode + "\n"
	fakePath := filepath.Join(dir, "codebuddy")
	writeTestExecutable(t, fakePath, []byte(script))
	return &codebuddyBackend{cfg: Config{ExecutablePath: fakePath, Logger: slog.Default()}}
}

func executeCodebuddyUsageFixture(t *testing.T, backend Backend, eventCount int) Result {
	t.Helper()
	return executeCodebuddyUsageFixtureWithOptions(t, backend, eventCount, ExecOptions{Timeout: 5 * time.Second})
}

func executeCodebuddyUsageFixtureWithOptions(t *testing.T, backend Backend, eventCount int, opts ExecOptions) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "fixture", opts)
	if err != nil {
		t.Fatal(err)
	}
	texts, tools := 0, 0
	for message := range session.Messages {
		switch message.Type {
		case MessageText:
			texts++
		case MessageToolUse:
			tools++
		}
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("missing result")
	}
	// Repeated response IDs must suppress only usage, never text or tool blocks.
	if texts != eventCount || tools != eventCount {
		t.Fatalf("forwarded text/tool events = %d/%d, want %d each", texts, tools, eventCount)
	}
	return result
}
