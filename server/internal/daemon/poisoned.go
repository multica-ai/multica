package daemon

import (
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// FailureReason values for tasks whose session is "poisoned" — i.e.
// resuming the same conversation on a follow-up task would deterministically
// reproduce the same failure. Listed here so the server-side query
// GetLastTaskSession can filter them out and the next task starts from
// a fresh agent session instead of inheriting the bad state.
//
// Two flavors:
//   - Output-side: agent "completed" with output that is actually a known
//     fallback marker (gave up mid-thought, emitted a meta message). Detected
//     via classifyPoisonedOutput.
//   - Error-side: the LLM API itself rejected the request with a 400
//     invalid_request_error (oversized payload, malformed image, etc.).
//     The bad message is already baked into the conversation history, so
//     every resume hits the same 400. Detected via classifyPoisonedError.
//   - Timeout-side: Codex reported semantic inactivity after the session got
//     stuck without agent progress. Resuming that Codex session can replay the
//     same stuck state, while a fresh manual rerun may succeed. Detected via
//     classifyResumeUnsafeTimeout.
const (
	FailureReasonIterationLimit          = "iteration_limit"
	FailureReasonAgentFallbackMsg        = "agent_fallback_message"
	FailureReasonAPIInvalidRequest       = "api_invalid_request"
	FailureReasonCodexSemanticInactivity = "codex_semantic_inactivity"
	FailureReasonAgentTransient          = "agent_transient"
)

// poisonedOutputMaxLen caps how long an output can be and still be
// classified as a poisoned fallback. Real fallback messages are short,
// one-sentence affairs; a long output that happens to mention a marker
// is almost certainly a real conclusion (e.g. a code-review reply
// quoting these strings, like the one currently quoting them in
// MUL-1630). The cap intentionally errs on the side of NOT classifying
// — a missed poisoned task gets retried by user action, but a
// false-positive turns a successful task into a failure and a system
// comment.
const poisonedOutputMaxLen = 320

// poisonedMarkers maps a substring fingerprint of a known agent fallback
// terminal message to its failure_reason classifier. Match is case-
// insensitive and substring-based; the cap above prevents long outputs
// that quote a marker from being misclassified.
var poisonedMarkers = []struct {
	Substring string
	Reason    string
}{
	{"i reached the iteration limit", FailureReasonIterationLimit},
	{"put your final update inside the content string", FailureReasonAgentFallbackMsg},
}

// classifyPoisonedOutput reports whether output matches a known agent
// fallback terminal message and, if so, returns the failure_reason that
// should be persisted on the task row. Long outputs are never
// classified: a real fallback is the agent's only utterance for the
// turn, so anything beyond ~one paragraph is treated as a real result
// even if it contains a marker substring.
func classifyPoisonedOutput(output string) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > poisonedOutputMaxLen {
		return "", false
	}
	lowered := strings.ToLower(trimmed)
	for _, m := range poisonedMarkers {
		if strings.Contains(lowered, m.Substring) {
			return m.Reason, true
		}
	}
	return "", false
}

// classifyPoisonedError reports whether an agent error message indicates
// the LLM API itself rejected the request body — i.e. the conversation
// history contains content the API will not accept (oversized image,
// malformed base64, prompt-too-long, etc.). The conversation cannot be
// resumed: every retry replays the same body and reproduces the same 400.
// The classifier returns FailureReasonAPIInvalidRequest so GetLastTaskSession
// excludes the task from the (agent_id, issue_id) resume lookup, and the
// next task on the issue starts a fresh session instead of permanently
// inheriting the bad state.
//
// Match shape: the Claude Code SDK and similar backends surface upstream
// API failures verbatim, e.g.
//
//	API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"},"request_id":"..."}
//
// Matching on both "400" and "invalid_request_error" keeps the classifier
// narrow: 429 rate-limits, 5xx overloads, and tool-shaped errors are
// transient and SHOULD resume on retry.
func classifyPoisonedError(errMsg string) (string, bool) {
	if errMsg == "" {
		return "", false
	}
	lowered := strings.ToLower(errMsg)
	// Both markers must be present: "400" alone is too generic (a tool
	// could surface a 400 from anywhere) and "invalid_request_error"
	// alone could in theory appear in non-poisoning contexts. The
	// combination is the canonical Anthropic error shape and indicates
	// the request body — i.e. the conversation history — is the problem.
	if strings.Contains(lowered, "invalid_request_error") && strings.Contains(lowered, "400") {
		return FailureReasonAPIInvalidRequest, true
	}
	return "", false
}

// permanentErrorPatterns are terminal failure shapes that must not become
// retryable even when the message also contains a generic transient-looking
// word. Keep this list conservative: false negatives surface to users, while
// false positives can create noisy retry loops.
var permanentErrorPatterns = []string{
	"authentication_error",
	"invalid api key",
	"unauthorized",
	"forbidden",
	"permission denied",
	"not permitted",
	"configuration error",
	"not configured",
	"missing api key",
	"content policy",
	"content_policy",
	"content blocked",
	"invalid_request_error",
	"invalid request",
	"bad request",
}

// transientErrorPatterns are infrastructure/provider interruption markers.
// Some strings come from external CLI stdout/stderr rather than Go literals
// in this repository, so absence from ripgrep is not enough to delete them.
var transientErrorPatterns = []string{
	"database is locked",
	"database locked",
	"sqlite busy",
	"sqlite_busy",
	"context deadline exceeded",
	"i/o timeout",
	"timed out",
	"timeout exceeded",
	"deadline exceeded",
	"returned empty output",
	"empty output",
	"empty response",
	"no output from",
	"produced no output",
	"no parseable output",
	"stream interrupted",
	"stream closed",
	"connection reset",
	"connection refused",
	"connection aborted",
	"broken pipe",
	"unexpected eof",
	"temporary failure",
	"temporarily unavailable",
	"try again",
	"rate limit",
	"rate_limit",
	"too many requests",
	"api error: 429",
	"status 429",
	" 429 ",
	"api error: 502",
	"status 502",
	" 502 ",
	"api error: 503",
	"status 503",
	" 503 ",
	"api error: 504",
	"status 504",
	" 504 ",
	"overloaded_error",
	"overloaded",
	"service unavailable",
	"bad gateway",
	"gateway timeout",
}

// classifyTransientError reports whether an agent error is retryable
// infrastructure/provider flakiness. It intentionally excludes auth,
// permission, configuration, content-policy, and invalid-request failures.
func classifyTransientError(errMsg string) (string, bool) {
	trimmed := strings.TrimSpace(errMsg)
	if trimmed == "" {
		return "", false
	}
	if _, ok := classifyPoisonedError(trimmed); ok {
		return "", false
	}
	lowered := strings.ToLower(trimmed)
	for _, marker := range permanentErrorPatterns {
		if strings.Contains(lowered, marker) {
			return "", false
		}
	}
	for _, marker := range transientErrorPatterns {
		if strings.Contains(lowered, marker) {
			return FailureReasonAgentTransient, true
		}
	}
	return "", false
}

// classifyResumeUnsafeTimeout reports whether a timeout means the recorded
// session should not be resumed. Keep this intentionally provider-specific:
// ordinary daemon/backend timeouts are infrastructure-shaped and should keep
// the resume pointer so retries can continue the in-flight conversation.
func classifyResumeUnsafeTimeout(provider, errMsg string) (string, bool) {
	if strings.ToLower(strings.TrimSpace(provider)) != "codex" || errMsg == "" {
		return "", false
	}
	lowered := strings.ToLower(errMsg)
	if strings.Contains(lowered, strings.ToLower(agent.CodexSemanticInactivityMarker)) ||
		strings.Contains(lowered, strings.ToLower(agent.CodexFirstTurnNoProgressMarker)) {
		return FailureReasonCodexSemanticInactivity, true
	}
	return "", false
}
