package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsDevinBackend(t *testing.T) {
	t.Parallel()
	b, err := New("devin", Config{ExecutablePath: "/nonexistent/devin"})
	if err != nil {
		t.Fatalf("New(devin) error: %v", err)
	}
	if _, ok := b.(*devinBackend); !ok {
		t.Fatalf("expected *devinBackend, got %T", b)
	}
}

func TestDevinToolNameFromTitle(t *testing.T) {
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
		got := devinToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("devinToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func fakeDevinACPScript() string {
	return `#!/bin/sh
if [ -n "$DEVIN_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEVIN_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$DEVIN_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$DEVIN_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_new","models":{"currentModelId":"opus","availableModels":[{"modelId":"opus","name":"opus"}]}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"history should be ignored"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"UsageUpdate","usage":{"inputTokens":1000,"outputTokens":1000,"cachedReadTokens":100}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"ToolCall","toolCallId":"tc-current","name":"Shell","status":"pending","parameters":{"command":"echo replay"}}}}\n'
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
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"ToolCallUpdate","toolCallId":"tc-current","status":"completed","name":"Shell","parameters":{"command":"echo current"},"output":"current tool output\\n"}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_loaded","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"loaded"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":1}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestDevinBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPScript()))

	backend, err := New("devin", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
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

func fakeDevinACPGoalCompleteCloseErrorScript(goalStatus string) string {
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
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Internal error","data":"Devin failed to generate a response"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestDevinBackendTreatsGoalCompleteCloseErrorAsCompleted(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "devin")
	writeTestExecutable(t, fakePath, []byte(fakeDevinACPGoalCompleteCloseErrorScript("completed")))

	backend, err := New("devin", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new devin backend: %v", err)
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

func TestDevinIssueCommentAddCommand(t *testing.T) {
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
		if got := isDevinIssueCommentAddCommand(tt.command); got != tt.want {
			t.Errorf("isDevinIssueCommentAddCommand(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}
