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
	args := buildPiArgs("/tmp/session.jsonl", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildPiArgs emits --tools %q; should not restrict tool registry (see #2379)", args[i+1])
		}
	}
}

func TestBuildPiArgsBasicFlags(t *testing.T) {
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		Model:         "anthropic/claude-sonnet-4-20250514",
		ThinkingLevel: "high",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--mode json", "--session /tmp/s.jsonl", "--model anthropic/claude-sonnet-4-20250514", "--thinking high"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	for _, arg := range args {
		if arg == "hello world" {
			t.Fatalf("prompt leaked into argv: %v", args)
		}
	}
}

// Pi reads the per-task AGENTS.md the daemon writes into the workdir, so the
// daemon never populates SystemPrompt for it (providerNeedsInlineSystemPrompt).
// Forwarding it anyway would duplicate the whole runtime brief on every turn.
func TestBuildPiArgsIgnoresSystemPrompt(t *testing.T) {
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		SystemPrompt: "the entire multica runtime brief",
	}, slog.Default())

	for _, a := range args {
		if a == "--append-system-prompt" {
			t.Fatalf("unexpected --append-system-prompt in args: %v", args)
		}
		if a == "the entire multica runtime brief" {
			t.Fatalf("SystemPrompt leaked into args: %v", args)
		}
	}
}

func TestBuildPiArgsCustomArgsAppended(t *testing.T) {
	// Users can still restrict tools via custom_args if desired.
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
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

func TestBuildPiArgsFiltersCustomInputButKeepsOptionValues(t *testing.T) {
	t.Parallel()

	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		CustomArgs: []string{
			"--tools", "read,bash",
			"positional-input",
			"@prompt.md",
			"--verbose",
			"after-boolean",
			"--extension-option", "extension-value",
			"--thinking", "low",
			"--thinking=medium",
			"--offline",
			"trailing-input",
		},
	}, slog.Default())

	joined := strings.Join(args, "\x00")
	for _, unwanted := range []string{"positional-input", "@prompt.md", "after-boolean", "trailing-input"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("custom input %q should be filtered, got %v", unwanted, args)
		}
	}
	for _, pair := range [][2]string{
		{"--tools", "read,bash"},
		{"--extension-option", "extension-value"},
	} {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == pair[0] && args[i+1] == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("option/value %q %q missing from %v", pair[0], pair[1], args)
		}
	}
	for _, arg := range args {
		if arg == "--thinking" || strings.HasPrefix(arg, "--thinking=") || arg == "low" || arg == "medium" {
			t.Errorf("custom --thinking must be owned by thinking_level and filtered, got %v", args)
		}
	}
}

func TestBuildPiArgsThinkingLevelOverridesCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		ThinkingLevel: "max",
		CustomArgs:    []string{"--thinking", "low", "--thinking=medium"},
	}, slog.Default())

	count := 0
	for i, arg := range args {
		if arg == "--thinking" {
			count++
			if i+1 >= len(args) || args[i+1] != "max" {
				t.Fatalf("selected thinking level missing from args: %v", args)
			}
		}
		if strings.HasPrefix(arg, "--thinking=") {
			t.Fatalf("custom inline thinking flag leaked through: %v", args)
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one daemon-owned --thinking flag, got %d in %v", count, args)
	}
}

func TestPiExecuteRejectsEmptyPrompt(t *testing.T) {
	t.Parallel()

	backend, err := New("pi", Config{ExecutablePath: "/does/not/need/to/exist", Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	if _, err := backend.Execute(t.Context(), " \n\t ", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "prompt must not be empty") {
		t.Fatalf("Execute error = %v, want empty-prompt error", err)
	}
}

// TestPiExecuteAttachesStdinPipe verifies that the Pi backend spawns the child
// with an explicit stdin pipe, writes the task prompt, and closes it. Closing
// delivers both the end-of-prompt signal and the EOF that keeps Pi from
// blocking under systemd (#2188).
//
// The probe is structural rather than behavioral: a shell script in
// place of `pi` inspects /proc/self/fd/0, drains it to EOF, and only emits a
// valid event stream when both the pipe type and prompt are correct.
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
		"payload=$(cat)\n" +
		"case \"$kind\" in\n" +
		"  fifo|*pipe*)\n" +
		"    if [ \"$payload\" = 'prompt-over-stdin' ]; then\n" +
		"      printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"      printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1,\"cacheRead\":0,\"cacheWrite\":0,\"totalTokens\":2}}}'\n" +
		"      exit 0\n" +
		"    fi\n" +
		"    ;;\n" +
		"esac\n" +
		"printf 'stdin was %s with payload %s; expected fifo and prompt\\n' \"$kind\" \"$payload\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	session, err := backend.Execute(ctx, "prompt-over-stdin", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: sessionPath,
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
			t.Fatalf("expected status=completed (stdin attached as fifo), got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// piEventStreamScript builds a sh script that prints each JSON event on
// its own stdout line. Fixtures must not contain single quotes.
func piEventStreamScript(events []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	// Real Pi reads the piped prompt to EOF before emitting events, so the fake
	// must drain stdin too. A fake that exits without reading closes the read end
	// while the backend is still writing the prompt, and the resulting EPIPE is
	// reported as "pi prompt write failed" — a load-dependent flake that has
	// nothing to do with what these tests assert.
	b.WriteString("cat > /dev/null\n")
	for _, e := range events {
		b.WriteString("printf '%s\\n' '")
		b.WriteString(e)
		b.WriteString("'\n")
	}
	return b.String()
}

// TestPiExecuteRetainsOnlyLastTurnOutput verifies turn_start resets the
// output buffer so Result.Output keeps only the final turn's text.
func TestPiExecuteRetainsOnlyLastTurnOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"intermediate"}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"final"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":" "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"answer"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":2,"output":2}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

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
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "final answer" {
			t.Fatalf("Output: got %q, want %q", result.Output, "final answer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestPiExecuteFailsOnUnretriedTurnError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Pi ends the turn with stopReason=error and declines to retry (401, 429
	// and friends). It emits no `error` event and no auto_retry_end, and exits
	// 0, so the terminal state lives only on turn_end.
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"OpenAI API error (401): invalid token"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

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
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "invalid token") {
			t.Fatalf("Error: got %q, want it to carry the provider message", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestPiExecuteSucceedsWhenRetryFollowsTurnError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// The same stopReason=error precedes an automatic retry. The retry
	// succeeds, so the run must not inherit the first turn's failure.
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"OpenAI API error (503): no available channel"}}`,
		`{"type":"agent_end","messages":[],"willRetry":true}`,
		`{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":1}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"recovered"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":2,"output":2}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScript(events)))

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
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "recovered" {
			t.Fatalf("Output: got %q, want %q", result.Output, "recovered")
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

// TestBuildPiArgsKeepsSlashShapedModelIDIntact is the GH #7300 regression.
// A gateway-style provider registers model ids that themselves contain a
// slash (`claude/claude-opus-5` under provider `multica-anthropic`). Splitting
// on that slash to fill --provider produced `--provider claude --model
// claude-opus-5`, and pi answers an unknown --provider with a hard
// `Unknown provider "claude"` before it ever looks at --model. The whole
// selector must reach --model instead, where pi's resolver matches it as a
// raw model id.
func TestBuildPiArgsKeepsSlashShapedModelIDIntact(t *testing.T) {
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{Model: "claude/claude-opus-5"}, slog.Default())

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model claude/claude-opus-5") {
		t.Errorf("model id was not passed through intact: %v", args)
	}
	for _, arg := range args {
		if arg == "--provider" {
			t.Errorf("buildPiArgs synthesized --provider from a model id: %v", args)
		}
	}
}

// A fully-qualified selector is passed through the same way: pi infers the
// provider from the prefix itself, so the daemon never has to take it apart.
func TestBuildPiArgsPassesQualifiedSelectorWhole(t *testing.T) {
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{Model: "multica-anthropic/claude/claude-opus-5"}, slog.Default())

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model multica-anthropic/claude/claude-opus-5") {
		t.Errorf("qualified selector was not passed through intact: %v", args)
	}
	if strings.Contains(joined, "--provider") {
		t.Errorf("buildPiArgs synthesized --provider: %v", args)
	}
}

// A bare model id stays bare, and an empty model still omits --model entirely
// so pi resolves its own default.
func TestBuildPiArgsBareAndEmptyModels(t *testing.T) {
	bare := buildPiArgs("", ExecOptions{Model: "  claude-opus-5  "}, slog.Default())
	if joined := strings.Join(bare, " "); !strings.Contains(joined, "--model claude-opus-5") {
		t.Errorf("bare model not forwarded (or not trimmed): %v", bare)
	}

	empty := buildPiArgs("", ExecOptions{Model: "   "}, slog.Default())
	for _, arg := range empty {
		if arg == "--model" {
			t.Errorf("blank model should omit --model so pi picks its default: %v", empty)
		}
	}
}
