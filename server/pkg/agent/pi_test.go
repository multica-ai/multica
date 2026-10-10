package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	if got := countArg(args, "--approve"); got != 0 {
		t.Errorf("--approve count = %d, want 0 in %v", got, args)
	}
	if countArg(args, "--extension") != 0 {
		t.Errorf("extension flag without a managed config: %v", args)
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
func TestBuildPiArgsStripsProjectTrustFlags(t *testing.T) {
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		CustomArgs: []string{"--no-approve", "-a", "--approve", "-na", "--offline"},
	}, slog.Default())

	if got := countArg(args, "--approve"); got != 0 {
		t.Fatalf("--approve count = %d, want 0 in %v", got, args)
	}
	if countArg(args, "-a") != 0 {
		t.Fatalf("custom -a survived in %v", args)
	}
	for _, kept := range []string{"--no-approve", "-na", "--offline"} {
		if countArg(args, kept) != 1 {
			t.Errorf("custom %s missing from %v", kept, args)
		}
	}
}

func TestBuildPiArgsAppendsMcpExtension(t *testing.T) {
	extensionPath := "/tmp/multica-pi-mcp-test/mcp-extension.mjs"
	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{
		CustomArgs:         []string{"--offline", "--approve"},
		piMcpExtensionPath: extensionPath,
	}, slog.Default())

	if countArg(args, "--approve") != 0 {
		t.Fatalf("--approve survived in %v", args)
	}
	if len(args) < 2 || args[len(args)-2] != "--extension" || args[len(args)-1] != extensionPath {
		t.Fatalf("args = %v, want --extension %s at the end", args, extensionPath)
	}
	if countArg(args, "--offline") != 1 {
		t.Fatalf("custom --offline missing from %v", args)
	}
}

func countArg(args []string, want string) int {
	n := 0
	for _, arg := range args {
		if arg == want {
			n++
		}
	}
	return n
}

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

func TestPiExecuteRejectsConcurrentSessionWriter(t *testing.T) {
	t.Parallel()

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("create session file: %v", err)
	}
	claim, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		t.Fatalf("lock session file: %v", err)
	}
	if !locked {
		t.Fatal("first session-file lock was unexpectedly busy")
	}
	defer releasePiSessionFileLock(claim)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	backend, err := New("pi", Config{ExecutablePath: executable, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	session, err := backend.Execute(t.Context(), "follow-up", ExecOptions{ResumeSessionID: sessionPath})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "failed" || !result.ResumeRejectedTransient {
		t.Fatalf("result = %+v, want failed ResumeRejectedTransient", result)
	}
	if result.ResumeRejected {
		t.Fatal("a busy session is still healthy and must not be permanently rejected")
	}
	if result.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty so the live transcript is not republished", result.SessionID)
	}
	if !strings.Contains(result.Error, "already in use") {
		t.Fatalf("error = %q, want session-in-use diagnostic", result.Error)
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
	claim, locked, err := tryLockPiSessionFile(sessionPath)
	if err != nil {
		t.Fatalf("lock completed session: %v", err)
	}
	if !locked {
		t.Fatal("Pi backend returned its result before releasing the session-file lock")
	}
	releasePiSessionFileLock(claim)
}

// piEventStreamScript builds a sh script that prints each JSON event on
// its own stdout line. Fixtures must not contain single quotes.
func piEventStreamScript(events []string) string {
	return piEventStreamScriptWithExit(events, 0)
}

// piEventStreamScriptWithExit is piEventStreamScript plus an explicit
// process exit code. Real Pi (and pi-print-clean-exit) exits 1 after a
// turn whose last assistant stopReason is "error".
func piEventStreamScriptWithExit(events []string, exitCode int) string {
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
	if exitCode != 0 {
		b.WriteString(fmt.Sprintf("exit %d\n", exitCode))
	}
	return b.String()
}

func newPiTestBackend(t *testing.T, script string, turnErrorGrace time.Duration) *piBackend {
	t.Helper()
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("pi", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}
	pi, ok := backend.(*piBackend)
	if !ok {
		t.Fatalf("New(pi) returned %T, want *piBackend", backend)
	}
	pi.turnErrorGrace = turnErrorGrace
	return pi
}

func waitPiResult(t *testing.T, session *Session, timeout time.Duration) Result {
	t.Helper()
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		return result
	case <-time.After(timeout):
		t.Fatal("timeout waiting for Pi result")
		return Result{}
	}
}

func TestPiExecutePreservesTurnErrorWhenCancelled(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const providerError = "OpenAI API error (413): Failed to buffer the request body: length limit exceeded"
	script := piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"` + providerError + `"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"post-error activity"}}`,
	}) + "exec sleep 300\n"
	backend := newPiTestBackend(t, script, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for msg := range session.Messages {
		if msg.Type == MessageThinking && msg.Content == "post-error activity" {
			cancel()
			break
		}
	}

	result := waitPiResult(t, session, 5*time.Second)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (error=%q)", result.Status, result.Error)
	}
	if result.Error != providerError {
		t.Fatalf("error = %q, want original provider error %q", result.Error, providerError)
	}
}

func TestPiExecuteCancellationWithoutTurnErrorKeepsAbortedResult(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	script := piEventStreamScript([]string{`{"type":"agent_start"}`}) + "exec sleep 300\n"
	backend := newPiTestBackend(t, script, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for msg := range session.Messages {
		if msg.Type == MessageStatus {
			cancel()
			break
		}
	}

	result := waitPiResult(t, session, 5*time.Second)
	if result.Status != "aborted" || result.Error != "execution cancelled" {
		t.Fatalf("result = %+v, want the existing no-error cancellation result", result)
	}
}

func TestPiExecuteEndsSilentTurnErrorAfterGraceOnce(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const providerError = "OpenAI API error (413): request body too large"
	const grace = 80 * time.Millisecond
	script := piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"` + providerError + `"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
	}) + "exec sleep 300\n"
	backend := newPiTestBackend(t, script, grace)

	started := time.Now()
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         15 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	result := waitPiResult(t, session, 20*time.Second)
	if result.Status != "failed" || result.Error != providerError {
		t.Fatalf("result = %+v, want one failed result with the provider error", result)
	}
	for msg := range session.Messages {
		if msg.Type == MessageError {
			t.Fatalf("turn-error grace emitted an error message and would refresh the daemon watchdog: %+v", msg)
		}
	}
	if elapsed := time.Since(started); elapsed < grace/2 || elapsed > 10*time.Second {
		t.Fatalf("error grace ended after %s, want approximately %s", elapsed, grace)
	}
	if _, ok := <-session.Result; ok {
		t.Fatal("result channel produced more than one terminal result")
	}
}

func TestPiExecuteTurnErrorActivityAndRetryRecoveryCancelTimer(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const grace = 100 * time.Millisecond
	script := "#!/bin/sh\n" +
		"cat > /dev/null\n" +
		`printf '%s\n' '{"type":"agent_start"}'` + "\n" +
		`printf '%s\n' '{"type":"turn_start"}'` + "\n" +
		`printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"temporary provider error"}}'` + "\n" +
		"sleep 0.06\n" +
		`printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"retrying"}}'` + "\n" +
		"sleep 0.06\n" +
		`printf '%s\n' '{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":1}'` + "\n" +
		"sleep 0.15\n" +
		`printf '%s\n' '{"type":"turn_start"}'` + "\n" +
		`printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"recovered"}}'` + "\n" +
		`printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test"}}'` + "\n"
	backend := newPiTestBackend(t, script, grace)

	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         15 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	result := waitPiResult(t, session, 20*time.Second)
	if result.Status != "completed" || result.Output != "recovered" || result.Error != "" {
		t.Fatalf("result = %+v, want successful recovered turn", result)
	}
}

func TestPiExecuteTurnErrorGraceWaitsForInFlightTool(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const grace = 60 * time.Millisecond
	script := "#!/bin/sh\n" +
		"cat > /dev/null\n" +
		`printf '%s\n' '{"type":"turn_start"}'` + "\n" +
		`printf '%s\n' '{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{}}'` + "\n" +
		`printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"temporary provider error"}}'` + "\n" +
		"sleep 0.15\n" +
		`printf '%s\n' '{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"ok"}'` + "\n" +
		`printf '%s\n' '{"type":"turn_start"}'` + "\n" +
		`printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done"}}'` + "\n" +
		`printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test"}}'` + "\n"
	backend := newPiTestBackend(t, script, grace)

	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         15 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	result := waitPiResult(t, session, 20*time.Second)
	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("result = %+v, want tool completion and recovered turn", result)
	}
}

func TestPiExecuteNormalSilenceDoesNotArmTurnErrorGrace(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const grace = 50 * time.Millisecond
	script := "#!/bin/sh\n" +
		"cat > /dev/null\n" +
		`printf '%s\n' '{"type":"agent_start"}'` + "\n" +
		"sleep 0.15\n" +
		`printf '%s\n' '{"type":"turn_start"}'` + "\n" +
		`printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"healthy"}}'` + "\n" +
		`printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test"}}'` + "\n"
	backend := newPiTestBackend(t, script, grace)

	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         15 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	result := waitPiResult(t, session, 20*time.Second)
	if result.Status != "completed" || result.Output != "healthy" {
		t.Fatalf("result = %+v, want normal silent run to complete", result)
	}
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

	// The same stopReason=error precedes an automatic retry, on turn_end and
	// again on agent_end. The retry succeeds, so the run must not inherit the
	// first turn's failure.
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"OpenAI API error (503): no available channel"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"OpenAI API error (503): no available channel"}],"willRetry":true}`,
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

func TestPiExecuteKeepsTurnErrorWhenProcessExitsNonZero(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Real Pi (and pi-print-clean-exit) exits 1 after stopReason=error.
	// Wait() used to win the completed-branch and drop lastTurnError, so
	// the classifier only saw "exit status 1" (non-retryable
	// process_failure). The provider message must survive so
	// "Connection error." / "Request timed out." classify as
	// provider_network.
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":0,"output":0},"stopReason":"error","errorMessage":"Connection error."}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(piEventStreamScriptWithExit(events, 1)))

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
		if !strings.Contains(result.Error, "Connection error.") {
			t.Fatalf("Error: got %q, want it to carry the provider message", result.Error)
		}
		if !strings.Contains(result.Error, "exited with error") {
			t.Fatalf("Error: got %q, want the process exit still visible as a suffix", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// piKeyErrorAgentEnd is the agent_end Pi 0.74.0 emits when the provider's API
// key command fails: one empty assistant message, and nothing else reports it.
const piKeyErrorAgentEnd = `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":""}],"model":"test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"error","errorMessage":"Failed to resolve API key for provider \"gateway\" from shell command: key-helper"}]}`

func TestPiExecuteFailsOnAgentEndOnlyError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// A failure raised before the first model call reaches the stream only on
	// agent_end: no turn_end, no error event, no auto_retry_end, exit 0.
	tests := []struct {
		name     string
		agentEnd string
		want     string
	}{
		{
			name:     "provider message",
			agentEnd: piKeyErrorAgentEnd,
			want:     `Failed to resolve API key for provider "gateway" from shell command: key-helper`,
		},
		{
			name:     "empty message",
			agentEnd: `{"type":"agent_end","messages":[{"role":"assistant","content":[],"model":"test","stopReason":"error"}]}`,
			want:     "pi ended the run with an error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			script := piEventStreamScript([]string{
				`{"type":"agent_start"}`,
				`{"type":"turn_start"}`,
				`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"prompt"}]}}`,
				`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"prompt"}]}}`,
				tt.agentEnd,
			})
			backend := newPiTestBackend(t, script, 0)
			session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
				Timeout:         5 * time.Second,
				ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()

			result := waitPiResult(t, session, 10*time.Second)
			if result.Status != "failed" || result.Error != tt.want {
				t.Fatalf("result = {Status:%q Error:%q}, want failed with %q", result.Status, result.Error, tt.want)
			}
		})
	}
}

func TestPiExecuteFailsOnAgentEndErrorAfterToolWork(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// Pi resolves the API key again for every model request, so a key that
	// stops resolving mid-run fails the next turn after real work. That turn
	// starts and then only agent_end reports the failure.
	script := piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"Checking."}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[],"model":"test","usage":{"input":5,"output":3,"cacheRead":0,"cacheWrite":0,"totalTokens":8},"stopReason":"toolUse"}}`,
		`{"type":"turn_start"}`,
		piKeyErrorAgentEnd,
	})
	backend := newPiTestBackend(t, script, 0)
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := waitPiResult(t, session, 10*time.Second)
	want := `Failed to resolve API key for provider "gateway" from shell command: key-helper`
	if result.Status != "failed" || result.Error != want {
		t.Fatalf("result = {Status:%q Error:%q}, want failed with %q", result.Status, result.Error, want)
	}
	if got := result.Usage["test"]; got.InputTokens != 5 || got.OutputTokens != 3 {
		t.Fatalf("Usage[test] = %+v, want the completed turn's usage kept", got)
	}
}

func TestPiExecuteFailsOnScannerOverflow(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// agent_end repeats every message of the run, so on a long run it is the
	// line most likely to outgrow the shared bound. Once the scanner stops, the
	// run's terminal state is unknown and must not read as success. The line is
	// sized from agentStreamMaxLineBytes so raising the cap keeps this test on
	// the overflow branch.
	script := piEventStreamScript([]string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`,
	}) + fmt.Sprintf("dd if=/dev/zero bs=1048576 count=%d 2>/dev/null | tr '\\000' x\nprintf '\\n'\n",
		agentStreamMaxLineBytes/(1024*1024)+1)
	backend := newPiTestBackend(t, script, 0)
	// A generous timeout: without the fix this run hangs until it expires, and
	// a loaded host must not turn the fixed run into a timeout either.
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{
		Timeout:         30 * time.Second,
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := waitPiResult(t, session, 40*time.Second)
	if result.Status != "failed" {
		t.Fatalf("result = {Status:%q Error:%q}, want failed", result.Status, result.Error)
	}
	for _, want := range []string{"pi stdout read error", "token too long"} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("Error = %q, want substring %q", result.Error, want)
		}
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

func TestWritePiMcpExtensionFiltersAndLeavesProjectFile(t *testing.T) {
	workDir := t.TempDir()
	projectPath := filepath.Join(workDir, ".pi", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"mcpServers":{"user":{"command":"echo"}}}`)
	if err := os.WriteFile(projectPath, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	raw := json.RawMessage(`{
		"mcpServers": {
			"probe": {"command": "echo", "args": ["hi"]},
			"bad name": {"command": "nope"},
			"legacy": {"type": "sse", "url": "http://example.invalid"}
		}
	}`)
	path, err := writePiMcpExtension(raw, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected an extension file")
	}
	t.Cleanup(func() { removePiMcpExtension(path) })

	if !strings.HasPrefix(filepath.Base(filepath.Dir(path)), piMcpExtensionDirPrefix) {
		t.Fatalf("extension dir = %s, want %s prefix", path, piMcpExtensionDirPrefix)
	}
	if filepath.Base(path) != "mcp-extension.mjs" {
		t.Fatalf("extension file = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("extension mode = %o, want 600", info.Mode().Perm())
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, want := range []string{"registerMcpServer", "probe", "echo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("script missing %q:\n%s", want, text)
		}
	}
	for _, absent := range []string{"bad name", "legacy", "example.invalid"} {
		if strings.Contains(text, absent) {
			t.Fatalf("script contains skipped server %q:\n%s", absent, text)
		}
	}

	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(existing) {
		t.Fatalf("project mcp.json changed to %s", data)
	}

	args := buildPiArgs("/tmp/s.jsonl", ExecOptions{piMcpExtensionPath: path}, slog.Default())
	if len(args) < 2 || args[len(args)-2] != "--extension" || args[len(args)-1] != path {
		t.Fatalf("args = %v, want extension %s", args, path)
	}
	if countArg(args, "--approve") != 0 {
		t.Fatalf("--approve present in %v", args)
	}
}

func TestWritePiMcpExtensionSkipsUnmanagedAndEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage(`{}`), json.RawMessage(`{"mcpServers":{}}`)} {
		path, err := writePiMcpExtension(raw, slog.Default())
		if err != nil {
			t.Fatalf("writePiMcpExtension(%s): %v", raw, err)
		}
		if path != "" {
			removePiMcpExtension(path)
			t.Fatalf("writePiMcpExtension(%s) wrote %s", raw, path)
		}
	}

	path, err := writePiMcpExtension(json.RawMessage(`{"mcpServers":{"bad name":{"command":"echo"},"legacy":{"type":"sse"}}}`), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		removePiMcpExtension(path)
		t.Fatalf("filtered config still wrote %s", path)
	}
}

func TestRemovePiMcpExtensionIgnoresOtherPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-extension.mjs")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	removePiMcpExtension(path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unrelated extension file was removed: %v", err)
	}
}

func TestPreparePiMcpExtensionSkipsOmp(t *testing.T) {
	backend := &piBackend{providerLabel: "omp"}
	path, err := backend.preparePiMcpExtension(json.RawMessage(`{"mcpServers":{"probe":{"command":"echo"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		removePiMcpExtension(path)
		t.Fatalf("omp wrote a pi extension at %s", path)
	}
}
