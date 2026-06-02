package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewReturnsClaudeBackend(t *testing.T) {
	t.Parallel()
	b, err := New("claude", Config{ExecutablePath: "/nonexistent/claude"})
	if err != nil {
		t.Fatalf("New(claude) error: %v", err)
	}
	if _, ok := b.(*claudeBackend); !ok {
		t.Fatalf("expected *claudeBackend, got %T", b)
	}
}

func TestNewReturnsCodexBackend(t *testing.T) {
	t.Parallel()
	b, err := New("codex", Config{ExecutablePath: "/nonexistent/codex"})
	if err != nil {
		t.Fatalf("New(codex) error: %v", err)
	}
	if _, ok := b.(*codexBackend); !ok {
		t.Fatalf("expected *codexBackend, got %T", b)
	}
}

func TestNewReturnsCopilotBackend(t *testing.T) {
	t.Parallel()
	b, err := New("copilot", Config{ExecutablePath: "/nonexistent/copilot"})
	if err != nil {
		t.Fatalf("New(copilot) error: %v", err)
	}
	if _, ok := b.(*copilotBackend); !ok {
		t.Fatalf("expected *copilotBackend, got %T", b)
	}
}

func TestNewReturnsAntigravityBackend(t *testing.T) {
	t.Parallel()
	b, err := New("antigravity", Config{ExecutablePath: "/nonexistent/agy"})
	if err != nil {
		t.Fatalf("New(antigravity) error: %v", err)
	}
	if _, ok := b.(*antigravityBackend); !ok {
		t.Fatalf("expected *antigravityBackend, got %T", b)
	}
}

func TestNewReturnsCustomBackendWhenInvocationConfigured(t *testing.T) {
	t.Parallel()
	b, err := New("king", Config{
		ExecutablePath: "/nonexistent/king",
		Custom: &CustomInvocation{
			Args: []string{"-p", "{{prompt}}"},
		},
	})
	if err != nil {
		t.Fatalf("New(custom) error: %v", err)
	}
	if _, ok := b.(*customBackend); !ok {
		t.Fatalf("expected *customBackend, got %T", b)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := New("gpt", Config{})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	t.Parallel()
	b, _ := New("claude", Config{})
	cb := b.(*claudeBackend)
	if cb.cfg.Logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestDetectVersionFailsForMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := DetectVersion(context.Background(), "/nonexistent/binary")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestLaunchHeaderCoversAllSupportedBackends(t *testing.T) {
	t.Parallel()

	// The factory in New() enumerates every supported agent type; LaunchHeader
	// must stay in sync so the UI preview never shows an empty skeleton for a
	// runtime the daemon actually spawns. If a new backend is added, add an
	// entry to launchHeaders in agent.go and extend this list.
	supported := []string{
		"antigravity", "claude", "codex", "copilot", "cursor", "gemini",
		"hermes", "kimi", "kiro", "openclaw", "opencode", "pi",
	}
	for _, t_ := range supported {
		if header := LaunchHeader(t_); header == "" {
			t.Errorf("LaunchHeader(%q) returned empty string — add it to launchHeaders", t_)
		}
	}
}

func TestLaunchHeaderReturnsEmptyForUnknownType(t *testing.T) {
	t.Parallel()
	if header := LaunchHeader("made-up-agent"); header != "" {
		t.Errorf("expected empty header for unknown type, got %q", header)
	}
}

func TestCustomBackendPassesPromptViaPlaceholderArg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "args.txt")
	fakePath := filepath.Join(dir, "king")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$KING_RECORD"
printf 'done: %s\n' "$2"
`))
	t.Setenv("KING_RECORD", recordPath)

	backend, err := New("king", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Custom: &CustomInvocation{
			Args: []string{"-p", "{{prompt}}"},
		},
	})
	if err != nil {
		t.Fatalf("New(custom): %v", err)
	}

	session, err := backend.Execute(context.Background(), "ship it", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result := <-session.Result

	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if strings.TrimSpace(result.Output) != "done: ship it" {
		t.Fatalf("output = %q", result.Output)
	}
	if got := strings.TrimSpace(readTestFile(t, recordPath)); got != "-p\nship it" {
		t.Fatalf("argv record = %q", got)
	}
}

func TestCustomBackendPassesPromptViaStdinWhenNoPlaceholder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "king")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
input="$(cat)"
printf 'stdin: %s\n' "$input"
`))

	backend, err := New("king", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Custom: &CustomInvocation{
			Args: []string{"run"},
		},
	})
	if err != nil {
		t.Fatalf("New(custom): %v", err)
	}

	session, err := backend.Execute(context.Background(), "read me", ExecOptions{Cwd: dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result := <-session.Result

	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if strings.TrimSpace(result.Output) != "stdin: read me" {
		t.Fatalf("output = %q", result.Output)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
