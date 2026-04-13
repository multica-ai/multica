package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewReturnsOpenclawBackend(t *testing.T) {
	t.Parallel()
	b, err := New("openclaw", Config{ExecutablePath: "/nonexistent/openclaw"})
	if err != nil {
		t.Fatalf("New(openclaw) error: %v", err)
	}
	if _, ok := b.(*openclawBackend); !ok {
		t.Fatalf("expected *openclawBackend, got %T", b)
	}
}

// ── processOutput tests ──

func TestOpenclawProcessOutputHappyPath(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Hello from openclaw"}},
		Meta: openclawMeta{
			DurationMs: 1234,
			AgentMeta: map[string]any{
				"sessionId": "ses_abc",
				"usage": map[string]any{
					"input":      float64(100),
					"output":     float64(50),
					"cacheRead":  float64(10),
					"cacheWrite": float64(5),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello from openclaw" {
		t.Errorf("output: got %q, want %q", res.output, "Hello from openclaw")
	}
	if res.sessionID != "ses_abc" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_abc")
	}
	if res.usage.InputTokens != 100 {
		t.Errorf("input tokens: got %d, want 100", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 50 {
		t.Errorf("output tokens: got %d, want 50", res.usage.OutputTokens)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Type != MessageText {
		t.Errorf("expected 1 text message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello from openclaw" {
		t.Errorf("message content: got %q", msgs[0].Content)
	}
}

func TestOpenclawProcessOutputMultiplePayloads(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{
			{Text: "First"},
			{Text: "Second"},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.output != "First\nSecond" {
		t.Errorf("output: got %q, want %q", res.output, "First\nSecond")
	}

	close(ch)
}

func TestOpenclawProcessOutputEmptyPayloads(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{Payloads: []openclawPayload{}}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "" {
		t.Errorf("output: got %q, want empty", res.output)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestOpenclawProcessOutputWithLeadingLogLines(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Done"}},
	}
	data, _ := json.Marshal(result)
	input := "some log line\nanother log\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Done" {
		t.Errorf("output: got %q, want %q", res.output, "Done")
	}

	close(ch)
}

func TestOpenclawProcessOutputIgnoresTrailingLogLinesAfterJSON(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Done"}},
	}
	data, _ := json.Marshal(result)
	input := string(data) + "\npost-result log line that should not block parsing"

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Done" {
		t.Errorf("output: got %q, want %q", res.output, "Done")
	}

	close(ch)
}

func TestOpenclawProcessOutputNoJSON(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader("not json at all"), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "not json at all" {
		t.Errorf("output: got %q", res.output)
	}

	close(ch)
}

func TestOpenclawProcessOutputEmptyInput(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader(""), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

func TestOpenclawProcessOutputReadError(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(&ioErrReader{data: ""}, ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if !strings.Contains(res.errMsg, "read stderr") {
		t.Errorf("errMsg: got %q, want it to contain 'read stderr'", res.errMsg)
	}

	close(ch)
}

func TestOpenclawProcessOutputWithBracesInLogLines(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Final answer"}},
		Meta:     openclawMeta{DurationMs: 500},
	}
	data, _ := json.Marshal(result)
	// Simulate error line containing braces before the real JSON (the exact bug scenario)
	input := `[tools] exec failed: complex interpreter invocation detected. raw_params={"command":"echo hello"}` + "\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Final answer" {
		t.Errorf("output: got %q, want %q", res.output, "Final answer")
	}

	close(ch)
}

// ── openclawInt64 tests ──

func TestOpenclawInt64Float(t *testing.T) {
	t.Parallel()
	data := map[string]any{"count": float64(42)}
	if got := openclawInt64(data, "count"); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestOpenclawInt64Missing(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	if got := openclawInt64(data, "count"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestOpenclawInt64Nil(t *testing.T) {
	t.Parallel()
	data := map[string]any{"count": "not a number"}
	if got := openclawInt64(data, "count"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}
