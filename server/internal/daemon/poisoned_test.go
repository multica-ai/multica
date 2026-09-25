package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestClassifyPoisonedOutput(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "iteration limit canonical",
			output:     "I reached the iteration limit and couldn't generate a summary.",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "iteration limit case insensitive",
			output:     "I REACHED THE ITERATION LIMIT and stopped",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "fallback meta message",
			output:     "Put your final update inside the content string. Keep it concise.",
			wantOK:     true,
			wantReason: FailureReasonAgentFallbackMsg,
		},
		{
			name:   "real conclusion is not poisoned",
			output: "Fixed the bug in auth.go and pushed PR #42.",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
		{
			name:   "mentions iteration but not the marker",
			output: "Each iteration of the loop processes one record.",
			wantOK: false,
		},
		{
			// Regression guard for the GPT-Boy review on MUL-1630:
			// a real review/analysis that quotes both markers must not
			// be misclassified. Without the length cap, this entire
			// PR's review thread would tank as a poisoned failure.
			name: "long review quoting both markers is not poisoned",
			output: `Review for the rerun fix.

Detection markers under consideration:
- "I reached the iteration limit and couldn't generate a summary."
- "Put your final update inside the content string. Keep it concise."

The implementation looks correct: the daemon classifies these as
fallback output, persists a dedicated failure_reason, and the SQL
filter excludes them from the resume lookup. Resume-safe auto-retry
still keeps the resume contract, while poisoned sessions are filtered.
Approving with a follow-up note about the matcher being too permissive
on long outputs.`,
			wantOK: false,
		},
		{
			name:   "marker buried inside a long agent conclusion",
			output: strings.Repeat("All checks passed and the bug is fixed. ", 10) + "i reached the iteration limit while debugging earlier.",
			wantOK: false,
		},
		{
			// GH #6402: the provider's context-exhaustion notice arriving as
			// the run's successful answer. Same poisoning shape as the markers
			// above — a session already over the limit cannot compact its way
			// back under, so every resume reproduces it — and the reason has to
			// be the one the resume blacklist covers.
			name:       "context exhaustion with the provider's full wording",
			output:     "Prompt is too long · the request is ~274931 tokens (limit 200000) but this conversation is only ~1597 tokens — the rest is system prompt, tool definitions, and attachment content. A single-exchange conversation cannot be compacted; reduce attached files/tools or start with less context.",
			wantOK:     true,
			wantReason: string(taskfailure.ReasonAgentContextOverflow),
		},
		{
			name:       "compaction exhausted",
			output:     "Compaction failed · conversation could not be reduced below the context limit",
			wantOK:     true,
			wantReason: string(taskfailure.ReasonAgentContextOverflow),
		},
		{
			name:   "an agent discussing /compact is a real answer",
			output: "The session is getting long; run /compact before the next batch.",
			wantOK: false,
		},
		{
			// The CLI's bare sentence is not matched on the success path: an
			// agent asked about prompt length can answer exactly this, and the
			// real provider frame always carries is_error, so it reaches the
			// failure path where Classify already handles it.
			name:   "bare provider sentence is left to the failure path",
			output: "Prompt is too long",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyPoisonedOutput(tc.output)
			if ok != tc.wantOK {
				t.Fatalf("classifyPoisonedOutput(%q) ok=%v, want %v", tc.output, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyPoisonedOutput(%q) reason=%q, want %q", tc.output, reason, tc.wantReason)
			}
		})
	}
}

func TestClassifyPoisonedError(t *testing.T) {
	cases := []struct {
		name       string
		errMsg     string
		wantOK     bool
		wantReason string
	}{
		{
			// MUL-1921 reproducer: a markdown image in the issue
			// description was downloaded as a 146-byte CDN auth-error
			// XML, then surfaced to the LLM as a base64 PNG. The API
			// rejected it and every follow-up task replayed the same
			// poisoned conversation.
			name:       "claude could not process image",
			errMsg:     `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"},"request_id":"req_011CarVEtBLj95zD7i8xardY"}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			name:       "prompt too long is also poisoning",
			errMsg:     `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 213000 tokens > 200000 maximum"}}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			name:       "case insensitive",
			errMsg:     `api error: 400 {"type":"INVALID_REQUEST_ERROR"}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			// Rate-limit must NOT be classified as poisoning — those
			// recover on retry and we want session resume to keep the
			// in-flight conversation memory.
			name:   "429 rate limit is transient",
			errMsg: `API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`,
			wantOK: false,
		},
		{
			name:   "5xx overloaded is transient",
			errMsg: `API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			wantOK: false,
		},
		{
			// 401/403 mean the daemon's credentials are bad; resuming
			// the session won't fix it but the failure is environmental,
			// not a poisoned conversation. Out of scope for this
			// classifier.
			name:   "401 auth error",
			errMsg: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`,
			wantOK: false,
		},
		{
			// A tool surfacing a 400 from somewhere unrelated must not
			// trigger the classifier — only the combination of 400 +
			// invalid_request_error indicates a corrupted body.
			name:   "tool 400 without invalid_request_error",
			errMsg: `agent tool returned status 400: not found`,
			wantOK: false,
		},
		{
			name:   "empty error message",
			errMsg: "",
			wantOK: false,
		},
		{
			name:   "unrelated execution error",
			errMsg: "claude execution timeout after 10m",
			wantOK: false,
		},
		{
			// GH #6066: a run killed mid-reply left an empty assistant
			// message in the transcript. The provider refuses to replay it
			// and words the refusal with neither "invalid_request_error"
			// nor a bare "400", so before taskfailure.UnresumableHistory
			// this landed in agent_error.unknown — resume-safe by omission
			// — and every later task on the issue resumed the dead session.
			name:       "gh6066 empty assistant message in history",
			errMsg:     "Invalid request: the message at position 37 with role 'assistant' must not be empty",
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			// GH #5760: the same defect on Kimi/ACP. Has a "400" but no
			// "invalid_request_error", so the Anthropic clause missed it too.
			name:       "gh5760 kimi empty assistant message",
			errMsg:     "kimi provider error: provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty",
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			// The narrowness guard: an emptiness complaint with no locator
			// into the message history is some tool's validation error, and
			// discarding a healthy session over it would lose real context.
			name:   "tool validation emptiness is not poisoning",
			errMsg: "validation error: field must not be empty",
			wantOK: false,
		},
		{
			// GH #5975: a Kiro resume rejected because the session
			// history replays an image over the provider's max pixel
			// dimensions. The conversation is unresumable, so it must be
			// classified api_invalid_request even though the error is a
			// -32603 "Internal error" (no 400 / invalid_request_error).
			name:       "kiro oversized history image",
			errMsg:     `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			// A plain -32603 "Internal error" (the transient close
			// handshake) shares the code but names neither image marker,
			// so it must NOT be classified as poisoning.
			name:   "plain kiro internal error is not poisoning",
			errMsg: `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Kiro failed to generate a response)`,
			wantOK: false,
		},
		{
			// The dimension phrase alone (without the image-content
			// marker) is too weak to classify as a poisoned history.
			name:   "dimension phrase without image-content marker",
			errMsg: `some tool reported: image dimensions exceed max allowed size: 8000 pixels`,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyPoisonedError(tc.errMsg)
			if ok != tc.wantOK {
				t.Fatalf("classifyPoisonedError(%q) ok=%v, want %v", tc.errMsg, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyPoisonedError(%q) reason=%q, want %q", tc.errMsg, reason, tc.wantReason)
			}
		})
	}
}

func TestClassifyResumeUnsafeTransport(t *testing.T) {
	// The exact string the codex backend produces: startOrResumeThread's
	// "codex thread/resume failed: %w" wrapping the reader goroutine's
	// errCodexProcessExited + bufio.ErrTooLong.
	const overflowErr = "codex thread/resume failed: codex process exited: bufio.Scanner: token too long"

	cases := []struct {
		name       string
		provider   string
		errMsg     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "codex resume overflow",
			provider:   "codex",
			errMsg:     overflowErr,
			wantOK:     true,
			wantReason: FailureReasonCodexResumeOversized,
		},
		{
			// Overflow on a different RPC says nothing about the session:
			// dropping the resume pointer would discard a healthy thread.
			name:     "overflow outside a resume stays resumable",
			provider: "codex",
			errMsg:   "codex thread/start failed: codex process exited: bufio.Scanner: token too long",
			wantOK:   false,
		},
		{
			// An ordinary resume rejection is already handled by the fresh
			// -session fallback and must not be retired through this path.
			name:     "plain resume failure stays resumable",
			provider: "codex",
			errMsg:   "codex thread/resume failed: thread not found",
			wantOK:   false,
		},
		{
			// Only codex replays its whole history through one line.
			name:     "other provider same text is not classified",
			provider: "claude",
			errMsg:   overflowErr,
			wantOK:   false,
		},
		{
			name:     "empty error",
			provider: "codex",
			errMsg:   "",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyResumeUnsafeTransport(tc.provider, tc.errMsg)
			if ok != tc.wantOK {
				t.Fatalf("classifyResumeUnsafeTransport(%q, %q) ok=%v, want %v", tc.provider, tc.errMsg, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyResumeUnsafeTransport(%q, %q) reason=%q, want %q", tc.provider, tc.errMsg, reason, tc.wantReason)
			}
		})
	}
}

// TestCodexResumeOversizedIsResumeUnsafe pins the cross-package contract that
// makes the classifier above worth anything: classifying the failure only helps
// if the resume lookup actually treats the reason as unsafe. The service-side
// list and the two SQL blacklists are edited by hand in three places, so this
// asserts the Go half rather than trusting that all three stayed in sync.
func TestCodexResumeOversizedIsResumeUnsafe(t *testing.T) {
	if !service.ResumeUnsafeFailure(FailureReasonCodexResumeOversized, "") {
		t.Fatalf("ResumeUnsafeFailure(%q) = false, want true — the reason is classified but the session would still be resumed",
			FailureReasonCodexResumeOversized)
	}
}

func TestClassifyResumeUnsafeTimeout(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		errMsg     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "codex semantic inactivity",
			provider:   "codex",
			errMsg:     agent.CodexSemanticInactivityMarker + " after 10m0s without agent progress (last activity: tool-result:exec_command)",
			wantOK:     true,
			wantReason: FailureReasonCodexSemanticInactivity,
		},
		{
			name:       "codex first turn no progress",
			provider:   "codex",
			errMsg:     agent.CodexFirstTurnNoProgressMarker + ` after 30s: received turn start but no item, turn/completed, or error event`,
			wantOK:     true,
			wantReason: FailureReasonCodexSemanticInactivity,
		},
		{
			name:     "codex ordinary timeout remains resumable",
			provider: "codex",
			errMsg:   "codex timed out after 30m0s",
			wantOK:   false,
		},
		{
			name:     "other provider same text is not classified",
			provider: "claude",
			errMsg:   agent.CodexSemanticInactivityMarker + " after 10m0s without agent progress",
			wantOK:   false,
		},
		{
			name:     "empty error",
			provider: "codex",
			errMsg:   "",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyResumeUnsafeTimeout(tc.provider, tc.errMsg)
			if ok != tc.wantOK {
				t.Fatalf("classifyResumeUnsafeTimeout(%q, %q) ok=%v, want %v", tc.provider, tc.errMsg, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyResumeUnsafeTimeout(%q, %q) reason=%q, want %q", tc.provider, tc.errMsg, reason, tc.wantReason)
			}
		})
	}
}
