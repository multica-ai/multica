package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsKimiBackend(t *testing.T) {
	t.Parallel()
	b, err := New("kimi", Config{ExecutablePath: "/nonexistent/kimi"})
	if err != nil {
		t.Fatalf("New(kimi) error: %v", err)
	}
	if _, ok := b.(*kimiBackend); !ok {
		t.Fatalf("expected *kimiBackend, got %T", b)
	}
}

func TestKimiToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
	}{
		{"Read file: /tmp/foo.go", "read_file"},
		{"read", "read_file"},
		{"Write: /tmp/bar.go", "write_file"},
		{"Edit", "edit_file"},
		{"Patch: /tmp/x", "edit_file"},
		{"Shell: ls -la", "terminal"},
		{"Bash", "terminal"},
		{"Run command: pwd", "terminal"},
		{"Search: foo", "search_files"},
		{"Glob: *.go", "glob"},
		{"Web search: golang acp", "web_search"},
		{"Fetch: https://example.com", "web_fetch"},
		{"Todo Write", "todo_write"},
		// Fallback: snake_case the title.
		{"Custom Thing", "custom_thing"},
		// Empty input returns empty — caller decides how to react.
		{"", ""},
	}
	for _, tt := range tests {
		got := kimiToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("kimiToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

// fakeKimiACPScript returns a POSIX-sh script that impersonates
// `kimi acp` for a single short ACP session: it acks initialize /
// session/new and then replies to session/set_model with a JSON-RPC
// error — the scenario the kimiBackend must propagate as a failed
// task rather than silently falling back to the default model.
func fakeKimiACPScript() string {
	return `#!/bin/sh
# Fake ` + "`kimi`" + ` binary — used by TestKimiBackendSetModelFailureFailsTask
# and TestKimiBackendPassesYoloFlag.
#
# Writes the full argv (one arg per line) to $KIMI_ARGS_FILE if that env
# var is set, so tests can assert that the daemon invokes us with the
# right flags (` + "`--yolo acp`" + `, not bare ` + "`acp`" + `).
#
# Then reads one JSON-RPC request per line from stdin, matches on the
# method name, and writes back a canned response. Exits after set_model
# so the kimiBackend cleanup path can run.
if [ -n "$KIMI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$KIMI_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendSetModelFailureFailsTask pins the "don't silently
// fall back" behaviour that landed in this PR: when kimi rejects the
// caller-selected model via session/set_model, the task result must
// report status=failed with a message that names the model and the
// upstream error — not claim success while actually running on the
// default model.
func TestKimiBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Drain message stream so the lifecycle goroutine can progress.
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "model not available") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
		if result.SessionID != "ses_fake" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// fakeKimiACPStaleResumeSetModelScript impersonates kimi-cli when a
// resumed session is gone and the caller picked a model:
// session/resume echoes the requested sessionId back, then
// session/set_model rejects the unknown session the way kimi-cli
// actually does — RequestError.invalid_params (-32602) with
// {"session_id": "Session not found"} in data
// (src/kimi_cli/acp/server.py, set_session_model).
// fakeKimiACPPromptScript is a fake `kimi` binary that completes a full
// ACP turn: initialize, session/new, session/prompt with a streamed
// message chunk, then the prompt response. When $KIMI_LATE_CHUNK is set
// it emits one more chunk shortly AFTER the session/prompt response, the
// way a real ACP agent can, so tests can prove the backend drains
// trailing notifications instead of cutting the process off at the
// response boundary.
func fakeKimiACPPromptScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      if [ -n "$KIMI_LATE_CHUNK" ]; then
        sleep 0.05
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_fake","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":" tail"}}}}\n'
      fi
      exit 0
      ;;
  esac
done
`
}

func fakeKimiACPStaleResumeSetModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"Invalid params","data":{"session_id":"Session not found"}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendClearsSessionIDWhenSetModelSessionNotFound pins the
// set_model sibling of the resumed-session fix: with a model override,
// session/set_model runs before session/prompt, so a dead resumed
// session surfaces there. The Result must carry an empty SessionID so
// the daemon's fresh-session retry (gated on SessionID == "") fires.
func TestKimiBackendClearsSessionIDWhenSetModelSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPStaleResumeSetModelScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
		Model:           "kimi-for-coding",
	})
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
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, `could not switch to model "kimi-for-coding"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestKimiBackendInvokesACPSubcommand pins the argv for `kimi`. An
// earlier fix tried passing `--yolo` to bypass per-tool approval
// prompts, but the `acp` subcommand in kimi-cli takes no options
// (see cli/__init__.py @cli.command def acp()), so `--yolo` was a
// no-op and the daemon still hung for 5 min on the first Shell call.
// The actual bypass is in hermesClient.handleAgentRequest, which
// auto-approves session/request_permission. This test catches
// accidental re-introduction of the dead flag.
func TestKimiBackendInvokesACPSubcommand(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPScript()))

	backend, err := New("kimi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIMI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set Model so the fake binary exits on set_model and we don't
	// have to wait for the prompt branch. We only care about argv here.
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
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
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 arg (acp), got %d: %q", len(lines), lines)
	}
	if lines[0] != "acp" {
		t.Errorf("expected first arg to be acp, got %q (full: %q)", lines[0], lines)
	}
	for _, l := range lines {
		switch l {
		case "--yolo", "--auto-approve", "--yes", "-y":
			t.Errorf("kimi acp doesn't accept %q; auto-approval is handled in hermesClient.handleAgentRequest", l)
		}
	}
}

// TestKimiResumeIncludesMcpServers pins the same contract as the matching
// Hermes test: session/resume must carry the managed MCP set so a resumed
// Kimi task has the same MCP tools as a fresh one.
func TestKimiResumeIncludesMcpServers(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_resume", `{}`)))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_resume",
		McpConfig:       json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`),
	})
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

	frame := findRecordedFrame(t, recordPath, "session/resume")
	params := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/resume.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 || servers[0].(map[string]any)["name"] != "fetch" {
		t.Fatalf("session/resume.mcpServers: got %v, want one entry named fetch", servers)
	}
}

// TestKimiDrainsNotificationsAfterPromptResponse pins the trailing-notification
// drain. kimi ACP can emit a final session update just after the
// session/prompt response returns; closing stdin and cancelling the context at
// that boundary raced the stdout reader and silently truncated the last chunk.
// The same defect was fixed for the sibling ACP backends in #5440 (grok) and
// #5675 (hermes).
func TestKimiDrainsNotificationsAfterPromptResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPPromptScript()))

	backend, err := New("kimi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIMI_LATE_CHUNK": "1"},
	})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
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

// fakeKimiACPResumeHistoryReplayScript impersonates kimi acp during session
// resume: after session/resume responds it emits a history-replay notification
// (the way a real Kimi CLI replays prior turns when loadSession:true). Then on
// session/prompt it emits a new chunk with the actual answer. The gate must
// ensure the history chunk never reaches output.
func fakeKimiACPResumeHistoryReplayScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      # Emit history replay BEFORE responding — this is what real Kimi does.
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_resumed","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"old history from previous turn"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_resumed"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_resumed","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"fresh answer"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKimiBackendDropsHistoryReplayOnResume pins the streaming gate: when a
// Kimi session is resumed, the runtime may replay prior-turn transcripts as
// ACP notifications before the client sends session/prompt. Without the
// streamingCurrentTurn gate those replayed messages would be appended to the
// message channel and contaminate Result.Output with the previous answer.
func TestKimiBackendDropsHistoryReplayOnResume(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPResumeHistoryReplayScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "do something new", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_resumed",
	})
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
	if result.SessionID != "ses_resumed" {
		t.Errorf("expected session id ses_resumed, got %q", result.SessionID)
	}
	// The gate must block the history replay chunk.
	if strings.Contains(result.Output, "old history") {
		t.Fatalf("history replay leaked into Result.Output: %q", result.Output)
	}
	// The current-turn answer must pass through.
	if !strings.Contains(result.Output, "fresh answer") {
		t.Fatalf("expected current-turn output to contain 'fresh answer', got %q", result.Output)
	}
	// Also verify the message channel didn't receive the history chunk.
	for _, m := range messages {
		if m.Type == MessageText && strings.Contains(m.Content, "old history") {
			t.Fatalf("history replay leaked into message stream: %+v", m)
		}
	}
}

// kimiScanTotal folds a per-model scan result into one total, for assertions
// that do not care about model attribution.
func kimiScanTotal(scan kimiUsageScan) TokenUsage {
	var total TokenUsage
	for _, u := range scanKimiSessionUsage(scan) {
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheReadTokens += u.CacheReadTokens
		total.CacheWriteTokens += u.CacheWriteTokens
	}
	return total
}

// kimiScanSingle asserts the scan produced exactly one model bucket and
// returns it with its model name.
func kimiScanSingle(tb testing.TB, scan kimiUsageScan) (TokenUsage, string) {
	tb.Helper()
	byModel := scanKimiSessionUsage(scan)
	if len(byModel) != 1 {
		tb.Fatalf("expected exactly one model bucket, got %v", byModel)
	}
	for model, u := range byModel {
		return u, model
	}
	return TokenUsage{}, ""
}

// kimiWireRecordLine is a verbatim `usage.record` line as kimi-code 0.33.0
// writes it, captured from a real session.
func kimiWireRecordLine(inputOther, output, cacheRead, cacheCreate int64) string {
	return kimiWireRecordLineAt(time.Now(), inputOther, output, cacheRead, cacheCreate)
}

// kimiWireRecordLineAt stamps the record, so resume tests can place records on
// either side of a turn boundary.
func kimiWireRecordLineAt(at time.Time, inputOther, output, cacheRead, cacheCreate int64) string {
	return kimiWireRecordLineFor("moonshot-cn/kimi-k2.7-code", at, inputOther, output, cacheRead, cacheCreate)
}

// kimiWireRecordLineFor names the model on the record, the way kimi does.
func kimiWireRecordLineFor(model string, at time.Time, inputOther, output, cacheRead, cacheCreate int64) string {
	return fmt.Sprintf(`{"type":"usage.record","model":%q,"usage":{"inputOther":%d,"output":%d,"inputCacheRead":%d,"inputCacheCreation":%d},"usageScope":"turn","time":%d}`,
		model, inputOther, output, cacheRead, cacheCreate, at.UnixMilli())
}

// kimiWireStepEndLine is the `step.end` loop event kimi writes alongside the
// usage record, repeating the same counters. Summing both double-bills.
func kimiWireStepEndLine(inputOther, output, cacheRead, cacheCreate int64) string {
	return fmt.Sprintf(`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"a3799b31","turnId":"0","step":1,"finishReason":"end_turn","usage":{"inputOther":%d,"output":%d,"inputCacheRead":%d,"inputCacheCreation":%d}},"time":1785997224849}`,
		inputOther, output, cacheRead, cacheCreate)
}

// writeKimiWireLog lays out a wire log at the path kimi uses:
// <root>/sessions/<workspace>/<sessionID>/agents/<agent>/wire.jsonl
func writeKimiWireLog(tb testing.TB, kimiHome, sessionID, agentName string, lines ...string) string {
	tb.Helper()
	dir := filepath.Join(kimiHome, "sessions", "wd_test_abc123", sessionID, "agents", agentName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatalf("mkdir wire log dir: %v", err)
	}
	path := filepath.Join(dir, "wire.jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		tb.Fatalf("write wire log: %v", err)
	}
	return path
}

// TestScanKimiSessionUsageSumsRecordsIgnoringStepEnd is the core guard for
// MUL-5773: kimi logs each LLM call's usage twice — once as `usage.record`
// and once inside the `step.end` loop event. Counting both doubles every
// number on the dashboard.
func TestScanKimiSessionUsageSumsRecordsIgnoringStepEnd(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	// A real two-step turn: the second call's uncached input collapses as
	// the prefix moves into cache.
	writeKimiWireLog(t, home, "session_abc", "main",
		`{"type":"metadata","protocol_version":"1"}`,
		kimiWireRecordLine(4883, 200, 17664, 0),
		kimiWireStepEndLine(4883, 200, 17664, 0),
		kimiWireRecordLine(244, 8, 22528, 0),
	)

	usage, model := kimiScanSingle(t, kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_abc", fallbackModel: "unknown"})

	if usage.InputTokens != 5127 {
		t.Errorf("InputTokens = %d, want 5127 (4883+244)", usage.InputTokens)
	}
	if usage.OutputTokens != 208 {
		t.Errorf("OutputTokens = %d, want 208 (200+8)", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 40192 {
		t.Errorf("CacheReadTokens = %d, want 40192 (17664+22528)", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 0 {
		t.Errorf("CacheWriteTokens = %d, want 0", usage.CacheWriteTokens)
	}
	if model != "moonshot-cn/kimi-k2.7-code" {
		t.Errorf("model = %q, want moonshot-cn/kimi-k2.7-code", model)
	}
}

// TestScanKimiSessionUsageMapsCacheCreation pins inputCacheCreation onto the
// cache-write bucket, the half of #6448 that stayed unfixed.
func TestScanKimiSessionUsageMapsCacheCreation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeKimiWireLog(t, home, "session_w", "main", kimiWireRecordLine(100, 20, 300, 400))

	usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_w", fallbackModel: "unknown"})
	if usage.CacheWriteTokens != 400 {
		t.Errorf("CacheWriteTokens = %d, want 400", usage.CacheWriteTokens)
	}
	if usage.CacheReadTokens != 300 {
		t.Errorf("CacheReadTokens = %d, want 300", usage.CacheReadTokens)
	}
}

// TestScanKimiSessionUsageBindsToSessionID: a concurrent kimi session must
// never be billed to this task.
func TestScanKimiSessionUsageBindsToSessionID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeKimiWireLog(t, home, "session_mine", "main", kimiWireRecordLine(10, 1, 0, 0))
	writeKimiWireLog(t, home, "session_theirs", "main", kimiWireRecordLine(9999, 9999, 9999, 9999))

	usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_mine", fallbackModel: "unknown"})
	if usage.InputTokens != 10 || usage.OutputTokens != 1 {
		t.Fatalf("billed the wrong session: %+v", usage)
	}
}

// TestScanKimiSessionUsageCountsSubagents: kimi's Agent tool gives each
// subagent its own wire log; delegated work must not vanish from the bill.
func TestScanKimiSessionUsageCountsSubagents(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeKimiWireLog(t, home, "session_s", "main", kimiWireRecordLine(100, 10, 0, 0))
	writeKimiWireLog(t, home, "session_s", "subagent-1", kimiWireRecordLine(50, 5, 0, 0))

	usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_s", fallbackModel: "unknown"})
	if usage.InputTokens != 150 || usage.OutputTokens != 15 {
		t.Fatalf("subagent usage not counted: %+v", usage)
	}
}

// TestScanKimiSessionUsageSkipsLogsUntouchedSinceStart: on a resumed session
// the previous run's log is still on disk and was already billed.
func TestScanKimiSessionUsageSkipsLogsUntouchedSinceStart(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeKimiWireLog(t, home, "session_old", "main", kimiWireRecordLine(500, 50, 0, 0))
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_old", resumed: true, fallbackModel: "unknown"})
	if acpTokenUsagePresent(usage) {
		t.Fatalf("re-billed a log from before this turn: %+v", usage)
	}
}

// TestScanKimiSessionUsageSkipsMalformedLines: the log is appended to live, so
// the tail can be a partial write. Partial data beats no data.
func TestScanKimiSessionUsageSkipsMalformedLines(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeKimiWireLog(t, home, "session_m", "main",
		kimiWireRecordLine(100, 10, 200, 0),
		`{"type":"usage.record","model":"x","usage":{"inputOther":7`,
	)

	usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_m", fallbackModel: "unknown"})
	if usage.InputTokens != 100 || usage.CacheReadTokens != 200 {
		t.Fatalf("complete records dropped because of a truncated tail: %+v", usage)
	}
}

// fakeKimiACPNoUsageScript reproduces kimi-code 0.33.0 exactly: the
// session/prompt result carries only stopReason — no `usage` — while the real
// counters go to the session wire log.
func fakeKimiACPNoUsageScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"session_e2e"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      dir="$KIMI_CODE_HOME/sessions/wd_test_abc123/session_e2e/agents/main"
      mkdir -p "$dir"
      # The scan drops records stamped before the turn began, so the test
      # injects the timestamp. ` + "`date +%s`" + ` would truncate to the start of
      # the current second and land before a sub-second startTime.
      printf '{"type":"usage.record","model":"moonshot-cn/kimi-k2.7-code","usage":{"inputOther":7694,"output":21,"inputCacheRead":14848,"inputCacheCreation":0},"usageScope":"turn","time":%s}\n' "$KIMI_TEST_RECORD_TIME" >> "$dir/wire.jsonl"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeKimiACPPromptUsageScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"session_prompt_usage"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"session_prompt_usage","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":17,"outputTokens":5,"cachedReadTokens":3,"cachedWriteTokens":2,"costUsdTicks":900}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestKimiBackendPropagatesACPPromptUsage(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPPromptUsageScript()))

	backend, err := New("kimi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
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
		t.Fatalf("status=%q error=%q", result.Status, result.Error)
	}
	want := TokenUsage{InputTokens: 17, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 2, CostUSDTicks: 900}
	if usage := result.Usage["unknown"]; usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

// TestKimiBackendReportsUsageFromWireLog is the end-to-end proof for
// MUL-5773 / #6448: with an ACP peer that reports no usage at all — which is
// what kimi-code 0.33.0 actually does — the task still lands on the dashboard
// with its real token split, keyed by the model the wire log names.
func TestKimiBackendReportsUsageFromWireLog(t *testing.T) {
	t.Parallel()

	kimiHome := t.TempDir()
	fakePath := filepath.Join(t.TempDir(), "kimi")
	writeTestExecutable(t, fakePath, []byte(fakeKimiACPNoUsageScript()))

	backend, err := New("kimi", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"KIMI_CODE_HOME": kimiHome,
			// Comfortably after the turn's startTime, so the assertion
			// tests the scan rather than clock granularity.
			"KIMI_TEST_RECORD_TIME": strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10),
		},
	})
	if err != nil {
		t.Fatalf("new kimi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status=%q error=%q", result.Status, result.Error)
	}

	usage, ok := result.Usage["moonshot-cn/kimi-k2.7-code"]
	if !ok {
		t.Fatalf("expected usage keyed by the wire log's model, got %v", result.Usage)
	}
	if usage.InputTokens != 7694 {
		t.Errorf("InputTokens = %d, want 7694", usage.InputTokens)
	}
	if usage.OutputTokens != 21 {
		t.Errorf("OutputTokens = %d, want 21", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 14848 {
		t.Errorf("CacheReadTokens = %d, want 14848", usage.CacheReadTokens)
	}
}

// TestScanKimiSessionUsageSkipsPriorRunOnResume is the double-billing guard:
// kimi appends to the same wire log when a session is resumed, so the file
// still holds records the earlier task already reported. Only records stamped
// after this turn began may be billed.
func TestScanKimiSessionUsageSkipsPriorRunOnResume(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	turnStart := time.Now().Truncate(time.Second).Add(500 * time.Millisecond)
	path := writeKimiWireLog(t, home, "session_r", "main",
		kimiWireRecordLineAt(turnStart.Add(-time.Hour), 5000, 500, 9000, 0), // previous task
		kimiWireRecordLineAt(turnStart.Add(time.Second), 120, 12, 340, 0),   // this task
	)
	// Simulate a filesystem that stores mtimes at one-second precision: the
	// log was touched during this turn, but its rounded mtime precedes the
	// sub-second turn boundary.
	coarseMtime := turnStart.Truncate(time.Second)
	if err := os.Chtimes(path, coarseMtime, coarseMtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	usage := kimiScanTotal(kimiUsageScan{startTime: turnStart, kimiHome: home, sessionID: "session_r", resumed: true, fallbackModel: "unknown"})

	if usage.InputTokens != 120 || usage.OutputTokens != 12 || usage.CacheReadTokens != 340 {
		t.Fatalf("re-billed the previous run on resume: %+v", usage)
	}
}

// TestScanKimiSessionUsageUntimedRecords pins the fallback for a hypothetical
// future format that drops the timestamp: bill it on a fresh session, drop it
// on a resume rather than risk charging twice.
func TestScanKimiSessionUsageUntimedRecords(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		resumed bool
		want    int64
	}{
		{name: "fresh session bills untimed records", resumed: false, want: 100},
		{name: "resumed session drops untimed records", resumed: true, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			writeKimiWireLog(t, home, "session_u", "main",
				`{"type":"usage.record","model":"m","usage":{"inputOther":100,"output":0,"inputCacheRead":0,"inputCacheCreation":0}}`)

			usage := kimiScanTotal(kimiUsageScan{startTime: time.Now().Add(-time.Minute), kimiHome: home, sessionID: "session_u", resumed: tc.resumed, fallbackModel: "unknown"})
			if usage.InputTokens != tc.want {
				t.Errorf("InputTokens = %d, want %d", usage.InputTokens, tc.want)
			}
		})
	}
}

// TestScanKimiSessionUsageSplitsByModel: a subagent may run a different model
// from the main agent. Folding both into one bucket prices one of them wrong —
// cache-read alone spans a 10x range across kimi models.
func TestScanKimiSessionUsageSplitsByModel(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	now := time.Now()
	writeKimiWireLog(t, home, "session_mm", "main",
		kimiWireRecordLineFor("moonshot-cn/kimi-k3", now, 1000, 100, 2000, 0))
	writeKimiWireLog(t, home, "session_mm", "subagent-1",
		kimiWireRecordLineFor("moonshot-cn/kimi-k2.5", now, 30, 3, 40, 0))

	byModel := scanKimiSessionUsage(kimiUsageScan{
		startTime: now.Add(-time.Minute), kimiHome: home,
		sessionID: "session_mm", fallbackModel: "unknown",
	})

	if len(byModel) != 2 {
		t.Fatalf("expected one bucket per model, got %v", byModel)
	}
	if got := byModel["moonshot-cn/kimi-k3"]; got.InputTokens != 1000 || got.CacheReadTokens != 2000 {
		t.Errorf("k3 bucket = %+v", got)
	}
	if got := byModel["moonshot-cn/kimi-k2.5"]; got.InputTokens != 30 || got.CacheReadTokens != 40 {
		t.Errorf("k2.5 bucket = %+v", got)
	}
}

// TestScanKimiSessionUsageFallbackModel: a record with no model of its own is
// billed to the model the task asked for, not dropped.
func TestScanKimiSessionUsageFallbackModel(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeKimiWireLog(t, home, "session_f", "main",
		fmt.Sprintf(`{"type":"usage.record","usage":{"inputOther":42,"output":1,"inputCacheRead":0,"inputCacheCreation":0},"time":%d}`,
			time.Now().UnixMilli()))

	byModel := scanKimiSessionUsage(kimiUsageScan{
		startTime: time.Now().Add(-time.Minute), kimiHome: home,
		sessionID: "session_f", fallbackModel: "picked-model",
	})
	if got := byModel["picked-model"]; got.InputTokens != 42 {
		t.Fatalf("expected the fallback model to be billed, got %v", byModel)
	}
}

// TestKimiSessionRootPrefersConfiguredThenEnv pins the discovery order.
// The kimi subprocess inherits os.Environ() via buildEnv, so a KIMI_CODE_HOME
// exported to the daemon relocates kimi's sessions without ever reaching
// cfg.Env — reading only cfg.Env would scan the wrong directory.
func TestKimiSessionRootPrefersConfiguredThenEnv(t *testing.T) {
	configured := t.TempDir()
	fromEnv := t.TempDir()
	for _, dir := range []string{configured, fromEnv} {
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	t.Setenv("KIMI_CODE_HOME", fromEnv)

	if got, want := kimiSessionRoot(configured), filepath.Join(configured, "sessions"); got != want {
		t.Errorf("explicit config should win: got %q, want %q", got, want)
	}
	if got, want := kimiSessionRoot(""), filepath.Join(fromEnv, "sessions"); got != want {
		t.Errorf("ambient KIMI_CODE_HOME should be used: got %q, want %q", got, want)
	}
}

// TestKimiSessionWireLogsRejectsTraversal: the session id comes from the
// agent and is joined into a path.
func TestKimiSessionWireLogsRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, id := range []string{"", ".", "..", "../escape", "a/b"} {
		if got := kimiSessionWireLogs(root, id); got != nil {
			t.Errorf("sessionID %q should be rejected, got %v", id, got)
		}
	}
}
