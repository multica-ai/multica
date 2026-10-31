package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsZcodeBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("zcode", Config{ExecutablePath: "/nonexistent/zcode"})
	if err != nil {
		t.Fatalf("New(zcode): %v", err)
	}
	if _, ok := backend.(*zcodeBackend); !ok {
		t.Fatalf("New(zcode) = %T, want *zcodeBackend", backend)
	}
}

func TestBuildZcodeArgsKeepsProtocolManaged(t *testing.T) {
	t.Parallel()
	args := buildZcodeArgs(ExecOptions{
		Model:           "mock/test-model",
		ResumeSessionID: "sess-zcode-1",
		ExtraArgs:       []string{"--output-format", "text", "--verbose"},
		CustomArgs: []string{
			"-p", "replace", "--prompt=replace", "--model", "other", "--resume", "other-session",
			"--effort", "high", "--continue", "--cwd", "/elsewhere", "--attach", "x.txt",
			"--json", "--no-color", "--verbose", "--locale", "zh-CN", "--mode", "yolo",
		},
	}, "the real prompt", slog.Default())
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"text", "other-session", "other", "high", "/elsewhere", "x.txt", "zh-CN", "yolo", "replace"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("managed argument %q leaked into %v", forbidden, args)
		}
	}
	wantPrefix := []string{"--output-format", "stream-json", "--no-color", "--model", "mock/test-model", "--resume", "sess-zcode-1"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short: %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all=%v", i, args[i], want, args)
		}
	}
	// ZCode has no stdin prompt mode: the prompt must be the LAST two argv
	// entries, so custom args can never push it out of parsing position.
	if args[len(args)-2] != "-p" || args[len(args)-1] != "the real prompt" {
		t.Fatalf("prompt must be argv-final after a fresh -p, got %v", args)
	}
	if count := strings.Count(joined, " -p "); count != 1 {
		t.Fatalf("-p count = %d in %v, want exactly 1 (daemon-owned)", count, args)
	}
	if count := strings.Count(joined, "--no-color"); count != 1 {
		t.Fatalf("--no-color count = %d in %v, want exactly 1 (daemon-owned)", count, args)
	}
}

func TestBuildZcodeArgsOmitEmptyModelAndResume(t *testing.T) {
	t.Parallel()
	args := buildZcodeArgs(ExecOptions{}, "hi", slog.Default())
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--model") || strings.Contains(joined, "--resume") {
		t.Fatalf("empty model/resume must be omitted: %v", args)
	}
	if args[len(args)-2] != "-p" || args[len(args)-1] != "hi" {
		t.Fatalf("prompt must still be argv-final: %v", args)
	}
}

func fakeZcodeScript() string {
	return `#!/bin/sh
if [ -n "$ZCODE_ARGS_FILE" ]; then printf '%s\n' "$@" > "$ZCODE_ARGS_FILE"; fi
case "${ZCODE_MODE:-success}" in
  success)
    printf '%s\n' '{"eventId":"e1","seq":1,"sessionId":"sess-zcode-1","type":"session.titleUpdated","payload":{}}'
    printf '%s\n' '{"eventId":"e2","seq":2,"sessionId":"sess-zcode-1","type":"turn.started","payload":{}}'
    printf '%s\n' '{"eventId":"e3","seq":3,"sessionId":"sess-zcode-1","type":"session.updated","payload":{"providerId":"mock","modelId":"zcode-test"}}'
    printf '%s\n' 'not json at all'
    printf '%s\n' '{"eventId":"e4","seq":4,"sessionId":"sess-zcode-1","type":"model.streaming","payload":{"kind":"text_delta","delta":"好的","done":false}}'
    printf '%s\n' '{"eventId":"e5","seq":5,"sessionId":"sess-zcode-1","type":"session.updated","payload":{"content":"好的","stopReason":"stop","usage":{"inputTokens":20,"outputTokens":4,"totalTokens":24,"cacheReadTokens":6}}}'
    ;;
  resume)
    printf '%s\n' '{"eventId":"r1","seq":1,"sessionId":"sess-zcode-1","type":"session.resumed","payload":{}}'
    printf '%s\n' '{"eventId":"r2","seq":2,"sessionId":"sess-zcode-1","type":"turn.started","payload":{}}'
    printf '%s\n' '{"eventId":"r3","seq":3,"sessionId":"sess-zcode-1","type":"model.streaming","payload":{"kind":"text_delta","delta":"resumed answer","done":false}}'
    printf '%s\n' '{"eventId":"r4","seq":4,"sessionId":"sess-zcode-1","type":"session.updated","payload":{"content":"resumed answer","stopReason":"stop","usage":{"inputTokens":90,"outputTokens":9,"totalTokens":99,"cacheReadTokens":0}}}'
    ;;
  turn-failed)
    printf '%s\n' '{"eventId":"f1","seq":1,"sessionId":"sess-zcode-1","type":"turn.started","payload":{}}'
    printf '%s\n' '{"eventId":"f2","seq":2,"sessionId":"sess-zcode-1","type":"turn.failed","payload":{"error":{"type":"unknown_error","code":"ECONNREFUSED","message":"Cannot connect to API: connect ECONNREFUSED 127.0.0.1:1","detail":"Turn execution failed"}}}'
    ;;
  no-terminal)
    # zcode exits 0 on early failures (unknown --model, bad flags) without
    # ever emitting a terminal event; the backend must fail the run anyway.
    printf '%s\n' '{"eventId":"n1","seq":1,"sessionId":"sess-zcode-1","type":"session.titleUpdated","payload":{}}'
    printf '%s\n' 'Error: Provider Registry 中不存在 Model: mock/nope' >&2
    ;;
esac
exit 0
`
}

func newFakeZcodeBackend(t *testing.T, env map[string]string) *zcodeBackend {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "zcode")
	writeTestExecutable(t, path, []byte(fakeZcodeScript()))
	return &zcodeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: env}}
}

func awaitZcodeResult(t *testing.T, session *Session) ([]Message, Result) {
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
		t.Fatal("timed out waiting for zcode result")
		return nil, Result{}
	}
}

func TestZcodeBackendStreamsNativeEvents(t *testing.T) {
	t.Parallel()
	backend := newFakeZcodeBackend(t, nil)
	session, err := backend.Execute(context.Background(), "reply 好的", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	messages, result := awaitZcodeResult(t, session)
	if result.Status != "completed" || result.Output != "好的" || result.SessionID != "sess-zcode-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	// The model id arrives mid-stream from session.updated bookkeeping and
	// keys the terminal usage.
	usage := result.Usage["zcode-test"]
	if usage.InputTokens != 14 || usage.OutputTokens != 4 || usage.CacheReadTokens != 6 {
		t.Fatalf("unexpected final usage: %+v", usage)
	}
	var running, text bool
	for _, message := range messages {
		switch message.Type {
		case MessageStatus:
			running = message.Status == "running" && message.SessionID == "sess-zcode-1"
		case MessageText:
			text = message.Content == "好的"
		}
	}
	if !running || !text {
		t.Fatalf("missing native events running=%v text=%v; messages=%+v", running, text, messages)
	}
}

func TestZcodeBackendPromptReachesArgvIntact(t *testing.T) {
	t.Parallel()
	argsPath := filepath.Join(t.TempDir(), "zcode.args")
	backend := newFakeZcodeBackend(t, map[string]string{"ZCODE_ARGS_FILE": argsPath})
	prompt := `reply with an em dash — and "double quotes" & $shell {chars}`
	session, err := backend.Execute(context.Background(), prompt, ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, result := awaitZcodeResult(t, session); result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 || lines[len(lines)-2] != "-p" || lines[len(lines)-1] != prompt {
		t.Fatalf("prompt not delivered as argv-final: %q", string(data))
	}
}

func TestZcodeBackendPreservesSuccessfulResumeSession(t *testing.T) {
	t.Parallel()
	backend := newFakeZcodeBackend(t, map[string]string{"ZCODE_MODE": "resume"})
	session, err := backend.Execute(context.Background(), "continue task", ExecOptions{
		ResumeSessionID: "sess-zcode-1", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitZcodeResult(t, session)
	if result.Status != "completed" || result.Output != "resumed answer" || result.SessionID != "sess-zcode-1" {
		t.Fatalf("resumed result = %+v", result)
	}
	// Resumed runs skip the terminal usage snapshot: whether zcode reports a
	// per-run or per-session total there is unverified, and overcounting is
	// the worse error (same policy as qwen).
	if len(result.Usage) != 0 {
		t.Fatalf("resumed usage should be omitted, got %+v", result.Usage)
	}
}

func TestZcodeBackendReportsTurnFailure(t *testing.T) {
	t.Parallel()
	backend := newFakeZcodeBackend(t, map[string]string{"ZCODE_MODE": "turn-failed"})
	session, err := backend.Execute(context.Background(), "break", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitZcodeResult(t, session)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "Cannot connect to API") {
		t.Fatalf("error = %q, want the turn.failed message", result.Error)
	}
}

func TestZcodeBackendFailsWithoutTerminalEvent(t *testing.T) {
	t.Parallel()
	// zcode exits 0 even when it fails before starting a turn; the missing
	// terminal event — not the exit code — must fail the run.
	backend := newFakeZcodeBackend(t, map[string]string{"ZCODE_MODE": "no-terminal"})
	session, err := backend.Execute(context.Background(), "hi", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitZcodeResult(t, session)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed despite exit 0", result.Status)
	}
	if !strings.Contains(result.Error, "不存在 Model") {
		t.Fatalf("error should carry the stderr tail, got %q", result.Error)
	}
}
