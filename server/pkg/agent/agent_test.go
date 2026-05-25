package agent

import (
	"context"
	"os/exec"
	"testing"
	"time"
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
		"claude", "codex", "copilot", "cursor", "gemini",
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

func TestWaitTimeoutReturnsWhenProcessExits(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	err := waitTimeout(cmd, 5*time.Second)
	if err != nil {
		t.Fatalf("expected nil error for successful exit, got %v", err)
	}
}

func TestWaitTimeoutKillsStuckProcess(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	err := waitTimeout(cmd, 100*time.Millisecond)
	elapsed := time.Since(start)
	if err != ErrProcessKilled {
		t.Fatalf("expected ErrProcessKilled after kill, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected fast kill, took %v", elapsed)
	}
}

func TestWaitTimeoutPropagatesNonZeroExit(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("false")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	err := waitTimeout(cmd, 5*time.Second)
	if err == nil {
		t.Fatal("expected non-nil error for non-zero exit")
	}
	if err == ErrProcessKilled {
		t.Fatal("expected exit error, not ErrProcessKilled")
	}
}
