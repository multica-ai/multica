package agent

import (
	"context"
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

func TestNewReturnsCodebuddyBackend(t *testing.T) {
	t.Parallel()
	b, err := New("codebuddy", Config{ExecutablePath: "/nonexistent/codebuddy"})
	if err != nil {
		t.Fatalf("New(codebuddy) error: %v", err)
	}
	if _, ok := b.(*codebuddyBackend); !ok {
		t.Fatalf("expected *codebuddyBackend, got %T", b)
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
		"antigravity", "claude", "codebuddy", "codex", "copilot", "cursor", "gemini",
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

func TestRunContextZeroTimeoutHasNoDeadline(t *testing.T) {
	t.Parallel()
	// A zero (or negative) timeout must NOT impose a wall-clock deadline:
	// liveness is delegated to the daemon's inactivity watchdog so an actively
	// streaming long-running session is never killed merely for running long
	// (MUL-3064).
	for _, d := range []time.Duration{0, -time.Second} {
		ctx, cancel := runContext(context.Background(), d)
		if _, ok := ctx.Deadline(); ok {
			cancel()
			t.Fatalf("runContext(%s) imposed a deadline; want none", d)
		}
		cancel()
		if ctx.Err() == nil {
			t.Fatalf("runContext(%s): context should be cancelled after cancel()", d)
		}
	}
}

func TestRunContextPositiveTimeoutHasDeadline(t *testing.T) {
	t.Parallel()
	// A positive timeout keeps the hard wall-clock deadline (the opt-in
	// absolute cap operators can still set via MULTICA_AGENT_TIMEOUT).
	ctx, cancel := runContext(context.Background(), time.Hour)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("runContext(1h) should impose a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > time.Hour+time.Minute {
		t.Fatalf("unexpected deadline remaining: %s", remaining)
	}
}
