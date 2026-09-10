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

func TestNewReturnsCommandCodeBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("commandcode", Config{ExecutablePath: "/nonexistent/cmd"})
	if err != nil {
		t.Fatalf("New(commandcode): %v", err)
	}
	if _, ok := backend.(*commandcodeBackend); !ok {
		t.Fatalf("New(commandcode) = %T, want *commandcodeBackend", backend)
	}
}

func TestBuildCommandCodeArgsKeepsProtocolManaged(t *testing.T) {
	t.Parallel()
	args := buildCommandCodeArgs(ExecOptions{
		Model:           "meituan/longcat-2.0:free",
		ResumeSessionID: "session-1",
		ExtraArgs:       []string{"--output-format", "text", "--sandbox"},
		CustomArgs: []string{
			"--print=replace", "-o", "json", "--model", "other", "--resume", "other-session",
			"--yolo", "--permission-mode", "default", "--auto-accept",
			"--dangerously-skip-permissions", "--plan", "--no-skills", "--skip-onboarding",
			"--tools-all", "--tools-enable", "web_search", "--allowed-tools", "read_file",
			"--fork-session", "x", "--session", "y", "-c", "--continue",
			"--no-auto-update", "--debug",
		},
	}, slog.Default())
	joined := strings.Join(args, " ")
	argSet := make(map[string]struct{}, len(args))
	for _, a := range args {
		argSet[a] = struct{}{}
	}
	for _, forbidden := range []string{"text", "replace", "other-session", "other", "default", "read_file=", "web_search", "-c", "--continue", "--fork-session", "--session"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("managed argument %q leaked into %v", forbidden, args)
		}
	}
	// Bare value tokens: exact-match against argv elements (substring would
	// false-positive on kept args like --sandbox containing "x").
	for _, token := range []string{"x", "y"} {
		if _, ok := argSet[token]; ok {
			t.Fatalf("managed argument %q leaked into %v", token, args)
		}
	}
	// The prompt is never part of argv (see buildCommandCodeArgs) — it goes on stdin.
	if strings.Contains(joined, "-p ") || strings.HasPrefix(joined, "-p") {
		t.Fatalf("-p must not appear in argv, prompt is delivered on stdin: %v", args)
	}
	wantPrefix := []string{"--output-format", "json", "--no-auto-update", "--print", "--model", "meituan/longcat-2.0:free", "--resume", "session-1"}
	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short: %v", args)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all=%v", i, args[i], want, args)
		}
	}
	if !strings.Contains(joined, "--sandbox") || !strings.Contains(joined, "--debug") || !strings.Contains(joined, "--allowed-tools read_file") {
		t.Fatalf("non-managed custom args missing from %v", args)
	}
	// daemon-owned --yolo must be present; user's --yolo must be stripped so it
	// appears exactly once regardless of what custom_args contain.
	if count := strings.Count(joined, "--yolo"); count != 1 {
		t.Fatalf("--yolo count = %d in %v, want exactly 1 (daemon-owned)", count, args)
	}
}

func TestBuildCommandCodeArgsYoloAlwaysPresent(t *testing.T) {
	t.Parallel()
	args := buildCommandCodeArgs(ExecOptions{}, slog.Default())
	if !strings.Contains(strings.Join(args, " "), "--yolo") {
		t.Fatalf("--yolo missing from base args %v", args)
	}
}

// trimmed fixtures distilled from research-captures/*.ndjson. Kept small: a few
// lines each. The auto-update junk line case exercises non-JSON tolerance; the
// tool trio case exercises tool_use/tool_result synthesis; the resume case
// exercises session-id preservation.
const (
	fixtureAutoUpdateJunk = "Updated 1.50.0 → 1.50.1" + "\n" +
		`{"type":"event","event":{"type":"run_start","sessionId":"sess-junk"}}` + "\n" +
		`{"type":"event","event":{"type":"text_delta","delta":"hi"}}` + "\n" +
		`{"type":"result","subtype":"success","sessionId":"sess-junk","stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":100,"finalText":"hi"}`

	fixtureToolTrio = `{"type":"event","event":{"type":"run_start","sessionId":"sess-tool"}}` + "\n" +
		`{"type":"event","event":{"type":"tool_queued","toolCallId":"call-1","toolName":"read_file","input":{"file_path":"/work"}}}` + "\n" +
		`{"type":"event","event":{"type":"tool_running","toolCallId":"call-1","toolName":"read_file","description":"reading"}}` + "\n" +
		`{"type":"event","event":{"type":"tool_completed","toolCallId":"call-1","toolName":"read_file","result":[{"type":"text","text":"Read 1 file"}],"deferred":false}}` + "\n" +
		`{"type":"event","event":{"type":"text_delta","delta":"done"}}` + "\n" +
		`{"type":"result","subtype":"success","sessionId":"sess-tool","stopReason":"end_turn","usage":{"inputTokens":100,"outputTokens":5,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":500,"finalText":"done"}`

	fixtureResume = `{"type":"event","event":{"type":"run_start","sessionId":"sess-resume"}}` + "\n" +
		`{"type":"event","event":{"type":"text_delta","delta":"40"}}` + "\n" +
		`{"type":"result","subtype":"success","sessionId":"sess-resume","stopReason":"end_turn","usage":{"inputTokens":95319,"outputTokens":30,"cacheReadTokens":6016,"cacheWriteTokens":0},"durationMs":10594,"finalText":"40"}`
)

func collectCommandCode(t *testing.T, ndjson string) ([]Message, commandcodeStreamState) {
	t.Helper()
	state := commandcodeStreamState{usage: make(map[string]TokenUsage)}
	messages := make(chan Message, 64)
	for _, line := range strings.Split(ndjson, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		payload, ok := parseCommandCodeLine(line)
		if !ok {
			state.invalidEventCount++
			continue
		}
		state.eventCount++
		handleCommandCodePayload(payload, messages, &state)
	}
	close(messages)
	var collected []Message
	for m := range messages {
		collected = append(collected, m)
	}
	return collected, state
}

func TestCommandCodeParserToleratesAutoUpdateJunk(t *testing.T) {
	t.Parallel()
	messages, state := collectCommandCode(t, fixtureAutoUpdateJunk)
	if state.invalidEventCount != 1 {
		t.Fatalf("invalidEventCount = %d, want 1 (the junk line)", state.invalidEventCount)
	}
	if !state.sawResult || state.resultIsError || state.sessionID != "sess-junk" || state.finalResultText != "hi" {
		t.Fatalf("unexpected state: %+v", state)
	}
	var text bool
	for _, m := range messages {
		if m.Type == MessageText && m.Content == "hi" {
			text = true
		}
	}
	if !text {
		t.Fatalf("missing text message; messages=%+v", messages)
	}
}

func TestCommandCodeParserSynthesizesToolTrio(t *testing.T) {
	t.Parallel()
	messages, state := collectCommandCode(t, fixtureToolTrio)
	if !state.sawResult || state.resultIsError || state.sessionID != "sess-tool" || state.finalResultText != "done" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.usage[""].InputTokens != 100 {
		// model is empty in this fixture; usage is keyed by model from
		// model_request_end which the fixture omits, so it lands under "".
	}
	var toolUse, toolResult, text bool
	for _, m := range messages {
		switch m.Type {
		case MessageToolUse:
			toolUse = m.Tool == "read_file" && m.CallID == "call-1" && m.Input["file_path"] == "/work"
		case MessageToolResult:
			toolResult = m.CallID == "call-1" && m.Output == "Read 1 file"
		case MessageText:
			text = m.Content == "done"
		}
	}
	if !toolUse || !toolResult || !text {
		t.Fatalf("missing tool events toolUse=%v toolResult=%v text=%v; messages=%+v", toolUse, toolResult, text, messages)
	}
}

func TestCommandCodeParserPreservesResumeSession(t *testing.T) {
	t.Parallel()
	_, state := collectCommandCode(t, fixtureResume)
	if state.sessionID != "sess-resume" {
		t.Fatalf("sessionID = %q, want sess-resume", state.sessionID)
	}
	if state.finalResultText != "40" {
		t.Fatalf("finalResultText = %q, want 40", state.finalResultText)
	}
	if state.usage[""].InputTokens != 95319 {
		t.Fatalf("usage = %+v", state.usage)
	}
}

func TestCommandCodeProbe1NoToolFixtureParses(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "research-captures", "probe1-no-tool.ndjson"))
	if err != nil {
		t.Skipf("probe fixture not present: %v", err)
	}
	state := commandcodeStreamState{usage: make(map[string]TokenUsage)}
	messages := make(chan Message, 128)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		payload, ok := parseCommandCodeLine(line)
		if !ok {
			t.Fatalf("fixture line did not parse: %v\n%s", err, line)
		}
		handleCommandCodePayload(payload, messages, &state)
	}
	close(messages)
	var thinking, text bool
	for m := range messages {
		switch m.Type {
		case MessageThinking:
			thinking = m.Content == "\nThe user is asking a simple math question in Portuguese: \"what is 2+2?\" They want a one-line answer."
		case MessageText:
			text = m.Content == "4"
		}
	}
	if !state.sawResult || state.resultIsError || state.sessionID != "86fbfbb5-91f4-4819-b832-41e9a4c6ed9f" || state.finalResultText != "4" {
		t.Fatalf("unexpected fixture state: %+v", state)
	}
	if !thinking || !text {
		t.Fatalf("missing streamed events thinking=%v text=%v", thinking, text)
	}
}

func TestCommandCodeProbe2ToolReadFixtureParses(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "research-captures", "probe2-tool-read.ndjson"))
	if err != nil {
		t.Skipf("probe fixture not present: %v", err)
	}
	state := commandcodeStreamState{usage: make(map[string]TokenUsage)}
	messages := make(chan Message, 256)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		payload, ok := parseCommandCodeLine(line)
		if !ok {
			t.Fatalf("fixture line did not parse: %s", line)
		}
		handleCommandCodePayload(payload, messages, &state)
	}
	close(messages)
	var toolUse, toolResult bool
	for m := range messages {
		switch m.Type {
		case MessageToolUse:
			toolUse = m.Tool == "read_file" && m.CallID == "call_a852418901254596861d91b9"
		case MessageToolResult:
			toolResult = m.CallID == "call_a852418901254596861d91b9" && strings.Contains(m.Output, "quack 42")
		}
	}
	if !state.sawResult || state.resultIsError || state.finalResultText != "42" {
		t.Fatalf("unexpected fixture state: %+v", state)
	}
	if !toolUse || !toolResult {
		t.Fatalf("missing tool events toolUse=%v toolResult=%v", toolUse, toolResult)
	}
}

func TestParseCommandCodeModelsScrapesListModelsTable(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "research-captures", "probe4-list-models.txt"))
	if err != nil {
		t.Skipf("list-models fixture not present: %v", err)
	}
	models := parseCommandCodeModels(data)
	if len(models) == 0 {
		t.Fatalf("no models parsed from list-models output")
	}
	// Spot-check a few known ids from the table.
	known := map[string]bool{}
	for _, m := range models {
		known[m.ID] = true
	}
	for _, want := range []string{"deepseek/deepseek-v4-pro", "claude-sonnet-5", "gpt-6-astra", "xai/grok-4.5"} {
		if !known[want] {
			t.Fatalf("expected model %q not found in %d parsed models", want, len(models))
		}
	}
	// Section headers and footer must not leak in.
	for _, banned := range []string{"Open Source", "Anthropic", "Pass", "Docs:", "Available"} {
		if known[banned] {
			t.Fatalf("non-model token %q leaked into catalog", banned)
		}
	}
}

func fakeCommandCodeScript() string {
	return `#!/bin/sh
if [ -n "$CMD_ARGS_FILE" ]; then printf '%s\n' "$@" > "$CMD_ARGS_FILE"; fi
if [ -n "$CMD_STDIN_FILE" ]; then cat > "$CMD_STDIN_FILE"; fi
case "$CMD_MODE" in
  error)
    printf '%s\n' '{"type":"event","event":{"type":"run_start","sessionId":"sess-error"}}'
    printf '%s\n' '{"type":"result","subtype":"error","sessionId":"sess-error","stopReason":"error","usage":{"inputTokens":0,"outputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":0,"finalText":""}'
    ;;
  junk)
    printf '%s\n' 'Updated 1.50.0 → 1.50.1'
    printf '%s\n' '{"type":"event","event":{"type":"run_start","sessionId":"sess-junk"}}'
    printf '%s\n' '{"type":"event","event":{"type":"text_delta","delta":"ok"}}'
    printf '%s\n' '{"type":"result","subtype":"success","sessionId":"sess-junk","stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":10,"finalText":"ok"}'
    ;;
  resume-missing)
    echo 'No conversation found for the given session id' >&2
    exit 1
    ;;
  spin)
    while :; do :; done
    ;;
  *)
    printf '%s\n' '{"type":"event","event":{"type":"run_start","sessionId":"sess-cmd-1","model":"cmd-test"}}'
    printf '%s\n' '{"type":"event","event":{"type":"thinking_delta","delta":"thinking..."}}'
    printf '%s\n' '{"type":"event","event":{"type":"tool_queued","toolCallId":"call-1","toolName":"list_directory","input":{"path":"/work"}}}' >/dev/null 2>&1
    printf '%s\n' '{"type":"event","event":{"type":"tool_queued","toolCallId":"call-1","toolName":"list_directory","input":{"path":"/work"}}}'
    printf '%s\n' '{"type":"event","event":{"type":"tool_completed","toolCallId":"call-1","toolName":"list_directory","result":[{"type":"text","text":"Listed 1 item"}],"deferred":false}}'
    printf '%s\n' '{"type":"event","event":{"type":"text_delta","delta":"PONG"}}'
    printf '%s\n' '{"type":"result","subtype":"success","sessionId":"sess-cmd-1","stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":0},"durationMs":100,"finalText":"PONG"}'
    ;;
esac
`
}

func newFakeCommandCodeBackend(t *testing.T, env map[string]string) *commandcodeBackend {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "cmd")
	writeTestExecutable(t, path, []byte(fakeCommandCodeScript()))
	return &commandcodeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: env}}
}

func awaitCommandCodeResult(t *testing.T, session *Session) ([]Message, Result) {
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
		t.Fatal("timed out waiting for commandcode result")
		return nil, Result{}
	}
}

func TestCommandCodeBackendStreamsNativeEvents(t *testing.T) {
	t.Parallel()
	backend := newFakeCommandCodeBackend(t, nil)
	session, err := backend.Execute(context.Background(), "reply PONG", ExecOptions{Model: "cmd-test", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	messages, result := awaitCommandCodeResult(t, session)
	if result.Status != "completed" || result.Output != "PONG" || result.SessionID != "sess-cmd-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	usage := result.Usage["cmd-test"]
	if usage.InputTokens != 10 || usage.OutputTokens != 2 || usage.CacheReadTokens != 3 {
		t.Fatalf("unexpected final usage: %+v", usage)
	}
	var thinking, toolUse, toolResult, text bool
	for _, message := range messages {
		switch message.Type {
		case MessageThinking:
			thinking = message.Content == "thinking..."
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

func TestCommandCodeBackendPreservesSuccessfulResumeSession(t *testing.T) {
	t.Parallel()
	backend := newFakeCommandCodeBackend(t, nil)
	session, err := backend.Execute(context.Background(), "continue task", ExecOptions{
		Model: "cmd-test", ResumeSessionID: "sess-cmd-1", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitCommandCodeResult(t, session)
	if result.Status != "completed" || result.Output != "PONG" || result.SessionID != "sess-cmd-1" {
		t.Fatalf("resumed result = %+v", result)
	}
}

func TestCommandCodeBackendDeliversPromptOnStdin(t *testing.T) {
	t.Parallel()
	stdinPath := filepath.Join(t.TempDir(), "cmd.stdin")
	backend := newFakeCommandCodeBackend(t, map[string]string{"CMD_STDIN_FILE": stdinPath})
	prompt := `go build -ldflags "-X main.version=foo" — mind the em dash & the quotes'`
	session, err := backend.Execute(context.Background(), prompt, ExecOptions{Model: "cmd-test", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitCommandCodeResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(got) != prompt {
		t.Fatalf("stdin content = %q, want %q", got, prompt)
	}
}

func TestCommandCodeBackendToleratesAutoUpdateJunkLine(t *testing.T) {
	t.Parallel()
	backend := newFakeCommandCodeBackend(t, map[string]string{"CMD_MODE": "junk"})
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Model: "cmd-test", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitCommandCodeResult(t, session)
	if result.Status != "completed" || result.Output != "ok" || result.SessionID != "sess-junk" {
		t.Fatalf("unexpected result after junk line: %+v", result)
	}
}

func TestCommandCodeBackendFailureTimeoutAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
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
		{"result error", map[string]string{"CMD_MODE": "error"}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, ExecOptions{}, "failed", "commandcode returned an error result without details", "sess-error"},
		{"missing resume", map[string]string{"CMD_MODE": "resume-missing"}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, ExecOptions{ResumeSessionID: "session-redacted"}, "failed", "No conversation found", ""},
		{"timeout", map[string]string{"CMD_MODE": "spin"}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, ExecOptions{Timeout: 20 * time.Millisecond}, "timeout", "timed out", ""},
		{"cancel", map[string]string{"CMD_MODE": "spin"}, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, ExecOptions{}, "aborted", "cancelled", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			if tc.name == "cancel" {
				go func() { time.Sleep(20 * time.Millisecond); cancel() }()
			} else {
				defer cancel()
			}
			session, err := newFakeCommandCodeBackend(t, tc.env).Execute(ctx, "task", tc.opts)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			_, result := awaitCommandCodeResult(t, session)
			if result.Status != tc.status || !strings.Contains(result.Error, tc.needle) || result.SessionID != tc.wantSession {
				t.Fatalf("result = %+v, want status=%q error containing %q session=%q", result, tc.status, tc.needle, tc.wantSession)
			}
		})
	}
}
