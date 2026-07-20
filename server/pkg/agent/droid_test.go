package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildDroidArgs_Basic(t *testing.T) {
	t.Parallel()

	args := buildDroidArgs("do the thing", ExecOptions{
		Cwd:             "/tmp/work",
		Model:           "claude-opus-4-8",
		ThinkingLevel:   "high",
		SystemPrompt:    "You are an agent.",
		ResumeSessionID: "sess-123",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"exec",
		"--output-format stream-json",
		"--auto high",
		"--cwd /tmp/work",
		"--model claude-opus-4-8",
		"--reasoning-effort high",
		"--append-system-prompt You are an agent.",
		"--session-id sess-123",
		"do the thing",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected args to contain %q, got %v", want, args)
		}
	}
}

func TestBuildDroidArgs_BlocksUserOverrides(t *testing.T) {
	t.Parallel()

	args := buildDroidArgs("prompt", ExecOptions{
		CustomArgs: []string{"--output-format", "text", "--auto", "low", "--model", "other"},
	}, slog.Default())

	// Daemon-owned flags must win: exactly one --output-format stream-json,
	// one --auto high, and no user-supplied --model other.
	if strings.Count(strings.Join(args, " "), "--output-format text") != 0 {
		t.Fatalf("user --output-format text should be filtered: %v", args)
	}
	if strings.Count(strings.Join(args, " "), "--auto low") != 0 {
		t.Fatalf("user --auto low should be filtered: %v", args)
	}
	for i, a := range args {
		if a == "other" {
			t.Fatalf("user --model other leaked at args[%d]: %v", i, args)
		}
	}
}

func TestDroidProcessEvents_ParsesCompletion(t *testing.T) {
	t.Parallel()

	input := strings.NewReader("" +
		`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-4-8"}` + "\n" +
		`{"type":"message","role":"assistant","text":"Done.","session_id":"abc"}` + "\n" +
		`{"type":"completion","finalText":"Done.","session_id":"abc","usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":1,"cache_creation_input_tokens":0}}` + "\n",
	)

	msgCh := make(chan Message, 8)
	b := &droidBackend{cfg: Config{Logger: slog.Default()}}
	result := b.processEvents(input, msgCh)
	close(msgCh)

	if result.status != "completed" {
		t.Fatalf("status = %q, want completed", result.status)
	}
	if result.output != "Done." {
		t.Fatalf("output = %q, want Done.", result.output)
	}
	if result.sessionID != "abc" {
		t.Fatalf("sessionID = %q, want abc", result.sessionID)
	}
	if result.usage.InputTokens != 10 || result.usage.OutputTokens != 2 || result.usage.CacheReadTokens != 1 {
		t.Fatalf("unexpected usage: %+v", result.usage)
	}
}

func TestNewReturnsDroidBackend(t *testing.T) {
	t.Parallel()
	b, err := New("droid", Config{ExecutablePath: "/nonexistent/droid"})
	if err != nil {
		t.Fatalf("New(droid) error: %v", err)
	}
	if _, ok := b.(*droidBackend); !ok {
		t.Fatalf("expected *droidBackend, got %T", b)
	}
}

func TestListModelsDroid(t *testing.T) {
	t.Parallel()
	models, err := ListModels(context.Background(), "droid", "")
	if err != nil {
		t.Fatalf("ListModels(droid): %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected non-empty droid model catalog")
	}
	foundDefault := false
	for _, m := range models {
		if m.Default {
			foundDefault = true
		}
		if m.Thinking == nil || len(m.Thinking.SupportedLevels) == 0 {
			t.Fatalf("model %q missing thinking catalog", m.ID)
		}
	}
	if !foundDefault {
		t.Fatal("expected a default droid model")
	}
}

func TestDroidExecute_Success(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "droid")
	script := `#!/bin/sh
case "$1" in
  exec)
    printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-4-8"}'
    printf '%s\n' '{"type":"message","role":"assistant","text":"HELLO","session_id":"sess-1"}'
    printf '%s\n' '{"type":"completion","finalText":"HELLO","session_id":"sess-1","usage":{"input_tokens":3,"output_tokens":1}}'
    exit 0
    ;;
esac
exit 1
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("droid", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess, err := backend.Execute(context.Background(), "say hello", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res := <-sess.Result
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed; err=%q", res.Status, res.Error)
	}
	if res.Output != "HELLO" {
		t.Fatalf("output = %q, want HELLO", res.Output)
	}
	if res.SessionID != "sess-1" {
		t.Fatalf("sessionID = %q, want sess-1", res.SessionID)
	}
}
