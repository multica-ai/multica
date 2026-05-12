package daemon

import (
	"strings"
	"testing"
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
filter excludes them from the resume lookup. Auto-retry of mid-flight
orphans still keeps the resume contract because CreateRetryTask does
not set force_fresh_session. Approving with a follow-up note about
the matcher being too permissive on long outputs.`,
			wantOK: false,
		},
		{
			name:   "marker buried inside a long agent conclusion",
			output: strings.Repeat("All checks passed and the bug is fixed. ", 10) + "i reached the iteration limit while debugging earlier.",
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
