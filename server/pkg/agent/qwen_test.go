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

func TestNewReturnsQwenBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("qwen", Config{ExecutablePath: "/nonexistent/qwen"})
	if err != nil {
		t.Fatalf("New(qwen): %v", err)
	}
	if _, ok := backend.(*qwenBackend); !ok {
		t.Fatalf("New(qwen) = %T, want *qwenBackend", backend)
	}
}

func TestBuildQwenArgsKeepsProtocolManaged(t *testing.T) {
	t.Parallel()
	args := buildQwenArgs("task prompt", ExecOptions{
		Model:           "qwen3.8-max-preview",
		ResumeSessionID: "session-1",
		ExtraArgs:       []string{"--output-format", "text", "--sandbox"},
		CustomArgs: []string{
			"--prompt=replace", "-o", "json", "--model", "other", "--resume", "other-session",
			"--safe-mode", "--chat-recording", "false", "--mcp-config", "injected-mcp.json", "--mcp-config=inline-mcp.json", "--debug",
		},
	}, slog.Default())
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"text", "replace", "other-session", "other", "--safe-mode", "--chat-recording", "injected-mcp.json", "inline-mcp.json"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("managed argument %q leaked into %v", forbidden, args)
		}
	}
	wantPrefix := []string{"-p", "task prompt", "--output-format", "stream-json", "--model", "qwen3.8-max-preview", "--resume", "session-1"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short: %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all=%v", i, args[i], want, args)
		}
	}
	if !strings.Contains(joined, "--sandbox") || !strings.Contains(joined, "--debug") {
		t.Fatalf("non-managed custom args missing from %v", args)
	}
}

func fakeQwenScript() string {
	return `#!/bin/sh
if [ -n "$QWEN_ARGS_FILE" ]; then printf '%s\n' "$@" > "$QWEN_ARGS_FILE"; fi
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--mcp-config" ] && [ -n "$QWEN_MCP_CAPTURE_FILE" ]; then cp "$2" "$QWEN_MCP_CAPTURE_FILE"; break; fi
  shift
done
case "$QWEN_MODE" in
  error)
    printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-error","model":"qwen-test"}'
    printf '%s\n' '{"type":"result","subtype":"error_during_execution","session_id":"sess-error","is_error":true,"error":{"type":"authentication_error","message":"synthetic Qwen authentication failure"}}'
    ;;
  exit)
    echo 'synthetic qwen stderr' >&2
    exit 7
    ;;
  resume-missing)
    cat "$QWEN_STDERR_FIXTURE" >&2
    exit 1
    ;;
  spin)
    while :; do :; done
    ;;
  *)
    printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-qwen-1","model":"qwen-test"}'
    printf '%s\n' '{"type":"assistant","session_id":"sess-qwen-1","message":{"role":"assistant","model":"qwen-test","content":[{"type":"thinking","thinking":"considering"}]}}'
    printf '%s\n' '{"type":"assistant","session_id":"sess-qwen-1","message":{"role":"assistant","model":"qwen-test","content":[{"type":"tool_use","id":"call-1","name":"list_directory","input":{"path":"/work"}}]}}'
    printf '%s\n' '{"type":"user","session_id":"sess-qwen-1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"Listed 1 item"}]}}'
    printf '%s\n' '{"type":"assistant","session_id":"sess-qwen-1","message":{"role":"assistant","model":"qwen-test","content":[{"type":"text","text":"PONG"}],"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":3}}}'
    printf '%s\n' '{"type":"result","subtype":"success","session_id":"sess-qwen-1","is_error":false,"result":"PONG","usage":{"input_tokens":20,"output_tokens":4,"cache_read_input_tokens":6}}'
    ;;
esac
`
}

func newFakeQwenBackend(t *testing.T, env map[string]string) *qwenBackend {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "qwen")
	writeTestExecutable(t, path, []byte(fakeQwenScript()))
	return &qwenBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: env}}
}

func awaitQwenResult(t *testing.T, session *Session) ([]Message, Result) {
	t.Helper()
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a result")
		}
		return messages, result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for qwen result")
		return nil, Result{}
	}
}

func TestQwenBackendStreamsNativeEvents(t *testing.T) {
	t.Parallel()
	backend := newFakeQwenBackend(t, nil)
	session, err := backend.Execute(context.Background(), "reply PONG", ExecOptions{Model: "qwen-test", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	messages, result := awaitQwenResult(t, session)
	if result.Status != "completed" || result.Output != "PONG" || result.SessionID != "sess-qwen-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	usage := result.Usage["qwen-test"]
	if usage.InputTokens != 20 || usage.OutputTokens != 4 || usage.CacheReadTokens != 6 {
		t.Fatalf("unexpected final usage: %+v", usage)
	}
	var thinking, toolUse, toolResult, text bool
	for _, message := range messages {
		switch message.Type {
		case MessageThinking:
			thinking = message.Content == "considering"
		case MessageToolUse:
			toolUse = message.Tool == "list_directory" && message.CallID == "call-1" && message.Input["path"] == "/work"
		case MessageToolResult:
			toolResult = message.CallID == "call-1" && message.Output == "Listed 1 item"
		case MessageText:
			text = message.Content == "PONG"
		}
	}
	if !thinking || !toolUse || !toolResult || !text {
		t.Fatalf("missing native events thinking=%v toolUse=%v toolResult=%v text=%v; messages=%+v", thinking, toolUse, toolResult, text, messages)
	}
}

func TestQwenBackendPreservesSuccessfulResumeSession(t *testing.T) {
	t.Parallel()
	backend := newFakeQwenBackend(t, nil)
	session, err := backend.Execute(context.Background(), "continue task", ExecOptions{
		Model: "qwen-test", ResumeSessionID: "sess-qwen-1", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitQwenResult(t, session)
	if result.Status != "completed" || result.Output != "PONG" || result.SessionID != "sess-qwen-1" {
		t.Fatalf("resumed result = %+v", result)
	}
}

func newQwenTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func TestQwenBackendFailureTimeoutAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	resumeFixture, err := filepath.Abs(filepath.Join("testdata", "qwen-code-0.20.0-resume-not-found.stderr.txt"))
	if err != nil {
		t.Fatalf("resolve resume fixture: %v", err)
	}
	for _, tc := range []struct {
		name        string
		env         map[string]string
		ctx         func() (context.Context, context.CancelFunc)
		opts        ExecOptions
		status      string
		needle      string
		wantSession string
	}{
		{"result error", map[string]string{"QWEN_MODE": "error"}, newQwenTestContext, ExecOptions{}, "failed", "synthetic Qwen authentication failure", "sess-error"},
		{"process error", map[string]string{"QWEN_MODE": "exit"}, newQwenTestContext, ExecOptions{}, "failed", "synthetic qwen stderr", ""},
		{"missing resume", map[string]string{"QWEN_MODE": "resume-missing", "QWEN_STDERR_FIXTURE": resumeFixture}, newQwenTestContext, ExecOptions{ResumeSessionID: "session-redacted"}, "failed", "No saved session found", ""},
		{"timeout", map[string]string{"QWEN_MODE": "spin"}, newQwenTestContext, ExecOptions{Timeout: 20 * time.Millisecond}, "timeout", "timed out", ""},
		{"cancel", map[string]string{"QWEN_MODE": "spin"}, newQwenTestContext, ExecOptions{}, "aborted", "cancelled", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			if tc.name == "cancel" {
				go func() { time.Sleep(20 * time.Millisecond); cancel() }()
			} else {
				defer cancel()
			}
			session, err := newFakeQwenBackend(t, tc.env).Execute(ctx, "task", tc.opts)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			_, result := awaitQwenResult(t, session)
			if result.Status != tc.status || !strings.Contains(result.Error, tc.needle) || result.SessionID != tc.wantSession {
				t.Fatalf("result = %+v, want status=%q error containing %q session=%q", result, tc.status, tc.needle, tc.wantSession)
			}
		})
	}
}

func TestQwenBackendPassesManagedMCPThroughTempFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	capturePath := filepath.Join(t.TempDir(), "qwen.mcp.json")
	argsPath := filepath.Join(t.TempDir(), "qwen.args")
	mcpConfig := json.RawMessage(`{"mcpServers":{"demo":{"command":"echo","args":["hello"]}}}`)
	backend := newFakeQwenBackend(t, map[string]string{"QWEN_ARGS_FILE": argsPath, "QWEN_MCP_CAPTURE_FILE": capturePath})
	session, err := backend.Execute(context.Background(), "task", ExecOptions{McpConfig: mcpConfig})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitQwenResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read qwen args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	mcpPath := ""
	for i, arg := range args {
		if arg == "--mcp-config" && i+1 < len(args) {
			mcpPath = args[i+1]
			break
		}
	}
	if mcpPath == "" {
		t.Fatalf("managed MCP argument missing from %v", args)
	}
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured managed MCP file: %v", err)
	}
	if string(data) != string(mcpConfig) {
		t.Fatalf("managed MCP file = %s, want %s", data, mcpConfig)
	}
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("managed MCP file should be removed after completion, stat err=%v", err)
	}
}

func TestQwenCode020FixtureParses(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "qwen-code-0.20.0-stream-json.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	state := qwenStreamState{usage: make(map[string]TokenUsage)}
	messages := make(chan Message, 16)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event qwenStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("fixture event: %v\n%s", err, line)
		}
		handleQwenEvent(event, messages, &state)
	}
	if !state.sawResult || state.resultIsError || state.sessionID != "session-redacted" || state.finalResultText != "DONE" {
		t.Fatalf("unexpected fixture state: %+v", state)
	}
	if state.usage["qwen3.8-max-preview"].InputTokens != 46539 {
		t.Fatalf("fixture usage = %+v", state.usage)
	}
}

func TestQwenCode020ErrorFixturePreservesErrorMessage(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "qwen-code-0.20.0-error-stream-json.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	state := qwenStreamState{usage: make(map[string]TokenUsage)}
	messages := make(chan Message, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event qwenStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("fixture event: %v\n%s", err, line)
		}
		handleQwenEvent(event, messages, &state)
	}
	if !state.sawResult || !state.resultIsError {
		t.Fatalf("unexpected terminal state: %+v", state)
	}
	if state.sessionID != "session-error-redacted" {
		t.Fatalf("session ID = %q", state.sessionID)
	}
	if state.finalResultText != "Authentication failed: credential redacted" {
		t.Fatalf("error detail = %q", state.finalResultText)
	}
}
