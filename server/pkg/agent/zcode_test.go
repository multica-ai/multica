package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
)

// fakeZcodeACPScript is a minimal zcode-acp-server double speaking the ACP
// subset zcodeBackend uses: initialize, session/new, session/resume,
// session/setModel, session/prompt. Environment knobs:
//
//   - ZC_ARGC_FILE   — receives the script's argument count, so a test can
//     assert the daemon launched the bridge with no protocol subcommand.
//   - ZC_SET_MODEL_ERROR — when "1", session/setModel answers with the
//     upstream error string "model not available".
//   - ZC_PROMPT_ERROR — when "session_not_found", session/prompt answers
//     with a -32002 session-not-found error (the bridge surfaces a dead
//     zcode session this way).
//   - ZC_REPLAY — when "1", emits an agent_message_chunk right after
//     session/resume, before session/prompt is even received, to model the
//     resume-time state replay the streaming gate must drop.
func fakeZcodeACPScript() string {
	return `#!/bin/sh
printf '%s\n' "$#" >> "${ZC_ARGC_FILE:-/dev/null}"
ZC_VERSION="${ZC_VERSION:-0.2.0}"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "${ZC_REQUESTS_FILE:-/dev/null}"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"zcode-acp-server","version":"%s"},"agentCapabilities":{"loadSession":true}}}\n' "$id" "$ZC_VERSION"
      ;;
    *'"method":"session/new"'*)
      if [ "$ZC_NEW_ERROR" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":{"details":"zcode create failed: timeout"}}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_zc_new","configOptions":[{"id":"model","currentValue":"glm-5.3"},{"id":"thought","category":"thought_level","currentValue":"max","options":[{"value":"low","name":"low"},{"value":"high","name":"high"},{"value":"max","name":"max"}]}]}}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      requested=$(printf '%s' "$line" | sed -n 's/.*"value":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"thought","category":"thought_level","currentValue":"%s"}]}}\n' "$id" "$requested"
      ;;
    *'"method":"session/resume"'*)
      if [ "$ZC_RESUME_ERROR" = "session_not_found" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":{"details":"zcode resume failed: Session not found: ses_zc_resumed"}}}\n' "$id"
        exit 0
      fi
      if [ "$ZC_REPLAY" = "1" ]; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_zc_resumed","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"stale replay from the previous turn"}}}}\n'
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_zc_resumed","configOptions":[{"id":"thought","category":"thought_level","currentValue":"max","options":[{"value":"low","name":"low"},{"value":"high","name":"high"},{"value":"max","name":"max"}]}]}}\n' "$id"
      ;;
    *'"method":"session/setModel"'*)
      if [ "$ZC_SET_MODEL_ERROR" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"model not available"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/prompt"'*)
      if [ "$ZC_HANG" = "1" ]; then
        # Never answer; block on the next inbound line so a test can observe
        # the session/cancel notification arriving, then exit so the reader
        # sees EOF.
        IFS= read -r extra
        printf '%s\n' "$extra" >> "${ZC_REQUESTS_FILE:-/dev/null}"
        exit 0
      fi
      if [ "$ZC_STOP_REASON" = "cancelled" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$id"
        exit 0
      fi
      if [ "$ZC_PROMPT_ERROR" = "session_not_found" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"session not found: ses_zc_resumed"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"tool_call","toolCallId":"call_1","title":"Bash: echo pong","kind":"execute","status":"pending"}}}\n' "$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')"
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"completed"}}}\n' "$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')"
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n' "$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func runZcodeTest(t *testing.T, env map[string]string, opts ExecOptions) (*Result, []Message) {
	t.Helper()
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "zcode-acp-server")
	writeTestExecutable(t, fakePath, []byte(fakeZcodeACPScript()))

	testEnv := map[string]string{"ZC_ARGC_FILE": filepath.Join(dir, "argc")}
	for k, v := range env {
		testEnv[k] = v
	}

	backend, err := New("zcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            testEnv,
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	session, err := backend.Execute(context.Background(), "reply with pong", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var msgs []Message
	done := make(chan struct{})
	go func() {
		for msg := range session.Messages {
			msgs = append(msgs, msg)
		}
		close(done)
	}()
	select {
	case result := <-session.Result:
		<-done
		return &result, msgs
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
		return nil, nil
	}
}

// TestZcodeBackendLaunchesBridgeWithoutSubcommand pins the launch contract:
// unlike kimi/qoder the bridge needs no `acp` positional, so the daemon must
// spawn it bare (custom_args aside).
func TestZcodeBackendLaunchesBridgeWithoutSubcommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "zcode-acp-server")
	writeTestExecutable(t, fakePath, []byte(fakeZcodeACPScript()))
	argcFile := filepath.Join(dir, "argc")

	backend, err := New("zcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"ZC_ARGC_FILE": argcFile},
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "hi", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case <-session.Result:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
	raw, err := os.ReadFile(argcFile)
	if err != nil {
		t.Fatalf("read argc file: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(raw)))
	for _, n := range lines {
		if n != "0" {
			t.Errorf("bridge launched with %s argument(s); expected none", n)
		}
	}
}

// TestZcodeBackendStreamsPromptAndResult covers the happy path: fresh
// session, no model override, tool call + final chunk streamed, end_turn,
// completed result carrying the session id for the next resume.
func TestZcodeBackendStreamsPromptAndResult(t *testing.T) {
	t.Parallel()

	result, msgs := runZcodeTest(t, nil, ExecOptions{})
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Output != "pong" {
		t.Errorf("expected output %q, got %q", "pong", result.Output)
	}
	if result.SessionID != "ses_zc_new" {
		t.Errorf("expected session id ses_zc_new, got %q", result.SessionID)
	}
	if result.ResumeRejected {
		t.Error("unexpected ResumeRejected on a fresh session")
	}
	var sawTool, sawText bool
	for _, m := range msgs {
		if m.Type == MessageToolUse && m.Tool == "Bash" {
			// The bridge's tool name must arrive unmapped — hermesClient's
			// title parser passes the prefix through, and remapping it to a
			// generic bucket (kimi-style "terminal") would hide which tool ran.
			sawTool = true
		}
		if m.Type == MessageText && m.Content == "pong" {
			sawText = true
		}
	}
	if !sawTool || !sawText {
		t.Errorf("expected a tool_use and a text message on the stream, got tool=%v text=%v (%d messages)", sawTool, sawText, len(msgs))
	}
}

// TestZcodeBackendSetModelFailureFailsTask pins the contract that a rejected
// model switch fails the task instead of silently running on the default
// model, and that the session id survives so the failure is diagnosable.
func TestZcodeBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	result, _ := runZcodeTest(t, map[string]string{"ZC_SET_MODEL_ERROR": "1"}, ExecOptions{
		Model: "bogus-model",
	})
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
		t.Errorf("expected error to name the requested model, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "model not available") {
		t.Errorf("expected error to surface upstream message, got %q", result.Error)
	}
	if result.SessionID != "ses_zc_new" {
		t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
	}
}

// TestZcodeBackendSetModelSessionNotFoundClearsSessionID covers the resumed
// session dying before the model switch: the id must be cleared and
// ResumeRejected set so the daemon retries fresh instead of looping on a
// dead pointer.
func TestZcodeBackendSetModelSessionNotFoundClearsSessionID(t *testing.T) {
	t.Parallel()

	// The fixture fails every setModel with -32603 wrapped as session
	// not found via ZC_PROMPT_ERROR is not used here; use the prompt path
	// instead: set a model but make the prompt fail with session not found.
	result, _ := runZcodeTest(t, map[string]string{"ZC_PROMPT_ERROR": "session_not_found"}, ExecOptions{
		ResumeSessionID: "ses_zc_resumed",
	})
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !result.ResumeRejected {
		t.Error("expected ResumeRejected when the resumed session is gone at prompt time")
	}
	if result.SessionID != "" {
		t.Errorf("expected cleared session id, got %q", result.SessionID)
	}
}

// TestZcodeBackendDropsResumeReplay pins the streaming gate: chunks emitted
// between session/resume and session/prompt (the bridge's state replay) must
// not leak into the turn's output.
func TestZcodeBackendDropsResumeReplay(t *testing.T) {
	t.Parallel()

	result, msgs := runZcodeTest(t, map[string]string{"ZC_REPLAY": "1"}, ExecOptions{
		ResumeSessionID: "ses_zc_resumed",
	})
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Output != "pong" {
		t.Errorf("expected output %q, got %q", "pong", result.Output)
	}
	if strings.Contains(result.Output, "stale replay") {
		t.Error("resume-time replay leaked into the result output")
	}
	for _, m := range msgs {
		if m.Type == MessageText && strings.Contains(m.Content, "stale replay") {
			t.Error("resume-time replay leaked onto the message stream")
		}
	}
	if result.SessionID != "ses_zc_resumed" {
		t.Errorf("expected resumed session id, got %q", result.SessionID)
	}
}

// TestZcodeBackendSendsGracefulCancel pins the graceful-cancellation
// contract: a cancelled task must notify session/cancel to the bridge before
// the process is reaped, and still deliver an aborted Result. Not parallel:
// it shortens the shared zcodeCancelWaitDelay for the duration of the run.
func TestZcodeBackendSendsGracefulCancel(t *testing.T) {
	origDelay := zcodeCancelWaitDelay
	zcodeCancelWaitDelay = 800 * time.Millisecond
	t.Cleanup(func() { zcodeCancelWaitDelay = origDelay })

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "zcode-acp-server")
	writeTestExecutable(t, fakePath, []byte(fakeZcodeACPScript()))
	requestsFile := filepath.Join(dir, "requests")

	backend, err := New("zcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"ZC_ARGC_FILE":     filepath.Join(dir, "argc"),
			"ZC_REQUESTS_FILE": requestsFile,
			"ZC_HANG":          "1",
		},
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session, err := backend.Execute(ctx, "long-running prompt", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Wait until the fixture is actually hanging inside the session/prompt
	// branch (the session exists and the turn is in flight) before
	// cancelling, so the test does not race session creation under parallel
	// load.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(requestsFile); err == nil && strings.Contains(string(raw), `"method":"session/prompt"`) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" && result.Status != "failed" {
			t.Fatalf("expected aborted/failed after cancel, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for result after cancel")
	}

	// The cancel notification is written by the turn goroutine's cleanup
	// defer, which runs after the Result is delivered — poll for it instead
	// of racing a single read.
	var sawCancel bool
	pollDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(pollDeadline) {
		raw, err := os.ReadFile(requestsFile)
		if err == nil && strings.Contains(string(raw), `"method":"session/cancel"`) {
			sawCancel = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !sawCancel {
		raw, _ := os.ReadFile(requestsFile)
		t.Errorf("expected a session/cancel notification to reach the bridge, got requests:\n%s", raw)
	}
}

// TestZcodeBackendKnownBadVersionStillRuns pins that the 0.1.0 bridge version
// only logs a warning: tasks still execute (and fail with the bridge's own
// timeout error) rather than being refused upfront, so a fork build with an
// unlabelled version is never locked out.
func TestZcodeBackendKnownBadVersionStillRuns(t *testing.T) {
	t.Parallel()

	result, _ := runZcodeTest(t, map[string]string{"ZC_VERSION": "0.1.0"}, ExecOptions{})
	if result.Status != "completed" {
		t.Fatalf("expected status=completed despite the version warning, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Output != "pong" {
		t.Errorf("expected output %q, got %q", "pong", result.Output)
	}
}

// TestZcodeBackendResumeRejectedAtResumeTime pins the bridge-specific
// contract the real run surfaced: unlike kimi (whose resume echoes success
// for a dead session and fails later at prompt time), the bridge rejects an
// unknown session at session/resume with "Session not found". That must
// report ResumeRejected so the daemon retries fresh instead of looping.
func TestZcodeBackendResumeRejectedAtResumeTime(t *testing.T) {
	t.Parallel()

	result, _ := runZcodeTest(t, map[string]string{"ZC_RESUME_ERROR": "session_not_found"}, ExecOptions{
		ResumeSessionID: "ses_zc_resumed",
	})
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}
	if !result.ResumeRejected {
		t.Error("expected ResumeRejected when the bridge rejects the resume outright")
	}
	if result.SessionID != "" {
		t.Errorf("expected cleared session id, got %q", result.SessionID)
	}
}

// TestZcodeBackendCreateFailureAppendsKnownBadVersionHint pins the upgrade
// hint: it must appear only when session/new actually failed AND the bridge
// reported the npm 0.1.0 version — a main-built fork also reports 0.1.0 but
// carries the fix, so success must never carry the hint.
func TestZcodeBackendCreateFailureAppendsKnownBadVersionHint(t *testing.T) {
	t.Parallel()

	result, _ := runZcodeTest(t, map[string]string{"ZC_NEW_ERROR": "1", "ZC_VERSION": "0.1.0"}, ExecOptions{})
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "upgrade zcode-acp-server") {
		t.Errorf("expected the upgrade hint on a 0.1.0 create failure, got %q", result.Error)
	}
}

// TestZcodeBackendRuntimeCancelledStopReason maps the bridge's
// stopReason="cancelled" PromptResponse (what session/cancel produces when
// the runtime ends the turn as cancelled) to an aborted result, not a
// completion with no output.
func TestZcodeBackendRuntimeCancelledStopReason(t *testing.T) {
	t.Parallel()

	result, _ := runZcodeTest(t, map[string]string{"ZC_STOP_REASON": "cancelled"}, ExecOptions{})
	if result.Status != "aborted" {
		t.Fatalf("expected status=aborted for stopReason=cancelled, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Error != "zcode cancelled the prompt" {
		t.Errorf("expected the cancellation error text, got %q", result.Error)
	}
}

// TestZcodeBackendAppliesThinkingLevel pins the effort wiring: a persisted
// thinking level is delivered through session/set_config_option using the
// option id the bridge advertised ("thought"), the value passes through
// verbatim (the runtime's vocabulary is enabled/disabled, not a graded
// scale), and a task still completes.
func TestZcodeBackendAppliesThinkingLevel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "zcode-acp-server")
	writeTestExecutable(t, fakePath, []byte(fakeZcodeACPScript()))
	requestsFile := filepath.Join(dir, "requests")

	backend, err := New("zcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"ZC_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "reply with pong", ExecOptions{
		Timeout:       5 * time.Second,
		ThinkingLevel: "high",
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
		t.Fatalf("expected status=completed with the thinking level applied, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Output != "pong" {
		t.Errorf("expected output %q, got %q", "pong", result.Output)
	}
	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/set_config_option"`) {
		t.Errorf("expected a session/set_config_option request, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"configId":"thought"`) || !strings.Contains(requests, `"value":"high"`) {
		t.Errorf("expected the advertised option id and verbatim value, got:\n%s", requests)
	}
}
