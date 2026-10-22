package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Re-exec the test binary, never an installed Claude CLI. The fixture is a
// stream-json response; no model account or network connection is involved.
func runFakeClaudeUsageFixture() {
	if !bufio.NewScanner(os.Stdin).Scan() {
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Getenv("CLAUDE_USAGE_FIXTURE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		os.Exit(2)
	}
	if appendPath := os.Getenv("CLAUDE_USAGE_APPEND_FILE"); appendPath != "" {
		appendData, err := os.ReadFile(os.Getenv("CLAUDE_USAGE_APPEND_FIXTURE"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		f, err := os.OpenFile(appendPath, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if _, err := f.Write(appendData); err != nil {
			_ = f.Close()
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := f.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if os.Getenv("CLAUDE_USAGE_FIXTURE_FAIL") == "1" {
		os.Exit(1)
	}
}

func TestClaudeExecuteFallbackUsage(t *testing.T) {
	t.Parallel()
	const model = "claude-sonnet-4-6"
	const otherModel = "claude-haiku-4-5"
	// output_tokens on assistant messages is a message_start placeholder, not
	// the completed response's output count. Only terminal usage can supply it.
	usage := &claudeUsage{InputTokens: 100, OutputTokens: 1, CacheReadInputTokens: 200, CacheCreationInputTokens: 10}
	secondUsage := &claudeUsage{InputTokens: 30, OutputTokens: 1, CacheReadInputTokens: 60, CacheCreationInputTokens: 5}
	blockIndex := 0
	assistant := func(id, model, parent string, u *claudeUsage) json.RawMessage {
		blockIndex++
		return mustMarshal(t, map[string]any{
			"type": "assistant", "parent_tool_use_id": parent,
			"message": map[string]any{"id": id, "role": "assistant", "model": model, "usage": u,
				"content": []any{
					map[string]any{"type": "thinking", "thinking": "fixture thinking", "signature": "fixture-signature"},
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
		return mustMarshal(t, map[string]any{"type": "result", "subtype": subtype, "is_error": failed, "result": "fixture result", "model": model, "usage": usage, "modelUsage": modelUsage})
	}
	a := assistant("msg_a", model, "", usage)
	b := assistant("msg_b", model, "", secondUsage)
	a2 := assistant("msg_a", model, "", usage)
	split := []json.RawMessage{a, a2}
	baseWant := map[string]TokenUsage{model: {InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 10}}
	finalUsage := map[string]claudeResultModelUsage{
		model:      {InputTokens: 150, OutputTokens: 50, CacheReadInputTokens: 300, CacheCreationInputTokens: 15},
		otherModel: {InputTokens: 20, OutputTokens: 10, CacheReadInputTokens: 40, CacheCreationInputTokens: 2},
	}
	finalWant := map[string]TokenUsage{
		model:      {InputTokens: 150, OutputTokens: 50, CacheReadTokens: 300, CacheWriteTokens: 15},
		otherModel: {InputTokens: 20, OutputTokens: 10, CacheReadTokens: 40, CacheWriteTokens: 2},
	}
	cases := []struct {
		name    string
		events  []json.RawMessage
		result  json.RawMessage
		success bool
		want    map[string]TokenUsage
	}{
		{name: "split_response_without_result", events: split, want: baseWant},
		{name: "split_response_zero_result_usage", events: split, result: terminal(true, &claudeUsage{}, map[string]claudeResultModelUsage{model: {}}), want: baseWant},
		{name: "zero_usage_arrives_before_real_usage", events: []json.RawMessage{assistant("msg_a", model, "", &claudeUsage{OutputTokens: 1}), a, a2}, want: baseWant},
		{name: "cache_only_response", events: []json.RawMessage{assistant("msg_cache", model, "", &claudeUsage{CacheReadInputTokens: 200, CacheCreationInputTokens: 10}), assistant("msg_cache", model, "", &claudeUsage{CacheReadInputTokens: 200, CacheCreationInputTokens: 10})}, want: map[string]TokenUsage{model: {CacheReadTokens: 200, CacheWriteTokens: 10}}},
		{name: "interleaved_response_ids", events: []json.RawMessage{a, b, a2}, want: map[string]TokenUsage{model: {InputTokens: 130, CacheReadTokens: 260, CacheWriteTokens: 15}}},
		{name: "distinct_models", events: []json.RawMessage{a, assistant("msg_b", otherModel, "", secondUsage), a}, want: map[string]TokenUsage{model: baseWant[model], otherModel: {InputTokens: 30, CacheReadTokens: 60, CacheWriteTokens: 5}}},
		{name: "usage_arrives_on_later_block", events: []json.RawMessage{assistant("msg_a", model, "", nil), a, a}, want: baseWant},
		{name: "model_arrives_on_later_block", events: []json.RawMessage{assistant("msg_a", "", "", usage), a, a}, want: baseWant},
		{name: "no_response_ids_remain_best_effort", events: []json.RawMessage{assistant("", model, "", usage), assistant("", model, "", secondUsage)}, want: map[string]TokenUsage{model: {InputTokens: 130, CacheReadTokens: 260, CacheWriteTokens: 15}}},
		{name: "subagent_events_are_not_main_loop_fallback", events: []json.RawMessage{assistant("msg_a", model, "parent_tool", usage), a, a}, want: baseWant},
		{name: "placeholder_output_alone_is_not_usage", events: []json.RawMessage{assistant("msg_a", model, "", &claudeUsage{OutputTokens: 1})}, want: map[string]TokenUsage{}},
		{name: "successful_model_totals_override_fallback", events: split, result: terminal(false, &claudeUsage{InputTokens: 999}, finalUsage), success: true, want: finalWant},
		{name: "failed_model_totals_override_fallback", events: split, result: terminal(true, &claudeUsage{InputTokens: 999}, finalUsage), want: finalWant},
		{name: "terminal_usage_without_model_totals", events: split, result: terminal(true, &claudeUsage{InputTokens: 150, OutputTokens: 50, CacheReadInputTokens: 300, CacheCreationInputTokens: 15}, nil), want: map[string]TokenUsage{model: finalWant[model]}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend := claudeUsageFixtureBackend(t, tt.events, tt.result, !tt.success)
			result := executeClaudeUsageFixture(t, backend, len(tt.events))
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

func TestClaudeFallbackUsageIsScopedToExecution(t *testing.T) {
	t.Parallel()
	event := json.RawMessage(`{"type":"assistant","message":{"id":"msg_reused","role":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":200,"cache_creation_input_tokens":10},"content":[{"type":"text","text":"visible text"},{"type":"tool_use","id":"tool_reused","name":"Read","input":{"file_path":"fixture.txt"}}]}}`)
	backend := claudeUsageFixtureBackend(t, []json.RawMessage{event, event}, nil, true)
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprintf("execution_%d", i), func(t *testing.T) {
			t.Parallel()
			result := executeClaudeUsageFixture(t, backend, 2)
			want := TokenUsage{InputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 10}
			if got := result.Usage["claude-sonnet-4-6"]; got != want {
				t.Fatalf("usage = %+v, want %+v", got, want)
			}
		})
	}
}

func TestClaudeExecuteSubtractsResumedCostStateBaseline(t *testing.T) {
	const sessionID = "session-cumulative"
	const model = "claude-sonnet-4-6"
	const otherModel = "claude-haiku-4-5"
	configDir := t.TempDir()
	writeClaudeUsageSession(t, configDir, sessionID,
		claudeCostStateFixture(t, map[string]claudeResultModelUsage{
			model: {InputTokens: 100, OutputTokens: 20, CacheReadInputTokens: 300, CacheCreationInputTokens: 40},
		}),
		[]byte(`{"type":"user","message":{"role":"user","content":"after baseline"}}`+"\n"),
	)

	resultEvent := mustMarshal(t, map[string]any{
		"type": "result", "subtype": "success", "session_id": sessionID, "result": "done",
		"modelUsage": map[string]claudeResultModelUsage{
			model:      {InputTokens: 115, OutputTokens: 27, CacheReadInputTokens: 360, CacheCreationInputTokens: 42},
			otherModel: {InputTokens: 3, OutputTokens: 5, CacheReadInputTokens: 11, CacheCreationInputTokens: 2},
		},
	})
	backend := claudeUsageFixtureBackendWithEnv(t, nil, resultEvent, false, map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	})
	result := executeClaudeUsageFixtureWithOptions(t, backend, 0, ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionID,
	})
	want := map[string]TokenUsage{
		model:      {InputTokens: 15, OutputTokens: 7, CacheReadTokens: 60, CacheWriteTokens: 2},
		otherModel: {InputTokens: 3, OutputTokens: 5, CacheReadTokens: 11, CacheWriteTokens: 2},
	}
	if !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestClaudeExecuteKeepsPerRunUsageWithoutCostState(t *testing.T) {
	const sessionID = "session-pre-cost-state"
	const model = "claude-sonnet-4-6"
	configDir := t.TempDir()
	writeClaudeUsageSession(t, configDir, sessionID,
		[]byte(`{"type":"user","message":{"role":"user","content":"old Claude Code session"}}`+"\n"),
	)
	resultEvent := mustMarshal(t, map[string]any{
		"type": "result", "subtype": "success", "session_id": sessionID, "result": "done",
		"modelUsage": map[string]claudeResultModelUsage{
			model: {InputTokens: 15, OutputTokens: 7, CacheReadInputTokens: 60, CacheCreationInputTokens: 2},
		},
	})
	backend := claudeUsageFixtureBackendWithEnv(t, nil, resultEvent, false, map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	})
	result := executeClaudeUsageFixtureWithOptions(t, backend, 0, ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionID,
	})
	want := map[string]TokenUsage{
		model: {InputTokens: 15, OutputTokens: 7, CacheReadTokens: 60, CacheWriteTokens: 2},
	}
	if !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestClaudeExecuteFallsBackWhenSessionFileIsMalformed(t *testing.T) {
	const sessionID = "session-malformed"
	const model = "claude-sonnet-4-6"
	configDir := t.TempDir()
	writeClaudeUsageSession(t, configDir, sessionID, []byte("not-json\n"))
	resultEvent := mustMarshal(t, map[string]any{
		"type": "result", "subtype": "success", "session_id": sessionID, "result": "done",
		"modelUsage": map[string]claudeResultModelUsage{
			model: {InputTokens: 15, OutputTokens: 7, CacheReadInputTokens: 60, CacheCreationInputTokens: 2},
		},
	})
	backend := claudeUsageFixtureBackendWithEnv(t, nil, resultEvent, false, map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	})
	result := executeClaudeUsageFixtureWithOptions(t, backend, 0, ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionID,
	})
	want := map[string]TokenUsage{
		model: {InputTokens: 15, OutputTokens: 7, CacheReadTokens: 60, CacheWriteTokens: 2},
	}
	if !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestClaudeExecuteUsesAppendedCostStateWithoutResult(t *testing.T) {
	const sessionID = "session-cancelled"
	const model = "claude-sonnet-4-6"
	configDir := t.TempDir()
	sessionPath := writeClaudeUsageSession(t, configDir, sessionID,
		claudeCostStateFixture(t, map[string]claudeResultModelUsage{
			model: {InputTokens: 100, OutputTokens: 20, CacheReadInputTokens: 300, CacheCreationInputTokens: 40},
		}),
	)
	appended := filepath.Join(t.TempDir(), "appended.jsonl")
	if err := os.WriteFile(appended, claudeCostStateFixture(t, map[string]claudeResultModelUsage{
		model: {InputTokens: 115, OutputTokens: 27, CacheReadInputTokens: 360, CacheCreationInputTokens: 42},
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	assistant := json.RawMessage(`{"type":"assistant","message":{"id":"cancelled","role":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":1},"content":[{"type":"text","text":"partial"},{"type":"tool_use","id":"tool_cancelled","name":"Read","input":{}}]}}`)
	backend := claudeUsageFixtureBackendWithEnv(t, []json.RawMessage{assistant}, nil, true, map[string]string{
		"CLAUDE_CONFIG_DIR":           configDir,
		"CLAUDE_USAGE_APPEND_FILE":    sessionPath,
		"CLAUDE_USAGE_APPEND_FIXTURE": appended,
	})
	result := executeClaudeUsageFixtureWithOptions(t, backend, 1, ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionID,
	})
	want := map[string]TokenUsage{
		model: {InputTokens: 15, OutputTokens: 7, CacheReadTokens: 60, CacheWriteTokens: 2},
	}
	if !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestClaudeExecuteKeepsStreamFallbackWithoutAppendedCostState(t *testing.T) {
	const sessionID = "session-killed"
	const model = "claude-sonnet-4-6"
	configDir := t.TempDir()
	writeClaudeUsageSession(t, configDir, sessionID,
		claudeCostStateFixture(t, map[string]claudeResultModelUsage{
			model: {InputTokens: 100, OutputTokens: 20, CacheReadInputTokens: 300, CacheCreationInputTokens: 40},
		}),
	)
	assistant := json.RawMessage(`{"type":"assistant","message":{"id":"killed","role":"assistant","model":"claude-sonnet-4-6","usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":1},"content":[{"type":"text","text":"partial"},{"type":"tool_use","id":"tool_killed","name":"Read","input":{}}]}}`)
	backend := claudeUsageFixtureBackendWithEnv(t, []json.RawMessage{assistant}, nil, true, map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	})
	result := executeClaudeUsageFixtureWithOptions(t, backend, 1, ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionID,
	})
	want := map[string]TokenUsage{
		model: {InputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 1},
	}
	if !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}
}

func TestClaudeResultUsageSinceCumulativeTurns(t *testing.T) {
	t.Parallel()
	const model = "claude-sonnet-4-6"
	totals := []claudeResultModelUsage{
		{InputTokens: 10, OutputTokens: 4, CacheReadInputTokens: 100, CacheCreationInputTokens: 20},
		{InputTokens: 17, OutputTokens: 9, CacheReadInputTokens: 160, CacheCreationInputTokens: 23},
		{InputTokens: 20, OutputTokens: 15, CacheReadInputTokens: 205, CacheCreationInputTokens: 31},
	}
	wants := []TokenUsage{
		{InputTokens: 10, OutputTokens: 4, CacheReadTokens: 100, CacheWriteTokens: 20},
		{InputTokens: 7, OutputTokens: 5, CacheReadTokens: 60, CacheWriteTokens: 3},
		{InputTokens: 3, OutputTokens: 6, CacheReadTokens: 45, CacheWriteTokens: 8},
	}
	var baseline map[string]TokenUsage
	for turn, total := range totals {
		msg := claudeSDKMessage{ModelUsage: map[string]claudeResultModelUsage{model: total}}
		usage, authoritative := claudeResultUsageSince(msg, model, baseline)
		if !authoritative {
			t.Fatalf("turn %d usage was not authoritative", turn+1)
		}
		if got := usage[model]; got != wants[turn] {
			t.Fatalf("turn %d usage = %+v, want %+v", turn+1, got, wants[turn])
		}
		baseline = claudeModelUsage(msg.ModelUsage)
	}

	perRun := claudeSDKMessage{
		Model: model,
		Usage: &claudeUsage{InputTokens: 2, OutputTokens: 3, CacheReadInputTokens: 4, CacheCreationInputTokens: 5},
	}
	usage, authoritative := claudeResultUsageSince(perRun, model, baseline)
	if !authoritative {
		t.Fatal("top-level per-run usage was not authoritative")
	}
	if got, want := usage[model], (TokenUsage{InputTokens: 2, OutputTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 5}); got != want {
		t.Fatalf("top-level per-run usage = %+v, want %+v", got, want)
	}

	reset := claudeSDKMessage{ModelUsage: map[string]claudeResultModelUsage{
		model: {InputTokens: 30, OutputTokens: 14, CacheReadInputTokens: 300, CacheCreationInputTokens: 40},
	}}
	usage, authoritative = claudeResultUsageSince(reset, model, baseline)
	if want := claudeModelUsage(reset.ModelUsage); !authoritative || !reflect.DeepEqual(usage, want) {
		t.Fatalf("reset usage = %#v, authoritative=%v; want raw usage %#v", usage, authoritative, want)
	}

	baselineWithAnotherModel := map[string]TokenUsage{
		model:              baseline[model],
		"claude-haiku-4-5": {InputTokens: 1},
	}
	currentWithoutModel := claudeSDKMessage{ModelUsage: map[string]claudeResultModelUsage{model: totals[2]}}
	usage, authoritative = claudeResultUsageSince(currentWithoutModel, model, baselineWithAnotherModel)
	if want := claudeModelUsage(currentWithoutModel.ModelUsage); !authoritative || !reflect.DeepEqual(usage, want) {
		t.Fatalf("missing-model usage = %#v, authoritative=%v; want raw usage %#v", usage, authoritative, want)
	}
}

func TestReadLastClaudeCostStateUsageAcrossChunks(t *testing.T) {
	t.Parallel()
	const model = "claude-sonnet-4-6"
	baseline := claudeCostStateFixture(t, map[string]claudeResultModelUsage{
		model: {InputTokens: 10, OutputTokens: 4, CacheReadInputTokens: 100, CacheCreationInputTokens: 20},
	})
	largeTrailingLine := mustMarshal(t, map[string]any{
		"type":    "user",
		"message": map[string]any{"content": strings.Repeat("x", claudeSessionReadChunkSize*2)},
	})
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := append(append([]byte(nil), baseline...), largeTrailingLine...)
	data = append(data, '\n')
	data = append(data, []byte(`{"type":"cost-state"`)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	usage, found, err := readLastClaudeCostStateUsage(f, 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("cost-state not found")
	}
	want := TokenUsage{InputTokens: 10, OutputTokens: 4, CacheReadTokens: 100, CacheWriteTokens: 20}
	if got := usage[model]; got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
	if _, found, err := readLastClaudeCostStateUsage(f, int64(len(baseline)), int64(len(data))); err != nil || found {
		t.Fatalf("appended range found=%v err=%v, want no cost-state", found, err)
	}

	emptyPath := filepath.Join(t.TempDir(), "empty-cost-state.jsonl")
	emptyCostState := claudeCostStateFixture(t, map[string]claudeResultModelUsage{})
	if err := os.WriteFile(emptyPath, emptyCostState, 0o600); err != nil {
		t.Fatal(err)
	}
	emptyFile, err := os.Open(emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer emptyFile.Close()
	if usage, found, err := readLastClaudeCostStateUsage(emptyFile, 0, int64(len(emptyCostState))); err != nil || found || len(usage) != 0 {
		t.Fatalf("empty cost-state usage=%#v found=%v err=%v, want non-authoritative", usage, found, err)
	}
}

func claudeCostStateFixture(t *testing.T, usage map[string]claudeResultModelUsage) []byte {
	t.Helper()
	data := mustMarshal(t, map[string]any{"type": "cost-state", "modelUsage": usage})
	return append(data, '\n')
}

func writeClaudeUsageSession(t *testing.T, configDir, sessionID string, lines ...[]byte) string {
	t.Helper()
	dir := filepath.Join(configDir, "projects", "fixture-project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	var data []byte
	for _, line := range lines {
		data = append(data, line...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func claudeUsageFixtureBackend(t *testing.T, events []json.RawMessage, result json.RawMessage, fail bool) Backend {
	return claudeUsageFixtureBackendWithEnv(t, events, result, fail, nil)
}

func claudeUsageFixtureBackendWithEnv(t *testing.T, events []json.RawMessage, result json.RawMessage, fail bool, extraEnv map[string]string) Backend {
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
	fixture := filepath.Join(t.TempDir(), "stream.jsonl")
	if err := os.WriteFile(fixture, []byte(stream.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	failEnv := "0"
	if fail {
		failEnv = "1"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"IS_SANDBOX": "1", "CLAUDE_FAKE_MODE": "usage_fixture", "CLAUDE_USAGE_FIXTURE": fixture, "CLAUDE_USAGE_FIXTURE_FAIL": failEnv,
	}
	for key, value := range extraEnv {
		env[key] = value
	}
	backend, err := New("claude", Config{ExecutablePath: executable, Logger: slog.Default(), Env: env})
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func executeClaudeUsageFixture(t *testing.T, backend Backend, eventCount int) Result {
	return executeClaudeUsageFixtureWithOptions(t, backend, eventCount, ExecOptions{Timeout: 5 * time.Second})
}

func executeClaudeUsageFixtureWithOptions(t *testing.T, backend Backend, eventCount int, opts ExecOptions) Result {
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
