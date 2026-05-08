package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsDeepseekBackend(t *testing.T) {
	t.Parallel()
	b, err := New("deepseek", Config{ExecutablePath: "/nonexistent/deepseek"})
	if err != nil {
		t.Fatalf("New(deepseek) error: %v", err)
	}
	if _, ok := b.(*deepseekBackend); !ok {
		t.Fatalf("expected *deepseekBackend, got %T", b)
	}
}

func TestDeepseekToolNameFromTitle(t *testing.T) {
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
		got := deepseekToolNameFromTitle(tt.title)
		if got != tt.want {
			t.Errorf("deepseekToolNameFromTitle(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

// fakeDeepseekACPScript returns a POSIX-sh script that impersonates
// `deepseek app-server --stdio` for a single short ACP session: it
// acks initialize / session/new and then replies to session/set_model
// with a JSON-RPC error — the scenario the deepseekBackend must
// propagate as a failed task rather than silently falling back to the
// default model.
func fakeDeepseekACPScript() string {
	return `#!/bin/sh
# Fake ` + "`deepseek`" + ` binary — used by TestDeepseekBackendSetModelFailureFailsTask
# and TestDeepseekBackendInvokesAppServerStdio.
#
# Writes the full argv (one arg per line) to $DEEPSEEK_ARGS_FILE if that env
# var is set, so tests can assert that the daemon invokes us with the
# right flags.
#
# Then reads one JSON-RPC request per line from stdin, matches on the
# method name, and writes back a canned response. Exits after set_model
# so the deepseekBackend cleanup path can run.
if [ -n "$DEEPSEEK_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEEPSEEK_ARGS_FILE"
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

// TestDeepseekBackendSetModelFailureFailsTask pins the "don't silently
// fall back" behaviour: when deepseek rejects the caller-selected model
// via session/set_model, the task result must report status=failed
// with a message that names the model and the upstream error.
func TestDeepseekBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekACPScript()), 0o755); err != nil {
		t.Fatalf("write fake deepseek: %v", err)
	}

	backend, err := New("deepseek", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
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

// TestDeepseekBackendInvokesAppServerStdio pins the argv for
// `deepseek`. The daemon must pass `app-server --stdio` to launch
// the ACP JSON-RPC transport.
func TestDeepseekBackendInvokesAppServerStdio(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "deepseek")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekACPScript()), 0o755); err != nil {
		t.Fatalf("write fake deepseek: %v", err)
	}

	backend, err := New("deepseek", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEEPSEEK_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
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
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 args (app-server --stdio), got %d: %q", len(lines), lines)
	}
	if lines[0] != "app-server" {
		t.Errorf("expected first arg to be app-server, got %q (full: %q)", lines[0], lines)
	}
	if lines[1] != "--stdio" {
		t.Errorf("expected second arg to be --stdio, got %q (full: %q)", lines[1], lines)
	}
}
