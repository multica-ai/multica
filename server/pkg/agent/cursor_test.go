package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewReturnsCursorBackend(t *testing.T) {
	t.Parallel()
	b, err := New("cursor", Config{ExecutablePath: "/nonexistent/cursor-agent"})
	if err != nil {
		t.Fatalf("New(cursor) error: %v", err)
	}
	if _, ok := b.(*cursorBackend); !ok {
		t.Fatalf("expected *cursorBackend, got %T", b)
	}
}

func TestBuildCursorArgs(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("do something", ExecOptions{
		Cwd:   "/tmp/work",
		Model: "composer-1.5",
	}, slog.Default())

	expected := []string{
		"-p", "do something",
		"--output-format", "stream-json",
		"--yolo",
		"--workspace", "/tmp/work",
		"--model", "composer-1.5",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestBuildCursorArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("continue", ExecOptions{
		ResumeSessionID: "sess-123",
	}, slog.Default())

	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "sess-123" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Fatalf("expected --resume sess-123, got %v", args)
	}
}

func TestBuildCursorArgsMinimal(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("hello", ExecOptions{}, slog.Default())
	expected := []string{"-p", "hello", "--output-format", "stream-json", "--yolo"}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
}

func TestBuildCursorArgsIgnoresSystemPromptAndMaxTurns(t *testing.T) {
	t.Parallel()

	// cursor-agent CLI does not support --system-prompt or --max-turns;
	// verify they are NOT emitted even when set in ExecOptions.
	args := buildCursorArgs("task", ExecOptions{
		SystemPrompt: "You are helpful",
		MaxTurns:     5,
	}, slog.Default())

	for _, a := range args {
		if a == "--system-prompt" {
			t.Fatalf("unexpected --system-prompt in args: %v", args)
		}
		if a == "--max-turns" {
			t.Fatalf("unexpected --max-turns in args: %v", args)
		}
	}
}

func TestBuildCursorArgsCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCursorArgs("task", ExecOptions{
		CustomArgs: []string{"--extra", "val", "--yolo", "--output-format", "text"},
	}, slog.Default())

	// --extra val should be present; --yolo and --output-format should be filtered out
	hasExtra := false
	hasBlockedYolo := false
	hasBlockedFormat := false
	for i, a := range args {
		if a == "--extra" && i+1 < len(args) && args[i+1] == "val" {
			hasExtra = true
		}
	}
	// Count occurrences of --yolo (should be exactly 1 — the hardcoded one)
	yoloCount := 0
	for _, a := range args {
		if a == "--yolo" {
			yoloCount++
		}
		if a == "text" {
			hasBlockedFormat = true
		}
	}
	if yoloCount > 1 {
		hasBlockedYolo = true
	}
	if !hasExtra {
		t.Fatalf("expected --extra val in args, got %v", args)
	}
	if hasBlockedYolo {
		t.Fatalf("--yolo from custom args should be filtered, got %v", args)
	}
	if hasBlockedFormat {
		t.Fatalf("--output-format from custom args should be filtered, got %v", args)
	}
}

func TestNormalizeCursorStreamLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{`stdout: {"type":"init"}`, `{"type":"init"}`},
		{`stderr: {"type":"error"}`, `{"type":"error"}`},
		{`stdout:{"type":"init"}`, `{"type":"init"}`},
		{`  {"type":"assistant"}  `, `{"type":"assistant"}`},
		{``, ``},
		{`  `, ``},
		{`plain text`, `plain text`},
	}

	for _, tc := range tests {
		got := normalizeCursorStreamLine(tc.input)
		if got != tc.want {
			t.Errorf("normalizeCursorStreamLine(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCursorHandleAssistantText(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Model: "composer-1.5",
			Content: []cursorContentBlock{
				{Type: "output_text", Text: "Hello from Cursor"},
			},
			Usage: &cursorUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	if output.String() != "Hello from Cursor" {
		t.Fatalf("expected output 'Hello from Cursor', got %q", output.String())
	}

	select {
	case m := <-ch:
		if m.Type != MessageText || m.Content != "Hello from Cursor" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestCursorHandleAssistantToolUse(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Content: []cursorContentBlock{
				{
					Type:  "tool_use",
					ID:    "call-42",
					Name:  "file_edit",
					Input: mustMarshal(t, map[string]any{"path": "/tmp/foo.go"}),
				},
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	select {
	case m := <-ch:
		if m.Type != MessageToolUse || m.Tool != "file_edit" || m.CallID != "call-42" {
			t.Fatalf("unexpected message: %+v", m)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestCursorErrorText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  cursorStreamEvent
		want string
	}{
		{"error field", cursorStreamEvent{ErrorMsg: "bad request"}, "bad request"},
		{"detail field", cursorStreamEvent{Detail: "not found"}, "not found"},
		{"result field", cursorStreamEvent{ResultText: "failed"}, "failed"},
		{"empty", cursorStreamEvent{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cursorErrorText(&tc.evt)
			if got != tc.want {
				t.Errorf("cursorErrorText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCursorUsageModelFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		evtModel         string
		configuredModel  string
		want             string
	}{
		{"event model wins", "gpt-5.3-codex", "composer-2.5", "gpt-5.3-codex"},
		{"configured model fallback", "", "composer-2.5", "composer-2.5"},
		{"default cursor", "", "", "cursor"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cursorUsageModel(tc.evtModel, tc.configuredModel)
			if got != tc.want {
				t.Fatalf("cursorUsageModel(%q, %q) = %q, want %q", tc.evtModel, tc.configuredModel, got, tc.want)
			}
		})
	}
}

func TestCursorAccumulateResultUsageUsesConfiguredModel(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	usage := make(map[string]TokenUsage)
	evt := &cursorStreamEvent{
		InputTokens:  400,
		OutputTokens: 200,
	}
	b.accumulateResultUsage(usage, evt, "composer-2.5")
	u := usage["composer-2.5"]
	if u.InputTokens != 400 || u.OutputTokens != 200 {
		t.Fatalf("unexpected usage: %+v", u)
	}
	if _, ok := usage["cursor"]; ok {
		t.Fatalf("expected configured model key, got cursor fallback: %+v", usage)
	}
}

func TestCursorAccumulateResultUsage(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}

	// Nested usage object (snake_case keys) — compatible with
	// cursor-agent versions that wrap usage in a sub-object.
	t.Run("nested_usage_object", func(t *testing.T) {
		usage := make(map[string]TokenUsage)
		evt := &cursorStreamEvent{
			Model: "gpt-5.3",
			Usage: &cursorUsage{
				InputTokens:           200,
				OutputTokens:          100,
				CacheReadInputTokens:  50,
				CacheWriteInputTokens: 25,
			},
		}
		b.accumulateResultUsage(usage, evt, "")
		u := usage["gpt-5.3"]
		if u.InputTokens != 200 || u.OutputTokens != 100 || u.CacheReadTokens != 50 || u.CacheWriteTokens != 25 {
			t.Fatalf("unexpected usage: %+v", u)
		}
	})

	// Top-level camelCase fields (cursor-agent v0.46+) — the current
	// default shape from the Cursor CLI. When present, they take
	// precedence over any nested usage object.
	t.Run("top_level_camelcase", func(t *testing.T) {
		usage := make(map[string]TokenUsage)
		evt := &cursorStreamEvent{
			Model:            "gpt-5.3",
			InputTokens:      300,
			OutputTokens:     150,
			CacheReadTokens:  75,
			CacheWriteTokens: 25,
		}
		b.accumulateResultUsage(usage, evt, "")
		u := usage["gpt-5.3"]
		if u.InputTokens != 300 || u.OutputTokens != 150 || u.CacheReadTokens != 75 || u.CacheWriteTokens != 25 {
			t.Fatalf("unexpected usage: %+v (want input=300 output=150 cache_read=75 cache_write=25)", u)
		}
	})

	// Top-level fields win when both shapes are present — this
	// prevents double-counting from the nested fallback.
	t.Run("top_level_wins_over_nested", func(t *testing.T) {
		usage := make(map[string]TokenUsage)
		evt := &cursorStreamEvent{
			Model:        "gpt-5.3",
			InputTokens:  300,
			OutputTokens: 150,
			Usage: &cursorUsage{
				InputTokens:          999,
				OutputTokens:         888,
				CacheReadInputTokens: 777,
			},
		}
		b.accumulateResultUsage(usage, evt, "")
		u := usage["gpt-5.3"]
		if u.InputTokens != 300 || u.OutputTokens != 150 || u.CacheReadTokens != 0 {
			t.Fatalf("unexpected usage: %+v (want input=300 output=150 cache=0)", u)
		}
	})

	// No usage at all — early return, map unchanged.
	t.Run("no_usage", func(t *testing.T) {
		usage := make(map[string]TokenUsage)
		evt := &cursorStreamEvent{
			Model: "gpt-5.3",
		}
		b.accumulateResultUsage(usage, evt, "")
		if _, ok := usage["gpt-5.3"]; ok {
			t.Fatalf("expected no entry, got %+v", usage["gpt-5.3"])
		}
	})

	// Empty model defaults to "cursor".
	t.Run("default_model", func(t *testing.T) {
		usage := make(map[string]TokenUsage)
		evt := &cursorStreamEvent{
			InputTokens:  50,
			OutputTokens: 25,
		}
		b.accumulateResultUsage(usage, evt, "")
		u := usage["cursor"]
		if u.InputTokens != 50 || u.OutputTokens != 25 {
			t.Fatalf("unexpected usage: %+v (want input=50 output=25)", u)
		}
	})
}

func TestCursorUsageOnlyFromResult(t *testing.T) {
	t.Parallel()

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	var output strings.Builder

	evt := &cursorStreamEvent{
		Type: "assistant",
		Message: mustMarshal(t, cursorAssistantMessage{
			Model: "gpt-5",
			Content: []cursorContentBlock{
				{Type: "text", Text: "hello"},
			},
			Usage: &cursorUsage{
				InputTokens:  999,
				OutputTokens: 888,
			},
		}),
	}

	b.handleCursorAssistant(evt, ch, &output)

	if output.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", output.String())
	}

	// handleCursorAssistant should NOT have accumulated usage anywhere —
	// usage is only taken from result events to avoid double-counting.
	// (no usage map to check; this test documents the intent)
}

func TestCursorStepFinishParsing(t *testing.T) {
	t.Parallel()

	part := cursorStepFinishPart{}
	data := `{"tokens":{"input":500,"output":200,"cache":{"read":100}},"cost":0.01}`
	if err := json.Unmarshal([]byte(data), &part); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if part.Tokens.Input != 500 || part.Tokens.Output != 200 || part.Tokens.Cache.Read != 100 {
		t.Fatalf("unexpected part: %+v", part)
	}
}

// TestCursorUsageNoDoubleCount verifies that step_finish and result usage
// are never double-counted. When a result event includes usage (session
// totals), step_finish values must be discarded entirely.
func TestCursorUsageNoDoubleCount(t *testing.T) {
	t.Parallel()

	type jsonlEvent struct {
		raw string
	}

	tests := []struct {
		name  string
		lines []string
		want  map[string]TokenUsage
	}{
		{
			name: "result_only — use result usage",
			lines: []string{
				`{"type":"result","model":"gpt-5","usage":{"input_tokens":1000,"output_tokens":500,"cached_input_tokens":200}}`,
			},
			want: map[string]TokenUsage{
				"gpt-5": {InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 200},
			},
		},
		{
			name: "step_finish_only — fallback to step usage",
			lines: []string{
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":300,"output":100,"cache":{"read":50}}}}`,
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":200,"output":80,"cache":{"read":30}}}}`,
				`{"type":"result","model":"gpt-5"}`,
			},
			want: map[string]TokenUsage{
				"gpt-5": {InputTokens: 500, OutputTokens: 180, CacheReadTokens: 80},
			},
		},
		{
			name: "step_finish_then_result — result wins, no double count",
			lines: []string{
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":300,"output":100,"cache":{"read":50}}}}`,
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":200,"output":80,"cache":{"read":30}}}}`,
				`{"type":"result","model":"gpt-5","usage":{"input_tokens":500,"output_tokens":180,"cached_input_tokens":80}}`,
			},
			want: map[string]TokenUsage{
				"gpt-5": {InputTokens: 500, OutputTokens: 180, CacheReadTokens: 80},
			},
		},
		{
			name: "multi_model — each model tracked independently",
			lines: []string{
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":100,"output":50,"cache":{"read":10}}}}`,
				`{"type":"step_finish","model":"sonnet-4","part":{"tokens":{"input":200,"output":80,"cache":{"read":20}}}}`,
				`{"type":"result","model":"gpt-5","usage":{"input_tokens":100,"output_tokens":50,"cached_input_tokens":10}}`,
			},
			want: map[string]TokenUsage{
				// result had usage → use result only, discard all step_finish
				"gpt-5": {InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10},
			},
		},
		{
			name: "camelcase_result — top-level inputTokens/outputTokens (v0.46+)",
			lines: []string{
				`{"type":"result","model":"gpt-5","inputTokens":1000,"outputTokens":500,"cacheReadTokens":250,"cacheWriteTokens":50}`,
			},
			want: map[string]TokenUsage{
				"gpt-5": {InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 250, CacheWriteTokens: 50},
			},
		},
		{
			name: "nested_camelcase_result — actual cursor-agent stream-json shape",
			lines: []string{
				`{"type":"result","subtype":"success","duration_ms":10606,"duration_api_ms":10606,"is_error":false,"result":"pong","session_id":"b729a81b-9825-471d-812d-377c547b91e4","request_id":"4126abbe-dbc7-4ea4-a83e-7fab284c559c","usage":{"inputTokens":26640,"outputTokens":40,"cacheReadTokens":467,"cacheWriteTokens":12}}`,
			},
			want: map[string]TokenUsage{
				"cursor": {InputTokens: 26640, OutputTokens: 40, CacheReadTokens: 467, CacheWriteTokens: 12},
			},
		},
		{
			name: "camelcase_result_wins_over_step_finish — no double count",
			lines: []string{
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":300,"output":100,"cache":{"read":50}}}}`,
				`{"type":"step_finish","model":"gpt-5","part":{"tokens":{"input":200,"output":80,"cache":{"read":30}}}}`,
				`{"type":"result","model":"gpt-5","inputTokens":500,"outputTokens":180}`,
			},
			want: map[string]TokenUsage{
				"gpt-5": {InputTokens: 500, OutputTokens: 180},
			},
		},
		{
			name: "camelcase_default_model — no model field defaults to cursor",
			lines: []string{
				`{"type":"result","inputTokens":400,"outputTokens":200}`,
			},
			want: map[string]TokenUsage{
				"cursor": {InputTokens: 400, OutputTokens: 200},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stepUsage := make(map[string]TokenUsage)
			resultUsage := make(map[string]TokenUsage)
			hasResultUsage := false

			b := &cursorBackend{cfg: Config{Logger: slog.Default()}}

			for _, line := range tc.lines {
				var evt cursorStreamEvent
				if err := json.Unmarshal([]byte(line), &evt); err != nil {
					t.Fatalf("unmarshal %q: %v", line, err)
				}

				switch evt.Type {
				case "result":
					b.accumulateResultUsage(resultUsage, &evt, "")
					if evt.hasResultUsage() {
						hasResultUsage = true
					}
				case "step_finish":
					if evt.Part != nil {
						var part cursorStepFinishPart
						_ = json.Unmarshal(evt.Part, &part)
						model := evt.Model
						if model == "" {
							model = "cursor"
						}
						u := stepUsage[model]
						u.InputTokens += int64(part.Tokens.Input)
						u.OutputTokens += int64(part.Tokens.Output)
						u.CacheReadTokens += int64(part.Tokens.Cache.Read)
						stepUsage[model] = u
					}
				}
			}

			if !hasResultUsage {
				resultUsage = stepUsage
			}

			if len(resultUsage) != len(tc.want) {
				t.Fatalf("got %d models, want %d: %+v", len(resultUsage), len(tc.want), resultUsage)
			}
			for model, want := range tc.want {
				got := resultUsage[model]
				if got != want {
					t.Errorf("model %q: got %+v, want %+v", model, got, want)
				}
			}
		})
	}
}

// TestCursorStreamEventUnmarshalTopLevelCamelCase verifies that the
// cursorStreamEvent struct correctly deserializes result events where token
// usage fields are top-level camelCase keys.
func TestCursorStreamEventUnmarshalTopLevelCamelCase(t *testing.T) {
	t.Parallel()

	raw := `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"abc-123","model":"gpt-5.3","inputTokens":1500,"outputTokens":300,"cacheReadTokens":75,"cacheWriteTokens":25,"duration_ms":5234,"duration_api_ms":5100}`

	var evt cursorStreamEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if evt.Type != "result" {
		t.Errorf("type = %q, want result", evt.Type)
	}
	if evt.SessionID != "abc-123" {
		t.Errorf("session_id = %q, want abc-123", evt.SessionID)
	}
	if evt.Model != "gpt-5.3" {
		t.Errorf("model = %q, want gpt-5.3", evt.Model)
	}
	if evt.InputTokens != 1500 {
		t.Errorf("inputTokens = %d, want 1500", evt.InputTokens)
	}
	if evt.OutputTokens != 300 {
		t.Errorf("outputTokens = %d, want 300", evt.OutputTokens)
	}
	if evt.CacheReadTokens != 75 {
		t.Errorf("cacheReadTokens = %d, want 75", evt.CacheReadTokens)
	}
	if evt.CacheWriteTokens != 25 {
		t.Errorf("cacheWriteTokens = %d, want 25", evt.CacheWriteTokens)
	}
	if evt.Usage != nil {
		t.Errorf("usage = %+v, want nil", evt.Usage)
	}

	// Verify accumulateResultUsage processes the new shape.
	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	usage := make(map[string]TokenUsage)
	b.accumulateResultUsage(usage, &evt, "")
	u := usage["gpt-5.3"]
	if u.InputTokens != 1500 || u.OutputTokens != 300 || u.CacheReadTokens != 75 || u.CacheWriteTokens != 25 {
		t.Fatalf("accumulated usage = %+v, want input=1500 output=300 cache_read=75 cache_write=25", u)
	}
}

// TestCursorStreamEventUnmarshalNestedCamelCase verifies the stream-json shape
// emitted by the locally installed cursor-agent version.
func TestCursorStreamEventUnmarshalNestedCamelCase(t *testing.T) {
	t.Parallel()

	raw := `{"type":"result","subtype":"success","duration_ms":10606,"duration_api_ms":10606,"is_error":false,"result":"pong","session_id":"b729a81b-9825-471d-812d-377c547b91e4","request_id":"4126abbe-dbc7-4ea4-a83e-7fab284c559c","usage":{"inputTokens":26640,"outputTokens":40,"cacheReadTokens":467,"cacheWriteTokens":12}}`

	var evt cursorStreamEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if evt.Usage == nil {
		t.Fatal("usage should be non-nil")
	}
	if evt.Usage.InputTokens != 26640 || evt.Usage.OutputTokens != 40 || evt.Usage.CacheReadInputTokens != 467 || evt.Usage.CacheWriteInputTokens != 12 {
		t.Fatalf("usage = %+v, want input=26640 output=40 cache_read=467 cache_write=12", evt.Usage)
	}

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	usage := make(map[string]TokenUsage)
	b.accumulateResultUsage(usage, &evt, "")
	u := usage["cursor"]
	if u.InputTokens != 26640 || u.OutputTokens != 40 || u.CacheReadTokens != 467 || u.CacheWriteTokens != 12 {
		t.Fatalf("accumulated usage = %+v, want input=26640 output=40 cache_read=467 cache_write=12", u)
	}
}

// TestCursorStreamEventUnmarshalLegacyUsage verifies backwards compatibility
// with cursor-agent versions that wrap usage in a nested object.
func TestCursorStreamEventUnmarshalLegacyUsage(t *testing.T) {
	t.Parallel()

	raw := `{"type":"result","model":"gpt-5","usage":{"input_tokens":800,"output_tokens":400,"cached_input_tokens":200,"cache_creation_input_tokens":100}}`

	var evt cursorStreamEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if evt.InputTokens != 0 || evt.OutputTokens != 0 {
		t.Errorf("top-level tokens should be 0 for nested-usage shape, got input=%d output=%d", evt.InputTokens, evt.OutputTokens)
	}
	if evt.Usage == nil {
		t.Fatal("usage should be non-nil for nested-usage shape")
	}
	if evt.Usage.InputTokens != 800 || evt.Usage.OutputTokens != 400 || evt.Usage.CacheReadInputTokens != 200 || evt.Usage.CacheWriteInputTokens != 100 {
		t.Fatalf("nested usage = %+v, want input=800 output=400 cache_read=200 cache_write=100", evt.Usage)
	}

	b := &cursorBackend{cfg: Config{Logger: slog.Default()}}
	usage := make(map[string]TokenUsage)
	b.accumulateResultUsage(usage, &evt, "")
	u := usage["gpt-5"]
	if u.InputTokens != 800 || u.OutputTokens != 400 || u.CacheReadTokens != 200 || u.CacheWriteTokens != 100 {
		t.Fatalf("accumulated usage = %+v, want input=800 output=400 cache_read=200 cache_write=100", u)
	}
}
