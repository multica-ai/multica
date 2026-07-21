package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsKiroBackend(t *testing.T) {
	t.Parallel()
	b, err := New("kiro", Config{ExecutablePath: "/nonexistent/kiro-cli"})
	if err != nil {
		t.Fatalf("New(kiro) error: %v", err)
	}
	if _, ok := b.(*kiroBackend); !ok {
		t.Fatalf("expected *kiroBackend, got %T", b)
	}
}

func TestKiroToolNameFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
	}{
		{"Read file: /tmp/foo.go", "read_file"},
		{"Write: /tmp/bar.go", "write_file"},
		{"Patch: /tmp/x", "edit_file"},
		{"Shell: ls -la", "terminal"},
		{"Run command: pwd", "terminal"},
		{"grep", "search_files"},
		{"Glob: *.go", "glob"},
		{"Code", "code"},
		{"Todo List", "todo_write"},
		{"Custom Thing", "custom_thing"},
		{"", ""},
	}
	for _, tt := range tests {
		got := kiroToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("kiroToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func fakeKiroACPScript() string {
	return `#!/bin/sh
if [ -n "$KIRO_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$KIRO_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$KIRO_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$KIRO_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"currentModelId":"auto","availableModels":[{"modelId":"auto","name":"auto"}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"history should be ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"UsageUpdate","usage":{"inputTokens":1000,"outputTokens":1000,"cachedReadTokens":100}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"ToolCall","toolCallId":"tc-current","name":"Shell","status":"pending","parameters":{"command":"echo replay"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"session/resume should not be used for kiro"}}\n' "$id"
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
      case "$line" in
        *'"content":'*)
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/prompt must send content and prompt"}}\n' "$id"
          exit 0
          ;;
      esac
      case "$line" in
        *'"prompt":'*)
          ;;
        *)
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/prompt must send content and prompt"}}\n' "$id"
          exit 0
          ;;
      esac
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"ToolCallUpdate","toolCallId":"tc-current","status":"completed","name":"Shell","parameters":{"command":"echo current"},"output":"current tool output\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"loaded"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":1,"cacheReadTokens":7,"cacheWriteTokens":3}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestKiroBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
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
		if result.SessionID != "ses_new" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestKiroBackendAttributesUsageToCurrentModel(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
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
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}
	if _, ok := result.Usage["unknown"]; ok {
		t.Fatalf("usage should use Kiro current model, got unknown entry: %+v", result.Usage)
	}
	usage, ok := result.Usage["auto"]
	if !ok {
		t.Fatalf("expected usage under current model auto, got %+v", result.Usage)
	}
	if usage.InputTokens != 2 || usage.OutputTokens != 1 || usage.CacheReadTokens != 7 || usage.CacheWriteTokens != 3 {
		t.Fatalf("usage = %+v, want input=2 output=1 cache_read=7 cache_write=3", usage)
	}
}

func fakeKiroACPGoalCompleteCloseErrorScript(goalStatus string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_goal_done"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_goal_done","update":{"type":"ToolCall","toolCallId":"tc-goal","name":"goal_complete","status":"pending","parameters":{}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_goal_done","update":{"type":"ToolCallUpdate","toolCallId":"tc-goal","status":"` + goalStatus + `","name":"goal_complete","output":"ok"}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"Kiro failed to generate a response"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestKiroBackendTreatsGoalCompleteCloseErrorAsCompleted(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPGoalCompleteCloseErrorScript("completed")))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
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
		if result.Status != "completed" {
			t.Fatalf("expected status=completed after goal_complete close error, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Error != "" {
			t.Fatalf("expected close-handshake error to be suppressed, got %q", result.Error)
		}
		if result.SessionID != "ses_goal_done" {
			t.Fatalf("session id = %q, want ses_goal_done", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestKiroBackendDoesNotCompleteAfterFailedGoalComplete(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPGoalCompleteCloseErrorScript("failed")))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
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
			t.Fatalf("expected status=failed after failed goal_complete, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "Kiro failed to generate a response") {
			t.Fatalf("expected original prompt error to be preserved, got %q", result.Error)
		}
		if result.SessionID != "ses_goal_done" {
			t.Fatalf("session id = %q, want ses_goal_done", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func fakeKiroACPIssueCommentCloseErrorScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_comment_done"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_comment_done","update":{"type":"ToolCall","toolCallId":"tc-comment","name":"Shell","status":"pending","parameters":{"command":"multica issue comment add issue-1 --content-file ./reply.md"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_comment_done","update":{"type":"ToolCallUpdate","toolCallId":"tc-comment","status":"completed","name":"Shell","output":"created comment"}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"Kiro failed to generate a response"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestKiroBackendTreatsCommentAddCloseErrorAsCompleted(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPIssueCommentCloseErrorScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	select {
	case result, ok := <-session.Result:
		<-messagesDone
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed after issue comment close error, got %q (error=%q, messages=%+v)", result.Status, result.Error, messages)
		}
		if result.Error != "" {
			t.Fatalf("expected close-handshake error to be suppressed, got %q", result.Error)
		}
		if result.SessionID != "ses_comment_done" {
			t.Fatalf("session id = %q, want ses_comment_done", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestKiroIssueCommentAddCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		command string
		want    bool
	}{
		{"multica issue comment add issue-1 --content-file ./reply.md", true},
		{"./multica issue comment add issue-1 --content-file ./reply.md", true},
		{"/usr/local/bin/multica issue comment add issue-1 --content-file ./reply.md", true},
		{"MULTICA_TOKEN=x multica issue comment add issue-1 --content-file ./reply.md", true},
		{"FOO=1 BAR=2 ./multica issue comment add issue-1", true},
		{`sh -c "multica issue comment add issue-1 --content-file ./reply.md"`, true},
		{`bash -c 'multica issue comment add issue-1'`, true},
		{`/bin/sh -c "multica issue comment add issue-1"`, true},
		{"multica issue get issue-1", false},
		{"echo multica issue comment add issue-1", false},
		{`sh -c "echo multica issue comment add issue-1"`, false},
		{"FOO=bar", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isKiroIssueCommentAddCommand(tt.command); got != tt.want {
			t.Errorf("isKiroIssueCommentAddCommand(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

// TestKiroIssueCommentAddToolIgnoresToolTitle pins the #5509 decoupling:
// `multica issue comment add` must be recognized from its command payload
// regardless of the tool's normalized name. The GPT-5.6 Sol adapter can title
// the shell tool something that doesn't fold into "terminal", and the old
// msg.Tool=="terminal" gate silently dropped those.
func TestKiroIssueCommentAddToolIgnoresToolTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"terminal", "terminal", true},
		{"gpt-style execute", "execute_bash", true},
		{"gpt-style run", "run", true},
		{"empty tool name", "", true},
		{"tool name is irrelevant when command matches", "read_file", true},
	}
	for _, tt := range tests {
		msg := Message{
			Type:  MessageToolUse,
			Tool:  tt.tool,
			Input: map[string]any{"command": "multica issue comment add issue-1 --content-file ./reply.md"},
		}
		if got := isKiroIssueCommentAddTool(msg); got != tt.want {
			t.Errorf("%s: isKiroIssueCommentAddTool(tool=%q) = %v, want %v", tt.name, tt.tool, got, tt.want)
		}
	}

	// A non-comment-add command must still be rejected regardless of title.
	notComment := Message{Type: MessageToolUse, Tool: "terminal", Input: map[string]any{"command": "ls -la"}}
	if isKiroIssueCommentAddTool(notComment) {
		t.Errorf("isKiroIssueCommentAddTool should reject a non-comment-add command")
	}
}

// runKiroCloseErrorScript executes a fake-kiro script that ends in the -32603
// close handshake and returns the final Result.
func runKiroCloseErrorScript(t *testing.T, script string) Result {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
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
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
		return Result{}
	}
}

// closeErrorFrame is the -32603 "failed to generate a response" close
// handshake Kiro raises after the task has already finished its work.
const closeErrorFrame = `printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"Kiro failed to generate a response"}}\n' "$id"`

// fakeKiroACPRunningToolCloseErrorScript reproduces the GPT-5.6 Sol shape from
// #5509: the finishing tool is invoked (ToolCall notification) but the adapter
// never emits a completed/failed ToolCallUpdate — the tool stays parked at
// "running", so no ToolResult is ever produced. Kiro then raises the -32603
// close handshake. `toolName` picks a title that does NOT normalize to
// "terminal"; `command` selects goal_complete (empty) vs comment-add.
func fakeKiroACPRunningToolCloseErrorScript(toolName, command string) string {
	params := `{}`
	if command != "" {
		params = `{"command":"` + command + `"}`
	}
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_running"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_running","update":{"type":"ToolCall","toolCallId":"tc-running","name":"` + toolName + `","status":"pending","parameters":` + params + `}}}\n'
      ` + closeErrorFrame + `
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendDoesNotCompleteRunningCommentAddWithoutResult is the core
// safety property from the #5511 review (Must-fix #1): a finishing tool that
// was only ever seen invoked — no terminal ToolResult — is NOT proof the work
// succeeded (a mid-command crash produces the same shape). It must stay failed.
func TestKiroBackendDoesNotCompleteRunningCommentAddWithoutResult(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPRunningToolCloseErrorScript(
		"execute_bash",
		"multica issue comment add issue-1 --content-file ./reply.md",
	))
	if result.Status != "failed" {
		t.Fatalf("expected status=failed for a result-less running comment-add, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "Kiro failed to generate a response") {
		t.Fatalf("expected original prompt error to be preserved, got %q", result.Error)
	}
}

// TestKiroBackendDoesNotCompleteRunningGoalCompleteWithoutResult is the
// goal_complete sibling of the result-less safety property.
func TestKiroBackendDoesNotCompleteRunningGoalCompleteWithoutResult(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPRunningToolCloseErrorScript("goal_complete", ""))
	if result.Status != "failed" {
		t.Fatalf("expected status=failed for a result-less running goal_complete, got %q (error=%q)", result.Status, result.Error)
	}
}

// fakeKiroACPCompletedToolCloseErrorScript is the positive-proof path: the
// finishing tool emits a completed ToolCallUpdate (the real completion signal)
// before the -32603 close handshake. `toolName` deliberately uses a title that
// does NOT normalize to "terminal" to prove recognition is title-independent.
func fakeKiroACPCompletedToolCloseErrorScript(toolName, command string) string {
	params := `{}`
	if command != "" {
		params = `{"command":"` + command + `"}`
	}
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_completed"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_completed","update":{"type":"ToolCall","toolCallId":"tc-done","name":"` + toolName + `","status":"pending","parameters":` + params + `}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_completed","update":{"type":"ToolCallUpdate","toolCallId":"tc-done","status":"completed","name":"` + toolName + `","output":"ok"}}}\n'
      ` + closeErrorFrame + `
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendTreatsCompletedCommentAddWithNonTerminalTitleAsCompleted is
// the #5509 fix proper: a comment-add whose shell tool is titled with a
// non-terminal name but DID complete (positive proof) must be preserved as
// completed through the -32603 close handshake.
func TestKiroBackendTreatsCompletedCommentAddWithNonTerminalTitleAsCompleted(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPCompletedToolCloseErrorScript(
		"execute_bash",
		"multica issue comment add issue-1 --content-file ./reply.md",
	))
	if result.Status != "completed" {
		t.Fatalf("expected status=completed for a completed comment-add + close error, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Error != "" {
		t.Fatalf("expected close-handshake error to be suppressed, got %q", result.Error)
	}
}

// fakeKiroACPRealGPT56SolCloseErrorScript reproduces the EXACT ACP frame shape
// captured from a live kiro-cli 2.12.3 + gpt-5.6-sol session (see #5509 /
// MUL-4860). Three things about this shape broke the older guards:
//   - the shell tool_call title is "Running: <cmd>" with kind "execute", which
//     normalizes to "running" — NOT "terminal";
//   - the command lives in rawInput.command alongside __tool_use_purpose;
//   - the completed tool_call_update sends rawOutput as an OBJECT
//     ({"items":[{"Json":{...}}]}), which — when rawOutput was typed as a Go
//     string — made json.Unmarshal fail and drop the whole update, status and
//     all, so no completion signal ever reached the guard.
//
// Frames use the real wire form: method "session/update" + "sessionUpdate".
func fakeKiroACPRealGPT56SolCloseErrorScript(command string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_real","models":{"currentModelId":"gpt-5.6-sol"}}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_real","update":{"sessionUpdate":"tool_call","toolCallId":"call_x","title":"Running: ` + command + `","kind":"execute","rawInput":{"command":"` + command + `","__tool_use_purpose":"deliver the final result"},"_meta":{"kiro":{"toolName":"shell"}}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_real","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_x","content":[{"type":"content","content":{"type":"text","text":"created comment\\n"}}]}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_real","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_x","kind":"execute","status":"completed","title":"Running: ` + command + `","rawInput":{"command":"` + command + `"},"rawOutput":{"items":[{"Json":{"exit_status":"exit status: 0","stdout":"created comment\\n","stderr":""}}]}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_real","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"DONE"}}}}\n'
      ` + closeErrorFrame + `
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendPreservesCompletionOnRealGPT56SolFrames is the end-to-end
// #5509 regression against the real captured wire shape: a `multica issue
// comment add` run through GPT-5.6 Sol's shell tool (title "Running: ...",
// object rawOutput) that completes, then hits the -32603 close handshake, must
// stay completed. This fails without BOTH the object-rawOutput parse fix in
// hermes.go and the payload-based comment-add recognition in kiro.go.
func TestKiroBackendPreservesCompletionOnRealGPT56SolFrames(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPRealGPT56SolCloseErrorScript(
		"multica issue comment add issue-1 --content-file ./reply.md",
	))
	if result.Status != "completed" {
		t.Fatalf("expected status=completed for the real GPT-5.6 Sol frame shape, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Error != "" {
		t.Fatalf("expected close-handshake error to be suppressed, got %q", result.Error)
	}
}

// fakeKiroACPTwoCommentCloseErrorScript emits two comment-add tool calls with
// distinct CallIDs and configurable terminal statuses (in order), then the
// -32603 close handshake. This exercises ordering: only the most recent
// finishing-tool result should decide completion.
func fakeKiroACPTwoCommentCloseErrorScript(firstStatus, secondStatus string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_two"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_two","update":{"type":"ToolCall","toolCallId":"tc-1","name":"terminal","status":"pending","parameters":{"command":"multica issue comment add issue-1 --content-file ./progress.md"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_two","update":{"type":"ToolCallUpdate","toolCallId":"tc-1","status":"` + firstStatus + `","name":"terminal","output":"first"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_two","update":{"type":"ToolCall","toolCallId":"tc-2","name":"terminal","status":"pending","parameters":{"command":"multica issue comment add issue-1 --content-file ./final.md"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_two","update":{"type":"ToolCallUpdate","toolCallId":"tc-2","status":"` + secondStatus + `","name":"terminal","output":"second"}}}\n'
      ` + closeErrorFrame + `
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendFailedFinalCommentOverridesEarlierSuccess is the #5511 review
// Must-fix #2: "progress comment completed → final comment failed → -32603"
// must stay failed. An earlier success must not mask a later failure.
func TestKiroBackendFailedFinalCommentOverridesEarlierSuccess(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPTwoCommentCloseErrorScript("completed", "failed"))
	if result.Status != "failed" {
		t.Fatalf("expected status=failed when the final comment failed after an earlier success, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "Kiro failed to generate a response") {
		t.Fatalf("expected original prompt error to be preserved, got %q", result.Error)
	}
}

// TestKiroBackendCompletedRetryAfterEarlierFailureIsCompleted is the reverse
// ordering the review flagged: "first attempt failed → retry completed →
// -32603" is a real completion and must be preserved.
func TestKiroBackendCompletedRetryAfterEarlierFailureIsCompleted(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPTwoCommentCloseErrorScript("failed", "completed"))
	if result.Status != "completed" {
		t.Fatalf("expected status=completed when a retry completed after an earlier failure, got %q (error=%q)", result.Status, result.Error)
	}
	if result.Error != "" {
		t.Fatalf("expected close-handshake error to be suppressed, got %q", result.Error)
	}
}

// fakeKiroACPFailedCommentAddCloseErrorScript: the finishing tool emits a
// failed ToolResult before the close error — a genuine failure that must stay
// failed.
func fakeKiroACPFailedCommentAddCloseErrorScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_failed"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_failed","update":{"type":"ToolCall","toolCallId":"tc-fail","name":"execute_bash","status":"pending","parameters":{"command":"multica issue comment add issue-1 --content-file ./reply.md"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_failed","update":{"type":"ToolCallUpdate","toolCallId":"tc-fail","status":"failed","name":"execute_bash","output":"exit status 1"}}}\n'
      ` + closeErrorFrame + `
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendDoesNotCompleteAfterFailedCommentAddResult guards the case
// where the finishing tool explicitly failed: it must stay failed.
func TestKiroBackendDoesNotCompleteAfterFailedCommentAddResult(t *testing.T) {
	t.Parallel()

	result := runKiroCloseErrorScript(t, fakeKiroACPFailedCommentAddCloseErrorScript())
	if result.Status != "failed" {
		t.Fatalf("expected status=failed after a failed comment-add result, got %q (error=%q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "Kiro failed to generate a response") {
		t.Fatalf("expected original prompt error to be preserved, got %q", result.Error)
	}
}

// fakeKiroACPStaleLoadSetModelScript impersonates kiro when a resumed
// session is gone and the caller picked a model: session/load returns
// an empty result (so the requested id is kept), then
// session/set_model rejects the unknown session with kiro's observed
// wording — -32603 with "No session found with id ..." in data.
func fakeKiroACPStaleLoadSetModelScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"No session found with id ses_stale"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendClearsSessionIDWhenSetModelSessionNotFound pins the
// set_model sibling of the resumed-session fix: with a model override,
// session/set_model runs before session/prompt, so a dead resumed
// session surfaces there. The Result must carry an empty SessionID so
// the daemon's fresh-session retry (gated on SessionID == "") fires.
func TestKiroBackendClearsSessionIDWhenSetModelSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPStaleLoadSetModelScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
		Model:           "auto",
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
		if !strings.Contains(result.Error, `could not switch to model "auto"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// fakeKiroACPStalePromptScript impersonates kiro when a resumed session is
// gone and there is NO model override: session/load returns an empty result
// (so the requested id is kept), then session/prompt — not set_model — is the
// call that surfaces the dead session, with kiro's observed -32603 +
// "No session found with id ..." wording.
func fakeKiroACPStalePromptScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"No session found with id ses_stale"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestKiroBackendClearsSessionIDWhenPromptSessionNotFound pins the prompt-path
// sibling of the stale-session fix. Without a model override, session/prompt
// (not session/set_model) is where a dead resumed session surfaces. The result
// MUST be failed with an empty SessionID so the daemon's fresh-session retry
// (gated on SessionID == "") fires. A regression in the -32603 guard once
// turned this into status="completed" + a preserved stale SessionID, which
// both faked success and skipped the retry.
func TestKiroBackendClearsSessionIDWhenPromptSessionNotFound(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPStalePromptScript()))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_stale",
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
			t.Fatalf("expected status=failed for a stale resumed session at prompt time, got %q (error=%q)", result.Status, result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected empty session id so the daemon's fresh-session retry fires, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestKiroBackendInvokesACPWithTrustAllTools(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIRO_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "bogus-model",
		Timeout:    5 * time.Second,
		CustomArgs: []string{"acp", "--trust-tools", "shell", "-a", "--agent", "multica"},
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
	wantPrefix := []string{"acp", "--trust-all-tools"}
	if len(lines) < len(wantPrefix) {
		t.Fatalf("expected at least %d args, got %d: %q", len(wantPrefix), len(lines), lines)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Fatalf("arg[%d] = %q, want %q (full: %q)", i, lines[i], want, lines)
		}
	}
	for _, blocked := range []string{"--trust-tools", "shell", "-a"} {
		for _, got := range lines {
			if got == blocked {
				t.Errorf("protocol-critical custom arg %q was not filtered: %q", blocked, lines)
			}
		}
	}
	if strings.Join(lines, "\n") != strings.Join([]string{"acp", "--trust-all-tools", "--agent", "multica"}, "\n") {
		t.Errorf("unexpected argv after filtering: %q", lines)
	}
}

func TestKiroBackendUsesSessionLoadForResume(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	requestsFile := filepath.Join(tempDir, "requests.jsonl")
	fakePath := filepath.Join(tempDir, "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeKiroACPScript()))

	backend, err := New("kiro", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"KIRO_REQUESTS_FILE": requestsFile},
	})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
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
	var messages []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	result := <-session.Result
	<-messagesDone
	if result.Status != "completed" {
		t.Fatalf("expected completed result, got status=%q error=%q", result.Status, result.Error)
	}
	if result.Output != "loaded" {
		t.Fatalf("output = %q, want loaded", result.Output)
	}
	if usage := result.Usage["unknown"]; usage.InputTokens != 2 || usage.OutputTokens != 1 || usage.CacheReadTokens != 7 || usage.CacheWriteTokens != 3 {
		t.Fatalf("usage = %+v, want input=2 output=1 cache_read=7 cache_write=3", usage)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v, want current tool use, tool result, and text only", messages)
	}
	if messages[0].Type != MessageToolUse {
		t.Fatalf("messages[0].Type = %v, want MessageToolUse", messages[0].Type)
	}
	if messages[0].Tool != "terminal" {
		t.Fatalf("messages[0].Tool = %q, want terminal", messages[0].Tool)
	}
	if command, _ := messages[0].Input["command"].(string); command != "echo current" {
		t.Fatalf("messages[0].Input[command] = %q, want echo current", command)
	}
	if messages[1].Type != MessageToolResult {
		t.Fatalf("messages[1].Type = %v, want MessageToolResult", messages[1].Type)
	}
	if messages[1].Output != "current tool output\n" {
		t.Fatalf("messages[1].Output = %q, want current tool output", messages[1].Output)
	}
	if messages[2].Type != MessageText || messages[2].Content != "loaded" {
		t.Fatalf("messages[2] = %+v, want text loaded", messages[2])
	}
	if result.SessionID != "ses_existing" {
		t.Fatalf("session id = %q, want ses_existing", result.SessionID)
	}

	raw, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load request, got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("kiro backend must not call session/resume, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"mcpServers":[]`) {
		t.Fatalf("session/load must include mcpServers, got:\n%s", requests)
	}
	// Kiro docs use content, but Kiro CLI 2.1.1 still requires prompt.
	if !strings.Contains(requests, `"content":[`) {
		t.Fatalf("session/prompt must send Kiro content field, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"prompt":[`) {
		t.Fatalf("session/prompt must send standard ACP prompt field for Kiro 2.1.1 compatibility, got:\n%s", requests)
	}
}

// TestKiroLoadIncludesMcpServersFromConfig pins that the agent's managed
// MCP set actually reaches the wire on session/load — the resume path is
// otherwise indistinguishable from the no-config case, which is how the
// missing-on-resume bug got past the first round of review.
func TestKiroLoadIncludesMcpServersFromConfig(t *testing.T) {
	t.Parallel()

	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "kiro-cli")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(recordPath, "ses_load", `{}`)))

	backend, err := New("kiro", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new kiro backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_load",
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

	frame := findRecordedFrame(t, recordPath, "session/load")
	params := frame["params"].(map[string]any)
	servers, ok := params["mcpServers"].([]any)
	if !ok {
		t.Fatalf("session/load.mcpServers: got %T, want []any", params["mcpServers"])
	}
	if len(servers) != 1 || servers[0].(map[string]any)["name"] != "fetch" {
		t.Fatalf("session/load.mcpServers: got %v, want one entry named fetch", servers)
	}
}
