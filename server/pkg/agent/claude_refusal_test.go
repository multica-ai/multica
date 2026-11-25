package agent

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// claudeRefusalResumeFixture is the stream-json Claude Code 2.1.280 writes when
// a session whose first message was refused is resumed with an unrelated
// prompt. Captured by running the real CLI with the daemon's flags against an
// endpoint that returns stop_reason=refusal (stop_details.category=null)
// whenever the refused message is in the request; the resume sends it again,
// so the new prompt is refused too. Only the init frame's local tool/skill
// inventory and absolute paths were stripped.
const claudeRefusalResumeFixture = "testdata/claude-code-2.1.280-refusal-resume.jsonl"

// claudeRefusalFallbackAnsweredFixture is the counterpart: the primary model
// refused (category=cyber), Claude Code retried on its fallback model, and the
// fallback answered. The turn emits model_refusal_fallback, not
// model_refusal_no_fallback, and the result carries stop_reason=end_turn.
const claudeRefusalFallbackAnsweredFixture = "testdata/claude-code-2.1.280-refusal-fallback-answered.jsonl"

func TestClaudeRefusalFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stopReason  string
		sawNoFall   bool
		isError     bool
		resultText  string
		wantFailure bool
	}{
		{
			name:        "stop_reason refusal on an error result",
			stopReason:  "refusal",
			isError:     true,
			resultText:  "API Error: Opus 5.5's safeguards flagged this message",
			wantFailure: true,
		},
		{
			name:        "stop_reason refusal without is_error still fails",
			stopReason:  "refusal",
			wantFailure: true,
		},
		{
			name: "no-fallback event on an error result without stop_reason",
			// A result frame that does not carry stop_reason still has the
			// system event the CLI emits before it.
			sawNoFall:   true,
			isError:     true,
			resultText:  "API Error: claude-opus-5-5 can't help with this.",
			wantFailure: true,
		},
		{
			name: "no-fallback event on a successful result is left alone",
			// Never observed; if a later step of the turn recovered, the
			// result decides.
			sawNoFall:  true,
			stopReason: "end_turn",
			resultText: "Done.",
		},
		{
			name:       "fallback model answered",
			stopReason: "end_turn",
			resultText: "OK",
		},
		{
			name:       "an ordinary API error is left to is_error",
			stopReason: "",
			isError:    true,
			resultText: "API Error: 529 overloaded",
		},
		{
			name:       "a normal answer that talks about refusals",
			stopReason: "end_turn",
			resultText: "The model returned stop_reason=refusal, so I added a fallback.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := claudeRefusalFailure(tc.stopReason, tc.sawNoFall, tc.isError, tc.resultText)
			if (got != "") != tc.wantFailure {
				t.Fatalf("claudeRefusalFailure(%q, %v, %v, %q) = %q, wantFailure=%v",
					tc.stopReason, tc.sawNoFall, tc.isError, tc.resultText, got, tc.wantFailure)
			}
			if !tc.wantFailure {
				return
			}
			if !strings.HasPrefix(got, ClaudeModelRefusalMarker) {
				t.Fatalf("error must start with the refusal marker, got %q", got)
			}
			if tc.resultText != "" && !strings.Contains(got, tc.resultText) {
				t.Fatalf("the CLI's own wording must survive for the user; got %q", got)
			}
		})
	}
}

// TestClaudeExecuteReportsCapturedRefusal replays the captured resume. The run
// was already reported as failed before this change; what changed is the error
// text, which now names the refusal so the daemon retires the session instead
// of filing it as agent_error.unknown and resuming it on the next comment.
func TestClaudeExecuteReportsCapturedRefusal(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	frames, err := os.ReadFile(claudeRefusalResumeFixture)
	if err != nil {
		t.Fatalf("read captured frames: %v", err)
	}
	const sessionID = "d2c9a8fb-96d0-477c-bb9e-9f3e2261fe34"
	result := runClaudeFixture(t, string(frames), sessionID)

	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q (output %q)", result.Status, result.Output)
	}
	if result.Output != "" {
		t.Fatalf("a refused run must publish no answer, got %q", result.Output)
	}
	if !strings.HasPrefix(result.Error, ClaudeModelRefusalMarker) {
		t.Fatalf("error should start with %q, got %q", ClaudeModelRefusalMarker, result.Error)
	}
	if !strings.Contains(result.Error, "safeguards flagged this message") {
		t.Fatalf("error should keep Claude Code's explanation for the user, got %q", result.Error)
	}
	// The id is needed to record which session the failure retires. The resume
	// itself was accepted, so this is not a rejected resume.
	if result.SessionID != sessionID {
		t.Fatalf("expected session id %q, got %q", sessionID, result.SessionID)
	}
	if result.ResumeRejected {
		t.Fatal("a refused turn on a resumed session is not a rejected resume")
	}
}

// TestClaudeExecuteKeepsRefusalAnsweredByFallback guards the other direction:
// when Claude Code's fallback model answers after the primary refused, the run
// is a normal success and its session stays resumable.
func TestClaudeExecuteKeepsRefusalAnsweredByFallback(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	frames, err := os.ReadFile(claudeRefusalFallbackAnsweredFixture)
	if err != nil {
		t.Fatalf("read captured frames: %v", err)
	}
	result := runClaudeFixture(t, string(frames), "")

	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error %q)", result.Status, result.Error)
	}
	if result.Output != "OK" {
		t.Fatalf("expected the fallback model's answer, got %q", result.Output)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %q", result.Error)
	}
}

// A model_refusal_no_fallback event only counts on the main thread. When a
// subagent was refused and the main thread later failed for an unrelated
// reason, the failure keeps its own text and the session is not retired.
func TestClaudeExecuteIgnoresSubagentRefusalEvent(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const rateLimited = "API Error: 429 rate_limit_error"
	result := func(parent string) string {
		event := `{"type":"system","subtype":"model_refusal_no_fallback","session_id":"sess-sub","content":""`
		if parent != "" {
			event += `,"parent_tool_use_id":"` + parent + `"`
		}
		event += "}"
		return `{"type":"system","subtype":"init","session_id":"sess-sub"}` + "\n" +
			event + "\n" +
			`{"type":"result","subtype":"success","is_error":true,"terminal_reason":"api_error","session_id":"sess-sub","result":"` + rateLimited + `"}` + "\n"
	}

	sub := runClaudeFixture(t, result("toolu_subagent"), "")
	if sub.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", sub.Status)
	}
	if strings.HasPrefix(sub.Error, ClaudeModelRefusalMarker) {
		t.Fatalf("a subagent's refusal must not mark the turn as refused, got %q", sub.Error)
	}
	if !strings.Contains(sub.Error, rateLimited) {
		t.Fatalf("the main thread's own error must be reported, got %q", sub.Error)
	}

	main := runClaudeFixture(t, result(""), "")
	if !strings.HasPrefix(main.Error, ClaudeModelRefusalMarker) {
		t.Fatalf("a main-thread refusal event on an error result must mark the turn as refused, got %q", main.Error)
	}
}
