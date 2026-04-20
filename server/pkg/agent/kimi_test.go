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
# Fake ` + "`kimi acp`" + ` — used by TestKimiBackendSetModelFailureFailsTask.
# Reads one JSON-RPC request per line from stdin, matches on method name,
# and writes back a canned response. Exits after set_model so the
# kimiBackend cleanup path can run.
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
	if err := os.WriteFile(fakePath, []byte(fakeKimiACPScript()), 0o755); err != nil {
		t.Fatalf("write fake kimi: %v", err)
	}

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
