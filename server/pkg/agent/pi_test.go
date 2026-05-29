package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildPiArgsNoToolAllowlist(t *testing.T) {
	// Extension tools registered via Pi's registerTool() must not be
	// filtered out by a hardcoded --tools allowlist. Omitting --tools
	// lets Pi use its full tool registry. See #2379.
	args := buildPiArgs("test prompt", "/tmp/session.jsonl", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildPiArgs emits --tools %q; should not restrict tool registry (see #2379)", args[i+1])
		}
	}
}

func TestBuildPiArgsBasicFlags(t *testing.T) {
	args := buildPiArgs("hello world", "/tmp/s.jsonl", ExecOptions{
		Model:        "anthropic/claude-sonnet-4-20250514",
		SystemPrompt: "be helpful",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--mode json", "--session /tmp/s.jsonl", "--provider anthropic", "--model claude-sonnet-4-20250514", "--append-system-prompt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	// Prompt must be the last positional argument.
	if args[len(args)-1] != "hello world" {
		t.Errorf("prompt should be last arg, got %q", args[len(args)-1])
	}
}

func TestBuildPiArgsCustomArgsAppended(t *testing.T) {
	// Users can still restrict tools via custom_args if desired.
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		CustomArgs: []string{"--tools", "read,bash"},
	}, slog.Default())

	found := false
	for i, arg := range args {
		if arg == "--tools" && i+1 < len(args) && args[i+1] == "read,bash" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom --tools should pass through via custom_args, got: %v", args)
	}
}

// TestPiExecuteAttachesStdinPipe verifies that the Pi backend spawns the
// child with an explicit stdin pipe (FIFO) instead of leaving cmd.Stdin
// nil. Without an explicit pipe, Pi has been observed to block under
// systemd waiting for stdin events (#2188); attaching and immediately
// closing a pipe delivers a clean EOF on a FIFO and unblocks Pi.
//
// The probe is structural rather than behavioral: a shell script in
// place of `pi` inspects /proc/self/fd/0 and only emits a valid event
// stream if stdin is a FIFO. If the fix regresses (stdin nil → /dev/null
// char device), the fake exits non-zero and the test fails.
func TestPiExecuteAttachesStdinPipe(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		// /proc/self/fd/0 is Linux-specific; skipping elsewhere keeps
		// the assertion portable without losing CI coverage.
		t.Skip("stdin fd inspection relies on /proc/self/fd/0")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := "#!/bin/sh\n" +
		"kind=$(stat -c '%F' -L /proc/self/fd/0 2>/dev/null || echo unknown)\n" +
		"case \"$kind\" in\n" +
		"  fifo|*pipe*)\n" +
		"    printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"    printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1,\"cacheRead\":0,\"cacheWrite\":0,\"totalTokens\":2}}}'\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		"printf 'stdin was %s; expected fifo\\n' \"$kind\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
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
		if result.Status != "completed" {
			t.Fatalf("expected status=completed (stdin attached as fifo), got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestStripPiToolCallMarkup(t *testing.T) {
	tests := map[string]string{
		`before call:bash{command:<|"|>cd repo/path && ls -F<|"|>}<tool_call|> after`:                           "before  after",
		`before call:read{path:<|"|>repo/path/roles/example/verify.yml<|"|>} after`:                             "before  after",
		`before response:bash{command:<|"|>multica issue comment list issue-id --all --output json<|"|>} after`: "before  after",
		`before call:bash{command:<|"|>printf '{"key":"value"}'<|"|>} after`:                                    "before  after",
		`before <|turn>model after`: "before  after",
	}
	for in, want := range tests {
		got := stripPiToolCallMarkup(in)
		if got != want {
			t.Fatalf("unexpected stripped text: %q, want %q", got, want)
		}
	}
}

func TestDrainPiTextBufferSplitToolCall(t *testing.T) {
	chunks := []string{
		"before ca",
		`ll:bash{command:<|"|>ls -R repo/path`,
		`/roles/example<|"|>}`,
		" after",
	}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainPiTextBuffer(&buf, chunk))
	}
	got.WriteString(flushPiTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestDrainPiTextBufferSplitControlToken(t *testing.T) {
	chunks := []string{"before <|tu", "rn>model after"}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainPiTextBuffer(&buf, chunk))
	}
	got.WriteString(flushPiTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestFlushPiTextBufferKeepsUnmatchedToolPrefixes(t *testing.T) {
	tests := []string{
		"plain response: see below",
		"plain call: see below",
		`plain call:bash{command:<|"|>unterminated`,
	}
	for _, want := range tests {
		var buf strings.Builder
		got := drainPiTextBuffer(&buf, want)
		got += flushPiTextBuffer(&buf)
		if got != want {
			t.Fatalf("unexpected flushed text: %q, want %q", got, want)
		}
	}
}

func argsContainsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func thinkingArgValue(args []string) string {
	for i, a := range args {
		if a == "--thinking" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestBuildPiArgsThinkingLevel_AllLevels verifies that each valid Pi thinking
// level produces exactly one "--thinking <level>" pair in the argv.
func TestBuildPiArgsThinkingLevel_AllLevels(t *testing.T) {
	t.Parallel()
	levels := []string{"off", "minimal", "low", "medium", "high", "xhigh"}
	for _, level := range levels {
		level := level
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
				ThinkingLevel: level,
			}, slog.Default())
			got := thinkingArgValue(args)
			if got != level {
				t.Errorf("expected --thinking %q, got %q in args %v", level, got, args)
			}
		})
	}
}

// TestBuildPiArgsThinkingLevel_Empty verifies that an empty thinking_level
// produces no --thinking flag (Pi SDK default applies).
func TestBuildPiArgsThinkingLevel_Empty(t *testing.T) {
	t.Parallel()
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		ThinkingLevel: "",
	}, slog.Default())
	if argsContainsFlag(args, "--thinking") {
		t.Errorf("empty ThinkingLevel must not produce --thinking flag, got: %v", args)
	}
}

// TestBuildPiArgsThinkingLevel_Invalid verifies that an unknown level is
// omitted and does not appear in the argv (daemon already validated, but
// buildPiArgs defends in depth).
func TestBuildPiArgsThinkingLevel_Invalid(t *testing.T) {
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		ThinkingLevel: "supersonic",
	}, logger)
	if argsContainsFlag(args, "--thinking") {
		t.Errorf("invalid ThinkingLevel must not produce --thinking flag, got: %v", args)
	}
	if !strings.Contains(logBuf.String(), "unknown thinking_level") {
		t.Errorf("expected error log for unknown thinking_level, got: %q", logBuf.String())
	}
}

// TestBuildPiArgsThinkingLevel_CustomArgsConflict verifies that when
// custom_args already carries --thinking (legacy workaround), thinking_level
// injection is skipped to avoid duplicate flags.
func TestBuildPiArgsThinkingLevel_CustomArgsConflict(t *testing.T) {
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		ThinkingLevel: "medium",
		CustomArgs:    []string{"--thinking", "high"},
	}, logger)

	// Count --thinking occurrences — must be exactly one (from custom_args).
	count := 0
	for _, a := range args {
		if a == "--thinking" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 --thinking flag (from custom_args), got %d in: %v", count, args)
	}
	// The value passed in custom_args must be preserved.
	if thinkingArgValue(args) != "high" {
		t.Errorf("custom_args --thinking value should be 'high', got %q in: %v", thinkingArgValue(args), args)
	}
	if !strings.Contains(logBuf.String(), "custom_args") {
		t.Errorf("expected warning log about custom_args conflict, got: %q", logBuf.String())
	}
}

// TestBuildPiArgsThinkingLevel_NoCustomArgsConflict verifies that when
// custom_args does NOT carry --thinking, thinking_level injection proceeds
// normally and produces the --thinking flag.
func TestBuildPiArgsThinkingLevel_NoCustomArgsConflict(t *testing.T) {
	args := buildPiArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		ThinkingLevel: "high",
		CustomArgs:    []string{"--tools", "read,bash"},
	}, slog.Default())
	if thinkingArgValue(args) != "high" {
		t.Errorf("expected --thinking high, got %q in: %v", thinkingArgValue(args), args)
	}
}
