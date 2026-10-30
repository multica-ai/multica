package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestClaudeTerminalReasonFailure covers the mapping in isolation: only the one
// terminal reason that has been observed escaping as a success is recognised,
// and the message it produces has to classify as a context overflow.
func TestClaudeTerminalReasonFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		terminalReason string
		resultText     string
		wantFailure    bool
	}{
		{
			name:           "prompt_too_long is a failure",
			terminalReason: "prompt_too_long",
			resultText:     "Prompt is too long",
			wantFailure:    true,
		},
		{
			name:           "prompt_too_long with no prose",
			terminalReason: "prompt_too_long",
			wantFailure:    true,
		},
		{
			name:           "surrounding whitespace still matches",
			terminalReason: " prompt_too_long ",
			wantFailure:    true,
		},
		{
			name:           "a normal completion is untouched",
			terminalReason: "completed",
			resultText:     "Done — pushed the fix.",
		},
		{
			name: "an absent field is untouched",
			// Every backend and every CLI version that does not emit the field
			// keeps the pre-existing is_error contract.
			terminalReason: "",
			resultText:     "Done — pushed the fix.",
		},
		{
			name: "other terminal reasons are left to is_error",
			// These already arrive with is_error set; claiming them here would
			// second-guess a contract that works.
			terminalReason: "max_turns",
			resultText:     "Reached maximum number of turns (10)",
		},
		{
			name:           "api_error is left to is_error",
			terminalReason: "api_error",
			resultText:     "API Error: 500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeTerminalReasonFailure(tc.terminalReason, tc.resultText)
			if (got != "") != tc.wantFailure {
				t.Fatalf("claudeTerminalReasonFailure(%q, %q) = %q, wantFailure=%v",
					tc.terminalReason, tc.resultText, got, tc.wantFailure)
			}
			if !tc.wantFailure {
				return
			}
			if reason := taskfailure.Classify(got); reason != taskfailure.ReasonAgentContextOverflow {
				t.Fatalf("error %q classifies as %q, want %q", got, reason, taskfailure.ReasonAgentContextOverflow)
			}
			if tc.resultText != "" && !strings.Contains(got, tc.resultText) {
				t.Fatalf("the provider's own wording must survive for diagnosis; got %q", got)
			}
		})
	}
}

// claudeContextExhaustedFixture is the verbatim stream-json transcript Claude
// Code 2.1.220 — the version GH #6402 reports — writes when a saturated session
// is resumed. Captured by resuming a saturated transcript against an endpoint
// returning the provider's 400 prompt-too-long shape; only the init frame's
// local tool/skill inventory and absolute paths were stripped.
const claudeContextExhaustedFixture = "testdata/claude-code-2.1.220-context-exhausted-resume.jsonl"

// TestClaudeExecuteFailsOnCapturedContextExhaustion replays that transcript
// unedited.
//
// What it pins, and why it is not merely "is_error already handled this": the
// captured terminal frame sets is_error AND terminal_reason together. The
// is_error path alone labels the failure with whatever prose the CLI put in
// `result` — here "Prompt is too long", which Classify happens to recognise.
// The next two tests take that luck away.
func TestClaudeExecuteFailsOnCapturedContextExhaustion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	frames, err := os.ReadFile(claudeContextExhaustedFixture)
	if err != nil {
		t.Fatalf("read captured frames: %v", err)
	}
	result := runClaudeFixture(t, string(frames), "7ff8bf88-4d29-47f6-8c54-87f7ec0b080b")

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (output %q)", result.Status, result.Output)
	}
	// A failed run must publish no answer at all — the whole defect was the
	// provider's notice reaching the issue as the agent's conclusion.
	if result.Output != "" {
		t.Fatalf("expected no output on a failed run, got %q", result.Output)
	}
	if reason := taskfailure.Classify(result.Error); reason != taskfailure.ReasonAgentContextOverflow {
		t.Fatalf("error %q classifies as %q, want %q", result.Error, reason, taskfailure.ReasonAgentContextOverflow)
	}
	// The failure has to name the structured reason, not just carry prose that
	// happens to classify — that is what makes the label survive a CLI reword.
	if !strings.Contains(result.Error, taskfailure.TerminalReasonPromptTooLong) {
		t.Fatalf("error should quote the structured terminal reason, got %q", result.Error)
	}
	// The session id must still be reported. It is not a rejected resume — the
	// transcript loaded fine, it is simply full — and the daemon needs the id
	// to record which session the failure retires.
	if result.SessionID != "7ff8bf88-4d29-47f6-8c54-87f7ec0b080b" {
		t.Fatalf("expected the session id to be reported, got %q", result.SessionID)
	}
	if result.ResumeRejected {
		t.Fatal("a full context window is not a rejected resume")
	}
}

// TestClaudeExecuteClassifiesContextExhaustionWithoutUsableProse is the
// regression for the ordering bug: with is_error checked first, a frame whose
// `result` is empty or reworded falls back to the generic
// "returned an error result without details" and classifies as
// agent_error.unknown — a reason no resume blacklist covers, so the saturated
// session stays pinned and #6402's stall returns. The structured field is
// present in both cases and has to decide.
func TestClaudeExecuteClassifiesContextExhaustionWithoutUsableProse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "empty result text",
			frame: `{"type":"result","subtype":"success","is_error":true,"terminal_reason":"prompt_too_long","api_error_status":400,"session_id":"sess-full","result":""}`,
		},
		{
			name: "prose reworded by a future release",
			// Carries none of Classify's context-overflow phrases, which is
			// exactly the drift the structured field exists to survive.
			frame: `{"type":"result","subtype":"success","is_error":true,"terminal_reason":"prompt_too_long","api_error_status":400,"session_id":"sess-full","result":"This chat has gotten too big to continue."}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			frames := `{"type":"system","subtype":"init","session_id":"sess-full"}` + "\n" + tc.frame + "\n"
			result := runClaudeFixture(t, frames, "sess-full")

			if result.Status != "failed" {
				t.Fatalf("expected status=failed, got %q", result.Status)
			}
			if reason := taskfailure.Classify(result.Error); reason != taskfailure.ReasonAgentContextOverflow {
				t.Fatalf("error %q classifies as %q, want %q — the session would not be retired",
					result.Error, reason, taskfailure.ReasonAgentContextOverflow)
			}
		})
	}
}

// TestClaudeExecuteKeepsIsErrorFailuresUnlabelled is the counterpart: a failure
// with NO recognised terminal reason still reports the CLI's own error text,
// unchanged. Guards against the reordering swallowing every other failure's
// diagnostics.
func TestClaudeExecuteKeepsIsErrorFailuresUnlabelled(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	frames := `{"type":"system","subtype":"init","session_id":"sess-x"}` + "\n" +
		`{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","session_id":"sess-x","result":"API Error: 529 overloaded"}` + "\n"
	result := runClaudeFixture(t, frames, "")

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "529 overloaded") {
		t.Fatalf("the CLI's own error text must survive, got %q", result.Error)
	}
	if reason := taskfailure.Classify(result.Error); reason != taskfailure.ReasonAgentProviderCapacityOrRateLimit {
		t.Fatalf("error %q classifies as %q, want %q", result.Error, reason, taskfailure.ReasonAgentProviderCapacityOrRateLimit)
	}
}

// runClaudeFixture runs the claude backend against a fake executable that
// replays `frames` on stdout, and returns the terminal Result.
func runClaudeFixture(t *testing.T, frames, resumeSessionID string) Result {
	t.Helper()

	fixturePath := filepath.Join(t.TempDir(), "frames.jsonl")
	if err := os.WriteFile(fixturePath, []byte(frames), 0o600); err != nil {
		t.Fatalf("write frames: %v", err)
	}
	fakePath := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakePath, []byte("#!/bin/sh\nIFS= read -r _\ncat "+fixturePath+"\n"))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{
		ResumeSessionID: resumeSessionID,
		Timeout:         5 * time.Second,
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
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
		return Result{}
	}
}

// The counterpart: a genuinely successful run that reports terminal_reason
// "completed" keeps completing. Guards against the new check swallowing normal
// traffic.
func TestClaudeExecuteKeepsCompletedTerminalReason(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		`echo '{"type":"system","subtype":"init","session_id":"sess-ok"}'` + "\n" +
		`echo '{"type":"result","subtype":"success","is_error":false,"terminal_reason":"completed","session_id":"sess-ok","result":"Fixed the redirect and pushed."}'` + "\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
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
			t.Fatalf("expected status=completed, got %q (error %q)", result.Status, result.Error)
		}
		if result.Output != "Fixed the redirect and pushed." {
			t.Fatalf("unexpected output %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
