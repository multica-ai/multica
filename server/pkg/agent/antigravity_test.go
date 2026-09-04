package agent

import (
	"context"
	"fmt"
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
		"--output-format", "stream-json",
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
		"--output-format", "stream-json",
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
		"--output-format", "stream-json",
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
			ExtraArgs: []string{
				"--settings", "/tmp/biz-gate.json", // Claude Code-only daemon/profile arg; agy rejects it
			},
			// Each blocked flag below must be stripped silently — the daemon
			// owns these because they're required for non-interactive,
			// resume-aware operation, or because the flag is unsupported by
			// Antigravity.
			CustomArgs: []string{
				"-p", "hijacked-prompt",
				"-i",
				"--prompt-interactive",
				"--continue",
				"-c",
				"--conversation", "bad-id",
				"--model", "sneaky-model", // managed via ExecOptions.Model
				"--output-format", "text",
				"--dangerously-skip-permissions",
				"--print-timeout", "1h",
				"--log-file", "/elsewhere.log",
				"--settings=/tmp/agent-settings.json",
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
	if strings.Contains(joined, "--output-format text") {
		t.Errorf("custom --output-format value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "/elsewhere.log") {
		t.Errorf("custom --log-file value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "--settings") || strings.Contains(joined, "biz-gate.json") || strings.Contains(joined, "agent-settings.json") {
		t.Errorf("Antigravity-incompatible --settings flag leaked through filter: %v", args)
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

// seedAntigravityTranscript writes a transcript.jsonl under appDataDir for the
// given conversation id, at the real path agy uses
// (<appDataDir>/brain/<cid>/.system_generated/logs/transcript.jsonl).
func seedAntigravityTranscript(t *testing.T, appDataDir, conversationID string, records []string) {
	t.Helper()
	dir := filepath.Join(appDataDir, "brain", conversationID, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(records, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadAntigravityTranscriptOutput(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "d1637a93-20c7-4d90-8edb-7395e71280d2"

	// The app data dir + conversation id both come from the per-run log, the
	// same source the daemon already owns via --log-file.
	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(strings.Join([]string{
		`I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir,
		`I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + cid + `, sending message`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	seedAntigravityTranscript(t, appDataDir, cid, []string{
		// USER_INPUT (source=USER_EXPLICIT in practice) opens the turn.
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"the user's prompt — must be ignored"}`,
		// Tool-only model steps carry content=null and must be skipped.
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":null}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":3,"content":"First I will read the file."}`,
		// Tool records can also be source=MODEL — excluded by the type check.
		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":4}`,
		// Non-MODEL planner text must be excluded.
		`{"type":"PLANNER_RESPONSE","source":"SYSTEM","status":"DONE","step_index":5,"content":"non-model planner text — must be ignored"}`,
		// A non-DONE (streaming/partial) model record must be excluded.
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"IN_PROGRESS","step_index":6,"content":"partial streaming text — must be ignored"}`,
		`{"type":"CODE_ACTION","source":"MODEL","status":"DONE","step_index":7}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":8,"content":"Done: created result.txt and verified it."}`,
	})

	// MODEL/DONE text is joined in order; null/tool/non-model/non-DONE records
	// are all dropped.
	got := readAntigravityTranscriptOutput(logPath, cid)
	want := "First I will read the file.\n\nDone: created result.txt and verified it."
	if got != want {
		t.Fatalf("readAntigravityTranscriptOutput mismatch\n got: %q\nwant: %q", got, want)
	}

	// Every soft-failure path must yield "" rather than erroring.
	if got := readAntigravityTranscriptOutput(logPath, "ffffffff-0000-0000-0000-000000000000"); got != "" {
		t.Errorf("unknown conversation id should yield empty, got %q", got)
	}
	if got := readAntigravityTranscriptOutput("/nonexistent/agy.log", cid); got != "" {
		t.Errorf("missing log (no app data dir) should yield empty, got %q", got)
	}
	if got := readAntigravityTranscriptOutput(logPath, ""); got != "" {
		t.Errorf("empty conversation id should yield empty, got %q", got)
	}
	if got := readAntigravityTranscriptOutput("", cid); got != "" {
		t.Errorf("empty log path should yield empty, got %q", got)
	}
}

// TestReadAntigravityTranscriptOutputResumeReturnsCurrentTurnOnly is the
// regression guard for the PR #4744 review must-fix: the transcript accumulates
// across resumed turns (daemon reuses the conversation via --conversation), so
// recovery must return ONLY the current turn's reply. Without the USER_INPUT
// turn boundary, a second empty-stdout turn would re-emit the previous turn's
// answer alongside the new one.
func TestReadAntigravityTranscriptOutputResumeReturnsCurrentTurnOnly(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "9e18418b-a431-4523-9616-75a94904554e"

	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(strings.Join([]string{
		`I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir,
		`I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + cid + `, sending message`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two turns in one accumulated transcript, matching real agy resume output:
	// each turn opens with its own USER_INPUT.
	seedAntigravityTranscript(t, appDataDir, cid, []string{
		// --- turn 1 ---
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"read marker.txt"}`,
		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":1}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":"The two values are alpha-1 and beta-2."}`,
		// --- turn 2 (resumed) ---
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":3,"content":"what is 7 times 8?"}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":4,"content":"56"}`,
	})

	got := readAntigravityTranscriptOutput(logPath, cid)
	if got != "56" {
		t.Fatalf("expected only the current turn's reply %q, got %q (prior turn leaked?)", "56", got)
	}
}

// fakeAgyEmptyStdoutScript reproduces agy 1.0.14's regressed print mode: the
// process logs its CLI app data directory and the conversation id, writes
// NOTHING to stdout, and exits 0 — the "PlannerResponse without ModifiedResponse"
// case (MUL-3726). The real reply lives only in the conversation transcript,
// which the test seeds under appDataDir.
func fakeAgyEmptyStdoutScript(appDataDir, conversationID string) string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$log" ]; then
  printf 'I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir + `\n' >> "$log"
  printf 'I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + conversationID + `, sending message\n' >> "$log"
fi
exit 0
`
}

// TestAntigravityBackendRecoversEmptyStdoutFromTranscript is the end-to-end
// guard for MUL-3726: agy 1.0.14 can complete a turn with empty stdout while the
// real reply lives only in the conversation transcript. The backend must recover
// that text into Result.Output instead of returning a blank "completed" run.
func TestAntigravityBackendRecoversEmptyStdoutFromTranscript(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "44a57718-801c-41e7-9691-3225be2b1cb8"
	seedAntigravityTranscript(t, appDataDir, cid, []string{
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"create result.txt"}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":null}`,
		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":3}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":4,"content":"I read marker.txt and created result.txt with VERIFIED=yes."}`,
	})

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyEmptyStdoutScript(appDataDir, cid)))

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

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, "created result.txt with VERIFIED=yes") {
			t.Fatalf("expected transcript reply recovered into output, got %q", result.Output)
		}
		// content=null tool steps must not leak the literal "null" into output.
		if strings.Contains(result.Output, "null") {
			t.Errorf("null tool-step content leaked into output: %q", result.Output)
		}
		if result.SessionID != cid {
			t.Errorf("expected session id %q recovered from log, got %q", cid, result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	var recoveredMessage bool
	for _, msg := range messages {
		if msg.Type == MessageText && strings.Contains(msg.Content, "created result.txt with VERIFIED=yes") {
			recoveredMessage = true
			break
		}
	}
	if !recoveredMessage {
		t.Fatalf("expected recovered transcript reply to be emitted as MessageText, got %#v", messages)
	}
}

// fakeAgyMarkdownReplyScript streams a blank-line-separated markdown reply to
// stdout — a heading and a list, exactly the shape that renders as literal
// `###`/`-` in chat when the streamed text loses its newlines (#6149).
func fakeAgyMarkdownReplyScript() string {
	return `#!/bin/sh
cat <<'AGYREPLY'
Intro line.

### Heading

- item one
- item two
AGYREPLY
exit 0
`
}

// TestAntigravityBackendStreamPreservesNewlines is the regression guard for
// #6149: the daemon concatenates streamed MessageText with no separator
// (pendingText.WriteString), so the adapter must emit the line breaks itself.
// Before the fix each line was streamed without its "\n", so the reconstructed
// task_message text glued every block onto one line and block markdown
// (headings, lists) rendered as literal text.
func TestAntigravityBackendStreamPreservesNewlines(t *testing.T) {
	t.Parallel()

	const wantReply = "Intro line.\n\n### Heading\n\n- item one\n- item two"

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyMarkdownReplyScript()))

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

	// Reconstruct the persisted text the way the daemon does: concatenate every
	// streamed MessageText with no separator.
	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	if got := streamed.String(); got != wantReply {
		t.Fatalf("streamed task_message text lost its line breaks (#6149):\n got  %q\n want %q", got, wantReply)
	}
}

func fakeAgyStreamJSONScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -z "$log" ] || printf '%s\n' 'I0903 12:00:00.000000 1 model_config_manager.go:311] Propagating selected model override to backend: label="Gemini 3.8 Flash (High)"' >> "$log"
cat <<'JSONL'
{"event":"init","conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","init":{}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hello"}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"\n","usage":{"input_tokens":1000,"output_tokens":50,"thinking_tokens":30,"cache_read_tokens":400,"cache_write_tokens":0,"total_tokens":1050}}}
{"event":"step_update","step_update":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":3,"state":"DONE","step_type":"checkpoint","usage":{"input_tokens":100,"output_tokens":4,"thinking_tokens":0,"cache_read_tokens":0,"cache_write_tokens":0,"total_tokens":104}}}
{"event":"result","result":{"conversation_id":"07a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"SUCCESS","response":"Hello\n","usage":{"input_tokens":5000,"output_tokens":200,"thinking_tokens":120,"cache_read_tokens":1200,"cache_write_tokens":0,"total_tokens":5200}}}
JSONL
exit 0
`
}

func fakeAgyFailedStreamJSONScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"result","result":{"conversation_id":"17a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","error":"provider failed","usage":{"input_tokens":12,"output_tokens":3,"thinking_tokens":2,"cache_read_tokens":7,"cache_write_tokens":2,"total_tokens":15}}}'
exit 1
`
}

func fakeAgyTrailingNetworkErrorScript(responseDone bool) string {
	state := "ACTIVE"
	if responseDone {
		state = "DONE"
	}
	return fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"event":"step_update","step_update":{"conversation_id":"37a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"%s","step_type":"agent_response","text_delta":"Complete answer."}}'
printf '%%s\n' '{"event":"result","result":{"conversation_id":"37a6d8f2-8523-46fc-8fc9-87e630cbe295","status":"ERROR","response":"Complete answer.","error":"There was a network issue connecting to the server, please try again."}}'
exit 1
`, state)
}

func fakeAgyCancelledStreamJSONScript() string {
	return `#!/bin/sh
printf '%s\n' '{"event":"init","conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","init":{"model":"gemini-3.8-flash-high"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"tool","usage":{"input_tokens":100,"output_tokens":10,"cache_read_tokens":20,"total_tokens":110}}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":1,"state":"DONE","step_type":"tool","usage":{"input_tokens":120,"output_tokens":12,"cache_read_tokens":20,"total_tokens":132}}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"27a6d8f2-8523-46fc-8fc9-87e630cbe295","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"working","usage":{"input_tokens":50,"output_tokens":5,"cache_read_tokens":10,"total_tokens":55}}}'
sleep 60
`
}

func TestAntigravityBackendStreamJSONCapturesUsage(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyStreamJSONScript()))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Hello\n" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
	if streamed.String() != "Hello\n" {
		t.Fatalf("streamed output = %q, want %q", streamed.String(), "Hello\n")
	}
	if result.SessionID != "07a6d8f2-8523-46fc-8fc9-87e630cbe295" {
		t.Fatalf("session id = %q", result.SessionID)
	}
	usage, ok := result.Usage["Gemini 3.8 Flash (High)"]
	if !ok {
		t.Fatalf("usage missing model key: %#v", result.Usage)
	}
	// result.usage is cumulative across a resumed conversation. The two DONE
	// steps are this execution's usage and must win without double-counting the
	// terminal aggregate.
	want := TokenUsage{InputTokens: 1100, OutputTokens: 54, CacheReadTokens: 400}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

func TestAntigravityBackendIgnoresTrailingNetworkErrorAfterDoneResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorScript(true)))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "completed" || result.Output != "Complete answer." || result.Error != "" {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

func TestAntigravityBackendKeepsNetworkFailureForPartialResponse(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyTrailingNetworkErrorScript(false)))

	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result, ok := <-session.Result
	if !ok {
		t.Fatal("result channel closed without a value")
	}
	if result.Status != "failed" || result.Output != "Complete answer." || !strings.Contains(result.Error, antigravityNetworkIssueError) {
		t.Fatalf("result = status %q output %q error %q", result.Status, result.Output, result.Error)
	}
}

func TestAntigravityStreamUsageMapsProviderBucketsDirectly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  antigravityStreamUsage
		want TokenUsage
	}{
		{
			name: "official example cache larger than input",
			raw:  antigravityStreamUsage{InputTokens: 278, OutputTokens: 4, CacheReadTokens: 30214, TotalTokens: 282},
			want: TokenUsage{InputTokens: 278, OutputTokens: 4, CacheReadTokens: 30214},
		},
		{
			name: "official example cache smaller than input",
			raw:  antigravityStreamUsage{InputTokens: 10418, OutputTokens: 589, CacheReadTokens: 8113, TotalTokens: 11007},
			want: TokenUsage{InputTokens: 10418, OutputTokens: 589, CacheReadTokens: 8113},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.raw.tokenUsage(); got != tt.want {
				t.Fatalf("tokenUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadAntigravitySelectedModelPrecedence(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(
		`I0903 12:00:00 model_config_manager.go:311] Propagating selected model override to backend: label="Gemini 3.8 Flash (High)"`+"\n",
	), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if got := readAntigravitySelectedModel(logPath, "gemini-stream-model", "gemini-configured-model"); got != "gemini-stream-model" {
		t.Fatalf("stream model = %q", got)
	}
	if got := readAntigravitySelectedModel(logPath, "", "gemini-configured-model"); got != "Gemini 3.8 Flash (High)" {
		t.Fatalf("log model = %q", got)
	}
	if got := readAntigravitySelectedModel("", "", "gemini-configured-model"); got != "gemini-configured-model" {
		t.Fatalf("configured model = %q", got)
	}
	if got := readAntigravitySelectedModel("", "", ""); got != "unknown" {
		t.Fatalf("fallback model = %q", got)
	}
}

func TestAntigravityResultStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"SUCCESS":        "completed",
		"completed":      "completed",
		"CANCELLED":      "cancelled",
		"canceled":       "cancelled",
		"ABORTED":        "aborted",
		"TIMEOUT":        "timeout",
		"timed_out":      "timeout",
		"provider_error": "failed",
	}
	for input, want := range tests {
		if got := antigravityResultStatus(input); got != want {
			t.Errorf("antigravityResultStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAntigravityBackendReportsStructuredFailureUsage(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyFailedStreamJSONScript()))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	session, err := backend.Execute(context.Background(), "prompt-ignored", ExecOptions{Model: ""})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || result.Error != "provider failed" {
		t.Fatalf("result = status %q error %q", result.Status, result.Error)
	}
	want := TokenUsage{InputTokens: 12, OutputTokens: 3, CacheReadTokens: 7, CacheWriteTokens: 2}
	if got := result.Usage["unknown"]; got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestAntigravityBackendSumsCompletedStepsWhenCancelled(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeAgyCancelledStreamJSONScript()))
	backend, err := New("antigravity", Config{ExecutablePath: fakePath, Logger: quietAntigravityLogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "working" {
			cancel()
		}
	}
	result := <-session.Result
	if result.Status != "aborted" {
		t.Fatalf("status = %q, want aborted (error=%q)", result.Status, result.Error)
	}
	// step 1's second DONE event replaces its first snapshot; step 2 is then
	// added once. Provider buckets retain their documented meanings.
	want := TokenUsage{InputTokens: 170, OutputTokens: 17, CacheReadTokens: 30}
	if got := result.Usage["gemini-3.8-flash-high"]; got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}
