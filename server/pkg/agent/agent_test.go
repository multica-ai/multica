package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

func TestNewReturnsQoderBackend(t *testing.T) {
	t.Parallel()
	b, err := New("qoder", Config{ExecutablePath: "/nonexistent/qodercli"})
	if err != nil {
		t.Fatalf("New(qoder) error: %v", err)
	}
	if _, ok := b.(*qoderBackend); !ok {
		t.Fatalf("expected *qoderBackend, got %T", b)
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

// TestDetectVersionTimesOutOnHang guards MUL-3812: a CLI whose `--version`
// never returns (e.g. a brew-installed claude wedged by a bun regression) must
// not stall version detection forever. The daemon detects every runtime's
// version sequentially inside its blocking preflight, so an unbounded probe
// would leave the daemon stuck "starting" and *every* runtime on the host
// disconnected. detectCLIVersion must bound the probe and return an error so
// the registration loop isolates the broken runtime and the rest still
// register. The script also leaves an orphaned child holding the stdout pipe
// open after the parent is killed, exercising the cmd.WaitDelay path.
func TestDetectVersionTimesOutOnHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on a /bin/sh hang script")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	pidFile := filepath.Join(dir, "child.pid")
	// The CLI hangs forever (`wait`) and backgrounds a child that inherits and
	// holds our stdout pipe open even after the parent is killed on timeout —
	// the exact case cmd.WaitDelay must cover. The child records its PID so we
	// can reap it in Cleanup instead of leaking a 60s `sleep` into CI.
	body := fmt.Sprintf("#!/bin/sh\nsleep 60 &\necho $! > %q\nwait\n", pidFile)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return // child never recorded its PID; nothing to reap
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return
		}
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	})

	orig := detectVersionTimeout
	detectVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { detectVersionTimeout = orig })

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := DetectVersion(context.Background(), script)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a hanging --version probe, got nil")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("detection took %v; expected it to be bounded by the timeout", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DetectVersion did not return: version probe is unbounded (regression of MUL-3812)")
	}
}

func TestLaunchHeaderCoversAllSupportedBackends(t *testing.T) {
	t.Parallel()

	// The factory in New() enumerates every supported agent type; LaunchHeader
	// must stay in sync so the UI preview never shows an empty skeleton for a
	// runtime the daemon actually spawns. If a new backend is added, add an
	// entry to launchHeaders in agent.go and extend this list.
	supported := []string{
		"antigravity", "claude", "codebuddy", "codex", "copilot", "cursor",
		"hermes", "kimi", "kiro", "openclaw", "opencode", "pi", "qoder", "traecli", "grok",
	}
	for _, t_ := range supported {
		if header := LaunchHeader(t_); header == "" {
			t.Errorf("LaunchHeader(%q) returned empty string — add it to launchHeaders", t_)
		}
	}
}

func TestLaunchHeaderAntigravityAvoidsTextOnlyPrintModeLabel(t *testing.T) {
	t.Parallel()

	header := LaunchHeader("antigravity")
	if header != "agy -p (non-interactive)" {
		t.Fatalf("unexpected Antigravity launch header: %q", header)
	}
	if strings.Contains(header, "print mode") {
		t.Fatalf("Antigravity launch header must not imply a text-only mode: %q", header)
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
