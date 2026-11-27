package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
)

// fakeTraecliACPScript impersonates the official `traecli acp serve` for unit
// tests. It speaks the SAME wire format the real traecli v0.120.42 emits
// (captured live): method "session/update" with an "update.sessionUpdate"
// discriminator (agent_thought_chunk / agent_message_chunk), session/new
// returning result.sessionId + models.availableModels [{modelId,name,
// description}], and session/prompt returning {stopReason:end_turn}.
func fakeTraecliACPScript() string {
	return `#!/bin/sh
if [ -n "$TRAECLI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$TRAECLI_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$TRAECLI_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$TRAECLI_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"availableModels":[{"modelId":"Doubao-Seed-2.1-Pro","name":"Doubao-Seed-2.1-Pro","description":""},{"modelId":"GLM-5.2","name":"GLM-5.2","description":""}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      [ -z "$TRAE_TEST_HISTORY" ] || printf '%s\n' "$TRAE_TEST_HISTORY"
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_loaded","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"history replay ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      case "$line" in
        *bogus-model*)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
          exit 0
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
          ;;
      esac
      ;;
    *'"method":"session/prompt"'*)
      if [ -n "$TRAE_TEST_ERROR" ]; then
        [ -z "$TRAE_TEST_UPDATES" ] || printf '%s\n' "$TRAE_TEST_UPDATES"
        printf '{"jsonrpc":"2.0","id":%s,"error":%s}\n' "$id" "$TRAE_TEST_ERROR"
        [ -z "$TRAE_TEST_TRAILING" ] || printf '%s\n' "$TRAE_TEST_TRAILING"
        [ -z "$TRAE_TEST_HOLD_OPEN" ] || sleep 4
        exit "${TRAE_TEST_EXIT:-0}"
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thinking about it"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","name":"Shell","status":"pending","parameters":{"command":"echo hi"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","name":"Shell","output":"hi\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":17,"outputTokens":5,"cachedReadTokens":3,"cachedWriteTokens":2,"costUsdTicks":900}}}\n' "$id"
      if [ -n "$TRAECLI_LATE_CHUNK" ]; then
        sleep 0.05
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" tail"}}}}\n'
      fi
      exit 0
      ;;
  esac
done
`
}

func TestTraecliBackendStreamsAndCompletes(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "traecli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say pong", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range session.Messages {
			messages = append(messages, m)
		}
	}()
	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong") {
		t.Fatalf("output = %q, want it to contain the assistant message 'pong'", result.Output)
	}
	if result.SessionID != "ses_new" {
		t.Fatalf("session id = %q, want ses_new", result.SessionID)
	}
	if usage := result.Usage["unknown"]; usage != (TokenUsage{InputTokens: 17, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 2, CostUSDTicks: 900}) {
		t.Fatalf("usage = %+v, want all prompt-result fields", usage)
	}
	// The agent_message_chunk must surface as MessageText; the tool_call must
	// surface as a MessageToolUse normalized to a canonical tool name.
	var sawText, sawToolUse bool
	for _, m := range messages {
		if m.Type == MessageText && strings.Contains(m.Content, "pong") {
			sawText = true
		}
		if m.Type == MessageToolUse && m.Tool == "terminal" {
			sawToolUse = true
		}
	}
	if !sawText {
		t.Error("expected a MessageText carrying the assistant 'pong'")
	}
	if !sawToolUse {
		t.Errorf("expected the Shell tool_call to normalize to 'terminal'; messages=%+v", messages)
	}
}

func TestTraecliBlockedArgsFiltering(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "traecli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))

	backend, err := New("traecli", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"TRAECLI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{
		Timeout: 5 * time.Second,
		// Users must not be able to strip ACP mode, switch to print mode,
		// override the permission mode, or duplicate the subcommand.
		CustomArgs: []string{"acp", "serve", "--yolo", "-p", "--output-format", "json", "--permission-mode", "default", "--add-dir", "/tmp/extra"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	wantPrefix := []string{"acp", "serve", "--yolo"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	// The hardcoded prefix must appear exactly once each.
	joined := strings.Join(lines, " ")
	for _, once := range []string{"acp", "serve", "--yolo"} {
		if c := countTokens(lines, once); c != 1 {
			t.Errorf("expected exactly one %q, got %d (full: %q)", once, c, joined)
		}
	}
	for _, blocked := range []string{"-p", "--output-format", "json", "--permission-mode", "default"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("blocked custom arg %q survived filtering: %q", blocked, lines)
			}
		}
	}
	// An allowed custom arg must survive.
	if !strings.Contains(joined, "--add-dir /tmp/extra") {
		t.Errorf("expected allowed custom arg --add-dir to survive, got %q", joined)
	}
}

func countTokens(lines []string, tok string) int {
	n := 0
	for _, l := range lines {
		if l == tok {
			n++
		}
	}
	return n
}

func TestTraecliExecutionModeNewAndResumedSessions(t *testing.T) {
	t.Parallel()
	for _, resumeID := range []string{"", "ses_existing"} {
		name := "new"
		if resumeID != "" {
			name = "resumed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "args.txt")
			requestsFile := filepath.Join(dir, "requests.jsonl")
			fakePath := filepath.Join(dir, "traecli")
			writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))
			backend, err := New("traecli", Config{
				ExecutablePath: fakePath, Logger: slog.Default(),
				Env: map[string]string{"TRAECLI_ARGS_FILE": argsFile, "TRAECLI_REQUESTS_FILE": requestsFile},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			const prompt = "Only explain the implementation plan. Do not change code or implement it."
			session, err := backend.Execute(ctx, prompt, ExecOptions{ResumeSessionID: resumeID, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := <-session.Result
			if result.Status != "completed" || result.Output != "pong" {
				t.Fatalf("status=%q error=%q output=%q", result.Status, result.Error, result.Output)
			}
			rawArgs, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
			for _, tool := range []string{"EnterPlanMode", "ExitPlanMode"} {
				found := false
				for i := 1; i < len(args); i++ {
					if args[i-1] == "--disallowed-tool" && args[i] == tool {
						found = true
					}
				}
				if !found {
					t.Errorf("native %s must be disabled, args=%q", tool, args)
				}
			}
			raw, err := os.ReadFile(requestsFile)
			if err != nil {
				t.Fatal(err)
			}
			prompts := 0
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var request struct {
					Method string `json:"method"`
					Params struct {
						SessionID string `json:"sessionId"`
						Prompt    []struct {
							Text string `json:"text"`
						} `json:"prompt"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}
				if request.Method != "session/prompt" {
					continue
				}
				prompts++
				if len(request.Params.Prompt) != 1 || request.Params.Prompt[0].Text != prompt || request.Params.SessionID != result.SessionID {
					t.Fatalf("original request or session changed: %+v", request)
				}
			}
			if prompts != 1 {
				t.Fatalf("sent %d prompts, want only the original request", prompts)
			}
		})
	}
}

func TestTraecliSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()
	fakePath := filepath.Join(t.TempDir(), "traecli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))

	backend, err := New("traecli", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{Model: "bogus-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed on set_model error, got %q", result.Status)
	}
	if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
		t.Errorf("expected error to name the model, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "model not available") {
		t.Errorf("expected upstream message surfaced, got %q", result.Error)
	}
}

func TestTraecliUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "traecli")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))

	backend, err := New("traecli", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"TRAECLI_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "continue", ExecOptions{
		ResumeSessionID: "ses_existing",
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load on resume, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("traecli must use session/load (loadSession:true), not session/resume:\n%s", requests)
	}
}

// TestTraecliDrainsNotificationsAfterPromptResponse pins the trailing-notification
// drain. traecli ACP can emit a final session update just after the
// session/prompt response returns; closing stdin and cancelling the context at
// that boundary raced the stdout reader and silently truncated the last chunk.
// The same defect was fixed for the sibling ACP backends in #5440 (grok) and
// #5675 (hermes).
func TestTraecliDrainsNotificationsAfterPromptResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "trae")
	writeTestExecutable(t, fakePath, []byte(fakeTraecliACPScript()))

	backend, err := New("traecli", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"TRAECLI_LATE_CHUNK": "1"},
	})
	if err != nil {
		t.Fatalf("new traecli backend: %v", err)
	}

	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "pong tail") {
		t.Fatalf("late output was truncated: %q", result.Output)
	}
}

func TestTraecliDisconnectToolHistory(t *testing.T) {
	const failure = `{"code":-32603,"message":"Internal error","data":{"message":"stream disconnected before completion: llm response empty stream","trae_stream_errors":[{"trae_error_info":{"response_stream_disconnected":{"http_status_code":null}}}]}}`
	update := func(body string) string {
		return `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_new","update":` + body + `}}`
	}
	usage := update(`{"sessionUpdate":"usage_update","used":1,"size":32}`)
	start := update(`{"sessionUpdate":"tool_call","toolCallId":"a","status":"in_progress","title":"Bash"}`)
	done := update(`{"sessionUpdate":"tool_call_update","toolCallId":"a","status":"completed"}`)
	fake := filepath.Join(t.TempDir(), "traecli")
	writeTestExecutable(t, fake, []byte(fakeTraecliACPScript()))
	for _, tc := range []struct {
		name, updates, trailing       string
		resume, holdOpen, crash, want bool
	}{
		{name: "verified clean turn", updates: usage, want: true},
		{name: "previous turn tool replay", updates: usage, resume: true, want: true},
		{name: "unfinished tool", updates: usage + "\n" + start},
		{name: "completed tool", updates: start + "\n" + done + "\n" + usage},
		{name: "orphan tool result", updates: usage + "\n" + done},
		{name: "late tool start", updates: usage, trailing: start},
		{name: "history absent"},
		{name: "session metadata only", updates: update(`{"sessionUpdate":"available_commands_update"}`)},
		{name: "agent request without a tool event", updates: usage + "\n" + `{"jsonrpc":"2.0","id":10,"method":"session/request_permission","params":{}}`},
		{name: "malformed history", updates: usage + "\n{"},
		{name: "unrecognised update", updates: usage + "\n" + update(`{"sessionUpdate":"new_activity"}`)},
		{name: "history for another session", updates: strings.Replace(usage, "ses_new", "another", 1)},
		{name: "stream never drained", updates: usage, holdOpen: true},
		{name: "provider crashed after sending the error", updates: usage, crash: true},
		{name: "tool start after display buffer fills", updates: strings.Repeat(update(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"a"}}`)+"\n", 270) + start},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"TRAE_TEST_ERROR": failure, "TRAE_TEST_UPDATES": tc.updates, "TRAE_TEST_TRAILING": tc.trailing}
			opts := ExecOptions{}
			if tc.resume {
				opts.ResumeSessionID = "ses_loaded"
				env["TRAE_TEST_HISTORY"] = strings.Replace(start, "ses_new", "ses_loaded", 1)
				env["TRAE_TEST_UPDATES"] = strings.Replace(tc.updates, "ses_new", "ses_loaded", 1)
			}
			if tc.crash {
				env["TRAE_TEST_EXIT"] = "1"
			}
			if tc.holdOpen {
				env["TRAE_TEST_HOLD_OPEN"] = "1"
			}
			b, err := New("traecli", Config{ExecutablePath: fake, Env: env, Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := b.Execute(ctx, "test prompt", opts)
			if err != nil {
				t.Fatal(err)
			}
			result := <-session.Result
			if result.Status != "failed" || !strings.Contains(result.Error, "llm response empty stream") || result.NoToolCallsStarted != tc.want {
				t.Fatalf("status=%q, no tool calls started=%t (want %t), error=%q", result.Status, result.NoToolCallsStarted, tc.want, result.Error)
			}
		})
	}
}
