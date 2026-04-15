package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// drainSession reads all messages and the final result with a hard cap so
// a hung test fails quickly instead of stalling the suite.
func drainSession(t *testing.T, s *Session, perOp time.Duration) ([]Message, Result) {
	t.Helper()
	var msgs []Message
	for {
		select {
		case m, ok := <-s.Messages:
			if !ok {
				select {
				case r := <-s.Result:
					return msgs, r
				case <-time.After(perOp):
					t.Fatalf("timed out waiting for Result after Messages closed")
				}
			}
			msgs = append(msgs, m)
		case <-time.After(perOp):
			t.Fatalf("timed out waiting for Message or Result")
		}
	}
}

// chatHandler returns a handler that responds with a sequence of
// ollamaChatResponse JSON bodies, one per call. After the sequence is
// exhausted it returns the last entry repeatedly (avoids panics if the
// model accidentally calls one extra time).
func chatHandler(t *testing.T, responses []ollamaChatResponse, capturedReqs *[]ollamaChatRequest) http.HandlerFunc {
	t.Helper()
	var idx atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var got ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if capturedReqs != nil {
			*capturedReqs = append(*capturedReqs, got)
		}
		i := int(idx.Load())
		if i >= len(responses) {
			i = len(responses) - 1
		} else {
			idx.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses[i])
	}
}

// ── Single-turn (no tool call) ────────────────────────────────────────────

func TestOllamaExecuteSingleTurnNoTools(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{{
		Model:           "qwen",
		Message:         ollamaChatMessage{Role: "assistant", Content: "Hello world!"},
		Done:            true,
		PromptEvalCount: 12,
		EvalCount:       34,
	}}, nil))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, err := b.Execute(context.Background(), "say hello", ExecOptions{Model: "qwen"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	msgs, res := drainSession(t, sess, 5*time.Second)

	if res.Status != "completed" {
		t.Fatalf("expected completed, got %q (err=%q)", res.Status, res.Error)
	}
	if res.Output != "Hello world!" {
		t.Errorf("expected output 'Hello world!', got %q", res.Output)
	}
	if got := countText(msgs); got != 1 {
		t.Errorf("expected 1 text message, got %d", got)
	}
	if u, ok := res.Usage["qwen"]; !ok || u.InputTokens != 12 || u.OutputTokens != 34 {
		t.Errorf("expected usage 12/34, got %+v", res.Usage)
	}
}

// ── Tool loop: model calls shell once, then returns final answer ─────────

func TestOllamaExecuteToolLoop(t *testing.T) {
	t.Parallel()

	var captured []ollamaChatRequest
	// Use a deterministic command: print a fixed string.
	cmdArg, _ := json.Marshal(map[string]string{"command": "echo OLLAMA_TOOL_OK"})
	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{
		// Turn 1: model decides to call shell.
		{
			Model: "gemma",
			Message: ollamaChatMessage{
				Role: "assistant",
				ToolCalls: []ollamaToolCall{{
					ID: "call_1",
					Function: ollamaToolCallFunc{
						Name:      "shell",
						Arguments: json.RawMessage(cmdArg),
					},
				}},
			},
			Done:            true,
			PromptEvalCount: 5,
			EvalCount:       3,
		},
		// Turn 2: model summarizes the tool output.
		{
			Model: "gemma",
			Message: ollamaChatMessage{
				Role:    "assistant",
				Content: "Done. The command output was OLLAMA_TOOL_OK.",
			},
			Done:            true,
			PromptEvalCount: 30,
			EvalCount:       11,
		},
	}, &captured))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, err := b.Execute(context.Background(), "do the thing", ExecOptions{Model: "gemma"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	msgs, res := drainSession(t, sess, 30*time.Second)

	if res.Status != "completed" {
		t.Fatalf("expected completed, got %q (err=%q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Output, "OLLAMA_TOOL_OK") {
		t.Errorf("expected output to mention OLLAMA_TOOL_OK, got %q", res.Output)
	}

	// Verify we made 2 chat calls.
	if len(captured) != 2 {
		t.Fatalf("expected 2 chat calls, got %d", len(captured))
	}
	// The second call must include the assistant tool_calls turn AND a
	// tool result message — total 4 messages (user, assistant w/ calls,
	// tool result, but no system because none was set).
	if got := len(captured[1].Messages); got != 3 {
		t.Errorf("expected 3 messages on turn 2 (user, assistant+tool_calls, tool result), got %d: %+v", got, captured[1].Messages)
	}
	// The shell tool must be advertised on every call.
	for i, c := range captured {
		if len(c.Tools) != 1 || c.Tools[0].Function.Name != "shell" {
			t.Errorf("call %d: expected shell tool advertised, got %+v", i, c.Tools)
		}
	}

	// Verify tool-use + tool-result were streamed.
	var sawToolUse, sawToolResult bool
	for _, m := range msgs {
		if m.Type == MessageToolUse && m.Tool == "shell" {
			sawToolUse = true
		}
		if m.Type == MessageToolResult && m.Tool == "shell" && strings.Contains(m.Output, "OLLAMA_TOOL_OK") {
			sawToolResult = true
		}
	}
	if !sawToolUse || !sawToolResult {
		t.Errorf("expected both ToolUse and ToolResult messages with shell output, got %+v", msgs)
	}

	// Token usage should sum across both turns.
	if u := res.Usage["gemma"]; u.InputTokens != 35 || u.OutputTokens != 14 {
		t.Errorf("expected summed usage 35/14, got %+v", u)
	}
}

// ── Tool loop: tool_call_id is propagated on the tool-result message ────

func TestOllamaToolCallIDIsPropagated(t *testing.T) {
	t.Parallel()

	var captured []ollamaChatRequest
	cmdArg, _ := json.Marshal(map[string]string{"command": "true"})
	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{
		{Message: ollamaChatMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{
			ID: "call_abc123", Function: ollamaToolCallFunc{Name: "shell", Arguments: cmdArg},
		}}}, Done: true},
		{Message: ollamaChatMessage{Role: "assistant", Content: "done"}, Done: true},
	}, &captured))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	_, res := drainSession(t, sess, 10*time.Second)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %q", res.Status)
	}

	// Second turn must carry a tool-role message with tool_call_id
	// set to the call ID from turn 1 — models like qwen2.5 require
	// this to correlate results back to their call.
	if len(captured) != 2 {
		t.Fatalf("expected 2 chat calls, got %d", len(captured))
	}
	var toolMsg *ollamaChatMessage
	for i := range captured[1].Messages {
		if captured[1].Messages[i].Role == "tool" {
			toolMsg = &captured[1].Messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("turn 2 request had no tool role message: %+v", captured[1].Messages)
	}
	if toolMsg.ToolCallID != "call_abc123" {
		t.Errorf("expected tool_call_id=call_abc123, got %q", toolMsg.ToolCallID)
	}
}

// ── Tool loop: shell command exits non-zero — model still gets output ────

func TestOllamaShellExitNonZeroIsForwardedToModel(t *testing.T) {
	t.Parallel()

	cmdArg, _ := json.Marshal(map[string]string{"command": "echo BEFORE; exit 7"})
	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{
		{Message: ollamaChatMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{
			ID: "c1", Function: ollamaToolCallFunc{Name: "shell", Arguments: cmdArg},
		}}}, Done: true},
		{Message: ollamaChatMessage{Role: "assistant", Content: "Saw exit 7"}, Done: true},
	}, nil))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	msgs, res := drainSession(t, sess, 10*time.Second)

	if res.Status != "completed" {
		t.Fatalf("expected completed even with non-zero exit, got %q", res.Status)
	}
	var toolOut string
	for _, m := range msgs {
		if m.Type == MessageToolResult {
			toolOut = m.Output
		}
	}
	if !strings.Contains(toolOut, "BEFORE") || !strings.Contains(toolOut, "exit code 7") {
		t.Errorf("expected tool output to include both 'BEFORE' and 'exit code 7', got %q", toolOut)
	}
}

// ── Tool loop: unknown tool name returns error string, not crash ────────

func TestOllamaUnknownToolReturnsErrorString(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{
		{Message: ollamaChatMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{
			ID: "c1", Function: ollamaToolCallFunc{Name: "delete_universe", Arguments: json.RawMessage(`{}`)},
		}}}, Done: true},
		{Message: ollamaChatMessage{Role: "assistant", Content: "ok"}, Done: true},
	}, nil))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	msgs, res := drainSession(t, sess, 10*time.Second)

	if res.Status != "completed" {
		t.Fatalf("expected completed, got %q", res.Status)
	}
	var saw bool
	for _, m := range msgs {
		if m.Type == MessageToolResult && strings.Contains(m.Output, "unknown tool") {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected ToolResult mentioning 'unknown tool', got %+v", msgs)
	}
}

// ── MaxTurns enforcement: model loops forever, we cap it ─────────────────

func TestOllamaMaxTurnsCap(t *testing.T) {
	t.Parallel()

	cmdArg, _ := json.Marshal(map[string]string{"command": "echo loop"})
	loopResp := ollamaChatResponse{
		Message: ollamaChatMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{
			ID: "c", Function: ollamaToolCallFunc{Name: "shell", Arguments: cmdArg},
		}}},
		Done: true,
	}
	// Always-loop server: every response is another tool call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(loopResp)
	}))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m", MaxTurns: 3})
	_, res := drainSession(t, sess, 10*time.Second)

	if res.Status != "failed" {
		t.Fatalf("expected status failed when MaxTurns hit, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "MaxTurns=3") {
		t.Errorf("expected error mentioning MaxTurns=3, got %q", res.Error)
	}
}

// ── Context cancellation / timeout (regression for Gemini PR #920 bug) ──

// slowHandler returns an http.Handler that blocks until EITHER the
// request context is cancelled (normal path) OR a fallback timer fires
// (test safety net — Go's http.Client can leave the connection open on
// context cancel and the server may not observe the disconnect
// immediately). The fallback MUST be longer than any timeout/cancel the
// caller sets, so the client's context error wins and we actually test
// the intended path rather than the server-side give-up.
func slowHandler(fallback time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(fallback):
		}
	})
}

func TestOllamaCancelMidLoopReportsAborted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(slowHandler(10 * time.Second))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, err := b.Execute(ctx, "x", ExecOptions{Model: "m"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, res := drainSession(t, sess, 15*time.Second)
	if res.Status != "aborted" {
		t.Fatalf("expected status 'aborted' on parent cancel, got %q (err=%q)", res.Status, res.Error)
	}
	if res.Error != "execution cancelled" {
		t.Errorf("expected error 'execution cancelled', got %q", res.Error)
	}
}

func TestOllamaTimeoutMidLoopReportsTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(slowHandler(10 * time.Second))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, err := b.Execute(context.Background(), "x", ExecOptions{Model: "m", Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, res := drainSession(t, sess, 15*time.Second)
	if res.Status != "timeout" {
		t.Fatalf("expected status 'timeout' on context deadline, got %q (err=%q)", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "timed out after") {
		t.Errorf("expected error mentioning 'timed out after', got %q", res.Error)
	}
}

// ── Server error / unreachable / missing model — same as before ──────────

func TestOllamaExecuteServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "model not found")
	}))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "hi", ExecOptions{Model: "missing"})
	_, res := drainSession(t, sess, 5*time.Second)
	if res.Status != "failed" {
		t.Fatalf("expected failed, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "500") || !strings.Contains(res.Error, "model not found") {
		t.Errorf("expected error mentioning 500 + 'model not found', got %q", res.Error)
	}
}

func TestOllamaExecuteUnreachable(t *testing.T) {
	t.Parallel()
	b := &ollamaBackend{cfg: Config{ExecutablePath: "http://127.0.0.1:1", Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "hi", ExecOptions{Model: "qwen"})
	_, res := drainSession(t, sess, 5*time.Second)
	if res.Status != "failed" {
		t.Fatalf("expected failed for unreachable host, got %q", res.Status)
	}
}

func TestOllamaExecuteRequiresModel(t *testing.T) {
	t.Parallel()
	b := &ollamaBackend{cfg: Config{ExecutablePath: "http://localhost:11434", Logger: slog.Default()}}
	_, err := b.Execute(context.Background(), "hi", ExecOptions{Model: ""})
	if err == nil {
		t.Fatal("expected error when Model is empty")
	}
}

// ── System prompt is forwarded as a system role message ──────────────────

func TestOllamaExecuteSendsSystemPrompt(t *testing.T) {
	t.Parallel()

	var captured []ollamaChatRequest
	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{{
		Message: ollamaChatMessage{Role: "assistant", Content: "ok"}, Done: true,
	}}, &captured))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "user prompt", ExecOptions{Model: "m", SystemPrompt: "you are helpful"})
	_, res := drainSession(t, sess, 5*time.Second)
	if res.Status != "completed" {
		t.Fatalf("expected completed, got %q", res.Status)
	}
	if len(captured) != 1 || len(captured[0].Messages) != 2 {
		t.Fatalf("expected 1 call with 2 messages, got %+v", captured)
	}
	if captured[0].Messages[0].Role != "system" || captured[0].Messages[0].Content != "you are helpful" {
		t.Errorf("expected system message first, got %+v", captured[0].Messages[0])
	}
}

// ── Version detection ────────────────────────────────────────────────────

func TestDetectOllamaVersionHappy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"version":"0.20.7"}`)
	}))
	t.Cleanup(srv.Close)
	v, err := detectOllamaVersion(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("detectOllamaVersion: %v", err)
	}
	if v != "0.20.7" {
		t.Errorf("expected 0.20.7, got %q", v)
	}
}

func TestDetectOllamaVersionUnreachable(t *testing.T) {
	t.Parallel()
	_, err := detectOllamaVersion(context.Background(), "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────

func TestBuildInitialMessagesNoSystemPrompt(t *testing.T) {
	t.Parallel()
	got := buildInitialMessages("hi", ExecOptions{})
	if len(got) != 1 || got[0].Role != "user" || got[0].Content != "hi" {
		t.Errorf("expected 1 user/hi message, got %+v", got)
	}
}

func TestBuildInitialMessagesWithSystemPrompt(t *testing.T) {
	t.Parallel()
	got := buildInitialMessages("hi", ExecOptions{SystemPrompt: "be concise"})
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "system" || got[1].Role != "user" {
		t.Errorf("expected system then user, got %+v", got)
	}
}

func countText(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		if m.Type == MessageText {
			n++
		}
	}
	return n
}

// ── Malformed response ───────────────────────────────────────────────────

func TestOllamaMalformedResponseFailsClearly(t *testing.T) {
	t.Parallel()

	// Ollama returns done=true with NO message content, role, or
	// tool_calls. Without the guard this gets treated as "completed"
	// with empty output.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaChatResponse{Done: true})
	}))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	_, res := drainSession(t, sess, 5*time.Second)

	if res.Status != "failed" {
		t.Fatalf("expected status failed on empty response, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "no message content") {
		t.Errorf("expected error mentioning 'no message content', got %q", res.Error)
	}
}

// Regression test for the OOM case: small quantized models under memory
// pressure can return {role:"assistant", content:"", tool_calls:[]} with
// done:true. The guard must NOT require Role to be empty — role is
// always "assistant" on responses.
func TestOllamaEmptyAssistantResponseFailsClearly(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ollamaChatResponse{
			Message: ollamaChatMessage{Role: "assistant", Content: "", ToolCalls: nil},
			Done:    true,
		})
	}))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	_, res := drainSession(t, sess, 5*time.Second)

	if res.Status != "failed" {
		t.Fatalf("expected status failed on assistant-with-no-output, got %q (output=%q)", res.Status, res.Output)
	}
	if !strings.Contains(res.Error, "no message content") {
		t.Errorf("expected error mentioning 'no message content', got %q", res.Error)
	}
}

// ── Env-configurable shell timeout + output cap ──────────────────────────

func TestShellExecTimeoutHonorsEnvVar(t *testing.T) {
	// Not t.Parallel — mutates env for the duration of this test.
	t.Setenv("MULTICA_OLLAMA_SHELL_TIMEOUT", "750ms")
	if got := shellExecTimeout(); got != 750*time.Millisecond {
		t.Errorf("expected 750ms, got %s", got)
	}
}

func TestShellExecTimeoutFallsBackOnInvalidEnv(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_SHELL_TIMEOUT", "not-a-duration")
	if got := shellExecTimeout(); got != shellExecTimeoutDefault {
		t.Errorf("expected default %s on invalid env, got %s", shellExecTimeoutDefault, got)
	}
}

func TestShellExecTimeoutFallsBackOnNegative(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_SHELL_TIMEOUT", "-1s")
	if got := shellExecTimeout(); got != shellExecTimeoutDefault {
		t.Errorf("expected default %s on negative duration, got %s", shellExecTimeoutDefault, got)
	}
}

func TestMaxToolOutputHonorsEnvVar(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_MAX_OUTPUT_BYTES", "2048")
	if got := maxToolOutput(); got != 2048 {
		t.Errorf("expected 2048, got %d", got)
	}
}

func TestMaxToolOutputFallsBackOnInvalidEnv(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_MAX_OUTPUT_BYTES", "not-a-number")
	if got := maxToolOutput(); got != maxToolOutputDefault {
		t.Errorf("expected default %d on invalid env, got %d", maxToolOutputDefault, got)
	}
}

func TestMaxToolOutputFallsBackOnNonPositive(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_MAX_OUTPUT_BYTES", "0")
	if got := maxToolOutput(); got != maxToolOutputDefault {
		t.Errorf("expected default on zero, got %d", got)
	}
}

func TestExecShellTruncatesLargeOutput(t *testing.T) {
	t.Setenv("MULTICA_OLLAMA_MAX_OUTPUT_BYTES", "32")

	// Script prints 200 bytes. With cap=32 the output should be
	// truncated and tagged with the "[truncated: N more bytes]" suffix.
	cmdArg, _ := json.Marshal(map[string]string{"command": "printf '%0.sX' $(seq 1 200)"})
	srv := httptest.NewServer(chatHandler(t, []ollamaChatResponse{
		{Message: ollamaChatMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{
			ID: "c", Function: ollamaToolCallFunc{Name: "shell", Arguments: cmdArg},
		}}}, Done: true},
		{Message: ollamaChatMessage{Role: "assistant", Content: "ok"}, Done: true},
	}, nil))
	t.Cleanup(srv.Close)

	b := &ollamaBackend{cfg: Config{ExecutablePath: srv.URL, Logger: slog.Default()}}
	sess, _ := b.Execute(context.Background(), "x", ExecOptions{Model: "m"})
	msgs, _ := drainSession(t, sess, 10*time.Second)

	var toolOut string
	for _, m := range msgs {
		if m.Type == MessageToolResult {
			toolOut = m.Output
		}
	}
	if !strings.Contains(toolOut, "truncated") {
		t.Errorf("expected truncation marker in tool output, got %q", toolOut)
	}
	if len(toolOut) > 100 { // 32 + truncation suffix ~= 55 chars
		t.Errorf("expected output capped near 32 bytes + suffix, got %d chars", len(toolOut))
	}
}

// ── Model capability probe ──────────────────────────────────────────────

func TestOllamaModelSupportsToolsTrue(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"capabilities":["completion","tools","thinking"]}`)
	}))
	t.Cleanup(srv.Close)

	ok, err := OllamaModelSupportsTools(context.Background(), srv.URL, "gemma4:latest")
	if err != nil {
		t.Fatalf("OllamaModelSupportsTools: %v", err)
	}
	if !ok {
		t.Error("expected true for model with 'tools' capability")
	}
}

func TestOllamaModelSupportsToolsFalse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"capabilities":["completion"]}`)
	}))
	t.Cleanup(srv.Close)

	ok, err := OllamaModelSupportsTools(context.Background(), srv.URL, "llama2")
	if err != nil {
		t.Fatalf("OllamaModelSupportsTools: %v", err)
	}
	if ok {
		t.Error("expected false for model without 'tools' capability")
	}
}

func TestOllamaModelSupportsToolsUnreachable(t *testing.T) {
	t.Parallel()
	_, err := OllamaModelSupportsTools(context.Background(), "http://127.0.0.1:1", "gemma4:latest")
	if err == nil {
		t.Fatal("expected error for unreachable /api/show")
	}
}

func TestOllamaModelSupportsToolsNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "model not found")
	}))
	t.Cleanup(srv.Close)

	_, err := OllamaModelSupportsTools(context.Background(), srv.URL, "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error mentioning 404, got %q", err.Error())
	}
}
