package daemon

import (
	"errors"
	"strings"
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
const (
	FailureReasonIterationLimit       = "iteration_limit"
	FailureReasonAgentFallbackMsg     = "agent_fallback_message"
	FailureReasonAPIInvalidRequest    = "api_invalid_request"
	FailureReasonInfrastructureError = "infrastructure_error"
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

// classifyInfrastructureError inspects the given error and returns
// FailureReasonInfrastructureError when the error indicates a platform
// infrastructure failure that the caller should NOT classify as an
// agent error. This function is intended to be called ONLY on errors
// that originate from daemon platform operations (server HTTP calls,
// daemon-local filesystem I/O) — NOT on errors reported by the agent
// itself. This call-site discipline is the boundary that distinguishes
// platform SQLite / infrastructure errors from application SQLite /
// agent errors without relying on bare string matching.
//
// Recognised patterns:
//   - *requestError with status >= 500 (server internal error,
//     which includes DB lock contention, deadlocks, overload).
//   - *requestError whose body text contains a known infrastructure
//     keyword (e.g. "database is locked"). This is defence-in-depth for
//     the edge case where an infrastructure error reaches the client
//     with a non-5xx status (e.g. a reverse-proxy layer returning 502).
func classifyInfrastructureError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lowered := strings.ToLower(msg)

	// Check for infrastructure error markers in the full error string.
	// This catches requestError (server communication errors) as well
	// as daemon-local I/O errors when the OS message happens to include
	// a known keyword.
	if strings.Contains(lowered, "database is locked") ||
		strings.Contains(lowered, "database locked") ||
		strings.Contains(lowered, "deadlock detected") {
		return FailureReasonInfrastructureError
	}

	// requestError from the daemon HTTP client signals a server-side
	// failure. 5xx status codes indicate the server infrastructure
	// (database, upstream services) is unhealthy, not that the request
	// itself is invalid. The caller must not classify these as agent
	// errors.
	var reqErr *requestError
	if errors.As(err, &reqErr) && reqErr.StatusCode >= 500 {
		return FailureReasonInfrastructureError
	}

	return ""
}
