package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietAntigravityLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildAntigravityArgsBasic(t *testing.T) {
	t.Parallel()

	args := buildAntigravityArgs(
		"hello",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{Cwd: "/work"},
		quietAntigravityLogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--print-timeout", "20m0s",
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs basic mismatch\n got: %v\nwant: %v", args, want)
	}
}

func TestBuildAntigravityArgsModel(t *testing.T) {
	t.Parallel()

	// agy 1.0.6's --model takes the exact human display string (spaces +
	// parens), not a slug. It must ride as a single argv element so no shell
	// quoting is required, and it must sit before the user's custom args.
	args := buildAntigravityArgs(
		"hello",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{Cwd: "/work", Model: "Claude Opus 4.6 (Thinking)"},
		quietAntigravityLogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--model", "Claude Opus 4.6 (Thinking)",
		"--print-timeout", "20m0s",
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs with model mismatch\n got: %v\nwant: %v", args, want)
	}

	// Empty model must omit the flag entirely so agy resolves its own default.
	bare := buildAntigravityArgs("hi", "/tmp/agy.log", 0, ExecOptions{}, quietAntigravityLogger())
	if slices.Contains(bare, "--model") {
		t.Fatalf("--model must be omitted when opts.Model is empty; got %v", bare)
	}
}

func TestBuildAntigravityArgsNoCapUsesLargePrintTimeout(t *testing.T) {
	t.Parallel()

	// timeout <= 0 means "no wall-clock cap", but agy's --print-timeout DEFAULTS
	// to 5m when omitted, so dropping the flag silently caps every turn at 5
	// minutes and kills any run whose build/tests outlive it (MUL-3570). "No cap"
	// must therefore be expressed by passing a value large enough to defer to the
	// daemon's idle/tool watchdogs — NOT by omitting the flag.
	args := buildAntigravityArgs(
		"hello",
		"/tmp/agy.log",
		0,
		ExecOptions{Cwd: "/work"},
		quietAntigravityLogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--print-timeout", antigravityFormatTimeout(antigravityNoCapPrintTimeout),
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs(timeout=0) mismatch\n got: %v\nwant: %v", args, want)
	}
	// The no-cap value must be well clear of agy's 5m default; otherwise the
	// guillotine still fires on a routine build+test turn.
	if antigravityNoCapPrintTimeout <= 5*time.Minute {
		t.Fatalf("antigravityNoCapPrintTimeout %s must be far larger than agy's 5m default", antigravityNoCapPrintTimeout)
	}
}

func TestAntigravityPrintTimeoutResolvesBudget(t *testing.T) {
	t.Parallel()

	if got := antigravityPrintTimeout(20 * time.Minute); got != 20*time.Minute {
		t.Errorf("positive cap should pass through: got %s, want 20m", got)
	}
	if got := antigravityPrintTimeout(0); got != antigravityNoCapPrintTimeout {
		t.Errorf("zero cap should resolve to no-cap sentinel: got %s, want %s", got, antigravityNoCapPrintTimeout)
	}
	if got := antigravityPrintTimeout(-1); got != antigravityNoCapPrintTimeout {
		t.Errorf("negative cap should resolve to no-cap sentinel: got %s, want %s", got, antigravityNoCapPrintTimeout)
	}
}

func TestAntigravityPrintTimedOut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	timedOut := filepath.Join(dir, "timeout.log")
	if err := os.WriteFile(timedOut, []byte(strings.Join([]string{
		`I0623 17:17:38.930400 65926 printmode.go:156] Print mode: conversation=ea49cf41-4156-425a-a2f7-4238335d4c8b, sending message`,
		`E0623 17:17:59.017212 65926 printmode.go:289] Print mode: timed out after 100 polls (printed=3)`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if !antigravityPrintTimedOut(timedOut) {
		t.Error("expected the print-mode timeout marker to be detected")
	}

	clean := filepath.Join(dir, "clean.log")
	if err := os.WriteFile(clean, []byte(
		`I0623 17:17:38.930400 65926 printmode.go:156] Print mode: conversation=abc, sending message`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if antigravityPrintTimedOut(clean) {
		t.Error("a clean log must not be flagged as timed out")
	}

	if antigravityPrintTimedOut("/nonexistent/path") {
		t.Error("missing log file must be treated as not-timed-out")
	}
	if antigravityPrintTimedOut("") {
		t.Error("empty log path must be treated as not-timed-out")
	}
}

func TestAntigravityProviderError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	providerErr := filepath.Join(dir, "provider-error.log")
	if err := os.WriteFile(providerErr, []byte(strings.Join([]string{
		`I0624 12:34:24.652899 94820 printmode.go:156] Print mode: conversation=44a57718-801c-41e7-9691-3225be2b1cb8, sending message`,
		`E0624 12:34:25.944050 94820 log.go:398] agent executor error: FAILED_PRECONDITION (code 400): User location is not supported for the API use.: FAILED_PRECONDITION (code 400): User location is not supported for the API use.`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got := antigravityProviderError(providerErr)
	if !strings.Contains(got, "FAILED_PRECONDITION") || !strings.Contains(got, "User location is not supported") {
		t.Fatalf("expected provider error to be extracted, got %q", got)
	}

	authNoise := filepath.Join(dir, "auth-noise.log")
	if err := os.WriteFile(authNoise, []byte(strings.Join([]string{
		`W0624 12:34:21.518895 94820 log_context.go:117] Cache(loadCodeAssistResponse): Singleflight refresh failed: error getting token source: You are not logged into Antigravity.`,
		`I0624 12:34:24.084675 94820 printmode.go:192] Print mode: silent auth succeeded`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := antigravityProviderError(authNoise); got != "" {
		t.Fatalf("auth retry noise must not be treated as terminal provider error, got %q", got)
	}

	if got := antigravityProviderError("/nonexistent/path"); got != "" {
		t.Fatalf("missing log should yield empty provider error, got %q", got)
	}
	if got := antigravityProviderError(""); got != "" {
		t.Fatalf("empty log path should yield empty provider error, got %q", got)
	}
}

func TestBuildAntigravityArgsResume(t *testing.T) {
	t.Parallel()

	args := buildAntigravityArgs(
		"continue",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{ResumeSessionID: "b8b263a4-4b2f-4339-acc9-78b248e2b606"},
		quietAntigravityLogger(),
	)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--conversation b8b263a4-4b2f-4339-acc9-78b248e2b606") {
		t.Fatalf("expected --conversation flag with id; got %v", args)
	}
}

func TestBuildAntigravityArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildAntigravityArgs(
		"go",
		"/tmp/agy.log",
		time.Minute,
		ExecOptions{
			// Each blocked flag below must be stripped silently — the daemon
			// owns these because they're required for non-interactive,
			// resume-aware operation.
			CustomArgs: []string{
				"-p", "hijacked-prompt",
				"-i",
				"--prompt-interactive",
				"--continue",
				"-c",
				"--conversation", "bad-id",
				"--model", "sneaky-model", // managed via ExecOptions.Model
				"--dangerously-skip-permissions",
				"--print-timeout", "1h",
				"--log-file", "/elsewhere.log",
				"--add-dir", "/extra", // user-added workspace dir should survive
			},
		},
		quietAntigravityLogger(),
	)

	joined := strings.Join(args, " ")
	// Prompt argument should appear exactly once — the daemon's, not the
	// user's hijacked copy.
	pCount := 0
	for _, a := range args {
		if a == "-p" {
			pCount++
		}
	}
	if pCount != 1 {
		t.Errorf("expected exactly one -p flag, got args=%v", args)
	}
	if strings.Contains(joined, "hijacked-prompt") {
		t.Errorf("custom -p value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "-i") || strings.Contains(joined, "--prompt-interactive") {
		t.Errorf("interactive-mode flags leaked through filter: %v", args)
	}
	if strings.Contains(joined, "bad-id") {
		t.Errorf("custom --conversation value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "sneaky-model") {
		t.Errorf("custom --model value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "/elsewhere.log") {
		t.Errorf("custom --log-file value leaked through filter: %v", args)
	}
	if !strings.Contains(joined, "--add-dir /extra") {
		t.Errorf("non-blocked --add-dir flag should pass through: %v", args)
	}
}

func TestAntigravityFormatTimeoutClampsSubSecond(t *testing.T) {
	t.Parallel()
	if got := antigravityFormatTimeout(0); got != "1s" {
		t.Errorf("antigravityFormatTimeout(0) = %q, want 1s", got)
	}
	if got := antigravityFormatTimeout(20 * time.Minute); got != "20m0s" {
		t.Errorf("antigravityFormatTimeout(20m) = %q, want 20m0s", got)
	}
}

func TestReadAntigravityConversationID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "agy.log")

	// Sample log content modelled on real agy glog output: the
	// conversation= line is what printmode.go writes once per dispatch.
	logBody := strings.Join([]string{
		`I0528 13:36:19.959748 73304 printmode.go:71] Print mode: starting (promptLength=18, model="", conversationID="")`,
		`I0528 13:36:23.318877 73304 printmode.go:130] Print mode: conversation=b8b263a4-4b2f-4339-acc9-78b248e2b606, sending message`,
		`I0528 13:36:23.318892 73304 server.go:1083] Sending user message to conversation b8b263a4-4b2f-4339-acc9-78b248e2b606 (items=1, media=0)`,
	}, "\n")
	if err := os.WriteFile(logPath, []byte(logBody), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readAntigravityConversationID(logPath)
	want := "b8b263a4-4b2f-4339-acc9-78b248e2b606"
	if got != want {
		t.Fatalf("readAntigravityConversationID = %q, want %q", got, want)
	}
}

func TestReadAntigravityConversationIDMissingFile(t *testing.T) {
	t.Parallel()
	if got := readAntigravityConversationID("/nonexistent/path"); got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
	if got := readAntigravityConversationID(""); got != "" {
		t.Errorf("expected empty string for empty path, got %q", got)
	}
}

// fakeAgyPrintTimeoutScript returns a POSIX-sh script that impersonates `agy -p`
// hitting its own --print-timeout: it prints a couple of "I will ..." narration
// lines (as agy streams to stdout), writes the printmode.go "timed out after N
// polls" marker into the --log-file the daemon handed it, prints agy's
// user-facing "Error: timed out waiting for response" line, and EXITS 0 — exactly
// the sequence that made a stalled turn look "completed" (MUL-3570).
func fakeAgyPrintTimeoutScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "I will run the Go unit tests to verify the build."
echo "I will wait for the Go unit tests to complete."
if [ -n "$log" ]; then
  printf 'I0623 17:17:38.930400 1 printmode.go:156] Print mode: conversation=ea49cf41-4156-425a-a2f7-4238335d4c8b, sending message\n' >> "$log"
  printf 'E0623 17:17:59.017212 1 printmode.go:289] Print mode: timed out after 100 polls (printed=2)\n' >> "$log"
fi
echo "Error: timed out waiting for response"
exit 0
`
}

// fakeAgyProviderErrorScript reproduces a real agy failure mode observed with
// Gemini 3.5 Flash (High): the process exits 0 and prints nothing to stdout,
// while the terminal provider error only appears in the daemon-owned log file.
func fakeAgyProviderErrorScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$log" ]; then
  printf 'I0624 12:34:24.652899 1 printmode.go:156] Print mode: conversation=44a57718-801c-41e7-9691-3225be2b1cb8, sending message\n' >> "$log"
  printf 'E0624 12:34:25.944050 1 log.go:398] agent executor error: FAILED_PRECONDITION (code 400): User location is not supported for the API use.: FAILED_PRECONDITION (code 400): User location is not supported for the API use.\n' >> "$log"
fi
exit 0
`
}

// TestAntigravityBackendPrintTimeoutSurfacesAsTimeout is the end-to-end guard for
// MUL-3570: agy aborts a long turn by printing its timeout sentinel and exiting
// 0, so the backend must classify the result as a timeout (not a truncated
// "completed") while still preserving the narration printed before the cut-off.
func TestAntigravityBackendPrintTimeoutSurfacesAsTimeout(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyPrintTimeoutScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Timeout: 0 ("no cap") so runContext never trips — the only signal that the
	// turn died is agy's own print-timeout marker in the log.
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Drain the message stream so the lifecycle goroutine can finish.
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "timeout" {
			t.Fatalf("expected status=timeout, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "agy --print-timeout elapsed") {
			t.Errorf("expected error to explain the agy print timeout, got %q", result.Error)
		}
		// Narration streamed before the cut-off must still reach the result so
		// the user sees how far the turn got.
		if !strings.Contains(result.Output, "I will wait for the Go unit tests to complete") {
			t.Errorf("expected partial narration to be preserved in output, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAntigravityBackendProviderErrorSurfacesAsFailed(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyProviderErrorScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
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
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "User location is not supported") {
			t.Errorf("expected provider error to be surfaced, got %q", result.Error)
		}
		if result.Output != "" {
			t.Errorf("expected empty stdout to remain empty, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestAntigravityModelError is the regression guard for the silent-no-op fix:
// agy exits 0 with empty output on an unrecognised --model, so Execute must
// reject a non-empty model that isn't in the `agy models` catalog instead of
// letting it run to a fake "completed + empty" success. This covers the same
// validation regardless of whether opts.Model originated from agent.model, a
// persisted/API value, or the daemon-wide MULTICA_ANTIGRAVITY_MODEL default —
// they all collapse to opts.Model before Execute runs this check.
func TestAntigravityModelError(t *testing.T) {
	t.Parallel()

	catalog := []Model{
		{ID: "Gemini 3.5 Flash (Medium)", Label: "Gemini 3.5 Flash (Medium)", Provider: "antigravity"},
		{ID: "Claude Opus 4.6 (Thinking)", Label: "Claude Opus 4.6 (Thinking)", Provider: "antigravity"},
	}

	// Exact catalog hit → accepted.
	if err := antigravityModelError("Claude Opus 4.6 (Thinking)", catalog); err != nil {
		t.Errorf("valid model rejected: %v", err)
	}

	// Empty model → accepted (flag omitted, agy resolves its own default).
	if err := antigravityModelError("", catalog); err != nil {
		t.Errorf("empty model should not error: %v", err)
	}

	// Empty / nil catalog → fail open (discovery couldn't produce a list, so we
	// can't prove the value is bad — let agy decide rather than block the run).
	if err := antigravityModelError("anything at all", nil); err != nil {
		t.Errorf("empty catalog should fail open, got: %v", err)
	}

	// Unknown model with a known catalog → actionable error that names the
	// rejected value and points at `agy models`. THIS is the case that stops
	// the silent empty-success.
	err := antigravityModelError("Totally Made Up Model", catalog)
	if err == nil {
		t.Fatal("unknown model should be rejected, not silently accepted")
	}
	if !strings.Contains(err.Error(), "Totally Made Up Model") {
		t.Errorf("error should name the rejected model: %v", err)
	}
	if !strings.Contains(err.Error(), "agy models") {
		t.Errorf("error should point the user at `agy models`: %v", err)
	}

	// Near-miss (trailing space / dropped suffix) → still rejected, because agy
	// needs the exact display string and would no-op on anything else.
	if err := antigravityModelError("Claude Opus 4.6 (Thinking) ", catalog); err == nil {
		t.Error("near-miss model (trailing space) should be rejected")
	}
	if err := antigravityModelError("Claude Opus 4.6", catalog); err == nil {
		t.Error("near-miss model (dropped suffix) should be rejected")
	}
}
