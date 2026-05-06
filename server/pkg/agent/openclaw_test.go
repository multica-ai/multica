package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestOpenclawExecuteParsesStdoutWrappedResult(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"echo '[info] diagnostic on stderr' >&2\n" +
		"printf '%s\\n' '{\"runId\":\"run_stdout\",\"status\":\"ok\",\"result\":{\"payloads\":[{\"type\":\"text\",\"text\":\"stdout wrapped\"}],\"meta\":{\"agentMeta\":{\"sessionId\":\"ses_stdout\",\"model\":\"deepseek-chat\",\"usage\":{\"input\":12,\"output\":3}}}}}'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("openclaw", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new openclaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("status: got %q, want completed (error=%q)", result.Status, result.Error)
		}
		if result.Output != "stdout wrapped" {
			t.Errorf("output: got %q, want %q", result.Output, "stdout wrapped")
		}
		if result.SessionID != "ses_stdout" {
			t.Errorf("sessionID: got %q, want %q", result.SessionID, "ses_stdout")
		}
		if got := result.Usage["deepseek-chat"].InputTokens; got != 12 {
			t.Errorf("input tokens: got %d, want 12", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestOpenclawExecuteParsesStderrLegacyResult(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"payloads\":[{\"text\":\"stderr legacy\"}],\"meta\":{\"agentMeta\":{\"sessionId\":\"ses_stderr\",\"model\":\"deepseek-chat\",\"usage\":{\"input\":8,\"output\":2}}}}' >&2\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("openclaw", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new openclaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("status: got %q, want completed (error=%q)", result.Status, result.Error)
		}
		if result.Output != "stderr legacy" {
			t.Errorf("output: got %q, want %q", result.Output, "stderr legacy")
		}
		if result.SessionID != "ses_stderr" {
			t.Errorf("sessionID: got %q, want %q", result.SessionID, "ses_stderr")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// ── Legacy result format tests (processOutput with final JSON blob) ──

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

func TestOpenclawProcessOutputWrappedResult(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawWrappedResult{
		RunID:  "run_123",
		Status: "ok",
		Result: &openclawResult{
			Payloads: []openclawPayload{{Text: "Hello from wrapped openclaw"}},
			Meta: openclawMeta{
				AgentMeta: map[string]any{
					"sessionId": "ses_wrapped",
					"model":     "deepseek-chat",
					"usage": map[string]any{
						"input":  float64(42),
						"output": float64(7),
					},
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello from wrapped openclaw" {
		t.Errorf("output: got %q, want wrapped payload text", res.output)
	}
	if res.sessionID != "ses_wrapped" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_wrapped")
	}
	if res.model != "deepseek-chat" {
		t.Errorf("model: got %q, want %q", res.model, "deepseek-chat")
	}
	if res.usage.InputTokens != 42 || res.usage.OutputTokens != 7 {
		t.Errorf("usage: got %+v", res.usage)
	}

	close(ch)
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

	if res.output != "FirstSecond" {
		t.Errorf("output: got %q, want %q", res.output, "FirstSecond")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
	if msgs[0].Content != "First" {
		t.Errorf("msg[0]: got %q, want %q", msgs[0].Content, "First")
	}
	if msgs[1].Content != "Second" {
		t.Errorf("msg[1]: got %q, want %q", msgs[1].Content, "Second")
	}
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

func TestOpenclawProcessOutputWithNoiseAndWrappedResult(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawWrappedResult{
		RunID:  "run_noise",
		Status: "ok",
		Result: &openclawResult{
			Payloads: []openclawPayload{{Text: "Done after noise"}},
			Meta:     openclawMeta{DurationMs: 100},
		},
	}
	data, _ := json.Marshal(result)
	input := "[info] starting\n" + string(data) + "\n[debug] finished"

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Done after noise" {
		t.Errorf("output: got %q, want %q", res.output, "Done after noise")
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

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

func TestOpenclawProcessOutputMalformedJSON(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader(`{"payloads":[`), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q", res.errMsg)
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
	if !strings.Contains(res.errMsg, "read openclaw output") {
		t.Errorf("errMsg: got %q, want it to contain 'read openclaw output'", res.errMsg)
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
	// Log line with braces should NOT be parsed as JSON — only lines starting
	// with '{' are considered. The result blob on its own line is still parsed.
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

func TestOpenclawResultBlobWithLeadingPrefixRejected(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// A line with a prefix before the JSON should NOT be parsed as a result.
	// This tests that the hardened parser rejects non-'{'-starting lines.
	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Should not match"}},
		Meta:     openclawMeta{DurationMs: 500},
	}
	data, _ := json.Marshal(result)
	input := "some prefix " + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	// Prefixed JSON is treated as malformed output; only JSON that starts at
	// the beginning of a line is accepted.
	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

// ── Streaming NDJSON event tests ──

func TestOpenclawStreamingTextEvents(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Hello "}`,
		`{"type":"text","text":"world"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello world" {
		t.Errorf("output: got %q, want %q", res.output, "Hello world")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Type != MessageText || msgs[0].Content != "Hello " {
		t.Errorf("msg[0]: type=%s content=%q", msgs[0].Type, msgs[0].Content)
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "world" {
		t.Errorf("msg[1]: type=%s content=%q", msgs[1].Type, msgs[1].Content)
	}
}

func TestOpenclawStreamingToolUseEvents(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"tool_use","tool":"bash","callId":"call_1","input":{"command":"ls -la"}}`,
		`{"type":"tool_result","tool":"bash","callId":"call_1","text":"total 42\ndrwxr-xr-x"}`,
		`{"type":"text","text":"Listed files."}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// tool_use
	if msgs[0].Type != MessageToolUse {
		t.Errorf("msg[0] type: got %s, want tool-use", msgs[0].Type)
	}
	if msgs[0].Tool != "bash" {
		t.Errorf("msg[0] tool: got %q, want %q", msgs[0].Tool, "bash")
	}
	if msgs[0].CallID != "call_1" {
		t.Errorf("msg[0] callID: got %q, want %q", msgs[0].CallID, "call_1")
	}
	if msgs[0].Input["command"] != "ls -la" {
		t.Errorf("msg[0] input: got %v", msgs[0].Input)
	}

	// tool_result
	if msgs[1].Type != MessageToolResult {
		t.Errorf("msg[1] type: got %s, want tool-result", msgs[1].Type)
	}
	if msgs[1].CallID != "call_1" {
		t.Errorf("msg[1] callID: got %q", msgs[1].CallID)
	}
	if msgs[1].Output != "total 42\ndrwxr-xr-x" {
		t.Errorf("msg[1] output: got %q", msgs[1].Output)
	}

	// text
	if msgs[2].Type != MessageText || msgs[2].Content != "Listed files." {
		t.Errorf("msg[2]: type=%s content=%q", msgs[2].Type, msgs[2].Content)
	}
}

func TestOpenclawStreamingErrorEvent(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Starting..."}`,
		`{"type":"error","text":"model not found: gpt-99"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "model not found: gpt-99" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Type != MessageError {
		t.Errorf("msg[1] type: got %s, want error", msgs[1].Type)
	}
}

func TestOpenclawStreamingStepFinishUsage(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"step_start"}`,
		`{"type":"text","text":"Done"}`,
		`{"type":"step_finish","usage":{"input":200,"output":100,"cacheRead":50,"cacheWrite":25}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.usage.InputTokens != 200 {
		t.Errorf("input tokens: got %d, want 200", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 100 {
		t.Errorf("output tokens: got %d, want 100", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 50 {
		t.Errorf("cache read: got %d, want 50", res.usage.CacheReadTokens)
	}
	if res.usage.CacheWriteTokens != 25 {
		t.Errorf("cache write: got %d, want 25", res.usage.CacheWriteTokens)
	}

	close(ch)
}

func TestOpenclawStreamingSessionID(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Hi","sessionId":"ses_stream_123"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.sessionID != "ses_stream_123" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_stream_123")
	}

	close(ch)
}

func TestOpenclawStreamingMixedWithLogLines(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		"[info] initializing agent...",
		`{"type":"text","text":"Hello"}`,
		"[debug] tool exec completed",
		`{"type":"text","text":" world"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello world" {
		t.Errorf("output: got %q, want %q", res.output, "Hello world")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
}

// ── Lifecycle event tests ──

func TestOpenclawLifecycleErrorPhase(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Working..."}`,
		`{"type":"lifecycle","phase":"error","text":"agent crashed unexpectedly"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "agent crashed unexpectedly" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Type != MessageError {
		t.Errorf("msg[1] type: got %s, want error", msgs[1].Type)
	}
}

func TestOpenclawLifecycleFailedPhase(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"failed","message":"timeout exceeded"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "timeout exceeded" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "timeout exceeded")
	}

	close(ch)
}

func TestOpenclawLifecycleCancelledPhase(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"cancelled"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	// With no text/message/error, should get the default.
	if res.errMsg != "unknown openclaw error" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

func TestOpenclawLifecycleRunningPhaseIgnored(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"running"}`,
		`{"type":"text","text":"Hello"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}

	close(ch)
}

// ── Structured error tests ──

func TestOpenclawStructuredErrorObject(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"name":"ModelNotFoundError","data":{"message":"model gpt-99 not available"}}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "model gpt-99 not available" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "model gpt-99 not available")
	}

	close(ch)
}

func TestOpenclawStructuredErrorNameOnly(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"name":"AuthenticationError"}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.errMsg != "AuthenticationError" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "AuthenticationError")
	}

	close(ch)
}

func TestOpenclawStructuredErrorMessageField(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"message":"rate limit exceeded"}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.errMsg != "rate limit exceeded" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "rate limit exceeded")
	}

	close(ch)
}

// ── Usage field name variant tests ──

func TestOpenclawUsageAlternativeFieldNames(t *testing.T) {
	t.Parallel()

	// Test PaperClip-style field names (inputTokens, outputTokens, etc.)
	data := map[string]any{
		"inputTokens":       float64(500),
		"outputTokens":      float64(200),
		"cachedInputTokens": float64(100),
	}
	usage := parseOpenclawUsage(data)

	if usage.InputTokens != 500 {
		t.Errorf("InputTokens: got %d, want 500", usage.InputTokens)
	}
	if usage.OutputTokens != 200 {
		t.Errorf("OutputTokens: got %d, want 200", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 100 {
		t.Errorf("CacheReadTokens: got %d, want 100", usage.CacheReadTokens)
	}
}

func TestOpenclawUsageSnakeCaseFieldNames(t *testing.T) {
	t.Parallel()

	// Test snake_case field names (Anthropic API style)
	data := map[string]any{
		"input_tokens":                float64(300),
		"output_tokens":               float64(150),
		"cache_read_input_tokens":     float64(80),
		"cache_creation_input_tokens": float64(40),
	}
	usage := parseOpenclawUsage(data)

	if usage.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 80 {
		t.Errorf("CacheReadTokens: got %d, want 80", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 40 {
		t.Errorf("CacheWriteTokens: got %d, want 40", usage.CacheWriteTokens)
	}
}

func TestOpenclawUsageOriginalFieldNames(t *testing.T) {
	t.Parallel()

	// Test the original short field names (input, output, cacheRead, cacheWrite)
	data := map[string]any{
		"input":      float64(100),
		"output":     float64(50),
		"cacheRead":  float64(10),
		"cacheWrite": float64(5),
	}
	usage := parseOpenclawUsage(data)

	if usage.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens: got %d, want 10", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens: got %d, want 5", usage.CacheWriteTokens)
	}
}

func TestOpenclawUsageAccumulationAcrossSteps(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"step_finish","usage":{"inputTokens":100,"outputTokens":50}}`,
		`{"type":"step_finish","usage":{"inputTokens":200,"outputTokens":80,"cachedInputTokens":60}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.usage.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 130 {
		t.Errorf("OutputTokens: got %d, want 130", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 60 {
		t.Errorf("CacheReadTokens: got %d, want 60", res.usage.CacheReadTokens)
	}

	close(ch)
}

func TestOpenclawUsageFinalResultAlternativeFields(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Done"}},
		Meta: openclawMeta{
			DurationMs: 1000,
			AgentMeta: map[string]any{
				"usage": map[string]any{
					"inputTokens":       float64(400),
					"outputTokens":      float64(180),
					"cachedInputTokens": float64(90),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.usage.InputTokens != 400 {
		t.Errorf("InputTokens: got %d, want 400", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 180 {
		t.Errorf("OutputTokens: got %d, want 180", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 90 {
		t.Errorf("CacheReadTokens: got %d, want 90", res.usage.CacheReadTokens)
	}

	close(ch)
}

func TestOpenclawProcessOutputMultilineJSON(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Pretty printed response"}},
		Meta: openclawMeta{
			DurationMs: 4764,
			AgentMeta: map[string]any{
				"sessionId": "test-session",
				"usage": map[string]any{
					"input":  float64(100),
					"output": float64(34),
				},
			},
		},
	}
	// Marshal with indentation to simulate openclaw's pretty-printed output.
	data, _ := json.MarshalIndent(result, "", "  ")

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Pretty printed response" {
		t.Errorf("output: got %q, want %q", res.output, "Pretty printed response")
	}
	if res.sessionID != "test-session" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "test-session")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Content != "Pretty printed response" {
		t.Errorf("expected 1 text message with content, got %d msgs", len(msgs))
	}
}

func TestOpenclawProcessOutputMultilineJSONWithLeadingLogs(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "Answer after logs"}},
		Meta:     openclawMeta{DurationMs: 100},
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	input := "some startup log\nanother log line\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Answer after logs" {
		t.Errorf("output: got %q, want %q", res.output, "Answer after logs")
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

// ── buildOpenclawArgs tests ──

// indexOf returns the first index of s in args, or -1 if absent.
func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestBuildOpenclawArgsMinimal(t *testing.T) {
	t.Parallel()

	args := buildOpenclawArgs("do work", "ses-1", ExecOptions{}, slog.Default())
	expected := []string{"agent", "--local", "--json", "--session-id", "ses-1", "--message", "do work"}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestBuildOpenclawArgsMapsModelToAgent(t *testing.T) {
	t.Parallel()

	// For openclaw, agent.model stores the pre-registered agent name;
	// the daemon must translate that to `--agent <name>` because the
	// CLI rejects `--model` entirely. `--system-prompt` is also
	// rejected and must not be emitted as a flag.
	args := buildOpenclawArgs("task", "ses-2", ExecOptions{
		Model:        "deepseek-v4-agent",
		SystemPrompt: "You are a helpful agent.",
	}, slog.Default())

	if idx := indexOf(args, "--model"); idx != -1 {
		t.Fatalf("unexpected --model flag at %d: %v", idx, args)
	}
	if idx := indexOf(args, "--system-prompt"); idx != -1 {
		t.Fatalf("unexpected --system-prompt flag at %d: %v", idx, args)
	}

	agentIdx := indexOf(args, "--agent")
	if agentIdx == -1 || agentIdx+1 >= len(args) {
		t.Fatalf("expected --agent <value> in args: %v", args)
	}
	if got := args[agentIdx+1]; got != "deepseek-v4-agent" {
		t.Errorf("--agent value = %q, want %q", got, "deepseek-v4-agent")
	}
}

func TestBuildOpenclawArgsCustomAgentWinsOverModel(t *testing.T) {
	t.Parallel()

	// If the user already configured --agent via custom_args, their
	// value wins — we don't double-inject. This keeps existing configs
	// working when they later set agent.model.
	args := buildOpenclawArgs("task", "ses-2b", ExecOptions{
		Model:      "from-dropdown",
		CustomArgs: []string{"--agent", "from-custom-args"},
	}, slog.Default())

	count := 0
	for _, a := range args {
		if a == "--agent" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one --agent flag, got %d: %v", count, args)
	}
	agentIdx := indexOf(args, "--agent")
	if args[agentIdx+1] != "from-custom-args" {
		t.Errorf("custom --agent should win, got %q", args[agentIdx+1])
	}
}

func TestBuildOpenclawArgsPrependsSystemPromptToMessage(t *testing.T) {
	t.Parallel()

	args := buildOpenclawArgs("do the thing", "ses-3", ExecOptions{
		SystemPrompt: "You are a read-only agent.",
	}, slog.Default())

	msgIdx := indexOf(args, "--message")
	if msgIdx == -1 || msgIdx+1 >= len(args) {
		t.Fatalf("expected --message <value> in args: %v", args)
	}
	got := args[msgIdx+1]
	want := "You are a read-only agent.\n\ndo the thing"
	if got != want {
		t.Errorf("--message payload mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestBuildOpenclawArgsEmptySystemPromptLeavesMessageUnchanged(t *testing.T) {
	t.Parallel()

	args := buildOpenclawArgs("just do it", "ses-4", ExecOptions{}, slog.Default())

	msgIdx := indexOf(args, "--message")
	if msgIdx == -1 || msgIdx+1 >= len(args) {
		t.Fatalf("expected --message <value> in args: %v", args)
	}
	if got := args[msgIdx+1]; got != "just do it" {
		t.Errorf("--message payload: got %q, want %q", got, "just do it")
	}
}

func TestBuildOpenclawArgsTimeout(t *testing.T) {
	t.Parallel()

	args := buildOpenclawArgs("task", "ses-5", ExecOptions{
		Timeout: 90 * time.Second,
	}, slog.Default())

	idx := indexOf(args, "--timeout")
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("expected --timeout <value> in args: %v", args)
	}
	if got := args[idx+1]; got != "90" {
		t.Errorf("--timeout value: got %q, want %q", got, "90")
	}
}

func TestBuildOpenclawArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	// Users must not be able to re-introduce the banned flags via custom_args —
	// they would crash `openclaw agent` just like the direct forward did.
	args := buildOpenclawArgs("task", "ses-6", ExecOptions{
		CustomArgs: []string{
			"--agent", "research-bot",
			"--model", "gpt-4o",
			"--system-prompt", "You are helpful",
			"--session-id", "hijacked",
			"--message", "hijacked",
		},
	}, slog.Default())

	if idx := indexOf(args, "--model"); idx != -1 {
		t.Errorf("--model should be filtered from custom_args: %v", args)
	}
	if idx := indexOf(args, "--system-prompt"); idx != -1 {
		t.Errorf("--system-prompt should be filtered from custom_args: %v", args)
	}
	// Whitelisted pass-through flag must survive filtering.
	if idx := indexOf(args, "--agent"); idx == -1 || idx+1 >= len(args) || args[idx+1] != "research-bot" {
		t.Errorf("expected --agent research-bot to survive filtering: %v", args)
	}
	// --session-id and --message appear exactly once — the daemon-managed ones.
	if count := countOccurrences(args, "--session-id"); count != 1 {
		t.Errorf("expected 1 --session-id (daemon-managed), got %d: %v", count, args)
	}
	if count := countOccurrences(args, "--message"); count != 1 {
		t.Errorf("expected 1 --message (daemon-managed), got %d: %v", count, args)
	}
}

func TestOpenclawProcessOutputExtractsModelFromAgentMeta(t *testing.T) {
	t.Parallel()

	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Mirrors a real openclaw `--json` blob captured locally: agentMeta
	// carries the actual LLM identifier under `model`, alongside the
	// session id, provider, and usage. The dashboard previously bucketed
	// usage under `unknown` because this field wasn't read; we now want
	// it surfaced as the runtime's reported model string.
	result := openclawResult{
		Payloads: []openclawPayload{{Text: "ok"}},
		Meta: openclawMeta{
			DurationMs: 9501,
			AgentMeta: map[string]any{
				"sessionId": "multica-1776752018613706000",
				"provider":  "deepseek",
				"model":     "deepseek-chat",
				"usage": map[string]any{
					"input":      float64(414),
					"output":     float64(163),
					"cacheRead":  float64(33280),
					"cacheWrite": float64(0),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.model != "deepseek-chat" {
		t.Errorf("model: got %q, want %q", res.model, "deepseek-chat")
	}
	if res.sessionID != "multica-1776752018613706000" {
		t.Errorf("sessionID: got %q", res.sessionID)
	}
	if res.usage.InputTokens != 414 {
		t.Errorf("input tokens: got %d, want 414", res.usage.InputTokens)
	}
}

func TestOpenclawProcessOutputModelEmptyWhenAgentMetaOmitsIt(t *testing.T) {
	t.Parallel()

	// Older openclaw versions / partial outputs may not include `model`
	// in agentMeta. processOutput must surface "" so the Execute loop
	// can fall back to opts.Model (the agent name) and ultimately the
	// daemon's "unknown" placeholder, preserving prior behavior for
	// runtimes that haven't been upgraded.
	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := openclawResult{
		Payloads: []openclawPayload{{Text: "ok"}},
		Meta: openclawMeta{
			AgentMeta: map[string]any{
				"sessionId": "ses_xyz",
				"usage": map[string]any{
					"input":  float64(10),
					"output": float64(5),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.model != "" {
		t.Errorf("model: got %q, want empty", res.model)
	}
}

func countOccurrences(args []string, s string) int {
	n := 0
	for _, a := range args {
		if a == s {
			n++
		}
	}
	return n
}
