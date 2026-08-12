package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsZcodeStreamBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("zcode", Config{ExecutablePath: "/nonexistent/zcode"})
	if err != nil {
		t.Fatalf("New(zcode): %v", err)
	}
	if _, ok := backend.(*zcodeStreamBackend); !ok {
		t.Fatalf("New(zcode) = %T, want *zcodeStreamBackend", backend)
	}
}

func TestBuildZcodeStreamArgs(t *testing.T) {
	t.Parallel()
	args := buildZcodeStreamArgs("task prompt", ExecOptions{
		ResumeSessionID: "sess_abc",
		Cwd:             "/work",
		MaxTurns:        5,
		ExtraArgs:       []string{"--verbose"},
		CustomArgs:      []string{"--prompt=replace", "--json", "--resume", "other"},
	}, slog.Default())
	joined := strings.Join(args, " ")

	wantPrefix := []string{"--prompt", "task prompt", "--stream-json", "--resume", "sess_abc", "--cwd", "/work", "--max-turns", "5"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short: %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all=%v", i, args[i], want, args)
		}
	}
	// Protocol-critical flags must be blocked from custom_args.
	for _, forbidden := range []string{"replace", "other"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("blocked arg %q leaked into %v", forbidden, args)
		}
	}
	// Non-managed extra args should survive.
	if !strings.Contains(joined, "--verbose") {
		t.Fatalf("--verbose missing from %v", args)
	}
	// --stream-json must appear exactly once (the managed one).
	if c := strings.Count(joined, "--stream-json"); c != 1 {
		t.Fatalf("--stream-json count = %d in %v, want 1", c, args)
	}
}

// fakeZcodeScript returns a shell script that replays a fixed NDJSON stream on
// stdout, simulating a patched zcode-cli --stream-json turn with a tool call.
func fakeZcodeScript(events []map[string]any) string {
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		raw, _ := json.Marshal(ev)
		lines = append(lines, string(raw))
	}
	// Emit each line with a tiny delay so the scanner sees them as separate events.
	script := "#!/bin/sh\n"
	for _, line := range lines {
		// printf '%s\n' to avoid printf interpreting backslash escapes in JSON.
		script += "printf '%s\\n' " + shSingleQuote(line) + "\n"
	}
	return script
}

func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestZcodeStreamBackendParsesEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	events := []map[string]any{
		{"type": "system", "subtype": "init", "session_id": "sess_test1"},
		{"type": "assistant", "session_id": "sess_test1", "message": map[string]any{
			"content": []map[string]any{{"type": "thinking", "thinking": "Let me think"}},
		}},
		{"type": "assistant", "session_id": "sess_test1", "message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Hello"}},
		}},
		{"type": "assistant", "session_id": "sess_test1", "message": map[string]any{
			"content": []map[string]any{{"type": "tool_use", "id": "call_1", "name": "Bash", "input": map[string]any{"command": "echo hi"}}},
		}},
		{"type": "user", "session_id": "sess_test1", "message": map[string]any{
			"content": []map[string]any{{"type": "tool_result", "tool_use_id": "call_1", "content": "hi"}},
		}},
		{"type": "assistant", "session_id": "sess_test1", "message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Done"}},
		}},
		{"type": "result", "subtype": "success", "session_id": "sess_test1", "is_error": false, "result": "Done", "usage": map[string]any{
			"input_tokens": json.Number("100"), "output_tokens": json.Number("5"), "cache_read_input_tokens": json.Number("80"),
		}},
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "zcode")
	if err := os.WriteFile(scriptPath, []byte(fakeZcodeScript(events)), 0o755); err != nil {
		t.Fatalf("write fake zcode: %v", err)
	}

	backend := &zcodeStreamBackend{cfg: Config{ExecutablePath: scriptPath, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "test prompt", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	result := <-session.Result

	// Verify the event sequence: status, thinking, text, tool-use, tool-result, text.
	var sawStatus, sawThinking, sawTextHello, sawToolUse, sawToolResult, sawTextDone bool
	for _, msg := range messages {
		switch msg.Type {
		case MessageStatus:
			sawStatus = true
			if msg.SessionID != "sess_test1" {
				t.Fatalf("status SessionID = %q, want sess_test1", msg.SessionID)
			}
		case MessageThinking:
			sawThinking = true
		case MessageText:
			if msg.Content == "Hello" {
				sawTextHello = true
			}
			if msg.Content == "Done" {
				sawTextDone = true
			}
		case MessageToolUse:
			sawToolUse = true
			if msg.Tool != "Bash" || msg.CallID != "call_1" {
				t.Fatalf("tool-use = %+v", msg)
			}
		case MessageToolResult:
			sawToolResult = true
			if msg.CallID != "call_1" || msg.Output != "hi" {
				t.Fatalf("tool-result = %+v", msg)
			}
		}
	}
	if !sawStatus || !sawThinking || !sawTextHello || !sawToolUse || !sawToolResult || !sawTextDone {
		t.Fatalf("missing expected messages; status=%v thinking=%v textHello=%v toolUse=%v toolResult=%v textDone=%v; all=%+v",
			sawStatus, sawThinking, sawTextHello, sawToolUse, sawToolResult, sawTextDone, messages)
	}

	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Output != "Done" {
		t.Fatalf("Output = %q, want Done", result.Output)
	}
	if result.SessionID != "sess_test1" {
		t.Fatalf("SessionID = %q, want sess_test1", result.SessionID)
	}
	usage, ok := result.Usage["zcode"]
	if !ok {
		t.Fatalf("usage bucket 'zcode' missing; got %+v", result.Usage)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v, want in=100 out=5", usage)
	}
}

func TestZcodeStreamBackendHandlesErrorResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	events := []map[string]any{
		{"type": "system", "subtype": "init", "session_id": "sess_err"},
		{"type": "result", "subtype": "error_during_execution", "session_id": "sess_err", "is_error": true, "error": map[string]any{"message": "boom"}},
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "zcode")
	if err := os.WriteFile(scriptPath, []byte(fakeZcodeScript(events)), 0o755); err != nil {
		t.Fatalf("write fake zcode: %v", err)
	}

	backend := &zcodeStreamBackend{cfg: Config{ExecutablePath: scriptPath, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "test", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "boom") {
		t.Fatalf("Error = %q, want it to contain 'boom'", result.Error)
	}
}
